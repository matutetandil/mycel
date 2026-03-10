package workflow

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/matutetandil/mycel/internal/saga"
)

func newTestEngine(t *testing.T) (*Engine, *SQLStore) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	store := NewSQLStore(db, DialectSQLite, "mycel_workflows")
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create executor with nil connectors (we test workflow logic, not connector calls)
	executor := saga.NewExecutor(nil)
	engine := NewEngine(store, executor, nil)

	return engine, store
}

func TestEngine_ExecuteSimpleSaga(t *testing.T) {
	engine, store := newTestEngine(t)
	engine.tickInterval = 100 * time.Millisecond
	ctx := context.Background()

	// Register a saga with delay step
	cfg := &saga.Config{
		Name: "test_delay",
		Steps: []*saga.StepConfig{
			{Name: "wait", Delay: "100ms"},
		},
	}
	engine.RegisterSaga(cfg)
	engine.Start(ctx)
	defer engine.Stop()

	// Execute
	inst, err := engine.Execute(ctx, "test_delay", map[string]interface{}{"key": "val"})
	if err != nil {
		t.Fatal(err)
	}

	if inst.ID == "" {
		t.Error("expected non-empty workflow ID")
	}

	// Wait a moment for async execution to save the paused state
	time.Sleep(50 * time.Millisecond)

	// Should be paused (waiting for delay)
	got, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatal("failed to get workflow:", err)
	}
	if got.Status != StatusPaused {
		t.Errorf("expected paused, got %s", got.Status)
	}
	if got.ResumeAt == nil {
		t.Error("expected resume_at to be set")
	}

	// Wait for the ticker to pick it up (delay 100ms + tick 100ms)
	time.Sleep(400 * time.Millisecond)

	// Should now be completed (delay expired, no more steps)
	got, err = store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatal("failed to get workflow:", err)
	}
	if got.Status != StatusCompleted {
		t.Errorf("expected completed after delay, got %s", got.Status)
	}
}

func TestEngine_ExecuteAwaitAndSignal(t *testing.T) {
	engine, store := newTestEngine(t)
	engine.tickInterval = 100 * time.Millisecond
	ctx := context.Background()

	cfg := &saga.Config{
		Name: "test_await",
		Steps: []*saga.StepConfig{
			{Name: "wait_payment", Await: "payment_confirmed"},
		},
	}
	engine.RegisterSaga(cfg)
	engine.Start(ctx)
	defer engine.Stop()

	inst, err := engine.Execute(ctx, "test_await", map[string]interface{}{"order": "123"})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for async execution
	time.Sleep(50 * time.Millisecond)

	// Should be paused awaiting event
	got, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPaused {
		t.Errorf("expected paused, got %s", got.Status)
	}
	if got.AwaitEvent != "payment_confirmed" {
		t.Errorf("expected await_event 'payment_confirmed', got %q", got.AwaitEvent)
	}

	// Signal the workflow
	err = engine.Signal(ctx, inst.ID, "payment_confirmed", map[string]interface{}{"amount": 99.99})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for async execution
	time.Sleep(200 * time.Millisecond)

	got, err = store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted {
		t.Errorf("expected completed after signal, got %s", got.Status)
	}
	if got.SignalData == nil {
		t.Error("expected signal data to be preserved")
	}
}

func TestEngine_SignalWrongEvent(t *testing.T) {
	engine, _ := newTestEngine(t)
	ctx := context.Background()

	cfg := &saga.Config{
		Name: "test_wrong_signal",
		Steps: []*saga.StepConfig{
			{Name: "wait", Await: "event_a"},
		},
	}
	engine.RegisterSaga(cfg)

	inst, _ := engine.Execute(ctx, "test_wrong_signal", map[string]interface{}{})
	time.Sleep(50 * time.Millisecond)

	// Signal with wrong event name
	err := engine.Signal(ctx, inst.ID, "event_b", nil)
	if err == nil {
		t.Error("expected error for wrong event")
	}
}

func TestEngine_Cancel(t *testing.T) {
	engine, store := newTestEngine(t)
	ctx := context.Background()

	cfg := &saga.Config{
		Name: "test_cancel",
		Steps: []*saga.StepConfig{
			{Name: "wait", Await: "never"},
		},
	}
	engine.RegisterSaga(cfg)

	inst, _ := engine.Execute(ctx, "test_cancel", map[string]interface{}{})
	time.Sleep(50 * time.Millisecond)

	err := engine.Cancel(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCancelled {
		t.Errorf("expected cancelled, got %s", got.Status)
	}
}

func TestEngine_SagaTimeout(t *testing.T) {
	engine, store := newTestEngine(t)
	engine.tickInterval = 100 * time.Millisecond
	ctx := context.Background()

	cfg := &saga.Config{
		Name:    "test_timeout",
		Timeout: "200ms",
		Steps: []*saga.StepConfig{
			{Name: "wait", Await: "never_comes"},
		},
	}
	engine.RegisterSaga(cfg)
	engine.Start(ctx)
	defer engine.Stop()

	inst, _ := engine.Execute(ctx, "test_timeout", map[string]interface{}{})

	// Wait for timeout + tick
	time.Sleep(600 * time.Millisecond)

	got, err := store.Get(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusTimeout {
		t.Errorf("expected timeout, got %s", got.Status)
	}
}

func TestEngine_NeedsPersistence(t *testing.T) {
	tests := []struct {
		name   string
		steps  []*saga.StepConfig
		expect bool
	}{
		{"no delay/await", []*saga.StepConfig{{Name: "a", Action: &saga.ActionConfig{}}}, false},
		{"with delay", []*saga.StepConfig{{Name: "a", Delay: "5m"}}, true},
		{"with await", []*saga.StepConfig{{Name: "a", Await: "event"}}, true},
		{"mixed", []*saga.StepConfig{{Name: "a", Action: &saga.ActionConfig{}}, {Name: "b", Delay: "1s"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &saga.Config{Name: "test", Steps: tt.steps}
			got := NeedsPersistence(cfg)
			if got != tt.expect {
				t.Errorf("NeedsPersistence = %v, want %v", got, tt.expect)
			}
		})
	}
}
