package workflow

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *SQLStore {
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
	return store
}

func TestSQLStore_SaveAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	inst := &Instance{
		ID:          "wf_1",
		SagaName:    "order_create",
		Status:      StatusRunning,
		CurrentStep: 0,
		Input:       map[string]interface{}{"order_id": "123"},
		StepResults: map[string]interface{}{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.Save(ctx, inst); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, "wf_1")
	if err != nil {
		t.Fatal(err)
	}

	if got.SagaName != "order_create" {
		t.Errorf("expected saga_name 'order_create', got %q", got.SagaName)
	}
	if got.Status != StatusRunning {
		t.Errorf("expected status 'running', got %q", got.Status)
	}
	if got.Input["order_id"] != "123" {
		t.Errorf("expected order_id '123', got %v", got.Input["order_id"])
	}
}

func TestSQLStore_Update(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	inst := &Instance{
		ID:          "wf_2",
		SagaName:    "test",
		Status:      StatusRunning,
		CurrentStep: 0,
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	store.Save(ctx, inst)

	// Update
	inst.Status = StatusCompleted
	inst.CurrentStep = 3
	inst.StepResults["step1"] = "done"
	store.Save(ctx, inst)

	got, _ := store.Get(ctx, "wf_2")
	if got.Status != StatusCompleted {
		t.Errorf("expected completed, got %q", got.Status)
	}
	if got.CurrentStep != 3 {
		t.Errorf("expected step 3, got %d", got.CurrentStep)
	}
}

func TestSQLStore_FindActive(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Create instances with different statuses
	for _, s := range []struct {
		id     string
		status Status
	}{
		{"wf_run", StatusRunning},
		{"wf_pause", StatusPaused},
		{"wf_done", StatusCompleted},
		{"wf_fail", StatusFailed},
	} {
		store.Save(ctx, &Instance{
			ID: s.id, SagaName: "test", Status: s.status,
			Input: map[string]interface{}{}, StepResults: map[string]interface{}{},
			CreatedAt: now, UpdatedAt: now,
		})
	}

	active, err := store.FindActive(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(active) != 2 {
		t.Errorf("expected 2 active instances, got %d", len(active))
	}
}

func TestSQLStore_FindReady(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	past := now.Add(-1 * time.Minute)
	future := now.Add(10 * time.Minute)

	// Ready (resume_at in the past)
	store.Save(ctx, &Instance{
		ID: "wf_ready", SagaName: "test", Status: StatusPaused,
		ResumeAt:    &past,
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now, UpdatedAt: now,
	})

	// Not ready (resume_at in the future)
	store.Save(ctx, &Instance{
		ID: "wf_waiting", SagaName: "test", Status: StatusPaused,
		ResumeAt:    &future,
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now, UpdatedAt: now,
	})

	ready, err := store.FindReady(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(ready) != 1 {
		t.Errorf("expected 1 ready instance, got %d", len(ready))
	}
	if len(ready) > 0 && ready[0].ID != "wf_ready" {
		t.Errorf("expected wf_ready, got %s", ready[0].ID)
	}
}

func TestSQLStore_FindByEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	store.Save(ctx, &Instance{
		ID: "wf_await", SagaName: "test", Status: StatusPaused,
		AwaitEvent:  "payment_confirmed",
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now, UpdatedAt: now,
	})

	store.Save(ctx, &Instance{
		ID: "wf_other", SagaName: "test", Status: StatusPaused,
		AwaitEvent:  "shipping_ready",
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now, UpdatedAt: now,
	})

	found, err := store.FindByEvent(ctx, "payment_confirmed")
	if err != nil {
		t.Fatal(err)
	}

	if len(found) != 1 {
		t.Errorf("expected 1 instance, got %d", len(found))
	}
	if len(found) > 0 && found[0].ID != "wf_await" {
		t.Errorf("expected wf_await, got %s", found[0].ID)
	}
}

func TestSQLStore_FindExpired(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	past := now.Add(-1 * time.Minute)

	store.Save(ctx, &Instance{
		ID: "wf_expired", SagaName: "test", Status: StatusRunning,
		ExpiresAt:   &past,
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now, UpdatedAt: now,
	})

	store.Save(ctx, &Instance{
		ID: "wf_ok", SagaName: "test", Status: StatusRunning,
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now, UpdatedAt: now,
	})

	expired, err := store.FindExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(expired) != 1 {
		t.Errorf("expected 1 expired instance, got %d", len(expired))
	}
}

func TestSQLStore_Delete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	store.Save(ctx, &Instance{
		ID: "wf_del", SagaName: "test", Status: StatusCompleted,
		Input:       map[string]interface{}{},
		StepResults: map[string]interface{}{},
		CreatedAt:   now, UpdatedAt: now,
	})

	if err := store.Delete(ctx, "wf_del"); err != nil {
		t.Fatal(err)
	}

	_, err := store.Get(ctx, "wf_del")
	if err == nil {
		t.Error("expected error after deletion")
	}
}
