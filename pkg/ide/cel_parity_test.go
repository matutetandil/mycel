package ide

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/transform"
)

// Every function the editor offers, compiled by the engine that will run it.
//
// The list was written by hand and nothing checked it, so it drifted: ten of
// the thirty-nine it offered did not exist. Four were CEL's own string methods
// written as though they were calls — starts_with(s, prefix) rather than
// "s".startsWith(prefix) — two were base64 helpers that are base64.encode, two
// were json helpers that were never implemented, one was an md5 that is not
// there, and min/max are min_val/max_val over a list.
//
// A completion is a promise. Accepting one of those produced a configuration
// that parses and fails when the flow runs, which is the worst moment to find
// out that the editor was guessing.
func TestEveryOfferedFunctionExists(t *testing.T) {
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("building a transformer: %v", err)
	}

	for _, fn := range celFunctionList() {
		if fn.example == "" {
			t.Errorf("%s is offered with nothing to check it against", fn.name)
			continue
		}
		if _, err := transformer.Compile(fn.example); err != nil {
			t.Errorf("the editor offers %s and the engine does not have it:\n      %s\n      %s",
				fn.name, fn.example, firstLine(err.Error()))
		}
	}
}

func TestEveryOfferedFunctionIsDescribed(t *testing.T) {
	// A completion with no signature is a name and a guess.
	seen := map[string]bool{}
	for _, fn := range celFunctionList() {
		if fn.sig == "" || fn.doc == "" {
			t.Errorf("%s is offered with no signature or no description", fn.name)
		}
		if seen[fn.name] {
			t.Errorf("%s is offered twice", fn.name)
		}
		seen[fn.name] = true
	}

	if len(seen) < 30 {
		t.Errorf("only %d functions are offered, which is fewer than the language has", len(seen))
	}
}

func TestTheOfferedListMatchesWhatIsCompleted(t *testing.T) {
	// celFunctions builds the completion items from the same list, so what an
	// editor shows and what this test compiles cannot come apart.
	items := celFunctions()
	if len(items) != len(celFunctionList()) {
		t.Fatalf("%d completions from %d functions", len(items), len(celFunctionList()))
	}
	for i, fn := range celFunctionList() {
		if items[i].Label != fn.name {
			t.Errorf("completion %d is %q and the list says %q", i, items[i].Label, fn.name)
		}
	}
}

func firstLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
