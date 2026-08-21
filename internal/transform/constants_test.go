package transform

import (
	"context"
	"strings"
	"testing"
)

// A constant is in scope in every expression, not in some of them.
//
// The point of the block is one name that works everywhere: a constant that
// resolved in a query and not in a filter would be worse than no constants at
// all, because the failure is per-expression and nothing announces it. There
// are ten places in this package that evaluate an expression, and they all go
// through one door for that reason.
func TestAConstantIsInScopeWhereverAnExpressionIs(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatal(err)
	}
	transformer.SetConstants(map[string]interface{}{
		"skus_to_skip": []interface{}{"SKU-1", "SKU-2"},
		"page_size":    25,
		"region":       "us",
	})

	input := map[string]interface{}{"sku": "SKU-1", "name": "Widget"}

	// A transform mapping.
	out, err := transformer.Transform(context.Background(), input, []Rule{
		{Target: "skipped", Expression: "input.sku in constants.skus_to_skip"},
		{Target: "region", Expression: "constants.region"},
		{Target: "half", Expression: "constants.page_size / 2"},
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if out["skipped"] != true {
		t.Errorf("skipped = %#v", out["skipped"])
	}
	if out["region"] != "us" {
		t.Errorf("region = %#v", out["region"])
	}

	// A condition, which is what a filter, an accept and a `when` all are.
	// A condition is given the whole activation, so the request goes under
	// the name the expression uses for it.
	held, err := transformer.EvaluateCondition(context.Background(),
		map[string]interface{}{"input": input},
		"!(input.sku in constants.skus_to_skip)")
	if err != nil {
		t.Fatalf("condition: %v", err)
	}
	if held {
		t.Error("the condition let through a sku the constant lists")
	}

	// A single expression, which is what a key, a fingerprint and a step
	// parameter are.
	value, err := transformer.EvaluateExpression(context.Background(), input, nil,
		"'page:' + string(constants.page_size)")
	if err != nil {
		t.Fatalf("expression: %v", err)
	}
	if value != "page:25" {
		t.Errorf("expression = %#v", value)
	}
}

// With nothing declared, an expression naming a constant says so rather than
// refusing to compile against a name that does not exist.
func TestNamingAConstantThatWasNeverDeclared(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatal(err)
	}

	_, err = transformer.Transform(context.Background(), map[string]interface{}{},
		[]Rule{{Target: "x", Expression: "constants.nope"}})
	if err == nil {
		t.Fatal("naming an undeclared constant was accepted")
	}
	if got := err.Error(); !strings.Contains(got, "no such key") && !strings.Contains(got, "nope") {
		t.Errorf("the error reads %q; it should point at the name", got)
	}
}

// And the values a service was given reach a transformer built afterwards,
// which is how the runtime hands them over: once, before any flow is wired.
func TestTransformersBuiltAfterwardsGetTheConstants(t *testing.T) {
	SetDefaultConstants(map[string]interface{}{"region": "nz"})
	t.Cleanup(func() { SetDefaultConstants(nil) })

	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatal(err)
	}
	out, err := transformer.Transform(context.Background(), map[string]interface{}{},
		[]Rule{{Target: "region", Expression: "constants.region"}})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if out["region"] != "nz" {
		t.Errorf("region = %#v, want what the service declared", out["region"])
	}
}
