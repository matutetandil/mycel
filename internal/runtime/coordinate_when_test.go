package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
)

// newCoordinateHandler constructs a flow with a coordinate.wait against
// memory-backed storage. The wait would block for `timeout` if it fires.
func newCoordinateHandler(t *testing.T, when, forKey string, timeout string) (*FlowHandler, func()) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	if err := dest.Connect(context.Background()); err != nil {
		t.Fatalf("dest.Connect: %v", err)
	}

	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()

	cfg := &flow.Config{
		Name: "guarded_wait",
		From: &flow.FromConfig{
			Connector:       "rabbit",
			ConnectorParams: map[string]interface{}{"target": "q"},
		},
		Coordinate: &flow.CoordinateConfig{
			Storage:   &flow.SyncStorageConfig{Driver: "memory"},
			Timeout:   timeout,
			OnTimeout: "fail",
			Wait: &flow.WaitConfig{
				When: when,
				For:  forKey,
			},
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
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

	return h, func() {
		srv.Close()
		_ = mgr.Close()
	}
}

// TestCoordinateWaitWhenFalse_SkipsWaitFastPath: with when=false the
// wait must be a no-op and the flow must complete in well under the
// configured timeout. Pre-fix the When attribute was captured by the
// parser but never consulted at runtime, so the wait fired
// unconditionally and the flow blocked for the full coordinate.timeout.
func TestCoordinateWaitWhenFalse_SkipsWaitFastPath(t *testing.T) {
	h, done := newCoordinateHandler(t, "false", "'never_emitted'", "5s")
	defer done()

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}

	start := time.Now()
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 1*time.Second {
		t.Errorf("with when=false the wait must be skipped — flow took %s, expected sub-second", elapsed)
	}
}

// TestCoordinateWaitWhenTrue_ActuallyWaits: with when=true the wait
// fires; configured timeout is short so the flow fails. This is the
// regression guard that proves we didn't unconditionally short-circuit.
func TestCoordinateWaitWhenTrue_ActuallyWaits(t *testing.T) {
	h, done := newCoordinateHandler(t, "true", "'never_emitted'", "100ms")
	defer done()

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X"}}

	start := time.Now()
	_, err := h.HandleRequest(context.Background(), input)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from coordinate wait")
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("wait must actually block for the timeout window, took only %s", elapsed)
	}
}

// TestCoordinateWaitWhenInputDriven: the When expression sees `input`,
// so it can decide based on the message body. This is the realistic use
// case (e.g. "wait if metadata.headers.kind == 'create'").
func TestCoordinateWaitWhenInputDriven(t *testing.T) {
	h, done := newCoordinateHandler(t, "input.body.kind == 'create'", "'never_emitted'", "5s")
	defer done()

	// kind=update → when=false → no wait, flow completes fast.
	input := map[string]interface{}{"body": map[string]interface{}{"kind": "update", "sku": "X"}}
	start := time.Now()
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest (update): %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("when=false (update) must skip the wait, took %s", elapsed)
	}
}

// TestCoordinateSignalWhenFalse_SkipsEmit: signal.when is evaluated
// post-success; if false, no signal is written to the coordinator.
func TestCoordinateSignalWhenFalse_SkipsEmit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "skip_signal",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		Coordinate: &flow.CoordinateConfig{
			Storage: &flow.SyncStorageConfig{Driver: "memory"},
			Signal: &flow.SignalConfig{
				When: "false",
				Emit: "'parent_ready:' + output.sku",
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
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	coord, _ := mgr.GetCoordinator(context.Background(), &msync.SyncStorageConfig{Driver: "memory"})
	exists, _ := coord.Exists(context.Background(), "parent_ready:X")
	if exists {
		t.Error("signal must NOT be emitted when signal.when evaluates to false")
	}
}

// TestCoordinateOnTimeoutAck: with on_timeout="ack" and no signal arriving
// before the timeout, the wait must short-circuit by returning a
// FilteredResultWithPolicy{Policy:"ack"} so the MQ consumer acks the
// broker delivery cleanly. The destination must NOT be hit (transform/to
// is skipped), and the timeout itself must elapse close to the configured
// duration (not extended by retry budget).
func TestCoordinateOnTimeoutAck(t *testing.T) {
	var destHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destHits++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "ack_on_timeout",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Coordinate: &flow.CoordinateConfig{
			Storage:   &flow.SyncStorageConfig{Driver: "memory"},
			Timeout:   "100ms",
			OnTimeout: "ack",
			Wait: &flow.WaitConfig{
				When: "true",
				For:  "'never_emitted'",
			},
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
		ErrorHandling: &flow.ErrorHandlingConfig{
			Retry: &flow.RetryConfig{Attempts: 3, Delay: "1ms"},
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

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "ORPHAN"}}
	start := time.Now()
	result, err := h.HandleRequest(context.Background(), input)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("on_timeout=ack must not surface as an error, got: %v", err)
	}

	filtered, ok := result.(*flow.FilteredResultWithPolicy)
	if !ok {
		t.Fatalf("expected *flow.FilteredResultWithPolicy result, got %T", result)
	}
	if filtered.Policy != "ack" {
		t.Errorf("expected policy=ack, got %q", filtered.Policy)
	}
	if destHits != 0 {
		t.Errorf("destination must not be called when wait times out with ack, got %d hits", destHits)
	}
	// Timeout is 100ms; with retry budget the old behavior would have been
	// 3 × 100ms minimum. ack must NOT consume retry attempts.
	if elapsed > 250*time.Millisecond {
		t.Errorf("ack should not consume retry budget, took %s", elapsed)
	}
}

// TestCoordinateSignalWhenTrue_StillEmits: regression — signal.when=true
// still emits as before.
func TestCoordinateSignalWhenTrue_StillEmits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "emit_signal",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
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

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "AI02LT"}}
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	coord, _ := mgr.GetCoordinator(context.Background(), &msync.SyncStorageConfig{Driver: "memory"})
	exists, _ := coord.Exists(context.Background(), "parent_ready:AI02LT")
	if !exists {
		t.Error("signal SHOULD be emitted when signal.when evaluates to true")
	}
}
