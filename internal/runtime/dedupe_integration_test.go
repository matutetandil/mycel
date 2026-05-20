package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/internal/connector/cache/types"
	"github.com/matutetandil/mycel/internal/flow"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
)

// newDedupeHandler builds a FlowHandler wired with an in-memory cache and
// SyncManager so the dedupe primitive's runtime path can be exercised
// without any external dependency (Redis, brokers, etc.).
func newDedupeHandler(t *testing.T) (*FlowHandler, func()) {
	t.Helper()

	memCache := memory.New("fp_cache", &types.Config{Driver: "memory"})
	if err := memCache.Connect(context.Background()); err != nil {
		t.Fatalf("cache connect: %v", err)
	}

	mgr := msync.NewManager()
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	cfg := &flow.Config{
		Name: "test_dedupe",
		From: &flow.FromConfig{Connector: "rabbit"},
		Dedupe: &flow.DedupeConfig{
			Cache:       "fp_cache",
			Key:         "'sku:' + input.sku",
			OnDuplicate: "ack",
			Fingerprint: map[string]string{
				"name":     "output.name",
				"price":    "output.price",
				"websites": "output.websites",
			},
			TTL: "1h",
		},
	}
	h := &FlowHandler{
		Config:      cfg,
		DedupeCache: memCache,
		SyncManager: mgr,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, func() {
		_ = memCache.Close(context.Background())
		_ = mgr.Close()
	}
}

// TestDedupe_SameMessageTwiceIsDropped is the headline contract: send the
// same content twice for the same key; the first call goes through, the
// second is filtered with the configured policy.
func TestDedupe_SameMessageTwiceIsDropped(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	payload := map[string]interface{}{
		"name":     "Widget",
		"price":    10,
		"websites": map[string]interface{}{"us": true, "uk": true},
	}

	var calls int32
	write := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return &connector.Result{Affected: 1}, nil
	}

	// First call: cache empty, write runs, fingerprint stored.
	r1, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, filtered := r1.(*flow.FilteredResultWithPolicy); filtered {
		t.Fatalf("first call should NOT be filtered; got %#v", r1)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("first call: write calls = %d, want 1", got)
	}

	// Second call with the same payload: dedupe should drop it without
	// invoking write.
	r2, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	filtered, ok := r2.(*flow.FilteredResultWithPolicy)
	if !ok {
		t.Fatalf("second call should be filtered; got %T %#v", r2, r2)
	}
	if filtered.Policy != "ack" {
		t.Errorf("filtered.Policy = %q, want ack", filtered.Policy)
	}
	if filtered.Reason != "dedupe_match" {
		t.Errorf("filtered.Reason = %q, want dedupe_match", filtered.Reason)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("second call: write calls = %d, want 1 (no new write)", got)
	}
}

// TestDedupe_DifferentContentBothPass: two messages for the same key with
// DIFFERENT projections both reach the downstream — dedupe must not
// over-aggregate on key alone.
func TestDedupe_DifferentContentBothPass(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	payloadA := map[string]interface{}{
		"name":     "Widget",
		"price":    10,
		"websites": map[string]interface{}{"us": true},
	}
	payloadB := map[string]interface{}{
		"name":     "Widget",
		"price":    11, // real change
		"websites": map[string]interface{}{"us": true},
	}

	var calls int32
	write := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return &connector.Result{Affected: 1}, nil
	}

	if _, err := h.dedupeAwareWrite(context.Background(), input, payloadA, write); err != nil {
		t.Fatalf("A: %v", err)
	}
	if _, err := h.dedupeAwareWrite(context.Background(), input, payloadB, write); err != nil {
		t.Fatalf("B: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("write calls = %d, want 2 (different payloads must both go through)", got)
	}
}

// TestDedupe_KeyOrderInsensitive: same content with shuffled top-level
// keys still matches. This guards the canonicalization path through the
// runtime (not just the encoder unit tests).
func TestDedupe_KeyOrderInsensitive(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	payloadA := map[string]interface{}{
		"name":     "Widget",
		"price":    10,
		"websites": map[string]interface{}{"us": true, "uk": false},
	}
	// Same content, different map ordering in nested websites map.
	payloadB := map[string]interface{}{
		"websites": map[string]interface{}{"uk": false, "us": true},
		"price":    10,
		"name":     "Widget",
	}

	var calls int32
	write := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return &connector.Result{Affected: 1}, nil
	}

	if _, err := h.dedupeAwareWrite(context.Background(), input, payloadA, write); err != nil {
		t.Fatalf("A: %v", err)
	}
	r2, err := h.dedupeAwareWrite(context.Background(), input, payloadB, write)
	if err != nil {
		t.Fatalf("B: %v", err)
	}
	if _, filtered := r2.(*flow.FilteredResultWithPolicy); !filtered {
		t.Errorf("B should match A's fingerprint regardless of key order; got %#v", r2)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("write calls = %d, want 1", got)
	}
}

// TestDedupe_WriteFailureSkipsCommit: when the downstream `to` errors,
// Phase B must NOT run, so a retry with the same content actually
// re-attempts the write. A bug here would silently swallow the retry.
func TestDedupe_WriteFailureSkipsCommit(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	payload := map[string]interface{}{
		"name":     "Widget",
		"price":    10,
		"websites": map[string]interface{}{"us": true},
	}

	var calls int32
	failOnce := errors.New("simulated downstream error")
	write := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		// Fail the first call, succeed on the retry.
		if atomic.LoadInt32(&calls) == 1 {
			return nil, failOnce
		}
		return &connector.Result{Affected: 1}, nil
	}

	// First call: write fails, Phase B must skip the SET.
	if _, err := h.dedupeAwareWrite(context.Background(), input, payload, write); !errors.Is(err, failOnce) {
		t.Fatalf("first call: expected failOnce, got %v", err)
	}

	// Second call: cache should still be empty (SET was skipped), write
	// must run again.
	r2, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if _, filtered := r2.(*flow.FilteredResultWithPolicy); filtered {
		t.Fatalf("second call must NOT be filtered (write failed first time); got %#v", r2)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("write calls = %d, want 2 (first fail + successful retry)", got)
	}

	// Third call: now Phase B has stored the fingerprint, so the next
	// identical message IS deduped. Validates the SET actually happened
	// on the second (successful) attempt.
	r3, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if _, filtered := r3.(*flow.FilteredResultWithPolicy); !filtered {
		t.Errorf("third call should be filtered now that the retry succeeded; got %#v", r3)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("write calls = %d, want still 2 (third call is filtered)", got)
	}
}

// TestDedupe_ConcurrentSameFingerprint: many workers race to send the
// same payload at the same time. The self-lock must serialize them; only
// the first goroutine to acquire the lock runs the write, the rest see
// the just-stored fingerprint and drop.
//
// Without the self-lock this test would catch the race: multiple
// goroutines would pass Phase A simultaneously and double-call write.
func TestDedupe_ConcurrentSameFingerprint(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	payload := map[string]interface{}{
		"name":     "Widget",
		"price":    10,
		"websites": map[string]interface{}{"us": true},
	}

	var calls int32
	write := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return &connector.Result{Affected: 1}, nil
	}

	const N = 20
	var wg sync.WaitGroup
	var filteredCount int32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
			if err != nil {
				t.Errorf("goroutine error: %v", err)
				return
			}
			if _, isFiltered := r.(*flow.FilteredResultWithPolicy); isFiltered {
				atomic.AddInt32(&filteredCount, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("write calls = %d, want exactly 1 across %d concurrent workers", got, N)
	}
	if got := atomic.LoadInt32(&filteredCount); got != N-1 {
		t.Errorf("filtered count = %d, want %d (N-1, all but the winner)", got, N-1)
	}
}

// TestDedupe_ConcurrentDifferentKeysParallel: workers with DIFFERENT keys
// must run in parallel — the self-lock is per-key, not global. We do not
// assert timing (flaky) but we assert all writes ran and none were
// filtered (different keys → no shared state).
func TestDedupe_ConcurrentDifferentKeysParallel(t *testing.T) {
	h, done := newDedupeHandler(t)
	defer done()

	payload := map[string]interface{}{
		"name":     "Widget",
		"price":    10,
		"websites": map[string]interface{}{"us": true},
	}

	var calls int32
	write := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return &connector.Result{Affected: 1}, nil
	}

	const N = 20
	var wg sync.WaitGroup
	var filteredCount int32
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			input := map[string]interface{}{"sku": "K" + string(rune('A'+i%26)) + string(rune('A'+i/26))}
			r, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
			if err != nil {
				t.Errorf("goroutine error: %v", err)
				return
			}
			if _, isFiltered := r.(*flow.FilteredResultWithPolicy); isFiltered {
				atomic.AddInt32(&filteredCount, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != N {
		t.Errorf("write calls = %d, want %d (all distinct keys must go through)", got, N)
	}
	if got := atomic.LoadInt32(&filteredCount); got != 0 {
		t.Errorf("filtered count = %d, want 0 (distinct keys must not collide)", got)
	}
}

// TestDedupe_NoConfigZeroOverhead: when Config.Dedupe is nil,
// dedupeAwareWrite must transparently delegate to write with no cache
// access and no lock acquisition. Verified by passing a handler with no
// DedupeCache and no SyncManager — both fields are checked only when
// dedupe is configured.
func TestDedupe_NoConfigZeroOverhead(t *testing.T) {
	h := &FlowHandler{
		Config: &flow.Config{Name: "no_dedupe", From: &flow.FromConfig{Connector: "rabbit"}},
		// DedupeCache and SyncManager intentionally nil
	}

	var calls int32
	write := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return &connector.Result{Affected: 1}, nil
	}
	r, err := h.dedupeAwareWrite(context.Background(), nil, nil, write)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("write should run exactly once; got %d", atomic.LoadInt32(&calls))
	}
	if _, ok := r.(*connector.Result); !ok {
		t.Errorf("expected pass-through Result, got %T", r)
	}
}

// TestDedupe_OnDuplicatePolicyPropagates: the configured on_duplicate
// value (ack | reject | requeue) is what the MQ consumer reads from the
// FilteredResultWithPolicy. Ensure all three values round-trip.
func TestDedupe_OnDuplicatePolicyPropagates(t *testing.T) {
	for _, policy := range []string{"ack", "reject", "requeue"} {
		t.Run(policy, func(t *testing.T) {
			h, done := newDedupeHandler(t)
			defer done()
			h.Config.Dedupe.OnDuplicate = policy

			input := map[string]interface{}{"sku": "X1"}
			payload := map[string]interface{}{
				"name":     "W",
				"price":    1,
				"websites": map[string]interface{}{"us": true},
			}
			write := func() (interface{}, error) {
				return &connector.Result{Affected: 1}, nil
			}

			if _, err := h.dedupeAwareWrite(context.Background(), input, payload, write); err != nil {
				t.Fatalf("first: %v", err)
			}
			r, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
			if err != nil {
				t.Fatalf("second: %v", err)
			}
			filtered, ok := r.(*flow.FilteredResultWithPolicy)
			if !ok {
				t.Fatalf("expected FilteredResultWithPolicy, got %T", r)
			}
			if filtered.Policy != policy {
				t.Errorf("Policy = %q, want %q", filtered.Policy, policy)
			}
		})
	}
}
