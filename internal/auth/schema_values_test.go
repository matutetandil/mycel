package auth

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// The values the schema offers have to be the values the code accepts.
//
// A completion that suggests a setting the runtime rejects is worse than no
// completion, and a setting the runtime accepts that the schema does not list
// is one nobody will discover. Three were wrong when this was written:
// `track_by` and `key_by` were offered as "both" where the code wants
// "ip+user", and `match_by` was missing "phone".
//
// The authority on both sides is in this package: the struct field carries the
// values it understands in a comment beside its hcl tag, which is where they
// were written down long before there was a schema.
func TestTheSchemaOffersTheValuesTheCodeAccepts(t *testing.T) {
	documented := valuesFromStructComments(t)
	if len(documented) < 8 {
		t.Fatalf("only %d fields carry a list of values; the convention has changed", len(documented))
	}

	offered := map[string][]string{}
	var walk func(schema.Block)
	walk = func(block schema.Block) {
		for _, attr := range block.Attrs {
			if len(attr.Values) > 0 {
				offered[attr.Name] = attr.Values
			}
		}
		for _, child := range block.Children {
			walk(child)
		}
	}
	walk(schema.AuthSchema())

	for field, want := range documented {
		got, described := offered[field]
		if !described {
			// Not every field with a comment is one the schema reaches yet —
			// the mfa method blocks are still to be described.
			continue
		}
		if !sameSet(got, want) {
			t.Errorf("%s: the schema offers %v, the code understands %v", field, sorted(got), sorted(want))
		}
	}
}

// valuesFromStructComments reads `Field string `hcl:"name,optional"` // a, b, c`.
func valuesFromStructComments(t *testing.T) map[string][]string {
	t.Helper()

	line := regexp.MustCompile("`hcl:\"([a-z_]+)(?:,[a-z]+)?\"`\\s*//\\s*([a-z_0-9+-]+(?:,\\s*[a-z_0-9+-]+)+)\\s*$")

	found := map[string][]string{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(".", entry.Name()))
		if readErr != nil {
			continue
		}
		for _, text := range strings.Split(string(body), "\n") {
			if match := line.FindStringSubmatch(text); match != nil {
				var values []string
				for _, value := range strings.Split(match[2], ",") {
					values = append(values, strings.TrimSpace(value))
				}
				found[match[1]] = values
			}
		}
	}
	return found
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left, right := sorted(a), sorted(b)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
