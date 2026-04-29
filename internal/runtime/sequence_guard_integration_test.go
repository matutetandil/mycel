package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
)

// newGuardedHandler builds a minimal FlowHandler with sequence_guard, lock,
// and a real HTTP destination wired up. Returns the handler plus a counter
// of how many times the destination was hit (so tests can assert dedup).
func newGuardedHandler(t *testing.T) (*FlowHandler, *int, func()) {
	t.Helper()

	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}

	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	cfg := &flow.Config{
		Name: "guarded",
		From: &flow.FromConfig{
			Connector:       "rabbit",
			ConnectorParams: map[string]interface{}{"target": "q"},
		},
		Lock: &flow.LockConfig{
			Storage: &flow.SyncStorageConfig{Driver: "memory"},
			Key:     "'sku:' + input.body.payload.sku",
			Timeout: "5s",
			Wait:    true,
		},
		SequenceGuard: &flow.SequenceGuardConfig{
			Storage:  &flow.SyncStorageConfig{Driver: "memory"},
			Key:      "'sku:' + input.body.payload.sku",
			Sequence: "input.body.payload.jobId",
			OnOlder:  "ack",
			TTL:      "1h",
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{
				"sku": "input.body.payload.sku",
				"jobId": "input.body.payload.jobId",
			},
		},
		To: &flow.ToConfig{
			Connector: "api",
			Parallel:  true,
			ConnectorParams: map[string]interface{}{
				"target":    "/post",
				"operation": "POST",
			},
		},
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		Transformer: tr,
		SyncManager: msync.NewManager(),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	cleanup := func() {
		srv.Close()
		_ = h.SyncManager.Close()
	}
	return h, &hits, cleanup
}

func msg(sku string, jobID int) map[string]interface{} {
	return map[string]interface{}{
		"body": map[string]interface{}{
			"payload": map[string]interface{}{
				"sku":   sku,
				"jobId": jobID,
			},
		},
	}
}

// TestSequenceGuard_FirstMessagePassesAndBumps: the first message for a key
// has no stored sequence; should execute and bump the store.
func TestSequenceGuard_FirstMessagePassesAndBumps(t *testing.T) {
	h, hits, done := newGuardedHandler(t)
	defer done()

	result, err := h.executeFlowCore(context.Background(), msg("AI02LT", 100))
	if err != nil {
		t.Fatalf("first msg: %v", err)
	}
	if _, isFiltered := result.(*flow.FilteredResultWithPolicy); isFiltered {
		t.Fatalf("first msg should not be filtered, got: %+v", result)
	}
	if *hits != 1 {
		t.Errorf("expected 1 destination hit, got %d", *hits)
	}
}

// TestSequenceGuard_OlderMessageRejected: a second message with smaller jobId
// for the same key should be rejected without hitting the destination.
func TestSequenceGuard_OlderMessageRejected(t *testing.T) {
	h, hits, done := newGuardedHandler(t)
	defer done()

	if _, err := h.executeFlowCore(context.Background(), msg("AI02LT", 100)); err != nil {
		t.Fatalf("first msg: %v", err)
	}

	result, err := h.executeFlowCore(context.Background(), msg("AI02LT", 50))
	if err != nil {
		t.Fatalf("older msg: %v", err)
	}
	filtered, ok := result.(*flow.FilteredResultWithPolicy)
	if !ok {
		t.Fatalf("expected filtered result for older message, got: %+v", result)
	}
	if filtered.Policy != "ack" {
		t.Errorf("expected policy=ack, got %q", filtered.Policy)
	}
	if *hits != 1 {
		t.Errorf("destination should not be hit a second time; got %d hits", *hits)
	}
}

// TestSequenceGuard_EqualMessageRejected: same jobId is also rejected
// (strict-greater semantics — already processed).
func TestSequenceGuard_EqualMessageRejected(t *testing.T) {
	h, hits, done := newGuardedHandler(t)
	defer done()

	if _, err := h.executeFlowCore(context.Background(), msg("AI02LT", 100)); err != nil {
		t.Fatalf("first msg: %v", err)
	}

	result, err := h.executeFlowCore(context.Background(), msg("AI02LT", 100))
	if err != nil {
		t.Fatalf("equal msg: %v", err)
	}
	if _, isFiltered := result.(*flow.FilteredResultWithPolicy); !isFiltered {
		t.Fatalf("equal jobId should be rejected (strict-greater), got: %+v", result)
	}
	if *hits != 1 {
		t.Errorf("destination should not be hit a second time; got %d hits", *hits)
	}
}

// TestSequenceGuard_NewerMessagePasses: a higher jobId after the first
// passes through and bumps the store.
func TestSequenceGuard_NewerMessagePasses(t *testing.T) {
	h, hits, done := newGuardedHandler(t)
	defer done()

	if _, err := h.executeFlowCore(context.Background(), msg("AI02LT", 100)); err != nil {
		t.Fatalf("first msg: %v", err)
	}
	if _, err := h.executeFlowCore(context.Background(), msg("AI02LT", 200)); err != nil {
		t.Fatalf("second msg: %v", err)
	}
	// Verify bump took effect: a third message in between should now be rejected.
	result, _ := h.executeFlowCore(context.Background(), msg("AI02LT", 150))
	if _, isFiltered := result.(*flow.FilteredResultWithPolicy); !isFiltered {
		t.Fatal("after bump to 200, jobId=150 should be rejected")
	}
	if *hits != 2 {
		t.Errorf("expected 2 destination hits (100 + 200), got %d", *hits)
	}
}

// TestSequenceGuard_DistinctKeysIndependent: messages for different SKUs do
// not interfere with each other.
func TestSequenceGuard_DistinctKeysIndependent(t *testing.T) {
	h, hits, done := newGuardedHandler(t)
	defer done()

	if _, err := h.executeFlowCore(context.Background(), msg("A", 50)); err != nil {
		t.Fatalf("A: %v", err)
	}
	if _, err := h.executeFlowCore(context.Background(), msg("B", 25)); err != nil {
		t.Fatalf("B: %v", err)
	}
	// B's lower jobId is fine because it's a different key.
	if *hits != 2 {
		t.Errorf("expected 2 destination hits, got %d", *hits)
	}
}

// TestSequenceGuard_FailureDoesNotBumpStore: when the destination fails, the
// stored sequence is not updated, so a retry of the same message can succeed.
func TestSequenceGuard_FailureDoesNotBumpStore(t *testing.T) {
	var hits int
	var mu sync.Mutex
	failNext := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		shouldFail := failNext
		failNext = false
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}
	tr, _ := transform.NewCELTransformer()

	cfg := &flow.Config{
		Name: "guarded_retry",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		SequenceGuard: &flow.SequenceGuardConfig{
			Storage:  &flow.SyncStorageConfig{Driver: "memory"},
			Key:      "'sku:' + input.body.payload.sku",
			Sequence: "input.body.payload.jobId",
			OnOlder:  "ack",
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.payload.sku"},
		},
		To: &flow.ToConfig{
			Connector: "api",
			Parallel:  true,
			ConnectorParams: map[string]interface{}{
				"target":    "/post",
				"operation": "POST",
			},
		},
	}
	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		Transformer: tr,
		SyncManager: msync.NewManager(),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	defer h.SyncManager.Close()

	// First attempt fails (HTTP 500) — store should NOT be bumped.
	_, err := h.executeFlowCore(context.Background(), msg("AI02LT", 100))
	if err == nil {
		t.Fatal("expected error from failing destination")
	}

	// Retry the same jobId — should pass through (store wasn't bumped).
	_, err = h.executeFlowCore(context.Background(), msg("AI02LT", 100))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if hits != 2 {
		t.Errorf("expected 2 destination hits (failed + retry), got %d", hits)
	}
}
