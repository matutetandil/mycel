package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A test that needs infrastructure and that nobody runs.
//
// The integration runner names the Go tests it runs one by one:
//
//	run_go_tests "mongodb write operations" \
//	  ./internal/connector/database/mongodb/ 'SeveralDocuments|UpdateChanges|…'
//
// which is deliberate — those packages hold ordinary unit tests too, and the
// runner only wants the ones that need a server. What it costs is that a new
// test against a real server has to be added to the list by hand, and a test
// left out of it is not skipped and not reported: it simply never runs, while
// `go test ./...` passes because it skips itself for want of a server.
//
// That happened to eight tests the day they were written. This reads both
// sides and refuses the next one.

var (
	// run_go_tests "<label>" \<newline>  ./<package>/ '<pattern>'
	suiteLine = regexp.MustCompile(`run_go_tests\s+"[^"]*"\s*\\?\s*\n?\s*(\./\S+?)/?\s+'([^']+)'`)
	testFunc  = regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	// A test that skips itself unless something is answering is one that
	// needs the stack: it is the skip that makes it invisible.
	needsServer = regexp.MustCompile(`t\.Skipf?\([^)]*(reachable|not answering|MYCEL_TEST_|set .* to run)`)
)

func TestEveryTestThatNeedsTheStackIsRunByTheRunner(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "tests", "integration", "run.sh"))
	if err != nil {
		t.Fatalf("reading the integration runner: %v", err)
	}

	// package path -> the patterns the runner will match against it
	listed := map[string][]*regexp.Regexp{}
	for _, m := range suiteLine.FindAllStringSubmatch(string(script), -1) {
		pkg := strings.TrimPrefix(strings.TrimSuffix(m[1], "/"), "./")
		pattern, err := regexp.Compile(m[2])
		if err != nil {
			t.Errorf("the runner's pattern for %s does not compile: %v", pkg, err)
			continue
		}
		listed[pkg] = append(listed[pkg], pattern)
	}
	if len(listed) == 0 {
		t.Fatal("no Go suites found in the integration runner — this test is reading the wrong thing")
	}

	var missing []string
	for pkg, patterns := range listed {
		// The whole package as one text: a test reaches the stack through a
		// helper (liveMongo, livePostgres, …) that usually lives in another
		// file of the same package, so reading one file at a time sees a test
		// that mentions a name it cannot find and concludes nothing. That is
		// how the first version of this test passed while eight tests were
		// going unrun.
		corpus, err := packageSource(pkg)
		if err != nil {
			t.Errorf("the runner names %s, which is not there: %v", pkg, err)
			continue
		}

		helpers := skipHelpers(corpus)
		for _, fn := range testFunc.FindAllStringSubmatch(corpus, -1) {
			name := fn[1]
			if !bodyNeedsServer(corpus, name, helpers) {
				continue
			}
			run := false
			for _, pattern := range patterns {
				if pattern.MatchString(name) {
					run = true
					break
				}
			}
			if !run {
				missing = append(missing, pkg+": "+name)
			}
		}
	}

	if len(missing) > 0 {
		t.Errorf("these tests need a running server and the integration runner never calls them, "+
			"so they skip themselves in CI and nothing reports it — add them to a run_go_tests "+
			"pattern in tests/integration/run.sh:\n  %s", strings.Join(missing, "\n  "))
	}
}

// packageSource reads every _test.go file of a package as one text.
func packageSource(pkg string) (string, error) {
	dir := filepath.Join("..", "..", filepath.FromSlash(pkg))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		out.Write(content)
		out.WriteString("\n")
	}
	return out.String(), nil
}

// bodyNeedsServer reports whether this test reaches the stack: either it skips
// itself when nothing answers, or it calls a helper in the file that does.
//
// The helpers are what most of these tests use (liveMongo, livePostgres, …),
// so the check is "the body mentions something whose own body skips", kept
// simple on purpose: a test that needs a server and says so in neither way is
// one this cannot see, and a heuristic that guesses further would start
// refusing unit tests.
func bodyNeedsServer(source, name string, helpers []string) bool {
	body := functionBody(source, name)
	if body == "" {
		return false
	}
	if needsServer.MatchString(body) {
		return true
	}
	for _, helper := range helpers {
		if strings.Contains(body, helper+"(") {
			return true
		}
	}
	return false
}

// skipHelpers names the functions in the package whose own body skips unless
// something is answering.
func skipHelpers(source string) []string {
	var helpers []string
	for _, m := range regexp.MustCompile(`(?m)^func (\w+)\(`).FindAllStringSubmatch(source, -1) {
		name := m[1]
		if strings.HasPrefix(name, "Test") {
			continue
		}
		if needsServer.MatchString(functionBody(source, name)) {
			helpers = append(helpers, name)
		}
	}
	return helpers
}

// functionBody returns the text of one top-level function, from its signature
// to the closing brace in column one.
func functionBody(source, name string) string {
	start := regexp.MustCompile(`(?m)^func (?:\([^)]*\) )?` + regexp.QuoteMeta(name) + `\(`).FindStringIndex(source)
	if start == nil {
		return ""
	}
	rest := source[start[0]:]
	if end := strings.Index(rest, "\n}\n"); end != -1 {
		return rest[:end]
	}
	return rest
}
