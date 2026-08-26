// Package examples holds no code. It exists for one test: the examples, started
// and driven the way their READMEs say to.
package examples

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// mycelBinary builds the binary once for the whole package.
var mycelBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mycel-examples-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "examples harness: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	mycelBinary = filepath.Join(dir, "mycel")
	build := exec.Command("go", "build", "-o", mycelBinary, "../../cmd/mycel")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "examples harness: building mycel: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// repoPath resolves a path relative to the repository root.
func repoPath(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

// freePort asks the operating system for a port nobody is using, and does not
// hand the same one out twice.
//
// The listener has to be closed before the port can be given to a service, and
// the operating system happily hands the just-released port to the next
// caller — so two examples in one run were occasionally given the same number
// and the second failed to bind. That used to be invisible, because a server
// that could not take its port went on reporting itself ready; now it is a
// failed start, so the race shows up as a flaky suite rather than a silent
// one.
func freePort(t *testing.T) int {
	t.Helper()

	for attempt := 0; attempt < 50; attempt++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("finding a free port: %v", err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()

		handedOutMu.Lock()
		taken := handedOut[port]
		if !taken {
			handedOut[port] = true
		}
		handedOutMu.Unlock()

		if !taken {
			return port
		}
	}
	t.Fatal("could not find a port that has not already been handed out")
	return 0
}

var (
	handedOutMu sync.Mutex
	handedOut   = map[int]bool{}
)

var (
	portInConfig  = regexp.MustCompile(`(?m)^(\s*(?:admin_)?port\s*=\s*)(\d+)`)
	portInCommand = regexp.MustCompile(`(localhost|127\.0\.0\.1):(\d+)`)
	// port = env("API_PORT", 3000), with or without quotes around the default.
	portFromEnv = regexp.MustCompile(`(?m)^(\s*(?:admin_)?port\s*=\s*)env\(\s*"([A-Z_]+)"\s*,\s*"?(\d+)"?\s*\)`)
	fencedBlock = regexp.MustCompile("(?s)```[a-zA-Z]*\n(.*?)```")
	lineJoin    = regexp.MustCompile(`\\\n\s*`)
	// A placeholder standing in for a value the reader is meant to supply,
	// inside the URL: /files/{id}/download, /orders/<uuid>.
	urlPlaceholder = regexp.MustCompile(`https?://[^\s'"]*[{<][a-zA-Z_]+[}>]`)
)

// service is one example, copied aside and running.
type service struct {
	dir   string
	ports map[int]int // the port written in the example → the one it listens on
	log   string

	// graphQL is where the example serves GraphQL, when it serves any.
	graphQL int
}

// start copies the example, moves every port it declares to one that is free,
// applies its migrations and runs it.
func start(t *testing.T, example string, environment ...string) *service {
	t.Helper()
	return startDir(t, repoPath("examples", example), example, environment...)
}

// startDir is start over any directory, so that a service assembled from a
// documentation page runs the same way an example does.
func startDir(t *testing.T, source, label string, environment ...string) *service {
	t.Helper()

	dir := t.TempDir()
	if out, err := exec.Command("cp", "-R", source+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copying %s: %v: %s", label, err, out)
	}
	// A database left behind by somebody's local run is not what a reader has.
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".db") {
			_ = os.Remove(path)
		}
		return nil
	})
	// The data directory is deliberately NOT created here. It is gitignored,
	// so a reader who has just cloned the repository does not have it either —
	// and while the harness made it, nothing noticed that `mycel migrate`, the
	// first command most of these READMEs tell you to run, could not create a
	// database whose directory was missing.

	svc := &service{dir: dir, ports: map[int]int{}}

	// Every port the example declares is moved out of the way, so this can run
	// beside anything else — including the integration stack, which holds the
	// ports the examples like to use.
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".mycel") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rewritten, graphQL := movePorts(t, string(source), svc.ports)
		if graphQL != 0 {
			svc.graphQL = graphQL
		}
		_ = os.WriteFile(path, []byte(rewritten), 0o644)
		return nil
	})

	svc.moveAdminPort(t, dir)

	for _, connector := range databaseConnectors(t, dir) {
		args := []string{"migrate", "--config", "."}
		if connector != "" {
			args = append(args, "--connector", connector)
		}
		migrate := exec.Command(mycelBinary, args...)
		migrate.Dir = dir
		migrate.Env = append(os.Environ(), environment...)
		if out, err := migrate.CombinedOutput(); err != nil {
			t.Fatalf("%s: migrate: %v: %s", label, err, out)
		}
	}

	svc.log = filepath.Join(dir, "service.log")
	logFile, err := os.Create(svc.log)
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	run := exec.Command(mycelBinary, "start", "--config", ".")
	run.Dir = dir
	run.Env = append(os.Environ(), environment...)
	run.Stdout = logFile
	run.Stderr = logFile
	if err := run.Start(); err != nil {
		t.Fatalf("%s: starting: %v", label, err)
	}
	t.Cleanup(func() {
		_ = run.Process.Kill()
		_, _ = run.Process.Wait()
		_ = logFile.Close()
	})

	svc.waitUntilListening(t, label)
	return svc
}

// listens names the connector types whose port is one the service binds. A
// port belonging to anything else is somewhere to connect to — a database, a
// broker — and moving it points the example at nothing.
var listens = map[string]bool{
	"rest":      true,
	"websocket": true,
	"sse":       true,
}

// listensWhenServing names the types that bind a port only when they are the
// server: the same connector is a client with the same attribute names, and
// moving a client's port sends it nowhere.
var listensWhenServing = map[string]bool{
	"graphql": true,
	"grpc":    true,
	"tcp":     true,
	"soap":    true,
}

// workflowAPIPort matches the `port = N` inside a workflow api block. The
// block is short and its only numeric attribute is the port, so matching the
// attribute after the `api {` that opens it is enough.
var workflowAPIPort = regexp.MustCompile(`(?s)(api\s*\{[^}]*?port\s*=\s*)(\d+)`)

// movePorts rewrites the ports the service will listen on, and leaves alone the
// ports it will connect to.
func movePorts(t *testing.T, source string, moved map[int]int) (string, int) {
	t.Helper()

	graphQL := 0
	var out strings.Builder

	// The workflow API binds a port of its own, and it is not a connector — it
	// lives in `service { workflow { api { port = N } } }`, so the loop below
	// never saw it. The workflows example left it on its declared 9091 while
	// its README's requests were moved along with everything else, and two
	// examples could not run at once. Anything already holding 9091 turned the
	// example into a thirty-second timeout.
	source = workflowAPIPort.ReplaceAllStringFunc(source, func(match string) string {
		parts := workflowAPIPort.FindStringSubmatch(match)
		declared, _ := strconv.Atoi(parts[2])
		to, seen := moved[declared]
		if !seen {
			to = freePort(t)
			moved[declared] = to
		}
		return parts[1] + strconv.Itoa(to)
	})

	remaining := source

	for {
		start := strings.Index(remaining, "connector \"")
		if start < 0 {
			out.WriteString(remaining)
			break
		}
		open := strings.Index(remaining[start:], "{")
		if open < 0 {
			out.WriteString(remaining)
			break
		}
		open += start

		depth, end := 0, -1
		for i := open; i < len(remaining); i++ {
			switch remaining[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i + 1
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			out.WriteString(remaining)
			break
		}

		block := remaining[start:end]
		out.WriteString(remaining[:start])

		kind := ""
		if m := regexp.MustCompile(`type\s*=\s*"([a-z]+)"`).FindStringSubmatch(block); m != nil {
			kind = m[1]
		}
		serving := listens[kind]
		if listensWhenServing[kind] {
			// A connector of these types is a client when it says so, and a
			// server otherwise: the SOAP example writes no driver at all and
			// binds a port, so asking for `driver = "server"` left its port
			// where it was and the README's requests went to whatever else was
			// on 8080.
			if !regexp.MustCompile(`driver\s*=\s*"client"`).MatchString(block) {
				serving = true
			}
		}
		if serving {
			block = portInConfig.ReplaceAllStringFunc(block, func(match string) string {
				parts := portInConfig.FindStringSubmatch(match)
				declared, _ := strconv.Atoi(parts[2])
				to, seen := moved[declared]
				if !seen {
					to = freePort(t)
					moved[declared] = to
				}
				if kind == "graphql" {
					graphQL = to
				}
				return parts[1] + strconv.Itoa(to)
			})

			// And the ports written as env("API_PORT", 3000), which are just
			// as much ports this service listens on and were left where they
			// were: the rule above only matches a literal. So the example
			// bound its default, the README's commands were not moved with it,
			// and the requests landed on whatever else was there — reported as
			// a route the example does not serve.
			//
			// After the literal rule rather than before it, since this one
			// produces a literal and would otherwise be moved a second time,
			// to a port nothing is listening on.
			block = portFromEnv.ReplaceAllStringFunc(block, func(match string) string {
				parts := portFromEnv.FindStringSubmatch(match)
				declared, _ := strconv.Atoi(parts[3])
				to, seen := moved[declared]
				if !seen {
					to = freePort(t)
					moved[declared] = to
				}
				if kind == "graphql" {
					graphQL = to
				}
				return parts[1] + strconv.Itoa(to)
			})
		}
		out.WriteString(block)
		remaining = remaining[end:]
	}

	return out.String(), graphQL
}

// moveAdminPort gives this copy an admin port of its own.
//
// The admin server listens on 9090 unless the configuration says otherwise, and
// almost no example says otherwise — so two of them cannot run at once, which
// is what running them all is.
func (s *service) moveAdminPort(t *testing.T, dir string) {
	t.Helper()

	port := freePort(t)
	declared := false

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".mycel") || declared {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		text := string(source)
		open := regexp.MustCompile(`(?m)^service\s*\{`)
		if !open.MatchString(text) {
			return nil
		}
		if strings.Contains(text, "admin_port") {
			text = regexp.MustCompile(`(?m)^(\s*admin_port\s*=\s*)\d+`).
				ReplaceAllString(text, "${1}"+strconv.Itoa(port))
		} else {
			text = open.ReplaceAllString(text, "service {\n  admin_port = "+strconv.Itoa(port))
		}
		_ = os.WriteFile(path, []byte(text), 0o644)
		declared = true
		return nil
	})

	if !declared {
		// No service block anywhere: give it one.
		_ = os.WriteFile(filepath.Join(dir, "zz-admin-port.mycel"),
			[]byte("service {\n  admin_port = "+strconv.Itoa(port)+"\n}\n"), 0o644)
	}
}

// databaseConnectors returns the connectors migrate has to be pointed at: one
// entry for each database when there are several, and a single empty name when
// there is one, which is when migrate finds it by itself.
func databaseConnectors(t *testing.T, dir string) []string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "migrations")); err != nil {
		return nil
	}

	entries, err := os.ReadDir(filepath.Join(dir, "migrations"))
	if err != nil {
		return nil
	}
	var perConnector []string
	for _, entry := range entries {
		if entry.IsDir() {
			perConnector = append(perConnector, entry.Name())
		}
	}
	if len(perConnector) > 0 {
		return perConnector
	}
	return []string{""}
}

func (s *service) waitUntilListening(t *testing.T, example string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for _, port := range s.ports {
		listening := false
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				listening = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !listening {
			t.Fatalf("%s did not start; its log says:\n%s", example, s.tail())
		}
	}
}

func (s *service) tail() string {
	content, err := os.ReadFile(s.log)
	if err != nil {
		return "(no log)"
	}
	text := stripANSI(string(content))
	if len(text) > 2000 {
		text = text[len(text)-2000:]
	}
	return text
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

// commands returns the curl commands an example's README shows, in order, with
// the ports moved to where the service actually listens.
func (s *service) commands(t *testing.T, example string) []string {
	t.Helper()

	return s.commandsIn(t, repoPath("examples", example, "README.md"), example)
}

// commandsIn reads the commands out of any page, so a documentation page is
// followed the same way a README is.
func (s *service) commandsIn(t *testing.T, path, label string) []string {
	t.Helper()

	readme, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s has no README: %v", label, err)
	}

	var out []string
	for _, block := range fencedBlock.FindAllStringSubmatch(string(readme), -1) {
		for _, line := range statements(lineJoin.ReplaceAllString(block[1], " ")) {
			if !strings.HasPrefix(line, "curl ") {
				continue
			}
			// A command whose URL has a placeholder in it was never meant to
			// be run as written. A JSON body has braces too, which is not the
			// same thing.
			if urlPlaceholder.MatchString(line) {
				continue
			}
			// A command piped into something else is checking a header or
			// filtering output, not whether the route is there.
			if strings.Contains(line, "|") {
				continue
			}
			moved := portInCommand.ReplaceAllStringFunc(line, func(match string) string {
				parts := portInCommand.FindStringSubmatch(match)
				declared, _ := strconv.Atoi(parts[2])
				if to, ok := s.ports[declared]; ok {
					return parts[1] + ":" + strconv.Itoa(to)
				}
				return match
			})
			if portInCommand.MatchString(moved) {
				out = append(out, moved)
			}
		}
	}
	return out
}

// statements splits a block into commands, keeping a quoted argument that runs
// over several lines together — a JSON body is usually written that way, and
// cutting it at the newline leaves a command that is not valid shell.
func statements(block string) []string {
	var out []string
	var current strings.Builder

	for _, line := range strings.Split(block, "\n") {
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)

		if strings.Count(current.String(), "'")%2 == 0 {
			out = append(out, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	if current.Len() > 0 {
		out = append(out, strings.TrimSpace(current.String()))
	}
	return out
}

// graphQLQueries returns the queries an example's README shows in graphql
// blocks, as commands posting each one.
//
// A GraphQL example demonstrates itself with queries meant for a playground
// rather than with curl, so a harness reading only curl lines checks nothing at
// all in the examples where the whole point is the query.
func (s *service) graphQLQueries(t *testing.T, example string) []string {
	t.Helper()

	if s.graphQL == 0 {
		return nil
	}

	readme, err := os.ReadFile(repoPath("examples", example, "README.md"))
	if err != nil {
		return nil
	}

	var out []string
	for _, block := range graphQLBlock.FindAllStringSubmatch(string(readme), -1) {
		var lines []string
		for _, line := range strings.Split(block[1], "\n") {
			if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue
		}

		// A block showing several queries one after another is an
		// illustration — GraphQL refuses it ("This anonymous operation must be
		// the only defined operation"), and refusing it back would be reading
		// the documentation as if it were a script.
		if operationsIn(lines) != 1 {
			continue
		}

		// A parameterised query needs values the README does not attach, so
		// running it would only ever produce "Variable $id of required type
		// ID! was not provided" — which says nothing about the example.
		if strings.Contains(lines[0], "$") {
			continue
		}

		body, err := json.Marshal(map[string]string{"query": strings.Join(lines, "\n")})
		if err != nil {
			continue
		}
		out = append(out, fmt.Sprintf(
			`curl -X POST http://127.0.0.1:%d/graphql -H 'Content-Type: application/json' -d %s`,
			s.graphQL, shellQuote(string(body))))
	}
	return out
}

// operationsIn counts the operations a block defines, by the braces that close
// them at the top level.
func operationsIn(lines []string) int {
	depth, operations := 0, 0
	for _, line := range lines {
		for _, c := range line {
			switch c {
			case '{':
				if depth == 0 {
					operations++
				}
				depth++
			case '}':
				depth--
			}
		}
	}
	return operations
}

var graphQLBlock = regexp.MustCompile("(?s)```graphql\n(.*?)```")

// shellQuote wraps a value so a shell hands it over unchanged.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// run executes one command and reports the status it was answered with.
func (s *service) run(t *testing.T, command string) (int, string) {
	t.Helper()

	body := filepath.Join(s.dir, "body.out")

	// A stream is not a request that finishes. `curl -N` on a server-sent
	// events endpoint stays open until somebody hangs up, so curl is stopped
	// on a short timer — and it never gets to report a status, which read as
	// "no answer at all" and kept every streaming example out of the harness.
	//
	// What a stream can be judged on is how it opened, so the headers are kept
	// and the status read from them. A silent stream is not a broken one: this
	// example sends nothing until an event is published or its heartbeat comes
	// round, and a client is told it is connected by the headers.
	if strings.Contains(command, "curl -N") {
		return s.runStream(t, command)
	}

	probe := strings.Replace(command, "curl ",
		fmt.Sprintf("curl -s -o %s -w '%%{http_code}' --max-time 15 ", body), 1)

	cmd := exec.Command("bash", "-c", probe)
	cmd.Dir = s.dir
	out, err := cmd.Output()
	if err != nil {
		return 0, ""
	}
	status, _ := strconv.Atoi(strings.Trim(strings.TrimSpace(string(out)), "'"))
	answer, _ := os.ReadFile(body)
	return status, string(answer)
}

// runStream opens a streaming endpoint, holds it briefly and reports how it
// answered, from the response headers rather than from a body that may never
// come. A stream that is working still has to be a stream: the content type is
// checked with the status, since an endpoint that answers 200 with HTML is not
// serving events.
func (s *service) runStream(t *testing.T, command string) (int, string) {
	t.Helper()

	headers := filepath.Join(s.dir, "headers.out")
	probe := strings.Replace(command, "curl ",
		fmt.Sprintf("curl -s -D %s -o /dev/null --max-time 4 ", headers), 1)

	cmd := exec.Command("bash", "-c", probe)
	cmd.Dir = s.dir
	_ = cmd.Run() // it is stopped by the timer, which is the expected ending

	dumped, err := os.ReadFile(headers)
	if err != nil || len(dumped) == 0 {
		return 0, ""
	}

	first, rest, _ := strings.Cut(string(dumped), "\n")
	fields := strings.Fields(first)
	if len(fields) < 2 {
		return 0, ""
	}
	status, _ := strconv.Atoi(fields[1])

	if status == http.StatusOK && !strings.Contains(strings.ToLower(rest), "text/event-stream") {
		// Reported as a failure rather than returned alongside a 200, which
		// the caller has no reason to look inside.
		return http.StatusInternalServerError,
			"the stream opened without a text/event-stream content type"
	}
	return status, ""
}

// selfContained lists the examples that need nothing but Mycel — no broker, no
// database server, no external service. They are the ones a reader is most
// likely to start with, and every one of them was broken.
var selfContained = []string{
	"aspects",
	"async-jobs",
	"exec",
	"graphql",
	"mocks",
	"named-operations",
	"sse",
	"tcp",
	"graphql-optimization",
	"graphql-subscription-client",
	"basic",
	"cache",
	"constants",
	"files",
	"format",
	"pdf",
	"plugin",
	"query-method",
	"rate-limit",
	"reusable-blocks",
	"scheduled",
	"soap",
	"security",
	"transactional-write",
	"validators",
	"wasm-functions",
	"wasm-validator",
	"websocket",
}

// cannotBeRunHere holds the commands an example shows that need something this
// harness does not have, keyed by the body that identifies them. Each is a
// decision with a reason written down, not an oversight — and the README says
// the same thing to whoever is reading it.
var cannotBeRunHere = map[string]string{
	`/products/enrich`: "the enrichment step calls a legacy SOAP service at a host that does not exist, which its README says",
	`/jobs/$JOB_ID`:    "the job id comes from the answer to the request before it; the two-step is asserted in async_test.go",
	`"id":"p1"`:        "writes to a downstream service the example does not ship; the flow beside it, which writes to its own database, is the one run here",
	`"id":"o1"`:        "the same downstream",
}

// refusedInTheBody reports an answer that failed while saying 200.
//
// GraphQL puts its errors in the body: a query naming a field that does not
// exist, or a resolver that blew up, comes back 200 with an `errors` array. So
// a check that reads the status alone passes an example whose every query is
// refused — which is what this harness did for the federation example until
// somebody ran it by hand and read the answers.
func refusedInTheBody(answer string) string {
	trimmed := strings.TrimSpace(answer)
	if !strings.HasPrefix(trimmed, "{") || !strings.Contains(trimmed, `"errors"`) {
		return ""
	}

	var body struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(trimmed), &body); err != nil || len(body.Errors) == 0 {
		return ""
	}
	return body.Errors[0].Message
}

// routeMissing reports whether the answer means there is nothing at that
// address, rather than a flow deciding to refuse.
//
// A README may deliberately show a request being turned away — asking for an
// order that is not there, to demonstrate a custom error — and that is a 404
// the example produced on purpose. What is not on purpose is the router
// answering because no flow claims the path, which it does in Go's own words.
func routeMissing(status int, answer string) bool {
	if status == 405 {
		return true
	}
	return status == 404 && strings.Contains(answer, "404 page not found")
}

// whyNotRun reports whether a command is one the harness deliberately leaves
// alone, and why.
func whyNotRun(command string) (string, bool) {
	for marker, reason := range cannotBeRunHere {
		if strings.Contains(command, marker) {
			return reason, true
		}
	}
	return "", false
}

// Following an example's README has to work.
//
// Nothing checked this, and thirty of the thirty-six examples using SQLite
// shipped no way to create their tables: they started, printed their routes,
// and answered 500 to every request. Others declared flows the README's
// commands did not match, or asked for a file the connector could not write.
//
// A command answering 4xx is not a failure here — several READMEs deliberately
// show a request being refused. What this asserts is that the service starts,
// that the route exists, and that nothing falls over.
func TestTheExamplesWorkWhenFollowed(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	for _, example := range selfContained {
		t.Run(example, func(t *testing.T) {
			t.Parallel()

			svc := start(t, example)
			commands := append(svc.commands(t, example), svc.graphQLQueries(t, example)...)
			if len(commands) == 0 {
				t.Fatalf("no commands found in the README; this example is not being checked")
			}

			for _, command := range commands {
				if reason, expected := whyNotRun(command); expected {
					t.Logf("not run: %s", reason)
					continue
				}
				status, answer := svc.run(t, command)
				short := command
				if len(short) > 110 {
					short = short[:110] + "…"
				}

				switch {
				case status == 0:
					t.Errorf("no answer at all:\n  %s", short)
				case status >= 500:
					t.Errorf("answered %d:\n  %s\n  %s", status, short, strings.TrimSpace(answer))
				case routeMissing(status, answer):
					t.Errorf("answered %d — the README shows a route the example does not serve:\n  %s", status, short)
				default:
					if refused := refusedInTheBody(answer); refused != "" {
						t.Errorf("answered %d and refused in the body:\n  %s\n  %s", status, short, refused)
					}
				}
			}
		})
	}
}
