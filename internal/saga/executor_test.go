package saga

import (
	"context"
	"fmt"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
)

// mockConnector implements connector.ReadWriter and Caller for saga tests.
type mockConnector struct {
	name       string
	writes     []*connector.Data
	readResult *connector.Result
	readErr    error
	writeErr   error
	callResult interface{}
	callErr    error
	callOps    []string
	failAt     int // fail write at this index (-1 = never)
	writeCount int
}

func (m *mockConnector) Name() string                      { return m.name }
func (m *mockConnector) Type() string                      { return "mock" }
func (m *mockConnector) Connect(ctx context.Context) error { return nil }
func (m *mockConnector) Close(ctx context.Context) error   { return nil }
func (m *mockConnector) Health(ctx context.Context) error  { return nil }

func (m *mockConnector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	if m.readResult != nil {
		return m.readResult, nil
	}
	return &connector.Result{}, nil
}

func (m *mockConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	if m.failAt >= 0 && m.writeCount == m.failAt {
		m.writeCount++
		return nil, fmt.Errorf("simulated write error at index %d", m.failAt)
	}
	if m.writeErr != nil {
		return nil, m.writeErr
	}
	m.writes = append(m.writes, data)
	m.writeCount++
	return &connector.Result{
		Affected: 1,
		LastID:   int64(m.writeCount),
		Rows: []map[string]interface{}{
			{"id": int64(m.writeCount)},
		},
	}, nil
}

func (m *mockConnector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	m.callOps = append(m.callOps, operation)
	if m.callErr != nil {
		return nil, m.callErr
	}
	if m.callResult != nil {
		return m.callResult, nil
	}
	return map[string]interface{}{"ok": true}, nil
}

// mockRegistry implements ConnectorGetter.
type mockRegistry struct {
	connectors map[string]connector.Connector
}

func (r *mockRegistry) Get(name string) (connector.Connector, error) {
	c, ok := r.connectors[name]
	if !ok {
		return nil, fmt.Errorf("connector %q not found", name)
	}
	return c, nil
}

func TestExecutor_AllStepsSucceed(t *testing.T) {
	db := &mockConnector{name: "orders_db", failAt: -1}
	api := &mockConnector{name: "stripe", failAt: -1, callResult: map[string]interface{}{"charge_id": "ch_123"}}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"orders_db": db,
		"stripe":    api,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "create_order",
		Steps: []*StepConfig{
			{
				Name: "order",
				Action: &ActionConfig{
					Connector: "orders_db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "pending"},
				},
				Compensate: &ActionConfig{
					Connector: "orders_db",
					Operation: "DELETE",
					Target:    "orders",
				},
			},
			{
				Name: "payment",
				Action: &ActionConfig{
					Connector: "stripe",
					Operation: "POST /charges",
					Body:      map[string]interface{}{"amount": 100},
				},
				Compensate: &ActionConfig{
					Connector: "stripe",
					Operation: "POST /refunds",
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected status completed, got %s", result.Status)
	}
	if len(db.writes) != 1 {
		t.Errorf("expected 1 write to db, got %d", len(db.writes))
	}
	if len(api.callOps) != 1 {
		t.Errorf("expected 1 call to api, got %d", len(api.callOps))
	}
}

func TestExecutor_StepFailsWithCompensation(t *testing.T) {
	db := &mockConnector{name: "orders_db", failAt: -1}
	api := &mockConnector{name: "stripe", failAt: -1, callErr: fmt.Errorf("payment declined")}
	notifications := &mockConnector{name: "notifications", failAt: -1}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"orders_db":     db,
		"stripe":        api,
		"notifications": notifications,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "create_order",
		Steps: []*StepConfig{
			{
				Name: "order",
				Action: &ActionConfig{
					Connector: "orders_db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "pending"},
				},
				Compensate: &ActionConfig{
					Connector: "orders_db",
					Operation: "DELETE",
					Target:    "orders",
				},
			},
			{
				Name: "payment",
				Action: &ActionConfig{
					Connector: "stripe",
					Operation: "POST /charges",
					Body:      map[string]interface{}{"amount": 100},
				},
			},
		},
		OnFailure: &ActionConfig{
			Connector: "notifications",
			Operation: "POST /send",
			Body:      map[string]interface{}{"template": "order_failed"},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "compensated" {
		t.Errorf("expected status compensated, got %s", result.Status)
	}
	// DB should have 2 writes: the original INSERT + the compensating DELETE
	if len(db.writes) != 2 {
		t.Errorf("expected 2 writes to db (action + compensate), got %d", len(db.writes))
	}
	// On failure notification should have been called
	if len(notifications.callOps) != 1 {
		t.Errorf("expected 1 call to notifications, got %d", len(notifications.callOps))
	}
}

func TestExecutor_CompensationFailure(t *testing.T) {
	api := &mockConnector{name: "stripe", failAt: -1, callErr: fmt.Errorf("declined")}

	// Make the compensate fail (first write = action INSERT succeeds, second write = compensate DELETE fails)
	compensateDB := &mockConnector{name: "orders_db", failAt: 1, writeErr: nil}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"orders_db": compensateDB,
		"stripe":    api,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "create_order",
		Steps: []*StepConfig{
			{
				Name: "order",
				Action: &ActionConfig{
					Connector: "orders_db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "pending"},
				},
				Compensate: &ActionConfig{
					Connector: "orders_db",
					Operation: "DELETE",
					Target:    "orders",
				},
			},
			{
				Name: "payment",
				Action: &ActionConfig{
					Connector: "stripe",
					Operation: "POST /charges",
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status failed (compensation also failed), got %s", result.Status)
	}
	if len(result.CompErrors) == 0 {
		t.Error("expected compensation errors")
	}
}

func TestExecutor_OnComplete(t *testing.T) {
	db := &mockConnector{name: "orders_db", failAt: -1}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"orders_db": db,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "simple_saga",
		Steps: []*StepConfig{
			{
				Name: "create",
				Action: &ActionConfig{
					Connector: "orders_db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "pending"},
				},
			},
		},
		OnComplete: &ActionConfig{
			Connector: "orders_db",
			Operation: "UPDATE",
			Target:    "orders",
			Set:       map[string]interface{}{"status": "confirmed"},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected status completed, got %s", result.Status)
	}
	// 2 writes: INSERT + UPDATE (on_complete)
	if len(db.writes) != 2 {
		t.Errorf("expected 2 writes (action + on_complete), got %d", len(db.writes))
	}
}

func TestExecutor_SkipOnError(t *testing.T) {
	db := &mockConnector{name: "orders_db", failAt: -1}
	api := &mockConnector{name: "optional_api", failAt: -1, callErr: fmt.Errorf("unavailable")}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"orders_db":    db,
		"optional_api": api,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "with_skip",
		Steps: []*StepConfig{
			{
				Name:    "optional",
				OnError: "skip",
				Action: &ActionConfig{
					Connector: "optional_api",
					Operation: "POST /enrich",
				},
			},
			{
				Name: "create",
				Action: &ActionConfig{
					Connector: "orders_db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "created"},
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed (skipped failed step), got %s", result.Status)
	}
	if result.Steps["optional"] != nil {
		t.Error("expected nil for skipped step")
	}
}

func TestExecutor_ConnectorNotFound(t *testing.T) {
	reg := &mockRegistry{connectors: map[string]connector.Connector{}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "missing_connector",
		Steps: []*StepConfig{
			{
				Name: "step1",
				Action: &ActionConfig{
					Connector: "nonexistent",
					Operation: "INSERT",
					Target:    "table",
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "compensated" && result.Status != "failed" {
		t.Errorf("expected failed/compensated status, got %s", result.Status)
	}
}

func TestExecutor_MultipleStepsCompensateInReverse(t *testing.T) {
	step1 := &mockConnector{name: "svc1", failAt: -1}
	step2 := &mockConnector{name: "svc2", failAt: -1}
	step3 := &mockConnector{name: "svc3", failAt: -1, callErr: fmt.Errorf("step3 failed")}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"svc1": step1,
		"svc2": step2,
		"svc3": step3,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "three_steps",
		Steps: []*StepConfig{
			{
				Name: "s1",
				Action: &ActionConfig{
					Connector: "svc1",
					Operation: "INSERT",
					Target:    "t1",
					Data:      map[string]interface{}{"v": "a"},
				},
				Compensate: &ActionConfig{
					Connector: "svc1",
					Operation: "DELETE",
					Target:    "t1",
				},
			},
			{
				Name: "s2",
				Action: &ActionConfig{
					Connector: "svc2",
					Operation: "INSERT",
					Target:    "t2",
					Data:      map[string]interface{}{"v": "b"},
				},
				Compensate: &ActionConfig{
					Connector: "svc2",
					Operation: "DELETE",
					Target:    "t2",
				},
			},
			{
				Name: "s3",
				Action: &ActionConfig{
					Connector: "svc3",
					Operation: "POST /fail",
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "compensated" {
		t.Errorf("expected compensated, got %s", result.Status)
	}
	// svc2 should be compensated (DELETE), svc1 should be compensated (DELETE)
	// svc2 had 1 action write + 1 compensate write = 2
	if len(step2.writes) != 2 {
		t.Errorf("expected svc2 to have 2 writes (action+compensate), got %d", len(step2.writes))
	}
	if len(step1.writes) != 2 {
		t.Errorf("expected svc1 to have 2 writes (action+compensate), got %d", len(step1.writes))
	}
}

func TestExecutor_FirstStepFails_NoCompensation(t *testing.T) {
	db := &mockConnector{name: "db", failAt: 0}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"db": db,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "first_fails",
		Steps: []*StepConfig{
			{
				Name: "create",
				Action: &ActionConfig{
					Connector: "db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "pending"},
				},
				Compensate: &ActionConfig{
					Connector: "db",
					Operation: "DELETE",
					Target:    "orders",
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First step failed, no previous steps to compensate
	if result.Status != "compensated" {
		t.Errorf("expected compensated (nothing to undo), got %s", result.Status)
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestExecutor_EmptyInput(t *testing.T) {
	db := &mockConnector{name: "db", failAt: -1}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"db": db,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "simple",
		Steps: []*StepConfig{
			{
				Name: "create",
				Action: &ActionConfig{
					Connector: "db",
					Operation: "INSERT",
					Target:    "items",
					Data:      map[string]interface{}{"name": "test"},
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s", result.Status)
	}
}

func TestExecutor_StepResultsAvailable(t *testing.T) {
	db := &mockConnector{name: "db", failAt: -1}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"db": db,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "with_results",
		Steps: []*StepConfig{
			{
				Name: "create",
				Action: &ActionConfig{
					Connector: "db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "pending"},
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Steps["create"] == nil {
		t.Error("expected step result for 'create'")
	}
}

func TestExecutor_NoCompensateBlock(t *testing.T) {
	db := &mockConnector{name: "db", failAt: -1}
	api := &mockConnector{name: "api", failAt: -1, callErr: fmt.Errorf("api down")}

	reg := &mockRegistry{connectors: map[string]connector.Connector{
		"db":  db,
		"api": api,
	}}

	exec := NewExecutor(reg)
	cfg := &Config{
		Name: "no_compensate",
		Steps: []*StepConfig{
			{
				Name: "create",
				Action: &ActionConfig{
					Connector: "db",
					Operation: "INSERT",
					Target:    "orders",
					Data:      map[string]interface{}{"status": "pending"},
				},
				// No Compensate block - should be skipped during compensation
			},
			{
				Name: "notify",
				Action: &ActionConfig{
					Connector: "api",
					Operation: "POST /notify",
				},
			},
		},
	}

	result, err := exec.Execute(context.Background(), cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "compensated" {
		t.Errorf("expected compensated, got %s", result.Status)
	}
	// Only 1 write to db (the action), no compensate
	if len(db.writes) != 1 {
		t.Errorf("expected 1 write (no compensate block), got %d", len(db.writes))
	}
}
