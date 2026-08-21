package transform

import (
	"context"
	"testing"
)

// default() has to work for the case it exists for.
//
// CEL evaluates a function's arguments before calling it, so
// `default(input.description, ”)` on a request carrying no description failed
// with "no such key: description" before default was ever reached — the
// documented way to give an optional field a value, failing on the optional
// field. The quick start reaches for it at exactly that moment.
func TestDefaultSurvivesAFieldThatIsNotThere(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	rules := []Rule{
		{Target: "name", Expression: "input.name"},
		{Target: "description", Expression: `default(input.description, 'none')`},
		{Target: "nested", Expression: `default(input.meta.tag, 'untagged')`},
	}

	out, err := transformer.Transform(context.Background(),
		map[string]interface{}{"name": "Widget"}, rules)
	if err != nil {
		t.Fatalf("a request without the optional field was refused: %v", err)
	}
	if out["description"] != "none" {
		t.Errorf("description = %#v, want the fallback", out["description"])
	}
	if out["nested"] != "untagged" {
		t.Errorf("nested = %#v, want the fallback", out["nested"])
	}
}

// And a field that is there still wins.
func TestDefaultYieldsToAFieldThatIsThere(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	out, err := transformer.Transform(context.Background(),
		map[string]interface{}{"description": "written"},
		[]Rule{{Target: "description", Expression: `default(input.description, 'none')`}})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if out["description"] != "written" {
		t.Errorf("description = %#v, want what was sent", out["description"])
	}
}

func TestRewriteDefaultLeavesWhatItCannotGuard(t *testing.T) {
	for _, expr := range []string{
		// No call at all.
		"input.name",
		// A literal that happens to contain the text.
		`'default(a, b)'`,
		// Not the two-argument form.
		"default(x)",
	} {
		if got := RewriteDefault(expr); got != expr {
			t.Errorf("RewriteDefault(%q) = %q, want it unchanged", expr, got)
		}
	}

	// A call it cannot wrap keeps its shape rather than being guarded.
	if got := RewriteDefault("default(size(input.items), 0)"); got != "default(size(input.items), 0)" {
		t.Errorf("a call argument was rewritten to %q", got)
	}
}

// Nested calls are rewritten too, since a fallback is often another lookup.
func TestDefaultNestsInsideItsOwnFallback(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	out, err := transformer.Transform(context.Background(),
		map[string]interface{}{},
		[]Rule{{Target: "who", Expression: `default(input.user, default(input.account, 'anonymous'))`}})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if out["who"] != "anonymous" {
		t.Errorf("who = %#v, want the innermost fallback", out["who"])
	}
}

// A comma belonging to a function call is not a separator.
//
// The rewriters split an expression at top-level commas and at top-level `??`,
// and neither looked inside brackets — so any expression pairing `??` with a
// two-argument call was rewritten into CEL that does not compile:
//
//	input.missing ?? join(input.tags, ",")
//	→ (has(input.missing) ? coalesce(input.missing, join(input.tags) : join(input.tags), ",")
//
// It is the commonest shape there is — join, replace, substring, pick,
// default all take two arguments — and it failed at compile time, at startup,
// with a CEL syntax error pointing at a string the author never wrote.
func TestACommaInsideACallIsNotASeparator(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	input := map[string]interface{}{"tags": []interface{}{"a", "b"}}

	for _, c := range []struct {
		expression string
		want       interface{}
	}{
		{`input.missing ?? join(input.tags, ",")`, "a,b"},
		{`join(input.tags, ",") ?? 'none'`, "a,b"},
		{`default(input.missing, join(input.tags, ","))`, "a,b"},
		{`input.absent.deeper ?? join(input.tags, "-")`, "a-b"},
	} {
		out, err := transformer.Transform(context.Background(), input,
			[]Rule{{Target: "v", Expression: c.expression}})
		if err != nil {
			t.Errorf("%s: %v", c.expression, err)
			continue
		}
		if out["v"] != c.want {
			t.Errorf("%s = %#v, want %#v", c.expression, out["v"], c.want)
		}
	}
}
