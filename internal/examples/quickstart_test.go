package examples

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The quick start, followed.
//
// It is the page that decides whether somebody gets anywhere, and every one of
// its three requests failed. The flows named `INSERT items` and
// `items WHERE id = :id` as their destination, which is not syntax Mycel has —
// both produced invalid SQL — and nothing on the page ever created the table,
// so even the one flow written correctly answered "no such table: items". The
// responses it printed were not the ones the service gives either.
//
// So the page is assembled from its own blocks here and run: the files it says
// to write, the migration it says to apply, the requests it says to make.
func TestTheQuickStartWorksWhenFollowed(t *testing.T) {
	if testing.Short() {
		t.Skip("starting a service")
	}

	page := repoPath("docs", "getting-started", "quick-start.md")
	source, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("reading the quick start: %v", err)
	}
	text := string(source)

	dir := t.TempDir()
	files := filesShownIn(t, text)
	// Named rather than counted, so a page that stops showing one of them
	// fails here instead of quietly testing less.
	for _, want := range []string{"config.mycel", "connectors.mycel", "flows.mycel", "migrations/001_create_items.sql"} {
		if _, shown := files[want]; !shown {
			t.Fatalf("the page no longer shows %s; it is what a reader is told to write", want)
		}
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	svc := startDir(t, dir, "quick-start")

	// Everything up to the validation step, which is a later configuration
	// than the one built here: the page adds a type and rewrites the flow, and
	// the request it then shows is one meant to be refused.
	commands := []string(nil)
	if cut := strings.Index(text, "## Step 5"); cut > 0 {
		commands = svc.commandsIn(t, page, "quick-start")[:countRequestsBefore(t, svc, page, text[:cut])]
	}
	if len(commands) < 3 {
		t.Fatalf("only %d requests found on the page; it shows more than that", len(commands))
	}

	for _, command := range commands {
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
			t.Errorf("answered %d — the page shows a route the service does not serve:\n  %s", status, short)
		}
	}
}

// The page's own claim, checked against the service: a request missing a
// required field is refused, and one missing the optional field is not.
//
// That second half is what `default()` is for, and it did not work for the
// case it exists for: CEL evaluates a function's arguments before calling it,
// so `default(input.description, '')` on a request with no description failed
// before default was reached.
func TestTheQuickStartsValidationRefusesAndAccepts(t *testing.T) {
	if testing.Short() {
		t.Skip("starting a service")
	}

	page := repoPath("docs", "getting-started", "quick-start.md")
	source, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("reading the quick start: %v", err)
	}
	text := string(source)

	dir := t.TempDir()
	for name, body := range filesShownIn(t, text) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Step 5: the type the page says to create, and the flow that references
	// it, replacing the one written earlier.
	itemType := blockContaining(t, text, `type "item_input"`)
	validating := blockContaining(t, text, "validate {")
	if err := os.WriteFile(filepath.Join(dir, "types.mycel"), []byte(itemType), 0o644); err != nil {
		t.Fatal(err)
	}
	flows, err := os.ReadFile(filepath.Join(dir, "flows.mycel"))
	if err != nil {
		t.Fatal(err)
	}
	replaced := replaceFlow(string(flows), "create_item", validating)
	if err := os.WriteFile(filepath.Join(dir, "flows.mycel"), []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := startDir(t, dir, "quick-start (validated)")
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the REST port was not moved, so this would collide with anything else running")
	}

	refused, body := svc.run(t, `curl -X POST http://localhost:`+strconv.Itoa(port)+`/items -H "Content-Type: application/json" -d '{}'`)
	if refused != 400 {
		t.Errorf("a request with no name answered %d, want 400: %s", refused, body)
	}

	accepted, body := svc.run(t, `curl -X POST http://localhost:`+strconv.Itoa(port)+`/items -H "Content-Type: application/json" -d '{"name":"No description"}'`)
	if accepted != 200 {
		t.Errorf("a request without the optional field answered %d: %s", accepted, body)
	}
}

// countRequestsBefore reports how many of the page's requests appear before a
// given point, so a test can follow the page as far as the configuration it
// built goes.
func countRequestsBefore(t *testing.T, svc *service, page, upTo string) int {
	t.Helper()
	partial := filepath.Join(t.TempDir(), "partial.md")
	if err := os.WriteFile(partial, []byte(upTo), 0o644); err != nil {
		t.Fatal(err)
	}
	return len(svc.commandsIn(t, partial, "quick-start"))
}

var (
	// ### `config.mycel` — service identity
	shownFile = regexp.MustCompile("(?m)^###\\s+`([^`]+)`[^\n]*\n+```(?:hcl|sql)\n")
	fenceEnd  = regexp.MustCompile("(?m)^```\\s*$")
)

// filesShownIn returns the files a page tells the reader to write, keyed by
// the name in its heading.
func filesShownIn(t *testing.T, text string) map[string]string {
	t.Helper()

	files := map[string]string{}
	for _, m := range shownFile.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		rest := text[m[1]:]
		end := fenceEnd.FindStringIndex(rest)
		if end == nil {
			continue
		}
		files[name] = rest[:end[0]]
	}
	return files
}

// blockContaining returns the fenced block that holds a marker, which is how a
// page's later steps are found: they show a file again, changed.
func blockContaining(t *testing.T, text, marker string) string {
	t.Helper()

	for _, block := range fencedBlock.FindAllStringSubmatch(text, -1) {
		if strings.Contains(block[1], marker) {
			return block[1]
		}
	}
	t.Fatalf("the page no longer shows a block containing %q", marker)
	return ""
}

// replaceFlow swaps one flow for another version of it.
func replaceFlow(config, name, replacement string) string {
	open := strings.Index(config, `flow "`+name+`"`)
	if open < 0 {
		return config + "\n" + replacement
	}
	depth, end := 0, -1
	for i := open; i < len(config); i++ {
		switch config[i] {
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
		return config
	}
	return config[:open] + replacement + config[end:]
}

