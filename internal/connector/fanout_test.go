package connector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/flow"
)

func TestChainRequestResponse_SingleHandler(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return "result1", nil
	})
	// No chaining needed for single handler
	result, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "result1" {
		t.Fatalf("expected result1, got %v", result)
	}
}

func TestChainRequestResponse_TwoHandlers(t *testing.T) {
	var secondCalled atomic.Bool

	primary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return "primary-result", nil
	})
	secondary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		secondCalled.Store(true)
		return "secondary-result", nil
	})

	chained := ChainRequestResponse(primary, secondary, nil)
	result, err := chained(context.Background(), map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "primary-result" {
		t.Fatalf("expected primary-result, got %v", result)
	}

	// Wait for fire-and-forget goroutine
	time.Sleep(50 * time.Millisecond)
	if !secondCalled.Load() {
		t.Fatal("secondary handler was not called")
	}
}

func TestChainRequestResponse_ThreeHandlers(t *testing.T) {
	var callCount atomic.Int32

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return "h1-result", nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return "h2-result", nil
	})
	h3 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return "h3-result", nil
	})

	// Chain: h1 + h2, then (h1+h2) + h3
	chained := ChainRequestResponse(h1, h2, nil)
	chained = ChainRequestResponse(chained, h3, nil)

	result, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if result != "h1-result" {
		t.Fatalf("expected h1-result (first registered), got %v", result)
	}

	time.Sleep(100 * time.Millisecond)
	if callCount.Load() != 3 {
		t.Fatalf("expected all 3 handlers called, got %d", callCount.Load())
	}
}

func TestChainRequestResponse_SecondaryError(t *testing.T) {
	primary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return "ok", nil
	})
	secondary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("secondary failed")
	})

	// Secondary errors should not affect primary result
	chained := ChainRequestResponse(primary, secondary, nil)
	result, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("primary should succeed even if secondary fails: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected ok, got %v", result)
	}
}

func TestChainRequestResponse_InputIsolation(t *testing.T) {
	var secondaryInput map[string]interface{}
	var mu sync.Mutex

	primary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		// Mutate input
		input["mutated"] = true
		return "ok", nil
	})
	secondary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		secondaryInput = input
		mu.Unlock()
		return nil, nil
	})

	chained := ChainRequestResponse(primary, secondary, nil)
	chained(context.Background(), map[string]interface{}{"key": "value"})

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if secondaryInput == nil {
		t.Fatal("secondary was not called")
	}
	// Secondary should get a copy, not see mutations from primary
	if _, ok := secondaryInput["mutated"]; ok {
		t.Fatal("secondary should not see mutations from primary's input")
	}
}

func TestChainEventDriven_TwoHandlers(t *testing.T) {
	var callOrder []string
	var mu sync.Mutex

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		callOrder = append(callOrder, "h1")
		mu.Unlock()
		return "h1-result", nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		callOrder = append(callOrder, "h2")
		mu.Unlock()
		return "h2-result", nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	result, err := chained(context.Background(), map[string]interface{}{"msg": "test"})
	if err != nil {
		t.Fatal(err)
	}
	// Result comes from the existing (first) handler
	if result != "h1-result" {
		t.Fatalf("expected h1-result, got %v", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(callOrder) != 2 {
		t.Fatalf("expected 2 handlers called, got %d", len(callOrder))
	}
}

func TestChainEventDriven_WaitsForAll(t *testing.T) {
	var completed atomic.Int32

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		completed.Add(1)
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		completed.Add(1)
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	_, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}

	// Both should be complete since ChainEventDriven waits
	if completed.Load() != 2 {
		t.Fatalf("expected 2 completed, got %d", completed.Load())
	}
}

func TestChainEventDriven_FirstError(t *testing.T) {
	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("h1 failed")
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	_, err := chained(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error from h1")
	}
	if err.Error() != "h1 failed" {
		t.Fatalf("expected 'h1 failed', got '%v'", err)
	}
}

func TestChainEventDriven_SecondError(t *testing.T) {
	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("h2 failed")
	})

	chained := ChainEventDriven(h1, h2, nil)
	_, err := chained(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error from h2")
	}
	if err.Error() != "h2 failed" {
		t.Fatalf("expected 'h2 failed', got '%v'", err)
	}
}

func TestChainEventDriven_ThreeHandlers(t *testing.T) {
	var callCount atomic.Int32

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return nil, nil
	})
	h3 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	chained = ChainEventDriven(chained, h3, nil)

	_, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if callCount.Load() != 3 {
		t.Fatalf("expected 3 handlers called, got %d", callCount.Load())
	}
}

func TestChainEventDriven_InputIsolation(t *testing.T) {
	var h2Input map[string]interface{}
	var mu sync.Mutex

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		input["mutated"] = true
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		h2Input = input
		mu.Unlock()
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	chained(context.Background(), map[string]interface{}{"key": "value"})

	mu.Lock()
	defer mu.Unlock()
	if h2Input == nil {
		t.Fatal("h2 was not called")
	}
	// h2 gets a copy, shouldn't see h1's mutations
	if _, ok := h2Input["mutated"]; ok {
		t.Fatal("h2 should not see mutations from h1's input")
	}
}

func TestCopyInput(t *testing.T) {
	original := map[string]interface{}{
		"name": "test",
		"age":  30,
	}
	copied := CopyInput(original)

	if copied["name"] != "test" || copied["age"] != 30 {
		t.Fatal("copy should have same values")
	}

	copied["name"] = "modified"
	if original["name"] != "test" {
		t.Fatal("modifying copy should not affect original")
	}
}

func TestCopyInput_Nil(t *testing.T) {
	if CopyInput(nil) != nil {
		t.Fatal("copy of nil should be nil")
	}
}

// --- Fan-out filter-rejection aggregation -----------------------------------
//
// These tests reproduce the symptom Mercury hit on v1.19.5: two flows on the
// same MQ source, one rejects via filter (on_reject = "requeue"), the other
// processes the message successfully. The pre-fix ChainEventDriven returned
// the FIRST handler's result, so the rejection masked the success and the
// broker re-delivered the message until the requeue tracker capped it at 3.
// Post-fix: a real success in any branch wins and the delivery is acked.

func TestChainEventDriven_RejectingFlowDoesNotMaskSuccess_RejectorFirst(t *testing.T) {
	rejecting := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return &flow.FilteredResultWithPolicy{
			Filtered: true,
			Policy:   "requeue",
		}, nil
	})
	succeeding := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"sku": "AI02LT"}, nil
	})

	chained := ChainEventDriven(rejecting, succeeding, nil)
	result, err := chained(context.Background(), map[string]interface{}{"op": "update"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, isFiltered := result.(*flow.FilteredResultWithPolicy); isFiltered {
		t.Fatalf("rejecting flow's filter result must not mask the sibling success — would cause spurious requeue")
	}
	got, ok := result.(map[string]interface{})
	if !ok || got["sku"] != "AI02LT" {
		t.Fatalf("expected the success result to win, got %T %v", result, result)
	}
}

func TestChainEventDriven_RejectingFlowDoesNotMaskSuccess_RejectorSecond(t *testing.T) {
	succeeding := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"sku": "AI02LT"}, nil
	})
	rejecting := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return &flow.FilteredResultWithPolicy{
			Filtered: true,
			Policy:   "requeue",
		}, nil
	})

	chained := ChainEventDriven(succeeding, rejecting, nil)
	result, err := chained(context.Background(), map[string]interface{}{"op": "update"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, isFiltered := result.(*flow.FilteredResultWithPolicy); isFiltered {
		t.Fatalf("rejecting flow's filter result must not mask the sibling success — would cause spurious requeue")
	}
}

func TestChainEventDriven_BothFiltered_PicksMostAggressivePolicy(t *testing.T) {
	rejectThenAck := []struct {
		name string
		a, b string
		want string
	}{
		{"requeue beats reject", "requeue", "reject", "requeue"},
		{"requeue beats ack", "requeue", "ack", "requeue"},
		{"reject beats ack", "reject", "ack", "reject"},
		{"ack vs ack", "ack", "ack", "ack"},
		{"order is irrelevant", "ack", "requeue", "requeue"},
	}
	for _, tc := range rejectThenAck {
		t.Run(tc.name, func(t *testing.T) {
			ha := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
				return &flow.FilteredResultWithPolicy{Filtered: true, Policy: tc.a}, nil
			})
			hb := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
				return &flow.FilteredResultWithPolicy{Filtered: true, Policy: tc.b}, nil
			})
			chained := ChainEventDriven(ha, hb, nil)
			result, err := chained(context.Background(), nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			f, ok := result.(*flow.FilteredResultWithPolicy)
			if !ok {
				t.Fatalf("both filtered: expected aggregate FilteredResultWithPolicy, got %T", result)
			}
			if f.Policy != tc.want {
				t.Errorf("expected policy=%q, got %q", tc.want, f.Policy)
			}
		})
	}
}

// --- Fan-out filter-coordination & on_drop suppression --------------------
//
// These tests cover the v1.21.2 fix: when several flows share the same MQ
// source and only one passes its filter, the rejecting siblings are
// intra-container routing, not real drops. The aggregator must:
//   1. Pick the result of whichever branch passed its filter (Reason !=
//      "filter") even if that branch was later deflected by a different
//      gate (accept / coordinate_timeout / sequence_older).
//   2. Suppress the rejecting siblings' deferred on_drop closures so the
//      operator does not get N notifications per delivery.
//   3. Honor the cross-service requeue path when ALL branches reject —
//      the message belongs to no flow in this container and the broker
//      must be free to redeliver to a different consumer.

func TestChainEventDriven_FilterCoordination_AccepterWinsOverRejecter(t *testing.T) {
	// Mercury scenario: 3 flows, 1 accepts filter and times out at
	// coordinate.wait, 2 reject at filter with on_reject="requeue".
	// Pre-fix: rejecter's "requeue" beat the accepter's "ack" via
	// most-aggressive policy → broker requeued the message → 3-cycle
	// retry storm. Post-fix: accepter (Reason != "filter") wins.

	var rejectorDrops atomic.Int32
	rejector := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{
			Filtered: true,
			Policy:   "requeue",
			Reason:   "filter",
		}
		// Stand-in for the deferred firing the runtime attaches.
		r.PendingOnDrop = func(ctx context.Context) {
			rejectorDrops.Add(1)
		}
		return r, nil
	})

	var accepterDrops atomic.Int32
	accepter := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{
			Filtered: true,
			Policy:   "ack",
			Reason:   "coordinate_timeout",
		}
		r.PendingOnDrop = func(ctx context.Context) {
			accepterDrops.Add(1)
		}
		return r, nil
	})

	chained := ChainEventDriven(rejector, accepter, nil)
	result, err := chained(context.Background(), map[string]interface{}{"op": "update"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Aggregator returned the accepter's result.
	f, ok := result.(*flow.FilteredResultWithPolicy)
	if !ok {
		t.Fatalf("expected FilteredResultWithPolicy aggregate, got %T", result)
	}
	if f.Reason != "coordinate_timeout" {
		t.Errorf("expected accepter (coordinate_timeout) to win, got Reason=%q", f.Reason)
	}
	if f.Policy != "ack" {
		t.Errorf("expected accepter's ack policy, got %q (the bug — rejecter's requeue masked accepter's ack)", f.Policy)
	}

	// Fire on the aggregate as the consumer would. Only the accepter's
	// closure should run; the rejecter's was suppressed.
	flow.FireDropAspect(context.Background(), result)

	if got := rejectorDrops.Load(); got != 0 {
		t.Errorf("rejecter's on_drop must be suppressed when sibling passed filter, fired %d", got)
	}
	if got := accepterDrops.Load(); got != 1 {
		t.Errorf("accepter's on_drop should fire once, fired %d", got)
	}
}

func TestChainEventDriven_FilterCoordination_AccepterFirst(t *testing.T) {
	// Same scenario as above but with the accepter chained first.
	// Aggregation must be order-independent.
	var rejectorDrops atomic.Int32
	rejector := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue", Reason: "filter"}
		r.PendingOnDrop = func(ctx context.Context) { rejectorDrops.Add(1) }
		return r, nil
	})
	var accepterDrops atomic.Int32
	accepter := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "ack", Reason: "coordinate_timeout"}
		r.PendingOnDrop = func(ctx context.Context) { accepterDrops.Add(1) }
		return r, nil
	})

	chained := ChainEventDriven(accepter, rejector, nil)
	result, _ := chained(context.Background(), nil)

	f := result.(*flow.FilteredResultWithPolicy)
	if f.Reason != "coordinate_timeout" {
		t.Errorf("expected accepter to win regardless of order, got Reason=%q", f.Reason)
	}
	flow.FireDropAspect(context.Background(), result)

	if rejectorDrops.Load() != 0 {
		t.Errorf("rejecter's on_drop must be suppressed, fired %d", rejectorDrops.Load())
	}
	if accepterDrops.Load() != 1 {
		t.Errorf("accepter's on_drop should fire once, fired %d", accepterDrops.Load())
	}
}

func TestChainEventDriven_FilterCoordination_ThreeFlowsMercuryScenario(t *testing.T) {
	// The exact Mercury reproduction: 3 flows, 2 reject filter + 1
	// accept-then-coord-timeout. End-to-end through the chained
	// aggregator: only the accepter's on_drop fires, the broker sees ack.
	var rejectorADrops, rejectorBDrops, accepterDrops atomic.Int32

	rejectorA := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue", Reason: "filter", MessageID: "m1"}
		r.PendingOnDrop = func(ctx context.Context) { rejectorADrops.Add(1) }
		return r, nil
	})
	accepter := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "ack", Reason: "coordinate_timeout", MessageID: "m1"}
		r.PendingOnDrop = func(ctx context.Context) { accepterDrops.Add(1) }
		return r, nil
	})
	rejectorB := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue", Reason: "filter", MessageID: "m1"}
		r.PendingOnDrop = func(ctx context.Context) { rejectorBDrops.Add(1) }
		return r, nil
	})

	chained := ChainEventDriven(rejectorA, accepter, nil)
	chained = ChainEventDriven(chained, rejectorB, nil)

	result, _ := chained(context.Background(), map[string]interface{}{"op": "update"})

	f := result.(*flow.FilteredResultWithPolicy)
	if f.Policy != "ack" {
		t.Fatalf("expected ack (no requeue cycle), got %q — broker would re-deliver and storm", f.Policy)
	}
	if f.Reason != "coordinate_timeout" {
		t.Errorf("expected accepter's coord_timeout to win, got Reason=%q", f.Reason)
	}

	flow.FireDropAspect(context.Background(), result)

	if rejectorADrops.Load() != 0 || rejectorBDrops.Load() != 0 {
		t.Errorf("filter-rejecting siblings must be silent, got A=%d B=%d", rejectorADrops.Load(), rejectorBDrops.Load())
	}
	if accepterDrops.Load() != 1 {
		t.Errorf("expected exactly 1 on_drop (accepter's), got %d", accepterDrops.Load())
	}
}

func TestChainEventDriven_FilterCoordination_AllRejectFiresOnDropOnce(t *testing.T) {
	// All branches filter-reject: cross-service "requeue" must be
	// honored (operator may have other consumers waiting for unknown
	// operations). on_drop fires exactly once on the merged winner.
	var aDrops, bDrops, cDrops atomic.Int32

	mk := func(policy string, counter *atomic.Int32) HandlerFunc {
		return HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			r := &flow.FilteredResultWithPolicy{Filtered: true, Policy: policy, Reason: "filter"}
			r.PendingOnDrop = func(ctx context.Context) { counter.Add(1) }
			return r, nil
		})
	}

	chained := ChainEventDriven(mk("ack", &aDrops), mk("requeue", &bDrops), nil)
	chained = ChainEventDriven(chained, mk("reject", &cDrops), nil)

	result, _ := chained(context.Background(), nil)

	f := result.(*flow.FilteredResultWithPolicy)
	if f.Policy != "requeue" {
		t.Errorf("most-aggressive policy across 3 filter-rejecters should be requeue, got %q", f.Policy)
	}

	flow.FireDropAspect(context.Background(), result)

	total := aDrops.Load() + bDrops.Load() + cDrops.Load()
	if total != 1 {
		t.Errorf("exactly one on_drop must fire when all reject, got %d total (a=%d b=%d c=%d)",
			total, aDrops.Load(), bDrops.Load(), cDrops.Load())
	}
}

func TestChainEventDriven_FilterCoordination_SuccessSuppressesFilterDrop(t *testing.T) {
	// Pure success on one branch must suppress sibling's filter drop.
	// This is the original v1.19.7 success-masking test, now also
	// asserting on_drop suppression.
	var rejectorDrops atomic.Int32
	rejector := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		r := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue", Reason: "filter"}
		r.PendingOnDrop = func(ctx context.Context) { rejectorDrops.Add(1) }
		return r, nil
	})
	winner := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})

	chained := ChainEventDriven(rejector, winner, nil)
	result, _ := chained(context.Background(), nil)
	flow.FireDropAspect(context.Background(), result)

	if rejectorDrops.Load() != 0 {
		t.Errorf("rejecter's on_drop must be suppressed when a sibling succeeded, fired %d", rejectorDrops.Load())
	}
}

func TestChainEventDriven_FilteredHandlerStillCallsSibling(t *testing.T) {
	// Regression: even though one branch is "filtered" early, the other
	// branch still must run — the user intended both flows to look at the
	// message and decide independently.
	var siblingCalled bool
	var mu sync.Mutex

	rejecting := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return &flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue"}, nil
	})
	sibling := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		siblingCalled = true
		mu.Unlock()
		return map[string]interface{}{"ok": true}, nil
	})

	chained := ChainEventDriven(rejecting, sibling, nil)
	if _, err := chained(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !siblingCalled {
		t.Fatal("sibling handler must run even when the other branch was filtered")
	}
}
