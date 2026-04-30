package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
)

// TestCoordinateSignalEmitResolvesOutputField is the regression test for the
// v1.20.0 bug where `coordinate.signal.emit = "'parent_ready:' + output.sku"`
// was stored verbatim in Redis instead of evaluated as CEL post-success.
// After the fix, the runtime captures the transform output and binds it
// to `output` when evaluating the signal key.
func TestCoordinateSignalEmitResolvesOutputField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dest := httpconn.New("magento", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}
	tr, _ := transform.NewCELTransformer()

	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "emitter",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{
				"sku":  "input.body.payload.styleNumber",
				"name": "input.body.payload.styleName",
			},
		},
		Coordinate: &flow.CoordinateConfig{
			Storage: &flow.SyncStorageConfig{Driver: "memory"},
			Signal: &flow.SignalConfig{
				When: "true",
				Emit: "'parent_ready:' + output.sku",
				TTL:  "1h",
			},
		},
		To: &flow.ToConfig{
			Connector: "magento",
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
		SyncManager: mgr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{
		"body": map[string]interface{}{
			"payload": map[string]interface{}{
				"styleNumber": "AI02LT",
				"styleName":   "Axil",
			},
		},
	}

	if _, err := h.executeFlowCore(context.Background(), input); err != nil {
		t.Fatalf("flow execution: %v", err)
	}

	// Verify the resolved signal exists. We use the same Manager so the
	// memory coordinator is shared.
	coord, err := mgr.GetCoordinator(context.Background(), &msync.SyncStorageConfig{Driver: "memory"})
	if err != nil {
		t.Fatalf("GetCoordinator: %v", err)
	}
	exists, err := coord.Exists(context.Background(), "parent_ready:AI02LT")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Errorf("expected signal 'parent_ready:AI02LT' to exist after emit")
	}

	// Negative: the literal pre-fix key must NOT exist.
	if litExists, _ := coord.Exists(context.Background(), "'parent_ready:' + output.sku"); litExists {
		t.Errorf("regression: literal CEL source string was stored as the signal key")
	}
}

// TestCoordinateSignalEmitFailsCleanlyOnUnknownVar guards against silent
// fallback to the literal expression when CEL evaluation fails. Pre-fix,
// `output.does_not_exist` would fall back to the literal source string.
// Post-fix, the runtime logs a warning and skips emitting.
func TestCoordinateSignalEmitFailsCleanlyOnUnknownVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "broken_emitter",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		Coordinate: &flow.CoordinateConfig{
			Storage: &flow.SyncStorageConfig{Driver: "memory"},
			Signal: &flow.SignalConfig{
				When: "true",
				Emit: "'parent_ready:' + output.does_not_exist",
				TTL:  "1h",
			},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		Transformer: tr,
		SyncManager: mgr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}
	if _, err := h.executeFlowCore(context.Background(), input); err != nil {
		t.Fatalf("flow execution: %v", err)
	}

	coord, _ := mgr.GetCoordinator(context.Background(), &msync.SyncStorageConfig{Driver: "memory"})
	if litExists, _ := coord.Exists(context.Background(), "'parent_ready:' + output.does_not_exist"); litExists {
		t.Errorf("literal expression was stored as a key — silent fallback is the bug we're guarding against")
	}
}

// TestLockReleasedOnFlowFailure: the lock must be released even when the
// inner flow body errors out, so queued workers don't pile up.
func TestLockReleasedOnFlowFailure(t *testing.T) {
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "fails",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Lock: &flow.LockConfig{
			Storage: &flow.SyncStorageConfig{Driver: "memory"},
			Key:     "'sku:X'",
			Timeout: "1s",
			Wait:    true,
		},
		Coordinate: &flow.CoordinateConfig{
			Storage:   &flow.SyncStorageConfig{Driver: "memory"},
			Timeout:   "100ms",
			OnTimeout: "fail",
			Wait: &flow.WaitConfig{
				When: "true",
				For:  "'never'",
			},
		},
		To: &flow.ToConfig{
			Connector:       "noop",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "x", "operation": "POST"},
		},
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Transformer: tr,
		SyncManager: mgr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{}}

	// Run the flow once — we expect coordinate to time out, the lock
	// should still get released.
	_, _ = h.executeFlowCore(context.Background(), input)

	// Try to acquire the same lock from a sibling routine. If the previous
	// run released cleanly, this acquires fast; if it didn't, we time out.
	lock, err := mgr.GetLock(context.Background(), &msync.SyncStorageConfig{Driver: "memory"})
	if err != nil {
		t.Fatalf("GetLock: %v", err)
	}

	done := make(chan bool, 1)
	go func() {
		acquired, _ := lock.Acquire(context.Background(), "sku:X", time.Second)
		done <- acquired
	}()

	select {
	case acquired := <-done:
		if !acquired {
			t.Fatal("could not acquire lock — previous flow's defer never released it")
		}
		_ = lock.Release(context.Background(), "sku:X")
	case <-time.After(2 * time.Second):
		t.Fatal("acquire timed out; lock from previous run was never released")
	}
}

// TestLockReleasedOnSuccess: the lock is also released on the happy path
// (sanity check; this would have always worked, but locks the contract in).
func TestLockReleasedOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "ok",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Lock: &flow.LockConfig{
			Storage: &flow.SyncStorageConfig{Driver: "memory"},
			Key:     "'sku:OK'",
			Timeout: "5s",
			Wait:    true,
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"x": "input.body.x"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		Transformer: tr,
		SyncManager: mgr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"x": "v"}}
	if _, err := h.executeFlowCore(context.Background(), input); err != nil {
		t.Fatalf("flow: %v", err)
	}

	// Acquire from sibling goroutine — should be immediate.
	lock, _ := mgr.GetLock(context.Background(), &msync.SyncStorageConfig{Driver: "memory"})
	var wg sync.WaitGroup
	wg.Add(1)
	got := false
	go func() {
		defer wg.Done()
		acquired, _ := lock.Acquire(context.Background(), "sku:OK", 500*time.Millisecond)
		got = acquired
		if acquired {
			_ = lock.Release(context.Background(), "sku:OK")
		}
	}()
	wg.Wait()
	if !got {
		t.Error("lock not released after successful flow")
	}
}
