package transform

import (
	"context"
	"strings"
	"testing"
)

func TestExpressionParser_Literals(t *testing.T) {
	transformer := NewDefaultTransformer()
	ctx := &EvalContext{
		Input:    make(map[string]interface{}),
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		{"string double quotes", `"hello"`, "hello"},
		{"string single quotes", `'world'`, "world"},
		{"integer", "42", 42},
		{"negative integer", "-10", -10},
		{"float", "3.14", 3.14},
		{"boolean true", "true", true},
		{"boolean false", "false", false},
		{"null", "null", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(ctx, tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v (%T), got %v (%T)", tt.expected, tt.expected, result, result)
			}
		})
	}
}

func TestExpressionParser_FieldReferences(t *testing.T) {
	transformer := NewDefaultTransformer()
	input := map[string]interface{}{
		"name":  "John",
		"email": "john@example.com",
		"user": map[string]interface{}{
			"id":   123,
			"role": "admin",
		},
	}
	ctx := &EvalContext{
		Input:    input,
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		{"simple field", "input.name", "John"},
		{"nested field", "input.user.role", "admin"},
		{"missing field", "input.missing", nil},
		{"without prefix", "name", "John"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(ctx, tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExpressionParser_Functions(t *testing.T) {
	transformer := NewDefaultTransformer()
	input := map[string]interface{}{
		"name":  "  John Doe  ",
		"email": "JOHN@EXAMPLE.COM",
	}
	ctx := &EvalContext{
		Input:    input,
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		{"lower", `lower("HELLO")`, "hello"},
		{"upper", `upper("hello")`, "HELLO"},
		{"trim", `trim("  hello  ")`, "hello"},
		{"lower with field", "lower(input.email)", "john@example.com"},
		{"trim with field", "trim(input.name)", "John Doe"},
		{"concat", `concat("Hello", " ", "World")`, "Hello World"},
		{"coalesce", `coalesce(null, "default")`, "default"},
		{"default", `default(null, "fallback")`, "fallback"},
		{"len string", `len("hello")`, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(ctx, tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExpressionParser_NestedFunctions(t *testing.T) {
	transformer := NewDefaultTransformer()
	input := map[string]interface{}{
		"email": "  JOHN@EXAMPLE.COM  ",
	}
	ctx := &EvalContext{
		Input:    input,
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	result, err := transformer.Evaluate(ctx, "lower(trim(input.email))")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "john@example.com"
	if result != expected {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestExpressionParser_UUID(t *testing.T) {
	transformer := NewDefaultTransformer()
	ctx := &EvalContext{
		Input:    make(map[string]interface{}),
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	result, err := transformer.Evaluate(ctx, "uuid()")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uuid, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}

	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	if len(uuid) != 36 {
		t.Errorf("invalid UUID length: %d", len(uuid))
	}

	parts := strings.Split(uuid, "-")
	if len(parts) != 5 {
		t.Errorf("invalid UUID format: %s", uuid)
	}
}

func TestExpressionParser_Now(t *testing.T) {
	transformer := NewDefaultTransformer()
	ctx := &EvalContext{
		Input:    make(map[string]interface{}),
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	result, err := transformer.Evaluate(ctx, "now()")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	timestamp, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}

	// Should be RFC3339 format
	if !strings.Contains(timestamp, "T") || !strings.Contains(timestamp, "Z") {
		t.Errorf("invalid timestamp format: %s", timestamp)
	}
}

func TestTransformer_Transform(t *testing.T) {
	transformer := NewDefaultTransformer()
	input := map[string]interface{}{
		"firstName": "John",
		"lastName":  "Doe",
		"email":     "JOHN@EXAMPLE.COM",
	}

	rules := []Rule{
		{Target: "name", Expression: `concat(input.firstName, " ", input.lastName)`},
		{Target: "email", Expression: "lower(input.email)"},
		{Target: "active", Expression: "true"},
	}

	result, err := transformer.Transform(context.Background(), input, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["name"] != "John Doe" {
		t.Errorf("expected 'John Doe', got %v", result["name"])
	}

	if result["email"] != "john@example.com" {
		t.Errorf("expected 'john@example.com', got %v", result["email"])
	}

	if result["active"] != true {
		t.Errorf("expected true, got %v", result["active"])
	}
}

func TestTransformer_NestedOutput(t *testing.T) {
	transformer := NewDefaultTransformer()
	input := map[string]interface{}{
		"name": "John",
	}

	rules := []Rule{
		{Target: "user.name", Expression: "input.name"},
		{Target: "user.active", Expression: "true"},
	}

	result, err := transformer.Transform(context.Background(), input, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user, ok := result["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map, got %T", result["user"])
	}

	if user["name"] != "John" {
		t.Errorf("expected 'John', got %v", user["name"])
	}

	if user["active"] != true {
		t.Errorf("expected true, got %v", user["active"])
	}
}

func TestTransformer_TypeConversions(t *testing.T) {
	transformer := NewDefaultTransformer()
	input := map[string]interface{}{
		"stringNum": "42",
		"number":    123,
	}
	ctx := &EvalContext{
		Input:    input,
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		{"to_string", "to_string(input.number)", "123"},
		{"to_int", `to_int("42")`, 42},
		{"to_float", `to_float("3.14")`, 3.14},
		{"to_bool true", `to_bool("true")`, true},
		{"to_bool false", `to_bool("false")`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(ctx, tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v (%T), got %v (%T)", tt.expected, tt.expected, result, result)
			}
		})
	}
}

func TestStringFunctions(t *testing.T) {
	transformer := NewDefaultTransformer()
	ctx := &EvalContext{
		Input:    make(map[string]interface{}),
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		{"trim_prefix", `trim_prefix("hello_world", "hello_")`, "world"},
		{"trim_suffix", `trim_suffix("hello_world", "_world")`, "hello"},
		{"replace", `replace("hello world", "world", "there")`, "hello there"},
		{"substring 2 args", `substring("hello", 1)`, "ello"},
		{"substring 3 args", `substring("hello", 1, 3)`, "ell"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(ctx, tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIfFunction(t *testing.T) {
	transformer := NewDefaultTransformer()
	ctx := &EvalContext{
		Input:    make(map[string]interface{}),
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		{"if true", `if(true, "yes", "no")`, "yes"},
		{"if false", `if(false, "yes", "no")`, "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(ctx, tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRandomString(t *testing.T) {
	transformer := NewDefaultTransformer()
	ctx := &EvalContext{
		Input:    make(map[string]interface{}),
		Output:   make(map[string]interface{}),
		Registry: transformer.registry,
	}

	result, err := transformer.Evaluate(ctx, "random_string(16)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}

	if len(str) != 16 {
		t.Errorf("expected length 16, got %d", len(str))
	}
}
