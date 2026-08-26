package docs

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

const readmePath = "../../README.md"

func readReadme(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	return string(body)
}

// section returns the body of a `## <title>` section, up to the next `## `.
func section(t *testing.T, doc, title string) string {
	t.Helper()
	start := strings.Index(doc, "\n## "+title+"\n")
	if start < 0 {
		t.Fatalf("README has no %q section", title)
	}
	rest := doc[start+1:]
	if end := strings.Index(rest[1:], "\n## "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

// A link the README offers has to point at something a reader can open.
//
// The README is the one page every reader opens, and it links out to about
// sixty files. Two ways for one of those to break: the file gets renamed, or
// the file is not published at all. The second is the quiet one — the
// benchmark suite sat in .gitignore, so a link to it resolved on the machine
// that wrote it and 404ed on GitHub for everybody else. So the check is
// against what git tracks, not against what happens to be on this disk.
//
// Anchors are not checked here, only the file a link points at.
func TestReadmeRelativeLinksResolve(t *testing.T) {
	doc := readReadme(t)
	link := regexp.MustCompile(`]\(([^)\s]+)\)`)

	tracked := trackedFiles(t)

	var broken []string
	for _, match := range link.FindAllStringSubmatch(doc, -1) {
		target := match[1]
		switch {
		case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"):
			continue
		case strings.HasPrefix(target, "#"), strings.HasPrefix(target, "mailto:"):
			continue
		}
		path := target
		if hash := strings.Index(path, "#"); hash >= 0 {
			path = path[:hash]
		}
		if path == "" {
			continue
		}
		info, err := os.Stat("../../" + path)
		if err != nil {
			broken = append(broken, target+" (no such file)")
			continue
		}
		if info.IsDir() {
			// A directory link is published if anything under it is.
			if !tracked.hasPrefix(strings.TrimSuffix(path, "/") + "/") {
				broken = append(broken, target+" (directory is not published)")
			}
			continue
		}
		if !tracked.has(path) {
			broken = append(broken, target+" (not tracked by git)")
		}
	}
	if len(broken) > 0 {
		t.Errorf("README links to files that do not exist: %s", strings.Join(broken, ", "))
	}
}

// Every number the README reports as a benchmark result has to be one that was
// actually measured.
//
// A proposed rewrite of this README arrived with a performance table quoting
// throughput, latency and memory figures on an AWS instance type — none of it
// measured, all of it plausible. Numbers are the part of a README a reader
// takes on trust, so the ones printed here have to trace back to
// benchmark/RESULTS.md, and stay traceable when the suite is re-run.
func TestReadmePerformanceNumbersComeFromTheBenchmark(t *testing.T) {
	results, err := os.ReadFile("../../benchmark/RESULTS.md")
	if err != nil {
		t.Fatalf("read benchmark results: %v", err)
	}
	measured := string(results)

	perf := section(t, readReadme(t), "Performance")
	number := regexp.MustCompile(`\d[\d,]*(\.\d+)?%?`)

	var unmeasured []string
	for _, line := range strings.Split(perf, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		for _, value := range number.FindAllString(line, -1) {
			if !strings.Contains(measured, value) {
				unmeasured = append(unmeasured, value)
			}
		}
	}
	if len(unmeasured) > 0 {
		t.Errorf("README reports figures that benchmark/RESULTS.md does not contain: %s",
			strings.Join(unmeasured, ", "))
	}
}

type trackedSet map[string]bool

func (t trackedSet) has(path string) bool { return t[strings.TrimPrefix(path, "./")] }

func (t trackedSet) hasPrefix(prefix string) bool {
	prefix = strings.TrimPrefix(prefix, "./")
	for path := range t {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// trackedFiles lists what git has, which is what a reader on GitHub can open.
func trackedFiles(t *testing.T) trackedSet {
	t.Helper()
	out, err := exec.Command("git", "-C", "../..", "ls-files").Output()
	if err != nil {
		t.Skipf("git ls-files unavailable: %v", err)
	}
	set := make(trackedSet)
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			set[line] = true
		}
	}
	return set
}
