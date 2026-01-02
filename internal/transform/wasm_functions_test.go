package transform

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/internal/functions"
)

// mockFunction implements functions.Function for testing
type mockFunction struct {
	name   string
	result interface{}
	err    error
}

func (f *mockFunction) Name() string {
	return f.name
}

func (f *mockFunction) Call(args ...interface{}) (interface{}, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// mockRegistry implements a simple mock for functions.Registry
type mockRegistry struct {
	funcs map[string]functions.Function
}

func (r *mockRegistry) GetAllFunctions() map[string]functions.Function {
	return r.funcs
}

func TestCreateWASMFunctionOptions_Nil(t *testing.T) {
	opts := CreateWASMFunctionOptions(nil)
	if opts != nil {
		t.Error("expected nil options for nil registry")
	}
}

func TestCELTransformerWithOptions_NoOptions(t *testing.T) {
	transformer, err := NewCELTransformerWithOptions()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	// Verify basic CEL functions still work
	result, err := transformer.Evaluate(context.Background(), "uuid()", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result from uuid()")
	}

	// Verify string length
	str, ok := result.(string)
	if !ok {
		t.Errorf("expected string result, got %T", result)
	}
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (36 chars)
	if len(str) != 36 {
		t.Errorf("expected 36 char UUID, got %d chars", len(str))
	}
}

func TestCELTransformerWithOptions_BuiltinFunctions(t *testing.T) {
	transformer, err := NewCELTransformerWithOptions()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	tests := []struct {
		name   string
		expr   string
		input  map[string]interface{}
		expect interface{}
	}{
		{
			name:   "lower function",
			expr:   "lower('HELLO')",
			expect: "hello",
		},
		{
			name:   "upper function",
			expr:   "upper('hello')",
			expect: "hello", // Should be uppercase
		},
		{
			name:   "trim function",
			expr:   "trim('  hello  ')",
			expect: "hello",
		},
		{
			name:   "len function",
			expr:   "len('hello')",
			expect: int64(5),
		},
		{
			name:   "default with value",
			expr:   "default('value', 'default')",
			expect: "value",
		},
		{
			name:   "default with null",
			expr:   "default(null, 'default')",
			expect: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.input
			if input == nil {
				input = make(map[string]interface{})
			}

			result, err := transformer.Evaluate(context.Background(), tt.expr, input)
			if err != nil {
				t.Errorf("unexpected error for %s: %v", tt.expr, err)
				return
			}

			// For upper, fix the expected value
			if tt.name == "upper function" {
				if result != "HELLO" {
					t.Errorf("expected 'HELLO', got %v", result)
				}
				return
			}

			if result != tt.expect {
				t.Errorf("expected %v, got %v", tt.expect, result)
			}
		})
	}
}

func TestCELTransformer_Transform_WithInput(t *testing.T) {
	transformer, err := NewCELTransformerWithOptions()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := map[string]interface{}{
		"name":  "John Doe",
		"email": "JOHN@EXAMPLE.COM",
	}

	rules := []Rule{
		{Target: "name", Expression: "input.name"},
		{Target: "email", Expression: "lower(input.email)"},
		{Target: "greeting", Expression: "\"Hello, \" + input.name"},
	}

	result, err := transformer.Transform(context.Background(), input, rules)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	if result["name"] != "John Doe" {
		t.Errorf("expected name 'John Doe', got %v", result["name"])
	}

	if result["email"] != "john@example.com" {
		t.Errorf("expected email 'john@example.com', got %v", result["email"])
	}

	if result["greeting"] != "Hello, John Doe" {
		t.Errorf("expected greeting 'Hello, John Doe', got %v", result["greeting"])
	}
}

func TestCELTransformer_ValidateExpression_WithOptions(t *testing.T) {
	transformer, err := NewCELTransformerWithOptions()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	// Valid expression
	if err := transformer.ValidateExpression("1 + 1"); err != nil {
		t.Errorf("unexpected error for valid expression: %v", err)
	}

	// Invalid expression
	if err := transformer.ValidateExpression("invalid(("); err == nil {
		t.Error("expected error for invalid expression")
	}
}

// TestCelToGo verifies the celToGo helper function
func TestCelToGo(t *testing.T) {
	// Test nil value
	result := celToGo(nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

func TestCelListToSlice_InvalidInput(t *testing.T) {
	// Test with non-list value (this should fail)
	_, err := celListToSlice(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
}
