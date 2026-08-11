package transform

import (
	"context"
	"testing"
)

func targets(rules []Rule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.Target
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRulesFromMappingsHonoursDeclarationOrder(t *testing.T) {
	mappings := map[string]string{
		"subtotal": "sum(pluck(input.items, 'price'))",
		"tax":      "output.subtotal * 0.21",
		"total":    "output.subtotal + output.tax",
	}
	order := []string{"subtotal", "tax", "total"}

	// Repeat: a map-iteration bug hides behind a single lucky run.
	for i := 0; i < 100; i++ {
		got := targets(RulesFromMappings(mappings, order))
		if !equal(got, order) {
			t.Fatalf("iteration %d: got %v, want %v", i, got, order)
		}
	}
}

func TestRulesFromMappingsWithoutOrderIsStillDeterministic(t *testing.T) {
	mappings := map[string]string{"c": "3", "a": "1", "b": "2"}
	want := []string{"a", "b", "c"}

	for i := 0; i < 100; i++ {
		got := targets(RulesFromMappings(mappings, nil))
		if !equal(got, want) {
			t.Fatalf("iteration %d: got %v, want sorted %v", i, got, want)
		}
	}
}

func TestRulesFromMappingsAppendsKeysMissingFromOrder(t *testing.T) {
	mappings := map[string]string{"first": "1", "z": "26", "a": "1"}
	got := targets(RulesFromMappings(mappings, []string{"first"}))
	want := []string{"first", "a", "z"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRulesFromMappingsIgnoresOrderEntriesWithNoMapping(t *testing.T) {
	// `use` is stripped from the mappings but could linger in a stale order.
	mappings := map[string]string{"id": "uuid()"}
	got := targets(RulesFromMappings(mappings, []string{"use", "id", "id"}))
	want := []string{"id"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMergeOrderKeepsOverriddenFieldsInPlace(t *testing.T) {
	base := []string{"email", "name", "created_at"}
	inline := []string{"name", "id"}

	got := MergeOrder(base, inline)
	want := []string{"email", "name", "created_at", "id"}
	if !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A field may reference one declared above it through `output`. That is the
// behaviour the ordering exists to protect, so assert it end to end rather
// than only asserting the rule list.
func TestTransformResolvesBackwardOutputReferences(t *testing.T) {
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	mappings := map[string]string{
		"subtotal": "100.0",
		"tax":      "output.subtotal * 0.21",
		"total":    "output.subtotal + output.tax",
	}
	order := []string{"subtotal", "tax", "total"}

	for i := 0; i < 50; i++ {
		out, err := tr.Transform(context.Background(),
			map[string]interface{}{}, RulesFromMappings(mappings, order))
		if err != nil {
			t.Fatalf("iteration %d: transform: %v", i, err)
		}
		if out["total"] != 121.0 {
			t.Fatalf("iteration %d: total = %v, want 121", i, out["total"])
		}
	}
}
