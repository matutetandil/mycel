// Package docs holds no code. It exists for one test: the configuration
// examples in the documentation, run through the real parser.
package docs

import (
	"context"
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

		where := fmt.Sprintf("%s:%d", b.file, b.line)
		known := knownBad[where]
		switch {
		case err != "" && !known:
			unexpected = append(unexpected, fmt.Sprintf("%s\n      %s", where, firstLine(err)))
		case err == "" && known:
			fixed = append(fixed, where)
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
	"docs/guides/troubleshooting.md:69":  true,
	"docs/guides/troubleshooting.md:81":  true,
	"docs/guides/troubleshooting.md:192": true,

	// An attribute the parser does not take.
	"docs/connectors/cache.md:62":          true,
	"docs/connectors/graphql.md:8":         true,
	"docs/core-concepts/flows.md:964":      true,
	"docs/guides/batch-processing.md:249":  true,
	"docs/guides/notifications.md:134":     true,
	"docs/guides/notifications.md:164":     true,
	"docs/guides/real-time.md:149":         true,
	"docs/guides/real-time.md:225":         true,
	"docs/guides/synchronization.md:105":   true,
	"docs/guides/synchronization.md:140":   true,
	"docs/guides/synchronization.md:380":   true,
	"docs/guides/troubleshooting.md:520":   true,
	"docs/guides/troubleshooting.md:529":   true,
	"docs/guides/use-cases.md:40":          true,
	"docs/guides/use-cases.md:382":         true,
	"docs/guides/use-cases.md:457":         true,
	"docs/guides/use-cases.md:659":         true,
	"docs/guides/use-cases.md:1138":        true,
	"docs/guides/use-cases.md:1496":        true,
	"docs/reference/configuration.md:157":  true,
	"docs/reference/configuration.md:183":  true,
	"docs/reference/configuration.md:500":  true,
	"docs/reference/configuration.md:511":  true,
	"docs/reference/configuration.md:522":  true,
	"docs/reference/configuration.md:1137": true,
	"docs/reference/configuration.md:1260": true,

	// A CEL expression written without quotes.
	"docs/core-concepts/flows.md:786":     true,
	"docs/guides/caching.md:125":          true,
	"docs/guides/caching.md:150":          true,
	"docs/guides/caching.md:267":          true,
	"docs/guides/multi-step-flows.md:155": true,

	// `validate` where the constraint is `validator`.
	"docs/core-concepts/types.md:170": true,
	"docs/guides/extending.md:47":     true,

	// A connector with no type, or another shape the parser refuses outright.
	"docs/connectors/database.md:113":            true,
	"docs/connectors/elasticsearch.md:46":        true,
	"docs/connectors/grpc.md:66":                 true,
	"docs/core-concepts/environments.md:74":      true,
	"docs/core-concepts/input-and-output.md:69":  true,
	"docs/core-concepts/input-and-output.md:173": true,
	"docs/guides/error-handling.md:582":          true,
	"docs/guides/use-cases.md:2112":              true,
	"docs/guides/use-cases.md:2167":              true,
}

type docBlock struct {
	file string
	line int
	body string
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
