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
// The ones still to fix are listed below by file and line. The list only
// shrinks: a block that starts failing and is not in it fails this test, which
// is the point — the backlog is visible and cannot grow quietly.
func TestTheDocumentationParses(t *testing.T) {
	blocks := collectHCLBlocks(t, "../../docs")
	if len(blocks) < 400 {
		t.Fatalf("only %d blocks found, so the walk is not reaching the documentation", len(blocks))
	}

	var (
		unexpected []string
		fixed      []string
	)
	for _, b := range blocks {
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
		// regression. Editing a known-bad block changes its key, which is
		// right — it has to be looked at again.
		where := fmt.Sprintf("%s:%d", b.file, b.line)
		known := knownBad[b.key()]
		switch {
		case err != "" && !known:
			unexpected = append(unexpected, fmt.Sprintf("%s  (%s)\n      %s", where, b.key(), firstLine(err)))
		case err == "" && known:
			fixed = append(fixed, fmt.Sprintf("%s  (%s)", where, b.key()))
		}
	}

	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Errorf("these documentation blocks do not parse, so somebody copying them gets an error:\n  %s",
			strings.Join(unexpected, "\n  "))
	}
	if len(fixed) > 0 {
		sort.Strings(fixed)
		t.Errorf("these are listed as known-bad and parse now — take them out of knownBad:\n  %s",
			strings.Join(fixed, "\n  "))
	}
}

// knownBad is the backlog: documentation blocks that do not parse yet, by file
// and the line the block starts on. Three of them are in the troubleshooting
// guide on purpose — that page shows what a broken configuration looks like —
// and are marked so.
var knownBad = map[string]bool{
	// Deliberately broken: the troubleshooting guide shows the mistake first.
	"2273da7a31ed": true, // docs/guides/troubleshooting.md:69
	"769fdafae6a9": true, // docs/guides/troubleshooting.md:81
	"65092ab9c49e": true, // docs/guides/troubleshooting.md:192

	// The backlog: an attribute the parser does not take, a block with no
	// type, a shape it refuses outright. Each is a page somebody can copy
	// from and get an error.
	"bea3901b5421": true, // docs/connectors/cache.md:62
	"630adf399eb0": true, // docs/connectors/database.md:113
	"b0803e3ee203": true, // docs/connectors/elasticsearch.md:46
	"48ca9802d493": true, // docs/connectors/grpc.md:66
	"ed7ae624487f": true, // docs/core-concepts/environments.md:74
	"a1e45ce65ec3": true, // docs/core-concepts/input-and-output.md:173
	"9a0983401947": true, // docs/core-concepts/input-and-output.md:69
	"89008aaee0ca": true, // docs/guides/batch-processing.md:249
	"1f3ebbc8b9c2": true, // docs/guides/error-handling.md:582
	"d0c24687d120": true, // docs/guides/notifications.md:134
	"be43f568dcff": true, // docs/guides/notifications.md:164
	"3cb966e12419": true, // docs/guides/troubleshooting.md:520
	"c8b0aa13d094": true, // docs/guides/troubleshooting.md:529
	"11acd078849e": true, // docs/guides/use-cases.md:1138
	"f19f2a2478ce": true, // docs/guides/use-cases.md:1496
	"bc40419a076d": true, // docs/guides/use-cases.md:2112
	"7f555de48e47": true, // docs/guides/use-cases.md:2167
	"b67908fb7ba1": true, // docs/guides/use-cases.md:382
	"8856e065440f": true, // docs/guides/use-cases.md:40
	"6a299f9fd08c": true, // docs/guides/use-cases.md:457
	"f1788423f678": true, // docs/guides/use-cases.md:659
	"24b42cfd4ce6": true, // docs/reference/configuration.md:1140
	"0600184a9afa": true, // docs/reference/configuration.md:1263
	"de2efe11502e": true, // docs/reference/configuration.md:157
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

		rel := "docs" + strings.TrimPrefix(path, root)
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
