package optimizer

import (
	"testing"

	"github.com/matutetandil/mycel/internal/flow"
)

func TestStepOptimizer_AnalyzeDependencies(t *testing.T) {
	steps := []*flow.StepConfig{
		{Name: "orders", Connector: "db", Query: "SELECT * FROM orders WHERE user_id = ?"},
		{Name: "reviews", Connector: "api", Operation: "GET /reviews"},
		{Name: "inventory", Connector: "inventory_api", Operation: "GET /stock"},
	}

	transformExprs := map[string]string{
		"id":       "input.id",
		"name":     "input.name",
		"orders":   "step.orders",
		"reviews":  "step.reviews",
		"inStock":  "step.inventory.quantity > 0",
	}

	t.Run("all fields requested - all steps needed", func(t *testing.T) {
		requestedFields := []string{"id", "name", "orders", "reviews", "inStock"}
		opt := NewStepOptimizer(steps, transformExprs, requestedFields)
		needed := opt.AnalyzeDependencies()

		if !needed["orders"] {
			t.Error("orders step should be needed")
		}
		if !needed["reviews"] {
			t.Error("reviews step should be needed")
		}
		if !needed["inventory"] {
			t.Error("inventory step should be needed")
		}
	})

	t.Run("only basic fields - no steps needed", func(t *testing.T) {
		requestedFields := []string{"id", "name"}
		opt := NewStepOptimizer(steps, transformExprs, requestedFields)
		_ = opt.AnalyzeDependencies()

		skippable := opt.GetSkippableSteps()
		if len(skippable) != 3 {
			t.Errorf("expected 3 skippable steps, got %d", len(skippable))
		}
	})

	t.Run("only orders requested - only orders step needed", func(t *testing.T) {
		requestedFields := []string{"id", "name", "orders"}
		opt := NewStepOptimizer(steps, transformExprs, requestedFields)
		needed := opt.AnalyzeDependencies()

		if !needed["orders"] {
			t.Error("orders step should be needed")
		}
		if needed["reviews"] {
			t.Error("reviews step should not be needed")
		}
		if needed["inventory"] {
			t.Error("inventory step should not be needed")
		}
	})

	t.Run("empty requested fields - all steps execute", func(t *testing.T) {
		requestedFields := []string{}
		opt := NewStepOptimizer(steps, transformExprs, requestedFields)
		needed := opt.AnalyzeDependencies()

		// All steps should execute when no optimization info available
		if !needed["orders"] || !needed["reviews"] || !needed["inventory"] {
			t.Error("all steps should execute when no requested fields")
		}
	})
}

func TestStepOptimizer_StepDependencies(t *testing.T) {
	// Step B depends on Step A's result
	steps := []*flow.StepConfig{
		{Name: "user", Connector: "db", Query: "SELECT * FROM users WHERE id = ?"},
		{Name: "user_orders", Connector: "db", Query: "SELECT * FROM orders WHERE user_id = ?",
			Params: map[string]interface{}{"user_id": "step.user.id"}},
	}

	transformExprs := map[string]string{
		"id":     "input.id",
		"orders": "step.user_orders",
	}

	t.Run("dependent step includes its dependency", func(t *testing.T) {
		requestedFields := []string{"id", "orders"}
		opt := NewStepOptimizer(steps, transformExprs, requestedFields)
		needed := opt.AnalyzeDependencies()

		// user_orders is needed for orders field
		if !needed["user_orders"] {
			t.Error("user_orders step should be needed")
		}
		// user is a dependency of user_orders
		if !needed["user"] {
			t.Error("user step should be needed (dependency of user_orders)")
		}
	})
}

func TestStepOptimizer_GenerateStepConditions(t *testing.T) {
	steps := []*flow.StepConfig{
		{Name: "orders", Connector: "db"},
		{Name: "reviews", Connector: "api"},
	}

	transformExprs := map[string]string{
		"orders":  "step.orders",
		"reviews": "step.reviews",
	}

	opt := NewStepOptimizer(steps, transformExprs, nil)
	conditions := opt.GenerateStepConditions()

	if conditions["orders"] != `has_field(input, "orders")` {
		t.Errorf("unexpected orders condition: %s", conditions["orders"])
	}
	if conditions["reviews"] != `has_field(input, "reviews")` {
		t.Errorf("unexpected reviews condition: %s", conditions["reviews"])
	}
}

func TestStepOptimizer_MultipleFieldsUseStep(t *testing.T) {
	steps := []*flow.StepConfig{
		{Name: "pricing", Connector: "api"},
	}

	transformExprs := map[string]string{
		"price":    "step.pricing.base",
		"discount": "step.pricing.discount",
		"total":    "step.pricing.total",
	}

	opt := NewStepOptimizer(steps, transformExprs, nil)
	conditions := opt.GenerateStepConditions()

	// Should have OR condition since multiple fields use this step
	cond := conditions["pricing"]
	if cond == "" {
		t.Error("expected condition for pricing step")
	}
	if !containsString(cond, "||") {
		t.Error("expected OR condition when multiple fields use a step")
	}
}

func TestOptimizeFlowSteps(t *testing.T) {
	steps := []*flow.StepConfig{
		{Name: "orders", Connector: "db", Query: "SELECT * FROM orders"},
		{Name: "existing", Connector: "api", When: "input.withDetails == true"},
	}

	transformExprs := map[string]string{
		"orders":  "step.orders",
		"details": "step.existing",
	}

	optimized := OptimizeFlowSteps(steps, transformExprs)

	// First step should have new condition
	if optimized[0].When != `has_field(input, "orders")` {
		t.Errorf("unexpected when for orders: %s", optimized[0].When)
	}

	// Second step should combine conditions
	if optimized[1].When == "" {
		t.Error("existing step should have combined condition")
	}
	if !containsString(optimized[1].When, "input.withDetails") {
		t.Error("existing condition should be preserved")
	}
	if !containsString(optimized[1].When, "has_field") {
		t.Error("field condition should be added")
	}
}

func TestExtractTransformExpressions(t *testing.T) {
	exprs := map[string]string{
		"output.id":     "input.id",
		"output.name":   "input.name",
		"output.orders": "step.orders",
	}

	result := ExtractTransformExpressions(exprs)

	if result["id"] != "input.id" {
		t.Error("expected id expression")
	}
	if result["name"] != "input.name" {
		t.Error("expected name expression")
	}
	if result["orders"] != "step.orders" {
		t.Error("expected orders expression")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
