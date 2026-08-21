package docs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/transform"
)

// Every CEL expression the documentation shows, compiled — and where the page
// states what it returns, evaluated and compared against that.
//
// The CEL reference is what somebody consults for what is available inside an
// expression, and it is written one expression per line with the answer in a
// trailing comment. That shape is checkable, so it is checked: a function that
// does not exist, or one that stopped taking what the page says it takes, is a
// person writing a flow that fails at run time from a page that promised it
// would work. `bool(1)` was documented and has no such overload.
func TestTheCELExamplesCompile(t *testing.T) {
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("building a transformer: %v", err)
	}

	examples := collectCELExamples(t, "../../docs")
	if len(examples) < 150 {
		t.Fatalf("only %d expressions found, so the walk is not reaching the pages", len(examples))
	}

	for _, ex := range examples {
		if _, err := transformer.Compile(ex.expr); err != nil {
			t.Errorf("%s:%d does not compile: %s\n      %s", ex.file, ex.line, ex.expr, firstLine(err.Error()))
		}
	}
}

func TestTheCELExamplesReturnWhatTheyClaim(t *testing.T) {
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("building a transformer: %v", err)
	}

	ctx := context.Background()
	checked := 0
	for _, ex := range collectCELExamples(t, "../../docs") {
		want, comparable := ex.claimedResult()
		if !comparable {
			continue
		}
		checked++

		got, err := transformer.EvaluateWith(ctx, ex.expr, map[string]interface{}{})
		if err != nil {
			t.Errorf("%s:%d could not be evaluated: %s\n      %v", ex.file, ex.line, ex.expr, err)
			continue
		}
		if rendered := renderCEL(got); !sameValue(rendered, want) {
			t.Errorf("%s:%d says %s returns %s and it returns %s",
				ex.file, ex.line, ex.expr, want, rendered)
		}
	}

	// A floor, so that a change to the comment format cannot turn this into a
	// test that compares nothing and passes.
	if checked < 40 {
		t.Errorf("only %d expressions had a result to compare against, which is too few "+
			"to be reading the pages correctly", checked)
	}
}

type celExample struct {
	file    string
	line    int
	expr    string
	comment string
}

// claimedResult is what the trailing comment says the expression returns, when
// that is something a comparison can be made against.
//
// A uuid, a timestamp and anything the page describes in prose rather than
// showing are all skipped: they are illustrations, not claims.
func (e celExample) claimedResult() (string, bool) {
	c := strings.TrimSpace(e.comment)
	if c == "" {
		return "", false
	}
	// An expression standing in for a payload cannot be evaluated without one,
	// and one that reads the clock has no fixed answer to compare against.
	if strings.Contains(e.expr, "input") || strings.Contains(e.expr, "now(") ||
		strings.Contains(e.expr, "uuid(") || strings.Contains(e.expr, "output") ||
		strings.Contains(e.expr, "step.") {
		return "", false
	}
	// Prose rather than a value: "// error", "// depends on ...".
	if strings.ContainsAny(c, " ") && !strings.HasPrefix(c, `"`) && !strings.HasPrefix(c, "[") && !strings.HasPrefix(c, "{") {
		return "", false
	}
	// Values that differ every call.
	for _, moving := range []string{"550e8400", "2025-", "2026-", "173"} {
		if strings.Contains(c, moving) {
			return "", false
		}
	}
	if strings.HasPrefix(c, "b\"") {
		return "", false // bytes render differently than they are written
	}
	return c, true
}

// sameValue compares what came back with what the page claims, allowing a
// whole double to be written either way: these pages show `double(42)` as 42.0
// and `math.ceil(4.2)` as 5, and both are the same number.
func sameValue(got, want string) bool {
	return got == want ||
		strings.TrimSuffix(got, ".0") == want ||
		got == strings.TrimSuffix(want, ".0")
}

// renderCEL prints a value the way the documentation writes one.
func renderCEL(v interface{}) string {
	switch t := v.(type) {
	case string:
		return `"` + t + `"`
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, renderCEL(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case float64:
		if t == float64(int64(t)) {
			// The pages write a whole double as 42.0.
			return fmt.Sprintf("%.1f", t)
		}
		return fmt.Sprintf("%g", t)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func collectCELExamples(t *testing.T, root string) []celExample {
	t.Helper()

	var examples []celExample
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		if strings.Contains(path, "/archive/") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		rel := strings.TrimPrefix(path, "../../")
		inBlock := false
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if !inBlock && strings.EqualFold(trimmed, "```cel") {
				inBlock = true
				continue
			}
			if inBlock && strings.HasPrefix(trimmed, "```") {
				inBlock = false
				continue
			}
			// A heading inside the block, written as a comment in either
			// language, is not an expression.
			if !inBlock || trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
				continue
			}

			expr, comment := trimmed, ""
			if idx := strings.Index(trimmed, "//"); idx > 0 {
				expr = strings.TrimSpace(trimmed[:idx])
				comment = strings.TrimSpace(trimmed[idx+2:])
			}
			if expr == "" {
				continue
			}
			examples = append(examples, celExample{file: rel, line: i + 1, expr: expr, comment: comment})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the documentation: %v", err)
	}
	return examples
}
