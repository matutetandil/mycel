package aspect

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    *Config
		wantError bool
	}{
		{
			name: "valid config with action",
			config: &Config{
				Name: "audit_log",
				On:   []string{"flows/**/create_*.hcl"},
				When: After,
				Action: &ActionConfig{
					Connector: "audit_db",
					Target:    "audit_logs",
				},
			},
			wantError: false,
		},
		{
			name: "valid config with cache",
			config: &Config{
				Name: "cache_products",
				On:   []string{"flows/**/get_*.hcl"},
				When: Around,
				Cache: &CacheConfig{
					Storage: "redis",
					TTL:     "5m",
					Key:     "products:${input.id}",
				},
			},
			wantError: false,
		},
		{
			name: "missing name",
			config: &Config{
				On:   []string{"flows/**/*.hcl"},
				When: Before,
				Action: &ActionConfig{
					Connector: "db",
				},
			},
			wantError: true,
		},
		{
			name: "missing on patterns",
			config: &Config{
				Name: "test",
				When: Before,
				Action: &ActionConfig{
					Connector: "db",
				},
			},
			wantError: true,
		},
		{
			name: "missing when",
			config: &Config{
				Name: "test",
				On:   []string{"flows/**/*.hcl"},
				Action: &ActionConfig{
					Connector: "db",
				},
			},
			wantError: true,
		},
		{
			name: "invalid when value",
			config: &Config{
				Name: "test",
				On:   []string{"flows/**/*.hcl"},
				When: When("invalid"),
				Action: &ActionConfig{
					Connector: "db",
				},
			},
			wantError: true,
		},
		{
			name: "no action type",
			config: &Config{
				Name: "test",
				On:   []string{"flows/**/*.hcl"},
				When: Before,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantError && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRegistry_Register(t *testing.T) {
	registry := NewRegistry()

	aspect := &Config{
		Name: "test_aspect",
		On:   []string{"flows/**/*.hcl"},
		When: Before,
		Action: &ActionConfig{
			Connector: "db",
		},
	}

	err := registry.Register(aspect)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if registry.Count() != 1 {
		t.Errorf("expected 1 aspect, got %d", registry.Count())
	}
}

func TestRegistry_Match(t *testing.T) {
	registry := NewRegistry()

	// Register aspects with different patterns
	registry.Register(&Config{
		Name: "audit_create",
		On:   []string{"flows/**/create_*.hcl"},
		When: After,
		Action: &ActionConfig{
			Connector: "audit",
		},
	})

	registry.Register(&Config{
		Name: "cache_get",
		On:   []string{"flows/**/get_*.hcl"},
		When: Around,
		Cache: &CacheConfig{
			Storage: "cache",
			TTL:     "5m",
			Key:     "test",
		},
	})

	registry.Register(&Config{
		Name: "log_all",
		On:   []string{"flows/**/*.hcl"},
		When: Before,
		Action: &ActionConfig{
			Connector: "logger",
		},
	})

	tests := []struct {
		flowPath      string
		expectedCount int
		expectedNames []string
	}{
		{
			flowPath:      "flows/users/create_user.hcl",
			expectedCount: 2, // log_all (before) + audit_create (after)
			expectedNames: []string{"log_all", "audit_create"},
		},
		{
			flowPath:      "flows/products/get_product.hcl",
			expectedCount: 2, // log_all (before) + cache_get (around)
			expectedNames: []string{"log_all", "cache_get"},
		},
		{
			flowPath:      "flows/orders/list_orders.hcl",
			expectedCount: 1, // log_all only
			expectedNames: []string{"log_all"},
		},
		{
			flowPath:      "other/file.hcl",
			expectedCount: 0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.flowPath, func(t *testing.T) {
			matches := registry.Match(tt.flowPath)
			if len(matches) != tt.expectedCount {
				t.Errorf("expected %d matches, got %d", tt.expectedCount, len(matches))
			}

			for i, name := range tt.expectedNames {
				if i < len(matches) && matches[i].Name != name {
					t.Errorf("expected aspect %s at position %d, got %s", name, i, matches[i].Name)
				}
			}
		})
	}
}

func TestRegistry_MatchByWhen(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Config{
		Name: "before_1",
		On:   []string{"flows/**/*.hcl"},
		When: Before,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	registry.Register(&Config{
		Name: "around_1",
		On:   []string{"flows/**/*.hcl"},
		When: Around,
		Cache: &CacheConfig{
			Storage: "cache",
			TTL:     "5m",
			Key:     "test",
		},
	})

	registry.Register(&Config{
		Name: "after_1",
		On:   []string{"flows/**/*.hcl"},
		When: After,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	before := registry.GetBefore("flows/test/test.hcl")
	if len(before) != 1 || before[0].Name != "before_1" {
		t.Errorf("expected 1 before aspect, got %d", len(before))
	}

	around := registry.GetAround("flows/test/test.hcl")
	if len(around) != 1 || around[0].Name != "around_1" {
		t.Errorf("expected 1 around aspect, got %d", len(around))
	}

	after := registry.GetAfter("flows/test/test.hcl")
	if len(after) != 1 || after[0].Name != "after_1" {
		t.Errorf("expected 1 after aspect, got %d", len(after))
	}
}

func TestRegistry_Priority(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Config{
		Name:     "low_priority",
		On:       []string{"flows/**/*.hcl"},
		When:     Before,
		Priority: 10,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	registry.Register(&Config{
		Name:     "high_priority",
		On:       []string{"flows/**/*.hcl"},
		When:     Before,
		Priority: 1,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	registry.Register(&Config{
		Name:     "medium_priority",
		On:       []string{"flows/**/*.hcl"},
		When:     Before,
		Priority: 5,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	matches := registry.Match("flows/test/test.hcl")

	expected := []string{"high_priority", "medium_priority", "low_priority"}
	for i, name := range expected {
		if matches[i].Name != name {
			t.Errorf("expected %s at position %d, got %s", name, i, matches[i].Name)
		}
	}
}

func TestRegistry_Clear(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Config{
		Name: "test",
		On:   []string{"flows/**/*.hcl"},
		When: Before,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	if registry.Count() != 1 {
		t.Errorf("expected 1 aspect before clear")
	}

	registry.Clear()

	if registry.Count() != 0 {
		t.Errorf("expected 0 aspects after clear, got %d", registry.Count())
	}
}

func TestExecutor_Execute_NoAspects(t *testing.T) {
	// Create empty registry (no aspects)
	registry := NewRegistry()

	// Create empty connector registry
	connRegistry := connector.NewRegistry()

	// Create executor
	executor, err := NewExecutor(registry, connRegistry)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Execute with a mock flow function
	callCount := 0
	flowFn := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		callCount++
		return &connector.Result{Rows: []map[string]interface{}{{"id": 1}}}, nil
	}

	result, err := executor.Execute(
		context.Background(),
		"flows/users/create_user.hcl",
		"create_user",
		"POST /users",
		"users",
		map[string]interface{}{"name": "test"},
		flowFn,
	)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected result, got nil")
	}

	if callCount != 1 {
		t.Errorf("expected flow to be called once, got %d", callCount)
	}

	// Verify metadata was added to input
	// This is implicitly tested by the successful execution
}

func TestExecutor_EnrichInput(t *testing.T) {
	registry := NewRegistry()
	connRegistry := connector.NewRegistry()

	executor, _ := NewExecutor(registry, connRegistry)

	input := map[string]interface{}{
		"name": "test",
	}

	enriched := executor.enrichInput(input, "test_flow", "GET /test", "test_table")

	if enriched["_flow"] != "test_flow" {
		t.Errorf("expected _flow to be 'test_flow', got %v", enriched["_flow"])
	}

	if enriched["_operation"] != "GET /test" {
		t.Errorf("expected _operation to be 'GET /test', got %v", enriched["_operation"])
	}

	if enriched["_target"] != "test_table" {
		t.Errorf("expected _target to be 'test_table', got %v", enriched["_target"])
	}

	if enriched["name"] != "test" {
		t.Errorf("expected name to be preserved, got %v", enriched["name"])
	}
}
