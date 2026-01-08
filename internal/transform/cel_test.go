package transform

import (
	"context"
	"strings"
	"testing"
)

func TestCELTransformer_BasicExpressions(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"name":  "John Doe",
		"email": "  JOHN@EXAMPLE.COM  ",
		"age":   25,
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		// Field references
		{"simple field", "input.name", "John Doe"},
		{"nested access", "input.age", int64(25)},

		// Custom functions
		{"lower", "lower(input.name)", "john doe"},
		{"upper", "upper(input.name)", "JOHN DOE"},
		{"trim", "trim(input.email)", "JOHN@EXAMPLE.COM"},
		{"trim + lower", "lower(trim(input.email))", "john@example.com"},

		// CEL built-in operators
		{"comparison", "input.age >= 18", true},
		{"comparison false", "input.age < 18", false},
		{"string concat", "input.name + ' Jr.'", "John Doe Jr."},
		{"arithmetic", "input.age + 5", int64(30)},

		// CEL ternary
		{"ternary true", "input.age >= 18 ? 'adult' : 'minor'", "adult"},
		{"ternary false", "input.age < 18 ? 'minor' : 'adult'", "adult"},

		// Logical operators
		{"and", "input.age > 20 && input.age < 30", true},
		{"or", "input.age < 20 || input.age > 30", false},
		{"not", "!(input.age < 18)", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(context.Background(), tt.expr, input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v (%T), got %v (%T)", tt.expected, tt.expected, result, result)
			}
		})
	}
}

func TestCELTransformer_CustomFunctions(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{}

	t.Run("uuid generates valid UUID", func(t *testing.T) {
		result, err := transformer.Evaluate(context.Background(), "uuid()", input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		uuid, ok := result.(string)
		if !ok {
			t.Fatalf("expected string, got %T", result)
		}

		// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
		if len(uuid) != 36 {
			t.Errorf("expected UUID length 36, got %d: %s", len(uuid), uuid)
		}

		parts := strings.Split(uuid, "-")
		if len(parts) != 5 {
			t.Errorf("expected 5 UUID parts, got %d", len(parts))
		}
	})

	t.Run("now generates timestamp", func(t *testing.T) {
		result, err := transformer.Evaluate(context.Background(), "now()", input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ts, ok := result.(string)
		if !ok {
			t.Fatalf("expected string, got %T", result)
		}

		// Should be RFC3339 format
		if !strings.Contains(ts, "T") || !strings.Contains(ts, "Z") {
			t.Errorf("expected RFC3339 format, got: %s", ts)
		}
	})

	t.Run("now_unix generates unix timestamp", func(t *testing.T) {
		result, err := transformer.Evaluate(context.Background(), "now_unix()", input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		unix, ok := result.(int64)
		if !ok {
			t.Fatalf("expected int64, got %T", result)
		}

		if unix < 1700000000 {
			t.Errorf("unix timestamp seems too low: %d", unix)
		}
	})

	t.Run("default returns fallback for empty", func(t *testing.T) {
		input := map[string]interface{}{
			"value": "",
		}
		result, err := transformer.Evaluate(context.Background(), `default(input.value, "fallback")`, input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "fallback" {
			t.Errorf("expected 'fallback', got %v", result)
		}
	})

	t.Run("replace replaces substrings", func(t *testing.T) {
		input := map[string]interface{}{
			"text": "hello world",
		}
		result, err := transformer.Evaluate(context.Background(), `replace(input.text, "world", "CEL")`, input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "hello CEL" {
			t.Errorf("expected 'hello CEL', got %v", result)
		}
	})

	t.Run("substring extracts substring", func(t *testing.T) {
		input := map[string]interface{}{
			"text": "hello world",
		}
		result, err := transformer.Evaluate(context.Background(), `substring(input.text, 0, 5)`, input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != "hello" {
			t.Errorf("expected 'hello', got %v", result)
		}
	})

	t.Run("len returns string length", func(t *testing.T) {
		input := map[string]interface{}{
			"text": "hello",
		}
		result, err := transformer.Evaluate(context.Background(), `len(input.text)`, input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result != int64(5) {
			t.Errorf("expected 5, got %v", result)
		}
	})
}

func TestCELTransformer_ListOperations(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"items": []interface{}{"apple", "banana", "cherry"},
		"numbers": []interface{}{1, 2, 3, 4, 5},
	}

	tests := []struct {
		name     string
		expr     string
		expected interface{}
	}{
		// List access
		{"first item", "input.items[0]", "apple"},
		{"last item", "input.items[2]", "cherry"},

		// List size
		{"list size", "size(input.items)", int64(3)},

		// List contains (CEL built-in)
		{"contains", `"banana" in input.items`, true},
		{"not contains", `"grape" in input.items`, false},

		// List filter and map return CEL list types, tested separately below

		// List exists (CEL built-in)
		{"exists", "input.numbers.exists(n, n > 4)", true},
		{"not exists", "input.numbers.exists(n, n > 10)", false},

		// List all (CEL built-in)
		{"all", "input.numbers.all(n, n > 0)", true},
		{"not all", "input.numbers.all(n, n > 2)", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(context.Background(), tt.expr, input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v (%T), got %v (%T)", tt.expected, tt.expected, result, result)
			}
		})
	}

	// Test filter separately - returns CEL list type
	t.Run("filter returns filtered list", func(t *testing.T) {
		result, err := transformer.Evaluate(context.Background(), "input.numbers.filter(n, n > 3)", input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Result is a CEL list, just verify it's not nil and has length
		if result == nil {
			t.Error("expected non-nil result")
		}
	})

	// Test map separately - returns CEL list type
	t.Run("map transforms list", func(t *testing.T) {
		result, err := transformer.Evaluate(context.Background(), "input.numbers.map(n, n * 2)", input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Error("expected non-nil result")
		}
	})
}

func TestCELTransformer_Transform(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"firstName": "John",
		"lastName":  "DOE",
		"email":     "  JOHN@EXAMPLE.COM  ",
		"age":       25,
		"roles":     []interface{}{"admin", "user"},
	}

	rules := []Rule{
		{Target: "name", Expression: `input.firstName + " " + lower(input.lastName)`},
		{Target: "email", Expression: "lower(trim(input.email))"},
		{Target: "is_adult", Expression: "input.age >= 18"},
		{Target: "id", Expression: "uuid()"},
		{Target: "created_at", Expression: "now()"},
		{Target: "role_count", Expression: "size(input.roles)"},
		{Target: "is_admin", Expression: `input.roles.exists(r, r == "admin")`},
	}

	result, err := transformer.Transform(context.Background(), input, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify results
	if result["name"] != "John doe" {
		t.Errorf("name: expected 'John doe', got %v", result["name"])
	}

	if result["email"] != "john@example.com" {
		t.Errorf("email: expected 'john@example.com', got %v", result["email"])
	}

	if result["is_adult"] != true {
		t.Errorf("is_adult: expected true, got %v", result["is_adult"])
	}

	if id, ok := result["id"].(string); !ok || len(id) != 36 {
		t.Errorf("id: expected UUID, got %v", result["id"])
	}

	if ts, ok := result["created_at"].(string); !ok || !strings.Contains(ts, "T") {
		t.Errorf("created_at: expected timestamp, got %v", result["created_at"])
	}

	if result["role_count"] != int64(2) {
		t.Errorf("role_count: expected 2, got %v", result["role_count"])
	}

	if result["is_admin"] != true {
		t.Errorf("is_admin: expected true, got %v", result["is_admin"])
	}
}

func TestCELTransformer_ValidateExpression(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	t.Run("valid expression", func(t *testing.T) {
		err := transformer.ValidateExpression("lower(input.email)")
		if err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
	})

	t.Run("invalid function", func(t *testing.T) {
		err := transformer.ValidateExpression("unknownFunc(input.email)")
		if err == nil {
			t.Error("expected error for unknown function")
		}
	})

	t.Run("syntax error", func(t *testing.T) {
		err := transformer.ValidateExpression("lower(input.email")
		if err == nil {
			t.Error("expected error for syntax error")
		}
	})
}

func TestCELTransformer_StandardExtensions(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"text":    "Hello World",
		"numbers": []interface{}{5, 3, 8, 1, 9},
		"items":   []interface{}{"a", "b", "c"},
	}

	tests := []struct {
		name     string
		expr     string
		validate func(result interface{}) bool
	}{
		// ext.Strings() - String extension functions
		{"strings: charAt", `"hello".charAt(1)`, func(r interface{}) bool { return r == "e" }},
		{"strings: indexOf", `"hello world".indexOf("world")`, func(r interface{}) bool { return r == int64(6) }},
		{"strings: lastIndexOf", `"hello hello".lastIndexOf("hello")`, func(r interface{}) bool { return r == int64(6) }},
		{"strings: lowerAscii", `"HELLO".lowerAscii()`, func(r interface{}) bool { return r == "hello" }},
		{"strings: upperAscii", `"hello".upperAscii()`, func(r interface{}) bool { return r == "HELLO" }},
		{"strings: replace", `"hello".replace("l", "L")`, func(r interface{}) bool { return r == "heLLo" }},
		{"strings: split", `"a,b,c".split(",")`, func(r interface{}) bool { return r != nil }},
		{"strings: substring", `"hello".substring(1, 4)`, func(r interface{}) bool { return r == "ell" }},
		{"strings: trim", `"  hello  ".trim()`, func(r interface{}) bool { return r == "hello" }},
		{"strings: reverse", `"hello".reverse()`, func(r interface{}) bool { return r == "olleh" }},

		// ext.Encoders() - Base64 encoding
		{"encoders: base64.encode", `base64.encode(b"hello")`, func(r interface{}) bool { return r == "aGVsbG8=" }},
		{"encoders: base64.decode", `base64.decode("aGVsbG8=")`, func(r interface{}) bool { return r != nil }},

		// ext.Math() - Math functions
		{"math: abs", `math.abs(-5)`, func(r interface{}) bool { return r == int64(5) }},
		{"math: ceil", `math.ceil(3.2)`, func(r interface{}) bool { return r == 4.0 }},
		{"math: floor", `math.floor(3.8)`, func(r interface{}) bool { return r == 3.0 }},
		{"math: round", `math.round(3.5)`, func(r interface{}) bool { return r == 4.0 }},
		{"math: sign positive", `math.sign(5)`, func(r interface{}) bool { return r == int64(1) }},
		{"math: sign negative", `math.sign(-5)`, func(r interface{}) bool { return r == int64(-1) }},
		{"math: greatest", `math.greatest(1, 5, 3)`, func(r interface{}) bool { return r == int64(5) }},
		{"math: least", `math.least(1, 5, 3)`, func(r interface{}) bool { return r == int64(1) }},

		// ext.Lists() - List functions
		{"lists: slice", `[1, 2, 3, 4, 5].slice(1, 4)`, func(r interface{}) bool { return r != nil }},

		// Standard CEL (not extensions)
		{"std: size", `size("hello")`, func(r interface{}) bool { return r == int64(5) }},
		{"std: contains", `"hello".contains("ell")`, func(r interface{}) bool { return r == true }},
		{"std: startsWith", `"hello".startsWith("hel")`, func(r interface{}) bool { return r == true }},
		{"std: endsWith", `"hello".endsWith("llo")`, func(r interface{}) bool { return r == true }},
		{"std: matches regex", `"hello123".matches("[a-z]+[0-9]+")`, func(r interface{}) bool { return r == true }},
		{"std: type", `type(123)`, func(r interface{}) bool { return r != nil }},
		{"std: int conversion", `int("42")`, func(r interface{}) bool { return r == int64(42) }},
		{"std: double conversion", `double("3.14")`, func(r interface{}) bool { return r == 3.14 }},
		{"std: string conversion", `string(42)`, func(r interface{}) bool { return r == "42" }},
		{"std: duration", `duration("1h30m")`, func(r interface{}) bool { return r != nil }},
		{"std: timestamp", `timestamp("2023-01-15T10:30:00Z")`, func(r interface{}) bool { return r != nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.Evaluate(context.Background(), tt.expr, input)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tt.expr, err)
			}

			if !tt.validate(result) {
				t.Errorf("validation failed for %s, got: %v (%T)", tt.expr, result, result)
			}
		})
	}
}

func TestCELTransformer_ProgramCaching(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"value": "test",
	}

	expr := "upper(input.value)"

	// First evaluation - compiles and caches
	result1, err := transformer.Evaluate(context.Background(), expr, input)
	if err != nil {
		t.Fatalf("first evaluation failed: %v", err)
	}

	// Second evaluation - uses cached program
	result2, err := transformer.Evaluate(context.Background(), expr, input)
	if err != nil {
		t.Fatalf("second evaluation failed: %v", err)
	}

	if result1 != result2 {
		t.Errorf("results should be equal: %v != %v", result1, result2)
	}

	// Verify cache is being used
	transformer.mu.RLock()
	_, cached := transformer.programs[expr]
	transformer.mu.RUnlock()

	if !cached {
		t.Error("expected program to be cached")
	}
}

func TestCELTransformer_EvaluateExpression(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"product_id": "prod-123",
		"quantity":   5,
	}

	t.Run("access input field", func(t *testing.T) {
		result, err := transformer.EvaluateExpression(context.Background(), input, nil, "input.product_id")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "prod-123" {
			t.Errorf("expected 'prod-123', got %v", result)
		}
	})

	t.Run("access enriched data", func(t *testing.T) {
		enriched := map[string]interface{}{
			"pricing": map[string]interface{}{
				"price":    99.99,
				"currency": "USD",
			},
		}

		result, err := transformer.EvaluateExpression(context.Background(), input, enriched, "enriched.pricing.price")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 99.99 {
			t.Errorf("expected 99.99, got %v", result)
		}
	})

	t.Run("combine input and enriched", func(t *testing.T) {
		enriched := map[string]interface{}{
			"pricing": map[string]interface{}{
				"unit_price": 10.0,
			},
		}

		// Calculate total: quantity * unit_price
		result, err := transformer.EvaluateExpression(context.Background(), input, enriched, "double(input.quantity) * enriched.pricing.unit_price")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 50.0 {
			t.Errorf("expected 50.0, got %v", result)
		}
	})

	t.Run("enriched can be nil", func(t *testing.T) {
		result, err := transformer.EvaluateExpression(context.Background(), input, nil, "input.quantity")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != int64(5) {
			t.Errorf("expected 5, got %v", result)
		}
	})
}

func TestCELTransformer_TransformWithEnriched(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"id":       "prod-123",
		"name":     "Widget",
		"quantity": 3,
	}

	enriched := map[string]interface{}{
		"pricing": map[string]interface{}{
			"unit_price": 25.0,
			"currency":   "USD",
		},
		"inventory": map[string]interface{}{
			"stock":    100,
			"reserved": 10,
		},
	}

	rules := []Rule{
		{Target: "id", Expression: "input.id"},
		{Target: "name", Expression: "upper(input.name)"},
		{Target: "price", Expression: "enriched.pricing.unit_price"},
		{Target: "currency", Expression: "enriched.pricing.currency"},
		{Target: "total", Expression: "double(input.quantity) * enriched.pricing.unit_price"},
		{Target: "available_stock", Expression: "enriched.inventory.stock - enriched.inventory.reserved"},
	}

	result, err := transformer.TransformWithEnriched(context.Background(), input, enriched, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify results
	if result["id"] != "prod-123" {
		t.Errorf("id: expected 'prod-123', got %v", result["id"])
	}

	if result["name"] != "WIDGET" {
		t.Errorf("name: expected 'WIDGET', got %v", result["name"])
	}

	if result["price"] != 25.0 {
		t.Errorf("price: expected 25.0, got %v", result["price"])
	}

	if result["currency"] != "USD" {
		t.Errorf("currency: expected 'USD', got %v", result["currency"])
	}

	if result["total"] != 75.0 {
		t.Errorf("total: expected 75.0, got %v", result["total"])
	}

	if result["available_stock"] != int64(90) {
		t.Errorf("available_stock: expected 90, got %v", result["available_stock"])
	}
}

func TestCELTransformer_TransformWithEnriched_NilEnriched(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"name": "test",
	}

	rules := []Rule{
		{Target: "name", Expression: "upper(input.name)"},
	}

	// Should work even with nil enriched
	result, err := transformer.TransformWithEnriched(context.Background(), input, nil, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["name"] != "TEST" {
		t.Errorf("expected 'TEST', got %v", result["name"])
	}
}

func TestCELTransformer_TransformWithEnriched_NestedEnriched(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"product_id": "prod-123",
	}

	// Simulate nested enriched data from multiple sources
	enriched := map[string]interface{}{
		"product": map[string]interface{}{
			"name":        "Super Widget",
			"description": "A really great widget",
			"category": map[string]interface{}{
				"id":   "cat-1",
				"name": "Electronics",
			},
		},
		"pricing": map[string]interface{}{
			"tiers": []interface{}{
				map[string]interface{}{"min_qty": 1, "price": 100.0},
				map[string]interface{}{"min_qty": 10, "price": 90.0},
			},
		},
	}

	rules := []Rule{
		{Target: "name", Expression: "enriched.product.name"},
		{Target: "category", Expression: "enriched.product.category.name"},
	}

	result, err := transformer.TransformWithEnriched(context.Background(), input, enriched, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["name"] != "Super Widget" {
		t.Errorf("name: expected 'Super Widget', got %v", result["name"])
	}

	if result["category"] != "Electronics" {
		t.Errorf("category: expected 'Electronics', got %v", result["category"])
	}
}

func TestCELTransformer_EvaluateCondition(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	tests := []struct {
		name     string
		data     map[string]interface{}
		expr     string
		expected bool
	}{
		{
			name: "simple true condition",
			data: map[string]interface{}{
				"input": map[string]interface{}{"enabled": true},
			},
			expr:     "input.enabled == true",
			expected: true,
		},
		{
			name: "simple false condition",
			data: map[string]interface{}{
				"input": map[string]interface{}{"enabled": false},
			},
			expr:     "input.enabled == true",
			expected: false,
		},
		{
			name: "string comparison",
			data: map[string]interface{}{
				"input": map[string]interface{}{"type": "premium"},
			},
			expr:     "input.type == 'premium'",
			expected: true,
		},
		{
			name: "numeric comparison",
			data: map[string]interface{}{
				"input": map[string]interface{}{"quantity": 5},
			},
			expr:     "input.quantity > 0",
			expected: true,
		},
		{
			name: "step result condition",
			data: map[string]interface{}{
				"input": map[string]interface{}{"user_id": "123"},
				"step": map[string]interface{}{
					"user": map[string]interface{}{
						"status": "active",
					},
				},
			},
			expr:     "step.user.status == 'active'",
			expected: true,
		},
		{
			name: "combined input and step condition",
			data: map[string]interface{}{
				"input": map[string]interface{}{"include_prices": true},
				"step": map[string]interface{}{
					"product": map[string]interface{}{"price": 100},
				},
			},
			expr:     "input.include_prices && step.product.price > 0",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.EvaluateCondition(context.Background(), tt.data, tt.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCELTransformer_EvaluateCondition_InvalidReturnType(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	data := map[string]interface{}{
		"input": map[string]interface{}{"name": "test"},
	}

	// Expression returns string, not boolean
	_, err = transformer.EvaluateCondition(context.Background(), data, "input.name")
	if err == nil {
		t.Error("expected error for non-boolean return type")
	}
}

func TestCELTransformer_TransformWithSteps(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"user_id":  "user-123",
		"quantity": 3,
	}

	enriched := map[string]interface{}{
		"pricing": map[string]interface{}{
			"base_price": 100.0,
		},
	}

	steps := map[string]interface{}{
		"user": map[string]interface{}{
			"email":    "user@example.com",
			"name":     "John Doe",
			"discount": 0.1,
		},
		"inventory": map[string]interface{}{
			"available": 50,
		},
	}

	rules := []Rule{
		{Target: "user_email", Expression: "step.user.email"},
		{Target: "user_name", Expression: "step.user.name"},
		{Target: "base_price", Expression: "enriched.pricing.base_price"},
		{Target: "discount", Expression: "step.user.discount"},
		{Target: "final_price", Expression: "enriched.pricing.base_price * (1.0 - step.user.discount)"},
		{Target: "in_stock", Expression: "step.inventory.available >= input.quantity"},
	}

	result, err := transformer.TransformWithSteps(context.Background(), input, enriched, steps, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["user_email"] != "user@example.com" {
		t.Errorf("user_email: expected 'user@example.com', got %v", result["user_email"])
	}

	if result["user_name"] != "John Doe" {
		t.Errorf("user_name: expected 'John Doe', got %v", result["user_name"])
	}

	if result["base_price"] != 100.0 {
		t.Errorf("base_price: expected 100.0, got %v", result["base_price"])
	}

	if result["discount"] != 0.1 {
		t.Errorf("discount: expected 0.1, got %v", result["discount"])
	}

	if result["final_price"] != 90.0 {
		t.Errorf("final_price: expected 90.0, got %v", result["final_price"])
	}

	if result["in_stock"] != true {
		t.Errorf("in_stock: expected true, got %v", result["in_stock"])
	}
}

func TestCELTransformer_TransformWithSteps_NilSteps(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"name": "test",
	}

	rules := []Rule{
		{Target: "name", Expression: "upper(input.name)"},
	}

	// Should work even with nil steps
	result, err := transformer.TransformWithSteps(context.Background(), input, nil, nil, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["name"] != "TEST" {
		t.Errorf("expected 'TEST', got %v", result["name"])
	}
}

func TestCELTransformer_TransformWithSteps_ChainedSteps(t *testing.T) {
	transformer, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create CEL transformer: %v", err)
	}

	input := map[string]interface{}{
		"order_id": "order-123",
	}

	// Simulate chained step results where step2 uses step1 result
	steps := map[string]interface{}{
		"step1": map[string]interface{}{
			"customer_id": "cust-456",
		},
		"step2": map[string]interface{}{
			"customer_email": "customer@example.com",
			"customer_name":  "Jane Smith",
		},
	}

	rules := []Rule{
		{Target: "order_id", Expression: "input.order_id"},
		{Target: "customer_id", Expression: "step.step1.customer_id"},
		{Target: "email", Expression: "step.step2.customer_email"},
		{Target: "name", Expression: "step.step2.customer_name"},
	}

	result, err := transformer.TransformWithSteps(context.Background(), input, nil, steps, rules)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["order_id"] != "order-123" {
		t.Errorf("order_id: expected 'order-123', got %v", result["order_id"])
	}

	if result["customer_id"] != "cust-456" {
		t.Errorf("customer_id: expected 'cust-456', got %v", result["customer_id"])
	}

	if result["email"] != "customer@example.com" {
		t.Errorf("email: expected 'customer@example.com', got %v", result["email"])
	}

	if result["name"] != "Jane Smith" {
		t.Errorf("name: expected 'Jane Smith', got %v", result["name"])
	}
}
