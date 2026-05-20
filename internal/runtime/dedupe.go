// Package runtime contains the dedupe primitive's runtime integration.
//
// Conceptually, dedupe runs in two phases bracketing the destination call:
//
//	Phase A (before `to`): compute a canonical fingerprint over the named
//	    projection, GET the previously stored fingerprint for the key, and
//	    compare byte-for-byte. On match the message is dropped according
//	    to on_duplicate without invoking `to`.
//
//	Phase B (after `to` ONLY on success): SET the new fingerprint for the
//	    key. Writing earlier would let a failed+retried message
//	    self-discard and lose a real change.
//
// Both phases run under an in-process lock keyed on the dedupe.key, so two
// workers in the same process cannot both pass Phase A with identical
// fingerprints and double-call the downstream. For cross-process
// serialization (e.g. several Mycel pods consuming the same queue),
// callers must compose dedupe with an outer `lock {}` block — the
// in-process lock alone makes dedupe correct but does not extend its
// effectiveness across processes.
//
// Correctness invariant: dedupe NEVER drops a message that contains a real
// change. If stored == new fingerprint, then those exact bytes were
// previously committed to Phase B, which only runs after a successful
// `to`. Re-sending the same content is a no-op by construction. Cache
// errors fail open (the message is processed) so a broken Redis cannot
// silently swallow traffic.
package runtime

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/flow"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
)

// dedupeAwareWrite is the integration entry point for the dedupe primitive.
// It wraps the destination call in Phase A + lock + Phase B. When the flow
// has no dedupe configured (the common case), it transparently delegates
// to write() with zero overhead.
//
// The closure write() is whatever the handler would have called to perform
// the destination side-effect (e.g. dest.Write inside a trace.RecordStage).
// It is invoked at most once per dedupeAwareWrite call. On match it is not
// invoked at all.
func (h *FlowHandler) dedupeAwareWrite(
	ctx context.Context,
	input map[string]interface{},
	payload map[string]interface{},
	write func() (interface{}, error),
) (interface{}, error) {
	cfg := h.Config.Dedupe
	if cfg == nil {
		return write()
	}
	if h.DedupeCache == nil {
		// Misconfiguration caught at registration time normally; defend
		// here so a missing connector cannot silently disable dedupe.
		return nil, fmt.Errorf("dedupe is configured for flow %q but its cache connector %q was not resolved", h.Config.Name, cfg.Cache)
	}
	if h.SyncManager == nil {
		return nil, fmt.Errorf("dedupe requires a SyncManager for in-process serialization (flow %q)", h.Config.Name)
	}

	if err := h.ensureTransformer(); err != nil {
		return nil, err
	}

	key, err := h.evalDedupeKey(ctx, input)
	if err != nil {
		return nil, err
	}

	fp, err := h.computeDedupeFingerprint(ctx, input, payload)
	if err != nil {
		return nil, err
	}

	storageKey := dedupeStorageKey(h.Config.Name, key)
	lockKey := dedupeLockKey(h.Config.Name, key)
	ttl := parseDedupeTTL(cfg.TTL)

	// SyncManager.ExecuteWithLock with nil Storage falls back to the
	// memory lock backend (internal/sync/manager.go:107). That is the
	// in-process serialization we want. Cross-process is the caller's
	// responsibility via the existing flow-level lock {} block.
	//
	// 5m is generous enough to outlast any reasonable downstream call
	// while still preventing a stuck flow from blocking other workers on
	// the same key forever. The internal heartbeat extends the TTL while
	// the inner write runs, so a long-running write cannot race-release
	// the lock mid-execution.
	lockCfg := &msync.FlowLockConfig{
		Storage: nil, // memory backend
		Key:     lockKey,
		Timeout: "5m",
		Wait:    true,
	}

	return h.SyncManager.ExecuteWithLock(ctx, lockCfg, lockKey, func() (interface{}, error) {
		// Phase A: GET stored, compare.
		stored, found, getErr := h.DedupeCache.Get(ctx, storageKey)
		if getErr != nil {
			// Fail open: cache errors should never block message processing.
			// Worst case is one extra downstream call.
			slog.Warn("dedupe Get failed; proceeding with message",
				"flow", h.Config.Name,
				"key", key,
				"error", getErr)
		} else if found && bytes.Equal(stored, fp) {
			slog.Info("dedupe match; dropping duplicate",
				"flow", h.Config.Name,
				"key", key,
				"policy", cfg.OnDuplicate)
			return &flow.FilteredResultWithPolicy{
				Filtered: true,
				Policy:   cfg.OnDuplicate,
				Reason:   "dedupe_match",
			}, nil
		}

		// Not a duplicate — run the actual write.
		result, writeErr := write()
		if writeErr != nil {
			// Phase B intentionally skipped: a failed write must not poison
			// the fingerprint cache. The next retry will re-attempt.
			return result, writeErr
		}

		// Phase B: SET the new fingerprint. Best-effort — a failure here
		// just means the next identical message will not be deduped; the
		// current message already succeeded downstream.
		if setErr := h.DedupeCache.Set(ctx, storageKey, fp, ttl); setErr != nil {
			slog.Warn("dedupe commit failed; next identical message will not be filtered",
				"flow", h.Config.Name,
				"key", key,
				"error", setErr)
		}
		return result, nil
	})
}

// evalDedupeKey evaluates the configured key CEL expression against the
// message input. Empty results are rejected — silent emptiness would
// cause all messages to share a key and dedupe everything together.
func (h *FlowHandler) evalDedupeKey(ctx context.Context, input map[string]interface{}) (string, error) {
	cfg := h.Config.Dedupe
	// EvaluateExpression binds `input` itself; callers must pass the raw
	// input map, not a nested wrapper.
	val, err := h.Transformer.EvaluateExpression(ctx, input, nil, cfg.Key)
	if err != nil {
		return "", fmt.Errorf("dedupe key evaluation: %w", err)
	}
	key := fmt.Sprintf("%v", val)
	if key == "" {
		return "", fmt.Errorf("dedupe key %q evaluated to empty string", cfg.Key)
	}
	return key, nil
}

// computeDedupeFingerprint evaluates each fingerprint expression against
// `input.*` and `output.*` (the transformed payload), then encodes the
// resulting projection into a deterministic byte string via Fingerprint.
func (h *FlowHandler) computeDedupeFingerprint(ctx context.Context, input map[string]interface{}, payload map[string]interface{}) ([]byte, error) {
	cfg := h.Config.Dedupe
	projection := make(map[string]interface{}, len(cfg.Fingerprint))
	for name, expr := range cfg.Fingerprint {
		// EvaluateExpressionWithOutput binds both `input` and `output`,
		// matching the documented projection scope.
		v, err := h.Transformer.EvaluateExpressionWithOutput(ctx, input, payload, expr)
		if err != nil {
			return nil, fmt.Errorf("dedupe fingerprint[%q] evaluation: %w", name, err)
		}
		projection[name] = v
	}
	return Fingerprint(projection)
}

// ensureTransformer initializes the CEL transformer lazily. Most handler
// paths call applyTransforms first which already does this, but a defensive
// check here keeps dedupe from depending on call order.
func (h *FlowHandler) ensureTransformer() error {
	if h.Transformer != nil {
		return nil
	}
	celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
	tr, err := transform.NewCELTransformerWithOptions(celOptions...)
	if err != nil {
		return fmt.Errorf("failed to create CEL transformer for dedupe: %w", err)
	}
	h.Transformer = tr
	return nil
}

// dedupeStorageKey namespaces the cache key by flow so two flows can use
// the same logical key value without colliding in the same cache.
func dedupeStorageKey(flowName, key string) string {
	return "dedupe:" + flowName + ":" + key
}

// dedupeLockKey is distinct from dedupeStorageKey so a user's outer
// lock { key = "..." } cannot collide with the dedupe-internal lock even
// if the same string is used as both the lock key and the dedupe key.
func dedupeLockKey(flowName, key string) string {
	return "dedupe:lock:" + flowName + ":" + key
}

// parseDedupeTTL is a small helper around time.ParseDuration that returns
// 0 (no expiry) on empty or invalid input. Validation against malformed
// strings happens at parse time; this is the runtime safety net.
func parseDedupeTTL(s string) time.Duration {
	if s == "" {
		return 0
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return 0
}
