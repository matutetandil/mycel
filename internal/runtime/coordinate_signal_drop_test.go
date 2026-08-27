package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
	httpconn "github.com/matutetandil/mycel/v3/internal/connector/http"
	"github.com/matutetandil/mycel/v3/internal/flow"
	msync "github.com/matutetandil/mycel/v3/internal/sync"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// newSignallingHandler builds a flow that emits a coordinate signal after a
// successful write, and drops messages through one of the two gates that sit
// INSIDE the coordinate wrapper (dedupe, sequence_guard).
//
// The signal key deliberately reads a field that neither the dedupe key nor
// the fingerprint projection covers, so a drop and the write that preceded it
// ask for two different signal keys. That is what makes "did the drop emit?"
// observable: without it, a false emit would be indistinguishable from the
// legitimate one already stored.
func newSignallingHandler(t *testing.T, withDedupe, withSequenceGuard bool) (*FlowHandler, *msync.Manager, func()) {
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

	memCache := memory.New("fp_cache", &types.Config{Driver: "memory"})
	if err := memCache.Connect(context.Background()); err != nil {
		t.Fatalf("cache connect: %v", err)
	}

	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}
	mgr := msync.NewManager()

	cfg := &flow.Config{
		Name: "signalling_flow",
		From: &flow.FromConfig{
			Connector:       "rabbit",
			ConnectorParams: map[string]interface{}{"target": "q"},
		},
		Coordinate: &flow.CoordinateConfig{
			Storage: &flow.SyncStorageConfig{Driver: "memory"},
			Timeout: "5s",
			Signal: &flow.SignalConfig{
				Emit: "'ready:' + input.body.n",
				TTL:  "1m",
			},
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.sku"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
	}

	if withDedupe {
		cfg.Dedupe = &flow.DedupeConfig{
			Cache:       "fp_cache",
			Key:         "'sku:' + input.body.sku",
			OnDuplicate: "ack",
			// Only the sku: two messages differing solely in `n` are duplicates.
			Fingerprint: map[string]string{"sku": "output.sku"},
			TTL:         "1h",
		}
	}
	if withSequenceGuard {
		cfg.SequenceGuard = &flow.SequenceGuardConfig{
			Storage:  &flow.SyncStorageConfig{Driver: "memory"},
			Key:      "'seq:' + input.body.sku",
			Sequence: "input.body.seq",
			OnOlder:  "ack",
			TTL:      "1h",
		}
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		DedupeCache: memCache,
		Transformer: tr,
		SyncManager: mgr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	return h, mgr, func() {
		srv.Close()
		_ = memCache.Close(context.Background())
		_ = mgr.Close()
	}
}

func signalExists(t *testing.T, mgr *msync.Manager, key string) bool {
	t.Helper()
	coord, err := mgr.GetCoordinator(context.Background(), &msync.SyncStorageConfig{Driver: "memory"})
	if err != nil {
		t.Fatalf("GetCoordinator: %v", err)
	}
	ok, err := coord.Exists(context.Background(), key)
	if err != nil {
		t.Fatalf("Exists(%q): %v", key, err)
	}
	return ok
}

func assertDropped(t *testing.T, result interface{}, wantReason string) {
	t.Helper()
	drop, ok := result.(*flow.FilteredResultWithPolicy)
	if !ok || !drop.Filtered {
		t.Fatalf("expected the message to be dropped, got %T (%v)", result, result)
	}
	if drop.Reason != wantReason {
		t.Fatalf("drop reason = %q, want %q", drop.Reason, wantReason)
	}
}

// TestCoordinateSignal_NotEmittedOnDedupeDrop is the headline contract: a
// message suppressed as a duplicate never reached `to`, so it must not tell
// waiters that its effect landed.
//
// Pre-fix, dedupe returned (*FilteredResultWithPolicy, nil) and
// ExecuteWithCoordinate emits whenever the inner call returns a nil error, so
// the drop signalled anyway. In production that released children against a
// parent that had been suppressed and no longer existed downstream.
func TestCoordinateSignal_NotEmittedOnDedupeDrop(t *testing.T) {
	h, mgr, done := newSignallingHandler(t, true, false)
	defer done()

	ctx := context.Background()

	// First delivery writes and legitimately signals.
	first := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "1"}}
	if _, err := h.HandleRequest(ctx, first); err != nil {
		t.Fatalf("first HandleRequest: %v", err)
	}
	if !signalExists(t, mgr, "ready:1") {
		t.Fatal("a successful write must emit its signal; ready:1 missing")
	}

	// Same content, so a duplicate — but a different signal key, so an emit
	// from the drop is visible rather than masked by the one above.
	second := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "2"}}
	result, err := h.HandleRequest(ctx, second)
	if err != nil {
		t.Fatalf("second HandleRequest: %v", err)
	}
	assertDropped(t, result, "dedupe_match")

	if signalExists(t, mgr, "ready:2") {
		t.Fatal("a dedupe-dropped message emitted its coordinate signal: nothing was written, so waiters were released against an effect that never happened")
	}
}

// TestCoordinateSignal_NotEmittedOnSequenceGuardDrop: the same gate, reached
// through the other primitive that returns a drop with a nil error.
func TestCoordinateSignal_NotEmittedOnSequenceGuardDrop(t *testing.T) {
	h, mgr, done := newSignallingHandler(t, false, true)
	defer done()

	ctx := context.Background()

	newer := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "1", "seq": 20}}
	if _, err := h.HandleRequest(ctx, newer); err != nil {
		t.Fatalf("first HandleRequest: %v", err)
	}
	if !signalExists(t, mgr, "ready:1") {
		t.Fatal("a successful write must emit its signal; ready:1 missing")
	}

	older := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "2", "seq": 10}}
	result, err := h.HandleRequest(ctx, older)
	if err != nil {
		t.Fatalf("second HandleRequest: %v", err)
	}
	assertDropped(t, result, "sequence_older")

	if signalExists(t, mgr, "ready:2") {
		t.Fatal("an out-of-order message emitted its coordinate signal despite writing nothing")
	}
}

// TestCoordinateSignal_StillEmittedOnSuccess guards the other direction: the
// fix must not make the signal conditional on anything but the drop, or every
// coordinate-based handoff stops working.
func TestCoordinateSignal_StillEmittedOnSuccess(t *testing.T) {
	h, mgr, done := newSignallingHandler(t, true, false)
	defer done()

	ctx := context.Background()

	for _, sku := range []string{"A", "B"} {
		input := map[string]interface{}{"body": map[string]interface{}{"sku": sku, "n": sku}}
		if _, err := h.HandleRequest(ctx, input); err != nil {
			t.Fatalf("HandleRequest(%s): %v", sku, err)
		}
		if !signalExists(t, mgr, "ready:"+sku) {
			t.Fatalf("distinct content must still signal; ready:%s missing", sku)
		}
	}
}
