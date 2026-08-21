// Package docs holds no code. It exists for one test: the configuration
// examples in the documentation, run through the real parser.
package docs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
)

// Every complete configuration the documentation shows, parsed.
//
// Nothing checked this. The documentation is where somebody copies from, so a
// block that does not parse is a person following the instructions and getting
// an error — and it drifts the moment an attribute is renamed, which is how
// the TCP page came to document `codec` for a connector that reads `protocol`,
// and the cache page `min_connections` for a pool that takes `min_idle`.
//
// A first run over the whole tree found 551 blocks, of which 134 are snippets
// that were never meant to stand alone and 49 of the rest did not parse.
//
// That backlog is empty now. What is left is five blocks that do not parse
// deliberately — pages showing what a broken configuration looks like — and
// anything that joins them is drift, which fails this test on the spot.
func TestTheDocumentationParses(t *testing.T) {
	// The guide, the README somebody lands on first, and the README beside
	// every example — all three are places to copy from, and the last two were
	// as unchecked as the first.
	var blocks []docBlock
	for _, root := range []string{"../../docs", "../../examples"} {
		blocks = append(blocks, collectHCLBlocks(t, root)...)
	}
	blocks = append(blocks, collectHCLBlocks(t, "../../README.md")...)

	// A floor rather than a count: the number grows as pages are written, and
	// a walk that quietly stops reaching one of the three roots would
	// otherwise pass by testing nothing.
	if len(blocks) < 600 {
		t.Fatalf("only %d blocks found, so the walk is not reaching everything", len(blocks))
	}
	roots := map[string]bool{}
	for _, b := range blocks {
		roots[strings.SplitN(b.file, "/", 2)[0]] = true
	}
	for _, want := range []string{"docs", "examples", "README.md"} {
		if !roots[want] {
			t.Errorf("no blocks found under %s", want)
		}
	}

	var (
		unexpected []string
		fixed      []string
	)
	seen := make(map[string]bool, len(blocks))
	for _, b := range blocks {
		seen[b.key()] = true
		if b.isFragment() {
			continue
		}
		err := parseBlock(b.body)
		// A block showing an inline block on its own — a flow's transform, an
		// accept — is a snippet of a flow rather than a document.
		if err != "" && strings.Contains(err, "Missing name for") {
			continue
		}

		// Keyed by what the block says rather than where it sits: a line
		// number moves every time anything above it is edited, which would
		// make this list wrong after every fix rather than after every
		// regression. Editing one of these blocks changes its key, which is
		// right — it has to be looked at again.
		where := fmt.Sprintf("%s:%d", b.file, b.line)
		deliberate := deliberatelyWrong[b.key()]
		switch {
		case err != "" && !deliberate:
			unexpected = append(unexpected, fmt.Sprintf("%s  (%s)\n      %s", where, b.key(), firstLine(err)))
		case err == "" && deliberate:
			fixed = append(fixed, fmt.Sprintf("%s  (%s)", where, b.key()))
		}
	}

	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Errorf("these documentation blocks do not parse, so somebody copying them gets an error:\n  %s",
			strings.Join(unexpected, "\n  "))
	}
	// An entry pointing at no block at all: the block was edited, so its key
	// changed, and the old one would otherwise sit in the list for ever
	// looking like outstanding work.
	var stale []string
	for key := range deliberatelyWrong {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("these are marked as deliberately wrong and match no block in the documentation — "+
			"the block was edited or removed, so take them out:\n  %s", strings.Join(stale, "\n  "))
	}

	if len(fixed) > 0 {
		sort.Strings(fixed)
		t.Errorf("these are marked as deliberately wrong and parse now — take them out:\n  %s",
			strings.Join(fixed, "\n  "))
	}
}

// deliberatelyWrong is what does not parse on purpose, and nothing else.
//
// It started at forty-nine entries and these five are what is left: pages that
// show a broken configuration deliberately. Anything that joins them is drift.
var deliberatelyWrong = map[string]bool{
	// The troubleshooting guide leads with the symptom; the input and output
	// page shows the spelling HCL rejects next to the one it takes.
	"2273da7a31ed": true, // docs/guides/troubleshooting.md:69
	"769fdafae6a9": true, // docs/guides/troubleshooting.md:81
	"65092ab9c49e": true, // docs/guides/troubleshooting.md:192

	"a1e45ce65ec3": true, // docs/core-concepts/input-and-output.md:173
	"9a0983401947": true, // docs/core-concepts/input-and-output.md:69
}

type docBlock struct {
	file string
	line int
	body string
}

// key identifies a block by what it says, so the backlog survives an edit
// anywhere else in the page.
func (b docBlock) key() string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(b.body)))
	return hex.EncodeToString(sum[:])[:12]
}

// isFragment reports whether a block was never meant to stand alone: a snippet
// of one nested block, or one with an elision in it.
func (b docBlock) isFragment() bool {
	if strings.Contains(b.body, "...") {
		return true
	}
	for _, line := range strings.Split(strings.TrimSpace(b.body), "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) == 0 {
			return true
		}
		return !rootBlocks[strings.TrimSuffix(fields[0], "{")]
	}
	return true
}

// rootBlocks are the words a whole configuration can start with.
var rootBlocks = map[string]bool{
	"service": true, "connector": true, "flow": true, "type": true,
	"transform": true, "aspect": true, "auth": true, "validator": true,
	"saga": true, "state_machine": true, "cache": true, "dedupe": true,
	"lock": true, "semaphore": true, "coordinate": true, "retry": true,
	"accept": true, "response": true, "error_handling": true,
	"sequence_guard": true, "transaction": true, "plugin": true,
	"provides": true, "functions": true, "security": true, "mock": true,
	"workflow": true, "schedule": true,
}

func collectHCLBlocks(t *testing.T, root string) []docBlock {
	t.Helper()

	var blocks []docBlock
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		// The archive is what the documentation used to say, kept on purpose.
		if strings.Contains(path, "/archive/") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		rel := strings.TrimPrefix(path, "../../")
		lines := strings.Split(string(body), "\n")
		inBlock, start := false, 0
		var current []string
		for i, l := range lines {
			trimmed := strings.TrimSpace(l)
			if !inBlock && strings.EqualFold(trimmed, "```hcl") {
				inBlock, start, current = true, i+2, nil
				continue
			}
			if inBlock && strings.HasPrefix(trimmed, "```") {
				inBlock = false
				blocks = append(blocks, docBlock{file: rel, line: start, body: strings.Join(current, "\n")})
				continue
			}
			if inBlock {
				current = append(current, l)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the documentation: %v", err)
	}
	return blocks
}

// parseBlock returns the parser's complaint, or "" when the block parses.
func parseBlock(body string) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
		}
	}()
	if strings.TrimSpace(body) == "" {
		return ""
	}

	dir, err := os.MkdirTemp("", "docblocks")
	if err != nil {
		return err.Error()
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.mycel")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err.Error()
	}
	if _, err := parser.NewHCLParser().ParseFile(context.Background(), path); err != nil {
		return err.Error()
	}
	return ""
}

func firstLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if i := strings.Index(s, "config.mycel:"); i > 0 {
		s = s[i+len("config.mycel:"):]
	}
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
