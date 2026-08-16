package metrics

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A metric nobody records is a metric that reads zero for ever.
//
// This has happened here twice. Eighteen connector metrics were declared,
// registered and never incremented, so a dashboard built on them showed a
// service doing nothing; and the flow metrics were recorded and not exposed,
// which looked the same from outside. Both were found by somebody watching a
// graph that stayed flat, which is the slowest way to find anything.
//
// So: every recorder this package offers must have a call site somewhere in
// the runtime. The check is mechanical because the failure is — nothing is
// wrong with the metric, it is simply never touched.

// notRecorded lists recorders that deliberately have no call site, and why.
// An entry that starts being called fails the test, so the list cannot go
// stale, and adding one takes a reason.
var notRecorded = map[string]string{
	"SetSemaphoreAvailable": "how many permits are free can only be answered by asking the store, " +
		"which is a round trip per message on a path that already has one. The acquired, " +
		"released and timeout counters cover the same question without the cost.",
}

func TestEveryMetricHasSomethingThatRecordsIt(t *testing.T) {
	recorders := recordersDeclaredHere(t)
	if len(recorders) < 20 {
		t.Fatalf("found %d recorders, which is too few to be the whole list", len(recorders))
	}

	source := runtimeSource(t)

	var silent []string
	for _, name := range recorders {
		called := strings.Contains(source, "."+name+"(")

		reason, allowed := notRecorded[name]
		switch {
		case called && allowed:
			t.Errorf("%s is recorded now — take it out of notRecorded (it said: %s)", name, reason)
		case !called && !allowed:
			silent = append(silent, name)
		}
	}
	sort.Strings(silent)

	if len(silent) > 0 {
		t.Errorf("these metrics are declared, registered, exposed and never recorded, "+
			"so they read zero for ever: %v", silent)
	}
}

// recordersDeclaredHere reads the method names off this package's own source.
func recordersDeclaredHere(t *testing.T) []string {
	t.Helper()

	body, err := os.ReadFile("metrics.go")
	if err != nil {
		t.Fatalf("read metrics.go: %v", err)
	}

	pattern := regexp.MustCompile(`func \(r \*Registry\) ((?:Record|Set|Inc|Dec)[A-Za-z]+)\(`)
	var names []string
	for _, match := range pattern.FindAllStringSubmatch(string(body), -1) {
		names = append(names, match[1])
	}
	sort.Strings(names)
	return names
}

// runtimeSource is every Go file outside this package, concatenated. Tests are
// left out: a metric recorded only by its own test is not recorded.
func runtimeSource(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	var all strings.Builder
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // an unreadable directory is not this test's business
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "mycel_plugins", "target", "metrics":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		all.Write(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if all.Len() == 0 {
		t.Fatal("no source was read, so the check would pass for the wrong reason")
	}
	return all.String()
}
