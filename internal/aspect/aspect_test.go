package aspect

import (
	"context"
	"fmt"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
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
				On:   []string{"create_*"},
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
				On:   []string{"get_*"},
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
				On:   []string{"*"},
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
				On:   []string{"*"},
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
				On:   []string{"*"},
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
				On:   []string{"*"},
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
		On:   []string{"*"},
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

	// Register aspects with different flow name patterns
	registry.Register(&Config{
		Name: "audit_create",
		On:   []string{"create_*"},
		When: After,
		Action: &ActionConfig{
			Connector: "audit",
		},
	})

	registry.Register(&Config{
		Name: "cache_get",
		On:   []string{"get_*"},
		When: Around,
		Cache: &CacheConfig{
			Storage: "cache",
			TTL:     "5m",
			Key:     "test",
		},
	})

	registry.Register(&Config{
		Name: "log_all",
		On:   []string{"*"},
		When: Before,
		Action: &ActionConfig{
			Connector: "logger",
		},
	})

	tests := []struct {
		flowName      string
		expectedCount int
		expectedNames []string
	}{
		{
			flowName:      "create_user",
			expectedCount: 2, // log_all (before) + audit_create (after)
			expectedNames: []string{"log_all", "audit_create"},
		},
		{
			flowName:      "get_product",
			expectedCount: 2, // log_all (before) + cache_get (around)
			expectedNames: []string{"log_all", "cache_get"},
		},
		{
			flowName:      "list_orders",
			expectedCount: 1, // log_all only
			expectedNames: []string{"log_all"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.flowName, func(t *testing.T) {
			matches := registry.Match(tt.flowName)
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
		On:   []string{"*"},
		When: Before,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	registry.Register(&Config{
		Name: "around_1",
		On:   []string{"*"},
		When: Around,
		Cache: &CacheConfig{
			Storage: "cache",
			TTL:     "5m",
			Key:     "test",
		},
	})

	registry.Register(&Config{
		Name: "after_1",
		On:   []string{"*"},
		When: After,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	before := registry.GetBefore("test_flow")
	if len(before) != 1 || before[0].Name != "before_1" {
		t.Errorf("expected 1 before aspect, got %d", len(before))
	}

	around := registry.GetAround("test_flow")
	if len(around) != 1 || around[0].Name != "around_1" {
		t.Errorf("expected 1 around aspect, got %d", len(around))
	}

	after := registry.GetAfter("test_flow")
	if len(after) != 1 || after[0].Name != "after_1" {
		t.Errorf("expected 1 after aspect, got %d", len(after))
	}
}

func TestRegistry_Priority(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Config{
		Name:     "low_priority",
		On:       []string{"*"},
		When:     Before,
		Priority: 10,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	registry.Register(&Config{
		Name:     "high_priority",
		On:       []string{"*"},
		When:     Before,
		Priority: 1,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	registry.Register(&Config{
		Name:     "medium_priority",
		On:       []string{"*"},
		When:     Before,
		Priority: 5,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	matches := registry.Match("test_flow")

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
		On:   []string{"*"},
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
}

func TestConfig_Validate_OnError(t *testing.T) {
	config := &Config{
		Name: "error_handler",
		On:   []string{"*"},
		When: OnError,
		Action: &ActionConfig{
			Connector: "error_db",
			Target:    "error_logs",
		},
	}

	if err := config.Validate(); err != nil {
		t.Errorf("expected on_error config to be valid, got: %v", err)
	}
}

func TestRegistry_GetOnError(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Config{
		Name: "before_1",
		On:   []string{"*"},
		When: Before,
		Action: &ActionConfig{
			Connector: "db",
		},
	})

	registry.Register(&Config{
		Name: "on_error_1",
		On:   []string{"*"},
		When: OnError,
		Action: &ActionConfig{
			Connector: "error_db",
			Target:    "errors",
		},
	})

	registry.Register(&Config{
		Name: "on_error_2",
		On:   []string{"create_*"},
		When: OnError,
		Action: &ActionConfig{
			Connector: "slack",
			Target:    "alerts",
		},
	})

	// Should return both on_error aspects for matching flow
	onError := registry.GetOnError("create_user")
	if len(onError) != 2 {
		t.Errorf("expected 2 on_error aspects, got %d", len(onError))
	}

	// Should return only 1 for non-create flow
	onError = registry.GetOnError("list_users")
	if len(onError) != 1 {
		t.Errorf("expected 1 on_error aspect, got %d", len(onError))
	}

	// Before should still work normally
	before := registry.GetBefore("create_user")
	if len(before) != 1 {
		t.Errorf("expected 1 before aspect, got %d", len(before))
	}
}

func TestExecutor_OnError_ExecutedOnFlowFailure(t *testing.T) {
	registry := NewRegistry()

	// Register an on_error aspect
	registry.Register(&Config{
		Name: "error_logger",
		On:   []string{"*"},
		When: OnError,
		Action: &ActionConfig{
			Connector: "error_db",
			Target:    "error_logs",
		},
	})

	connRegistry := connector.NewRegistry()
	executor, err := NewExecutor(registry, connRegistry)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Flow that fails
	flowFn := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		return nil, fmt.Errorf("database connection failed")
	}

	// Should still return the flow error (on_error aspects don't swallow errors)
	_, flowErr := executor.Execute(
		context.Background(),
		"create_user",
		"POST /users",
		"users",
		map[string]interface{}{"name": "test"},
		flowFn,
	)

	if flowErr == nil {
		t.Error("expected flow error to be preserved")
	}
	if flowErr.Error() != "database connection failed" {
		t.Errorf("expected original error, got: %v", flowErr)
	}
}

func TestExecutor_OnError_NotExecutedOnSuccess(t *testing.T) {
	registry := NewRegistry()
	connRegistry := connector.NewRegistry()

	// Register on_error aspect - should NOT fire on success
	registry.Register(&Config{
		Name: "error_logger",
		On:   []string{"*"},
		When: OnError,
		Action: &ActionConfig{
			Connector: "error_db",
			Target:    "error_logs",
		},
	})

	executor, _ := NewExecutor(registry, connRegistry)

	// Successful flow
	flowFn := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		return &connector.Result{Rows: []map[string]interface{}{{"id": 1}}}, nil
	}

	result, err := executor.Execute(
		context.Background(),
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

func TestBuildErrorInfo(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode int64
		expectedType string
	}{
		{
			name:         "HTTP 404 error",
			err:          &httpconn.HTTPError{StatusCode: 404, Status: "Not Found", Body: "user not found"},
			expectedCode: 404,
			expectedType: "http",
		},
		{
			name:         "HTTP 500 error",
			err:          &httpconn.HTTPError{StatusCode: 500, Status: "Internal Server Error", Body: "db error"},
			expectedCode: 500,
			expectedType: "http",
		},
		{
			name:         "FlowError with status",
			err:          flow.NewFlowError(fmt.Errorf("bad request"), 422, nil, nil),
			expectedCode: 422,
			expectedType: "flow",
		},
		{
			name:         "not found heuristic",
			err:          fmt.Errorf("resource not found"),
			expectedCode: 404,
			expectedType: "not_found",
		},
		{
			name:         "timeout heuristic",
			err:          fmt.Errorf("operation timed out"),
			expectedCode: 504,
			expectedType: "timeout",
		},
		{
			name:         "connection refused heuristic",
			err:          fmt.Errorf("connection refused"),
			expectedCode: 503,
			expectedType: "connection",
		},
		{
			name:         "unknown error",
			err:          fmt.Errorf("something went wrong"),
			expectedCode: 0,
			expectedType: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := buildErrorInfo(tt.err)

			code, ok := info["code"].(int64)
			if !ok {
				t.Fatalf("expected code to be int64, got %T", info["code"])
			}
			if code != tt.expectedCode {
				t.Errorf("expected code %d, got %d", tt.expectedCode, code)
			}

			errType, ok := info["type"].(string)
			if !ok {
				t.Fatalf("expected type to be string, got %T", info["type"])
			}
			if errType != tt.expectedType {
				t.Errorf("expected type %q, got %q", tt.expectedType, errType)
			}

			msg, ok := info["message"].(string)
			if !ok {
				t.Fatalf("expected message to be string, got %T", info["message"])
			}
			if msg == "" {
				t.Error("expected non-empty message")
			}
		})
	}
}

// mockFlowInvoker implements FlowInvoker for testing.
type mockFlowInvoker struct {
	invocations []struct {
		FlowName string
		Input    map[string]interface{}
	}
	err error
}

func (m *mockFlowInvoker) InvokeFlow(ctx context.Context, flowName string, input map[string]interface{}) (interface{}, error) {
	m.invocations = append(m.invocations, struct {
		FlowName string
		Input    map[string]interface{}
	}{FlowName: flowName, Input: input})
	if m.err != nil {
		return nil, m.err
	}
	return map[string]interface{}{"ok": true}, nil
}

func TestExecutor_FlowAction(t *testing.T) {
	registry := NewRegistry()

	// Register an after aspect that invokes a flow
	registry.Register(&Config{
		Name: "notify_flow",
		On:   []string{"create_*"},
		When: After,
		Action: &ActionConfig{
			Flow: "send_notification",
			Transform: map[string]string{
				"message": "'User created: ' + input.name",
			},
		},
	})

	connRegistry := connector.NewRegistry()
	executor, err := NewExecutor(registry, connRegistry)
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Set up mock flow invoker
	invoker := &mockFlowInvoker{}
	executor.SetFlowInvoker(invoker)

	// Execute flow
	flowFn := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		return &connector.Result{Rows: []map[string]interface{}{{"id": 1}}}, nil
	}

	_, err = executor.Execute(
		context.Background(),
		"create_user",
		"POST /users",
		"users",
		map[string]interface{}{"name": "Alice"},
		flowFn,
	)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify flow was invoked
	if len(invoker.invocations) != 1 {
		t.Fatalf("expected 1 flow invocation, got %d", len(invoker.invocations))
	}
	if invoker.invocations[0].FlowName != "send_notification" {
		t.Errorf("expected flow 'send_notification', got %q", invoker.invocations[0].FlowName)
	}
	msg, ok := invoker.invocations[0].Input["message"].(string)
	if !ok || msg != "User created: Alice" {
		t.Errorf("expected transformed message, got %v", invoker.invocations[0].Input["message"])
	}
}

func TestExecutor_FlowAction_ErrorIsSoftFailure(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Config{
		Name: "broken_flow",
		On:   []string{"*"},
		When: After,
		Action: &ActionConfig{
			Flow: "failing_flow",
		},
	})

	connRegistry := connector.NewRegistry()
	executor, _ := NewExecutor(registry, connRegistry)

	// Flow invoker that returns an error
	invoker := &mockFlowInvoker{err: fmt.Errorf("flow failed")}
	executor.SetFlowInvoker(invoker)

	flowFn := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		return &connector.Result{Rows: []map[string]interface{}{{"id": 1}}}, nil
	}

	// Main flow should still succeed (after aspect errors are soft failures)
	result, err := executor.Execute(
		context.Background(),
		"create_user",
		"POST /users",
		"users",
		map[string]interface{}{"name": "test"},
		flowFn,
	)
	if err != nil {
		t.Errorf("expected main flow to succeed despite aspect flow error, got: %v", err)
	}
	if result == nil {
		t.Error("expected result, got nil")
	}
}

func TestExecutor_FlowAction_OnError(t *testing.T) {
	registry := NewRegistry()

	// Register on_error aspect that invokes a flow (no transform — just invoke)
	registry.Register(&Config{
		Name: "error_handler_flow",
		On:   []string{"*"},
		When: OnError,
		Action: &ActionConfig{
			Flow: "handle_error",
			Transform: map[string]string{
				"flow_name": "_flow",
			},
		},
	})

	connRegistry := connector.NewRegistry()
	executor, _ := NewExecutor(registry, connRegistry)

	invoker := &mockFlowInvoker{}
	executor.SetFlowInvoker(invoker)

	// Flow that fails
	flowFn := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		return nil, fmt.Errorf("database connection refused")
	}

	_, flowErr := executor.Execute(
		context.Background(),
		"create_user",
		"POST /users",
		"users",
		map[string]interface{}{"name": "test"},
		flowFn,
	)

	// Original error should still be returned
	if flowErr == nil {
		t.Error("expected flow error to be preserved")
	}

	// Verify the error handler flow was invoked
	if len(invoker.invocations) != 1 {
		t.Fatalf("expected 1 flow invocation, got %d", len(invoker.invocations))
	}
	if invoker.invocations[0].FlowName != "handle_error" {
		t.Errorf("expected flow 'handle_error', got %q", invoker.invocations[0].FlowName)
	}
	if invoker.invocations[0].Input["flow_name"] != "create_user" {
		t.Errorf("expected flow_name 'create_user', got %v", invoker.invocations[0].Input["flow_name"])
	}
}

func TestRegistry_MatchFlowNamePatterns(t *testing.T) {
	registry := NewRegistry()

	registry.Register(&Config{
		Name: "write_audit",
		On:   []string{"create_*", "update_*", "delete_*"},
		When: After,
		Action: &ActionConfig{
			Connector: "audit",
		},
	})

	registry.Register(&Config{
		Name: "read_cache",
		On:   []string{"get_*", "list_*"},
		When: Around,
		Cache: &CacheConfig{
			Storage: "cache",
			TTL:     "5m",
			Key:     "test",
		},
	})

	tests := []struct {
		flowName      string
		expectedCount int
		expectedNames []string
	}{
		{"create_user", 1, []string{"write_audit"}},
		{"update_order", 1, []string{"write_audit"}},
		{"delete_product", 1, []string{"write_audit"}},
		{"get_user", 1, []string{"read_cache"}},
		{"list_products", 1, []string{"read_cache"}},
		{"health_check", 0, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.flowName, func(t *testing.T) {
			matches := registry.Match(tt.flowName)
			if len(matches) != tt.expectedCount {
				t.Errorf("expected %d matches for %s, got %d", tt.expectedCount, tt.flowName, len(matches))
			}
			for i, name := range tt.expectedNames {
				if i < len(matches) && matches[i].Name != name {
					t.Errorf("expected aspect %s at position %d, got %s", name, i, matches[i].Name)
				}
			}
		})
	}
}
