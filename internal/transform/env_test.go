package transform

import (
	"context"
	"testing"
)

// env is how a connector profile is chosen — select = "env('STORE_PROFILE')"
// is what the documentation shows and the whole reason profiles exist. It did
// not compile, so every evaluation failed and the connector fell back to its
// default without anybody choosing that.

func TestAnEnvironmentVariableCanBeReadFromAnExpression(t *testing.T) {
	t.Setenv("STORE_PROFILE", "remote")

	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	got, err := transformer.EvaluateExpression(context.Background(), nil, nil, "env('STORE_PROFILE')")
	if err != nil {
		t.Fatalf("the expression the documentation shows does not compile: %v", err)
	}
	if got != "remote" {
		t.Errorf("got = %v, want the value of the variable", got)
	}
}

func TestAVariableThatIsNotSetFallsBackToWhatWasGiven(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	got, err := transformer.EvaluateExpression(context.Background(), nil, nil,
		"env('A_VARIABLE_NOBODY_SET', 'local')")
	if err != nil {
		t.Fatalf("EvaluateExpression: %v", err)
	}
	if got != "local" {
		t.Errorf("got = %v, want the fallback", got)
	}

	// And with no fallback it is empty rather than an error, so an expression
	// around it can decide what to do.
	got, err = transformer.EvaluateExpression(context.Background(), nil, nil,
		"env('A_VARIABLE_NOBODY_SET')")
	if err != nil {
		t.Fatalf("EvaluateExpression: %v", err)
	}
	if got != "" {
		t.Errorf("got = %v, want nothing", got)
	}
}

func TestAnEnvironmentVariableComposesWithTheRest(t *testing.T) {
	// The point of reading it in CEL rather than in HCL: the value takes part
	// in an expression evaluated per message.
	t.Setenv("REGION", "nz")

	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	got, err := transformer.EvaluateExpression(context.Background(),
		map[string]interface{}{"sku": "ABC"}, nil,
		"upper(env('REGION')) + '-' + input.sku")
	if err != nil {
		t.Fatalf("EvaluateExpression: %v", err)
	}
	if got != "NZ-ABC" {
		t.Errorf("got = %v", got)
	}
}
