package runtime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/v3/internal/flow"
	msync "github.com/matutetandil/mycel/v3/internal/sync"
)

// Facets are the same primitive at a finer grain.
//
// A plain fingerprint answers one question — did anything change — and its
// only verb is to drop the message. That is the wrong shape when one message
// carries work of different weights: a product's data and its images arrive
// together, the images take minutes because the far side downloads them, and
// re-sending both because a name changed makes the cheap half wait for the
// expensive one.
//
// Each facet is fingerprinted, stored and committed on its own. A destination
// naming a facet runs only when that facet changed, and the message is dropped
// only when none did. What makes it worth the machinery is the commit: when
// the data lands and the asset enqueue fails, the data facet is committed and
// the assets facet is not, so the retry re-sends only what did not land.
// Committing them together would lose the assets for as long as the entry
// lives — the same failure the biphasic commit exists to prevent, one level
// down.

// changedFacetsKey carries the set of facets a message changed from the
// dedupe decision to the destinations, which cannot be told any other way:
// the write closure is opaque to dedupe and the destinations run inside it.
type changedFacetsKey struct{}

// withChangedFacets returns a context carrying the facets this message
// changed. A nil set means the flow has no facets, which is every flow that
// existed before them.
func withChangedFacets(ctx context.Context, changed map[string]bool) context.Context {
	return context.WithValue(ctx, changedFacetsKey{}, changed)
}

// facetChanged reports whether a destination naming this facet should run.
//
// A destination with no facet always runs, and so does every destination on a
// flow with no facets — that is what keeps this invisible to flows that do not
// use it.
func facetChanged(ctx context.Context, facet string) bool {
	if facet == "" {
		return true
	}
	changed, ok := ctx.Value(changedFacetsKey{}).(map[string]bool)
	if !ok || changed == nil {
		return true
	}
	return changed[facet]
}

// dedupeFacetStorageKey namespaces a facet's fingerprint under the message's
// key. A facet has its own entry rather than sharing one map, so introducing
// facets leaves every fingerprint already stored untouched and readable.
func dedupeFacetStorageKey(flowName, key, facet string) string {
	return dedupeStorageKey(flowName, key) + ":" + facet
}

// computeDedupeFacetFingerprints evaluates each facet's projection.
func (h *FlowHandler) computeDedupeFacetFingerprints(
	ctx context.Context,
	input map[string]interface{},
	payload map[string]interface{},
) (map[string][]byte, error) {
	cfg := h.Config.Dedupe
	fingerprints := make(map[string][]byte, len(cfg.Facets))

	for _, facet := range cfg.Facets {
		projection := make(map[string]interface{}, len(facet.Fingerprint))
		for name, expr := range facet.Fingerprint {
			v, err := h.Transformer.EvaluateExpressionWithOutput(ctx, input, payload, expr)
			if err != nil {
				return nil, fmt.Errorf("dedupe facet %q fingerprint[%q] evaluation: %w", facet.Name, name, err)
			}
			projection[name] = v
		}
		fp, err := Fingerprint(projection)
		if err != nil {
			return nil, fmt.Errorf("dedupe facet %q: %w", facet.Name, err)
		}
		fingerprints[facet.Name] = fp
	}
	return fingerprints, nil
}

// dedupeDecideFacets is Phase A and Phase B for a flow with facets.
//
// Phase A reads each facet's stored fingerprint and builds the set that
// changed; when none did, the message is dropped exactly as a whole-message
// match would drop it. Phase B commits each changed facet whose destinations
// all succeeded.
func (h *FlowHandler) dedupeDecideFacets(
	ctx context.Context,
	lockCfg *msync.FlowLockConfig,
	storageKey string,
	key string,
	fingerprints map[string][]byte,
	ttl time.Duration,
	cfg *flow.DedupeConfig,
	compare bool,
	write func(context.Context) (interface{}, error),
) (interface{}, error) {
	return h.SyncManager.ExecuteWithLock(ctx, lockCfg, lockCfg.Key, func() (interface{}, error) {
		changed := make(map[string]bool, len(cfg.Facets))

		for _, facet := range cfg.Facets {
			// compare_when stays a single flow-level gate. When it is false
			// nothing is consulted, so every facet runs — which is what a
			// missing downstream record requires: an assets-only message
			// against a record that no longer exists would otherwise never
			// re-create it, because the facet describing the record still
			// matches.
			if !compare {
				changed[facet.Name] = true
				continue
			}

			stored, found, err := h.DedupeCache.Get(ctx, dedupeFacetStorageKey(h.Config.Name, key, facet.Name))
			if err != nil {
				// Fail open, as the whole-message path does: one extra
				// downstream call is recoverable, a swallowed message is not.
				slog.Warn("dedupe facet Get failed; treating the facet as changed",
					"flow", h.Config.Name,
					"key", key,
					"facet", facet.Name,
					"error", err)
				changed[facet.Name] = true
				continue
			}
			changed[facet.Name] = !found || !bytes.Equal(stored, fingerprints[facet.Name])
		}

		if !anyChanged(changed) {
			slog.Info("dedupe match on every facet; dropping duplicate",
				"flow", h.Config.Name,
				"key", key,
				"policy", cfg.OnDuplicate)
			return &flow.FilteredResultWithPolicy{
				Filtered: true,
				Policy:   cfg.OnDuplicate,
				Reason:   "dedupe_match",
				Detail:   fmt.Sprintf("key %q already seen within the dedupe window, on every facet", key),
			}, nil
		}

		result, writeErr := write(withChangedFacets(ctx, changed))

		// Phase B. A facet is committed only when every destination naming it
		// succeeded, so a partial failure leaves the part that did not land
		// looking changed and the retry re-sends only that part.
		landed := facetsThatLanded(h.Config, changed, result, writeErr)
		for name := range landed {
			if setErr := h.DedupeCache.Set(ctx, dedupeFacetStorageKey(h.Config.Name, key, name), fingerprints[name], ttl); setErr != nil {
				slog.Warn("dedupe facet commit failed; next identical message will not be filtered on this facet",
					"flow", h.Config.Name,
					"key", key,
					"facet", name,
					"error", setErr)
			}
		}

		if writeErr != nil {
			return nil, writeErr
		}
		return result, nil
	})
}

func anyChanged(changed map[string]bool) bool {
	for _, c := range changed {
		if c {
			return true
		}
	}
	return false
}

// facetsThatLanded returns the facets whose every destination succeeded.
//
// Only facets that ran are considered: one that did not change was not
// written and its stored fingerprint is already the right one.
func facetsThatLanded(
	cfg *flow.Config,
	changed map[string]bool,
	result interface{},
	writeErr error,
) map[string]bool {
	landed := make(map[string]bool, len(changed))

	// A single destination: the flow's own error is the whole verdict.
	if len(cfg.MultiTo) == 0 {
		if writeErr != nil {
			return landed
		}
		for name, didChange := range changed {
			if !didChange {
				continue
			}
			// With one destination there is nothing to attribute: either it
			// named this facet, or the facet has no destination at all and
			// validation refused the flow before it ran.
			if cfg.To == nil || cfg.To.Facet == "" || cfg.To.Facet == name {
				landed[name] = true
			}
		}
		return landed
	}

	// Several destinations: the per-destination report says which of them
	// failed, so a facet is judged on its own destinations rather than on
	// whether the message as a whole succeeded.
	failed := failedDestinations(result, cfg)

	for name, didChange := range changed {
		if !didChange {
			continue
		}
		ran := false
		ok := true
		for _, dest := range cfg.MultiTo {
			if dest.Facet != name {
				continue
			}
			ran = true
			if failed[dest] {
				ok = false
			}
		}
		if ran && ok {
			landed[name] = true
		}
	}
	return landed
}

// failedDestinations maps each destination to whether it reported an error.
// When the write produced no per-destination report at all — it failed before
// reaching them — every destination counts as failed, so nothing is committed.
func failedDestinations(result interface{}, cfg *flow.Config) map[*flow.ToConfig]bool {
	failed := make(map[*flow.ToConfig]bool, len(cfg.MultiTo))

	multi, ok := result.(*MultiDestResult)
	if !ok || multi == nil {
		for _, dest := range cfg.MultiTo {
			failed[dest] = true
		}
		return failed
	}

	labels := destinationLabels(cfg.MultiTo)
	for _, dest := range cfg.MultiTo {
		_, hasError := multi.Errors[labels[dest]]
		failed[dest] = hasError
	}
	return failed
}
