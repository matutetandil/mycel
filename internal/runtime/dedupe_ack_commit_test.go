package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
)

var dedupeTestPayload = map[string]interface{}{
	"name":     "Widget",
	"price":    10,
	"websites": map[string]interface{}{"us": true},
}

// TestDedupe_AckOnTimeoutCommitsFingerprint is the headline change: when a
// write times out but the flow's on_timeout disposition is "ack", Phase B
// commits the fingerprint anyway. The duplicate the upstream redelivers is
// then filtered instead of being reprocessed into a concurrent second
// operation on the backend.
func TestDedupe_AckOnTimeoutCommitsFingerprint(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()
	h.Config.ErrorHandling = &flow.ErrorHandlingConfig{
		OnTimeout: &flow.ErrorClassHandler{Action: "ack"},
	}

	input := map[string]interface{}{"sku": "X1"}

	var calls int
	write := func() (interface{}, error) {
		calls++
		return nil, context.DeadlineExceeded // simulate the backend timeout
	}

	// First call: the write "times out". The original error must still
	// propagate so error_handling can apply the ack disposition...
	_, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first call: expected deadline-exceeded to propagate, got %v", err)
	}

	// ...but the fingerprint must have been committed (Phase B ran), so the
	// redelivered duplicate is filtered and the write is NOT called again.
	r2, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if _, filtered := r2.(*flow.FilteredResultWithPolicy); !filtered {
		t.Fatalf("second call must be filtered (ack committed the fingerprint); got %#v", r2)
	}
	if calls != 1 {
		t.Errorf("write calls = %d, want 1 (duplicate filtered, not reprocessed)", calls)
	}
}

// TestDedupe_RequeueOnErrorSkipsCommit: a failed write whose disposition is
// requeue must NOT commit — the message will be redelivered and must be
// reprocessed, so Phase A must not find a stored fingerprint.
func TestDedupe_RequeueOnErrorSkipsCommit(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()
	h.Config.ErrorHandling = &flow.ErrorHandlingConfig{
		OnError: &flow.ErrorClassHandler{Action: "requeue"},
	}

	input := map[string]interface{}{"sku": "X1"}

	boom := errors.New("downstream blew up")
	var calls int
	write := func() (interface{}, error) {
		calls++
		if calls == 1 {
			return nil, boom
		}
		return &connector.Result{Affected: 1}, nil
	}

	if _, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write); !errors.Is(err, boom) {
		t.Fatalf("first call: expected boom, got %v", err)
	}

	// Not committed → second call re-runs the write (not filtered).
	r2, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if _, filtered := r2.(*flow.FilteredResultWithPolicy); filtered {
		t.Fatalf("requeue disposition must NOT commit; second call should reprocess, got %#v", r2)
	}
	if calls != 2 {
		t.Errorf("write calls = %d, want 2 (requeue reprocesses)", calls)
	}
}

// TestDedupe_NoHandlerSkipsCommit: backward compatibility. A failed write with
// NO class handler — even a timeout — must not commit, exactly as before.
func TestDedupe_NoHandlerSkipsCommit(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()
	// No ErrorHandling configured — the pre-existing default.

	input := map[string]interface{}{"sku": "X1"}

	var calls int
	write := func() (interface{}, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return &connector.Result{Affected: 1}, nil
	}

	if _, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first call: expected deadline-exceeded, got %v", err)
	}

	r2, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if _, filtered := r2.(*flow.FilteredResultWithPolicy); filtered {
		t.Fatalf("no class handler: a failed write must not commit; got filtered %#v", r2)
	}
	if calls != 2 {
		t.Errorf("write calls = %d, want 2 (no commit → reprocess)", calls)
	}
}

// TestDedupe_SuccessStillCommits: sanity that the success path is unchanged by
// the new ack branch — a successful write commits and dedupes the next
// identical message. error_handling is present to exercise the new code path.
func TestDedupe_SuccessStillCommits(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()
	h.Config.ErrorHandling = &flow.ErrorHandlingConfig{
		OnTimeout: &flow.ErrorClassHandler{Action: "ack"},
	}

	input := map[string]interface{}{"sku": "X1"}

	var calls int
	write := func() (interface{}, error) {
		calls++
		return &connector.Result{Affected: 1}, nil
	}

	if _, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write); err != nil {
		t.Fatalf("first call: %v", err)
	}
	r2, err := h.dedupeAwareWrite(context.Background(), input, dedupeTestPayload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if _, filtered := r2.(*flow.FilteredResultWithPolicy); !filtered {
		t.Fatalf("successful write must commit; second identical message should be filtered, got %#v", r2)
	}
	if calls != 1 {
		t.Errorf("write calls = %d, want 1 (second filtered)", calls)
	}
}
