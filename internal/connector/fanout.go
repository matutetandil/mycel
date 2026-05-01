package connector

import (
	"context"
	"log/slog"
	"sync"

	"github.com/matutetandil/mycel/internal/flow"
)

// HandlerFunc is the universal handler signature used by all connectors.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// ChainRequestResponse creates a composite handler for request-response connectors (REST, gRPC, TCP, etc.).
// The existing handler is the primary and returns the response to the caller.
// The additional handler runs concurrently as fire-and-forget in a background goroutine.
func ChainRequestResponse(existing, additional HandlerFunc, logger *slog.Logger) HandlerFunc {
	return func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		// Copy input before launching goroutine to avoid races with primary handler
		inputCopy := CopyInput(input)
		// Fire-and-forget the additional handler
		go func() {
			if _, err := additional(context.WithoutCancel(ctx), inputCopy); err != nil && logger != nil {
				logger.Warn("fan-out handler error", "error", err)
			}
		}()
		// Primary handler returns the response
		return existing(ctx, input)
	}
}

// ChainEventDriven creates a composite handler for event-driven connectors (MQ, CDC, etc.).
// Both handlers run concurrently. The function waits for all handlers to complete
// and returns the first error encountered (if any).
//
// Result aggregation rules — important for filter-rejection semantics:
//   - If any handler returns an error, the first error is returned and the
//     consumer's retry / DLQ path takes over.
//   - If at least one handler returns a real success (i.e. NOT a
//     *flow.FilteredResultWithPolicy), that success becomes the aggregate
//     result. The delivery is acked. A sibling flow that rejected the
//     message via its own filter must not requeue what another flow already
//     processed.
//   - If all handlers returned filter rejections, the most "demanding"
//     policy wins: requeue > reject > ack (most-aggressive-first). This
//     matches the operator intuition that if any flow asked the broker to
//     try again, we should try again, even if other flows would have
//     dropped it silently.
func ChainEventDriven(existing, additional HandlerFunc, logger *slog.Logger) HandlerFunc {
	return func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		var wg sync.WaitGroup
		var result1, result2 interface{}
		var err1, err2 error

		// Copy input before launching goroutines to avoid races
		inputCopy := CopyInput(input)

		wg.Add(2)
		go func() {
			defer wg.Done()
			result1, err1 = existing(ctx, input)
		}()
		go func() {
			defer wg.Done()
			result2, err2 = additional(ctx, inputCopy)
		}()
		wg.Wait()

		if err1 != nil {
			return nil, err1
		}
		if err2 != nil {
			return nil, err2
		}
		return aggregateFanoutResults(result1, result2), nil
	}
}

// aggregateFanoutResults picks the right aggregate result for two fan-out
// branches. See ChainEventDriven for the rules.
//
// Filter coordination: when one branch passed its filter (Reason != "filter")
// and the other rejected at filter, the rejecter is treated as intra-container
// routing — its drop is suppressed (PendingOnDrop closure cleared so it never
// fires) and its policy is discarded. The winner's policy is honored. This
// stops a single fan-out delivery from triggering N-1 spurious on_drop
// notifications and an `on_reject = "requeue"` from a sibling that doesn't
// own the message.
//
// Cross-service `on_reject = "requeue"` semantics are preserved: that
// pattern only matters when ALL flows in the local container reject — in
// that case both branches have Reason="filter" and we fall through to
// mergeFilteredPolicies, which picks the most-aggressive policy and lets
// the broker hand the message to a different service's consumer.
func aggregateFanoutResults(a, b interface{}) interface{} {
	fa, aFiltered := a.(*flow.FilteredResultWithPolicy)
	fb, bFiltered := b.(*flow.FilteredResultWithPolicy)

	switch {
	case !aFiltered && !bFiltered:
		// Two real successes — caller doesn't care about either result
		// individually (it just acks). Return the first deterministically.
		return a
	case !aFiltered:
		// a succeeded, b filtered out — a wins, ack the delivery.
		// Suppress b's deferred on_drop: it was intra-routing.
		suppressDrop(fb)
		return a
	case !bFiltered:
		// b succeeded, a filtered out — b wins, ack the delivery.
		// Suppress a's deferred on_drop.
		suppressDrop(fa)
		return b
	}

	// Both branches returned a FilteredResultWithPolicy. Apply
	// filter-coordination: a non-"filter" reason means the branch
	// passed its filter and was deflected later (accept gate /
	// coordinate timeout / sequence_guard older). When exactly one
	// branch passed its filter, that branch's outcome owns the
	// delivery and the rejecting sibling is silenced.
	aPassedFilter := fa.Reason != "filter"
	bPassedFilter := fb.Reason != "filter"
	switch {
	case aPassedFilter && !bPassedFilter:
		suppressDrop(fb)
		return fa
	case !aPassedFilter && bPassedFilter:
		suppressDrop(fa)
		return fb
	}

	// Either both passed filter (concurrent post-filter drops) or
	// both rejected at filter (no flow in this container owns the
	// message). Fall through to most-aggressive policy. The losing
	// branch's PendingOnDrop closure is naturally suppressed by not
	// being returned — we only fire the winner's.
	return mergeFilteredPolicies(fa, fb)
}

// suppressDrop nils out a result's PendingOnDrop closure so it can never
// be fired. Used when fan-out determines the branch lost — its drop is
// not a real drop, just intra-container routing noise.
func suppressDrop(r *flow.FilteredResultWithPolicy) {
	if r != nil {
		r.PendingOnDrop = nil
	}
}

// mergeFilteredPolicies returns the FilteredResultWithPolicy whose policy
// asks the broker for the most retention: requeue > reject > ack.
// Identifiers (MessageID, MaxRequeue, IDField) are taken from whichever
// result wins so the consumer's dedup tracker keys remain stable. The
// loser's PendingOnDrop closure is suppressed so only the winner fires
// on_drop (consistent with the operator intuition that one delivery
// produces one on_drop notification, not N).
func mergeFilteredPolicies(a, b *flow.FilteredResultWithPolicy) *flow.FilteredResultWithPolicy {
	rank := func(p string) int {
		switch p {
		case "requeue":
			return 2
		case "reject":
			return 1
		default: // "ack" or unknown
			return 0
		}
	}
	if rank(a.Policy) >= rank(b.Policy) {
		suppressDrop(b)
		return a
	}
	suppressDrop(a)
	return b
}

// CopyInput creates a shallow copy of an input map.
func CopyInput(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(input))
	for k, v := range input {
		cp[k] = v
	}
	return cp
}
