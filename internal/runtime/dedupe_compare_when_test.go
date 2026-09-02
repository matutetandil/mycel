package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
	"github.com/matutetandil/mycel/v3/internal/flow"
	msync "github.com/matutetandil/mycel/v3/internal/sync"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// newCompareWhenHandler builds the shape compare_when exists for: a flow whose
// downstream record can vanish by a path it never observes, so a step reports
// whether the record is still there and the dedupe gate reads that report.
//
// The existence field lives ONLY in compare_when. Putting it in the projection
// instead is the trap this attribute is here to avoid, and
// TestDedupe_ExistenceInFingerprintIsTheTrap below demonstrates why.
func newCompareWhenHandler(t *testing.T, compareWhen string) (*FlowHandler, *bytes.Buffer, func()) {
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

	logs := &bytes.Buffer{}

	h := &FlowHandler{
		Config: &flow.Config{
			Name: "compare_when_flow",
			From: &flow.FromConfig{Connector: "rabbit"},
			Dedupe: &flow.DedupeConfig{
				Cache:       "fp_cache",
				Key:         "'sku:' + input.sku",
				OnDuplicate: "ack",
				CompareWhen: compareWhen,
				Fingerprint: map[string]string{"name": "output.name"},
				TTL:         "1h",
			},
		},
		DedupeCache: memCache,
		SyncManager: mgr,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, logs, func() {
		_ = memCache.Close(context.Background())
		_ = mgr.Close()
	}
}

// countingWrite returns a write closure plus a live counter of its calls.
func countingWrite() (func(context.Context) (interface{}, error), *int32) {
	var calls int32
	return func(context.Context) (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return &connector.Result{Affected: 1}, nil
	}, &calls
}

func isDrop(result interface{}) bool {
	drop, ok := result.(*flow.FilteredResultWithPolicy)
	return ok && drop.Filtered
}

// storedFingerprint reads what Phase B committed, so a test can assert the
// cache was written even on a run where the comparison never happened.
func storedFingerprint(t *testing.T, h *FlowHandler, key string) []byte {
	t.Helper()
	v, found, err := h.DedupeCache.Get(context.Background(), dedupeStorageKey(h.Config.Name, key))
	if err != nil {
		t.Fatalf("cache Get: %v", err)
	}
	if !found {
		return nil
	}
	return v
}

// TestDedupe_CompareWhenAbsent_UnchangedBehaviour: the attribute is additive,
// so a flow that does not set it must behave exactly as before.
func TestDedupe_CompareWhenAbsent_UnchangedBehaviour(t *testing.T) {
	h, _, done := newCompareWhenHandler(t, "")
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	payload := map[string]interface{}{"name": "Widget"}
	write, calls := countingWrite()

	if r, err := h.dedupeAwareWrite(context.Background(), input, payload, write); err != nil || isDrop(r) {
		t.Fatalf("first call: err=%v drop=%v", err, isDrop(r))
	}
	r2, err := h.dedupeAwareWrite(context.Background(), input, payload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !isDrop(r2) {
		t.Fatal("without compare_when the second identical message must still be dropped")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("write calls = %d, want 1", got)
	}
}

// TestDedupe_CompareWhenTrue_DropsDuplicate: the gate open, dedupe behaves
// normally. This is the steady state the feature is supposed to preserve —
// the record exists, so a re-send of identical content is suppressed.
func TestDedupe_CompareWhenTrue_DropsDuplicate(t *testing.T) {
	h, _, done := newCompareWhenHandler(t, "output.row_exists == 1")
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	present := map[string]interface{}{"name": "Widget", "row_exists": 1}
	write, calls := countingWrite()

	// Seed the cache through a first pass so there is something to match.
	if _, err := h.dedupeAwareWrite(context.Background(), input, present, write); err != nil {
		t.Fatalf("first call: %v", err)
	}
	r2, err := h.dedupeAwareWrite(context.Background(), input, present, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !isDrop(r2) {
		t.Fatal("with compare_when true and a matching fingerprint the message must be dropped")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("write calls = %d, want 1", got)
	}
}

// TestDedupe_CompareWhenFalse_ProcessesDespiteMatch is the point of the
// feature: the stored fingerprint matches byte for byte, and the message is
// processed anyway because the flow has declared that fingerprint no longer
// describes anything that exists.
func TestDedupe_CompareWhenFalse_ProcessesDespiteMatch(t *testing.T) {
	h, _, done := newCompareWhenHandler(t, "output.row_exists == 1")
	defer done()

	input := map[string]interface{}{"sku": "X1"}
	write, calls := countingWrite()

	// Record present: writes and stores the fingerprint.
	present := map[string]interface{}{"name": "Widget", "row_exists": 1}
	if _, err := h.dedupeAwareWrite(context.Background(), input, present, write); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Same content — the projection covers `name` only, so the fingerprint is
	// identical — but the record has since been deleted downstream.
	gone := map[string]interface{}{"name": "Widget", "row_exists": 0}
	r2, err := h.dedupeAwareWrite(context.Background(), input, gone, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if isDrop(r2) {
		t.Fatal("compare_when false must never drop: the record is gone, so the message has to be reprocessed")
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("write calls = %d, want 2 (the second message must reach the destination)", got)
	}
}

// TestDedupe_CompareWhenFalse_StillCommitsPhaseB is the regression that would
// otherwise break suppression forever: gating both phases would leave the
// cache un-refreshed, so no later message could ever be suppressed either.
func TestDedupe_CompareWhenFalse_StillCommitsPhaseB(t *testing.T) {
	h, _, done := newCompareWhenHandler(t, "output.row_exists == 1")
	defer done()

	ctx := context.Background()
	input := map[string]interface{}{"sku": "X1"}
	write, _ := countingWrite()

	// A run with the gate closed and nothing in the cache.
	gone := map[string]interface{}{"name": "Widget", "row_exists": 0}
	if _, err := h.dedupeAwareWrite(ctx, input, gone, write); err != nil {
		t.Fatalf("gated call: %v", err)
	}
	stored := storedFingerprint(t, h, "sku:X1")
	if stored == nil {
		t.Fatal("Phase B must commit even when compare_when skipped the comparison, or suppression can never re-establish itself")
	}

	// And the very next message, with the record now present, is suppressed
	// against exactly that fingerprint.
	present := map[string]interface{}{"name": "Widget", "row_exists": 1}
	r, err := h.dedupeAwareWrite(ctx, input, present, write)
	if err != nil {
		t.Fatalf("follow-up call: %v", err)
	}
	if !isDrop(r) {
		t.Fatal("the fingerprint committed on the gated run must suppress the next duplicate")
	}
}

// TestDedupe_CompareWhenEvaluationFails_FailsOpen: a broken predicate must
// process the message, never swallow it. Same direction as the cache-error
// path: one extra downstream call is recoverable, a silently dropped message
// is not.
func TestDedupe_CompareWhenEvaluationFails_FailsOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr string
	}{
		{"undefined field", "output.no_such_field == 1"},
		{"not a boolean", "output.name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, done := newCompareWhenHandler(t, tc.expr)
			defer done()

			ctx := context.Background()
			input := map[string]interface{}{"sku": "X1"}
			payload := map[string]interface{}{"name": "Widget"}
			write, calls := countingWrite()

			// Seed the cache with a matching fingerprint by hand, so the only
			// thing standing between this message and a drop is the predicate.
			fp, err := h.computeDedupeFingerprint(ctx, input, payload)
			if err != nil {
				t.Fatalf("fingerprint: %v", err)
			}
			if err := h.DedupeCache.Set(ctx, dedupeStorageKey(h.Config.Name, "sku:X1"), fp, 0); err != nil {
				t.Fatalf("cache Set: %v", err)
			}

			r, err := h.dedupeAwareWrite(ctx, input, payload, write)
			if err != nil {
				t.Fatalf("dedupeAwareWrite: %v", err)
			}
			if isDrop(r) {
				t.Fatal("a compare_when that cannot be evaluated must fail open, not drop the message")
			}
			if got := atomic.LoadInt32(calls); got != 1 {
				t.Errorf("write calls = %d, want 1", got)
			}
		})
	}
}

// TestDedupe_CompareWhenSeesTransformOutput: the predicate resolves against
// the transformed payload, not the raw input — the same scope as the
// projection, which is what makes a step result reachable from it.
func TestDedupe_CompareWhenSeesTransformOutput(t *testing.T) {
	// Reads output.row_exists while input carries the opposite value: if the
	// scope were wrong, the verdict would flip.
	h, _, done := newCompareWhenHandler(t, "output.row_exists == 1 && input.sku == 'X1'")
	defer done()

	ctx := context.Background()
	input := map[string]interface{}{"sku": "X1", "row_exists": 0}
	payload := map[string]interface{}{"name": "Widget", "row_exists": 1}
	write, _ := countingWrite()

	if _, err := h.dedupeAwareWrite(ctx, input, payload, write); err != nil {
		t.Fatalf("first call: %v", err)
	}
	r, err := h.dedupeAwareWrite(ctx, input, payload, write)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !isDrop(r) {
		t.Fatal("compare_when must resolve output.* against the transform result and input.* against the raw input")
	}
}

// TestDedupe_CompareWhenFalse_Concurrent: with the gate closed every worker
// still serializes on the dedupe lock, none of them drops, and the last one
// out leaves a valid fingerprint behind.
func TestDedupe_CompareWhenFalse_Concurrent(t *testing.T) {
	h, _, done := newCompareWhenHandler(t, "output.row_exists == 1")
	defer done()

	ctx := context.Background()
	input := map[string]interface{}{"sku": "X1"}
	payload := map[string]interface{}{"name": "Widget", "row_exists": 0}
	write, calls := countingWrite()

	const workers = 8
	var wg sync.WaitGroup
	drops := int32(0)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := h.dedupeAwareWrite(ctx, input, payload, write)
			if err != nil {
				t.Errorf("concurrent call: %v", err)
				return
			}
			if isDrop(r) {
				atomic.AddInt32(&drops, 1)
			}
		}()
	}
	wg.Wait()

	if drops != 0 {
		t.Errorf("compare_when false dropped %d of %d concurrent messages; it must never drop", drops, workers)
	}
	if got := atomic.LoadInt32(calls); got != workers {
		t.Errorf("write calls = %d, want %d", got, workers)
	}
	if storedFingerprint(t, h, "sku:X1") == nil {
		t.Error("the last writer must still leave a fingerprint")
	}
}

// TestDedupe_ExistenceInFingerprintIsTheTrap documents, executably, why
// compare_when exists rather than "just add the existence check to the
// projection". It is the reasoning in DedupeConfig.CompareWhen, run.
//
// Phase B commits the fingerprint Phase A computed, i.e. the PRE-write
// reading. On a create, "does this record exist" is 0 by definition, so the
// stored projection says 0 forever while every later message says 1. The
// field's two directions land backwards: real duplicates stop being
// suppressed, and the deletion the field was added to catch still matches.
func TestDedupe_ExistenceInFingerprintIsTheTrap(t *testing.T) {
	ctx := context.Background()
	input := map[string]interface{}{"sku": "X1"}

	creating := map[string]interface{}{"name": "Widget", "row_exists": 0}
	resend := map[string]interface{}{"name": "Widget", "row_exists": 1}
	afterDelete := map[string]interface{}{"name": "Widget", "row_exists": 0}

	// The mistake: existence as a projection field instead of a gate. Both
	// branches below start from the same state — one create, which stored a
	// projection saying the record did not exist — because that is the state
	// a create flow is permanently in.
	trapped := func(t *testing.T) (*FlowHandler, func()) {
		t.Helper()
		h, _, done := newCompareWhenHandler(t, "")
		h.Config.Dedupe.Fingerprint["row_exists"] = "output.row_exists"
		write, _ := countingWrite()
		if _, err := h.dedupeAwareWrite(ctx, input, creating, write); err != nil {
			t.Fatalf("create: %v", err)
		}
		return h, done
	}

	t.Run("suppression is defeated", func(t *testing.T) {
		h, done := trapped(t)
		defer done()
		write, _ := countingWrite()
		// Identical content, record now present → computes 1 against a stored
		// 0 → no match → the duplicate reaches the destination.
		r, err := h.dedupeAwareWrite(ctx, input, resend, write)
		if err != nil {
			t.Fatalf("resend: %v", err)
		}
		if isDrop(r) {
			t.Fatal("guard broke: existence in the projection is supposed to DEFEAT suppression here")
		}
	})

	t.Run("the deletion it was added to catch is dropped anyway", func(t *testing.T) {
		h, done := trapped(t)
		defer done()
		write, _ := countingWrite()
		// Record deleted downstream → computes 0 again → matches the stored 0
		// → dropped. Exactly the message that had to be reprocessed.
		r, err := h.dedupeAwareWrite(ctx, input, afterDelete, write)
		if err != nil {
			t.Fatalf("after delete: %v", err)
		}
		if !isDrop(r) {
			t.Fatal("guard broke: existence in the projection is supposed to DROP the re-create here")
		}
	})

	// The same two messages with the check moved to the gate where it
	// belongs: suppression works, and the deleted record reprocesses.
	gated := func(t *testing.T) (*FlowHandler, func()) {
		t.Helper()
		h, _, done := newCompareWhenHandler(t, "output.row_exists == 1")
		write, _ := countingWrite()
		if _, err := h.dedupeAwareWrite(ctx, input, creating, write); err != nil {
			t.Fatalf("gated create: %v", err)
		}
		return h, done
	}

	t.Run("gated: duplicate suppressed", func(t *testing.T) {
		h, done := gated(t)
		defer done()
		write, _ := countingWrite()
		r, err := h.dedupeAwareWrite(ctx, input, resend, write)
		if err != nil {
			t.Fatalf("gated resend: %v", err)
		}
		if !isDrop(r) {
			t.Error("with the check in compare_when, a re-send against an existing record must be suppressed")
		}
	})

	t.Run("gated: external delete reprocesses", func(t *testing.T) {
		h, done := gated(t)
		defer done()
		write, calls := countingWrite()
		r, err := h.dedupeAwareWrite(ctx, input, afterDelete, write)
		if err != nil {
			t.Fatalf("gated after delete: %v", err)
		}
		if isDrop(r) {
			t.Error("with the check in compare_when, a re-create after an external delete must be processed")
		}
		if got := atomic.LoadInt32(calls); got != 1 {
			t.Errorf("write calls = %d, want 1", got)
		}
	})
}

// jsonKeys is a small helper asserting the fingerprint is still the canonical
// encoding of the projection and nothing about compare_when leaked into it.
func TestDedupe_CompareWhenNotPartOfFingerprint(t *testing.T) {
	withGate, _, done := newCompareWhenHandler(t, "output.row_exists == 1")
	defer done()
	without, _, done2 := newCompareWhenHandler(t, "")
	defer done2()

	ctx := context.Background()
	input := map[string]interface{}{"sku": "X1"}
	payload := map[string]interface{}{"name": "Widget", "row_exists": 1}

	a, err := withGate.computeDedupeFingerprint(ctx, input, payload)
	if err != nil {
		t.Fatalf("fingerprint with gate: %v", err)
	}
	b, err := without.computeDedupeFingerprint(ctx, input, payload)
	if err != nil {
		t.Fatalf("fingerprint without gate: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("compare_when must not change the fingerprint: %s vs %s", a, b)
	}
	// And it really is the projection, not something else.
	var projection map[string]interface{}
	if err := json.Unmarshal(a, &projection); err == nil {
		if _, leaked := projection["row_exists"]; leaked {
			t.Error("row_exists leaked into the projection")
		}
	} else if strings.Contains(string(a), "row_exists") {
		t.Error("row_exists leaked into the fingerprint bytes")
	}
}
