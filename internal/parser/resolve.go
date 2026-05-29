package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/internal/flow"
)

// ResolveReferences walks every flow and folds reusable-block references
// (e.g. `dedupe { use = "dedupe.standard"; ttl = "1h" }`) into a single
// self-contained block per flow. It does two things in one pass:
//
//  1. **Validation.** Every `use` value must name a registered top-level
//     block of the matching kind. A typo here would otherwise survive parse
//     and only blow up when the flow is dispatched at runtime — too late.
//     The error message lists the available names to make typos obvious.
//
//  2. **Merge.** The named block is taken as the base, the inline block's
//     non-zero attributes overlay it. Map-typed fields (e.g. dedupe's
//     fingerprint) are merged key by key. After this pass, the runtime
//     never has to know whether a block came from a named reference or
//     was written inline — Config.Dedupe etc. are always complete.
//
// The set of reusable kinds lives in the reusableKinds registry; this loop is
// generic over them. Each kind builds its name index once (in newResolver),
// not per flow. Mutates the receiver in place. Idempotent: a second pass finds
// an empty `Use` on the already-folded blocks and is a no-op.
func (c *Configuration) ResolveReferences() error {
	resolvers := make([]func(*flow.Config) error, len(reusableKinds))
	for i, k := range reusableKinds {
		resolvers[i] = k.newResolver(c)
	}

	for _, f := range c.Flows {
		for _, resolve := range resolvers {
			if err := resolve(f); err != nil {
				return fmt.Errorf("flow %q: %w", f.Name, err)
			}
		}
	}
	return nil
}

// indexByName builds a name → entry map from a slice. The key function lets
// callers reuse the helper across different Named* types without reflection.
func indexByName[T any](items []*T, name func(*T) string) map[string]*T {
	out := make(map[string]*T, len(items))
	for _, item := range items {
		out[name(item)] = item
	}
	return out
}

// availableNames returns a sorted, comma-joined list of map keys for use in
// "did you mean" style error messages.
func availableNames[T any](m map[string]*T) string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(none defined)"
	}
	return strings.Join(names, ", ")
}

// mergeDedupe overlays an inline dedupe block (with `use` set) on top of the
// named base it references. Scalars override when non-empty; the fingerprint
// map is merged key by key (inline wins per key, named-only keys preserved).
func mergeDedupe(base, inline *flow.DedupeConfig) *flow.DedupeConfig {
	merged := *base
	merged.Name = ""        // inline blocks have no name
	merged.Use = inline.Use // preserve the reference for tracing/debugging

	if inline.Cache != "" {
		merged.Cache = inline.Cache
	}
	if inline.Key != "" {
		merged.Key = inline.Key
	}
	if inline.TTL != "" {
		merged.TTL = inline.TTL
	}
	if inline.OnDuplicate != "" {
		merged.OnDuplicate = inline.OnDuplicate
	}
	if len(inline.Fingerprint) > 0 {
		merged.Fingerprint = make(map[string]string, len(base.Fingerprint)+len(inline.Fingerprint))
		for k, v := range base.Fingerprint {
			merged.Fingerprint[k] = v
		}
		for k, v := range inline.Fingerprint {
			merged.Fingerprint[k] = v
		}
	}
	return &merged
}

// mergeRetry overlays an inline retry block on top of the named base it
// references. Inline non-zero attributes win; unset inline fields inherit.
func mergeRetry(base, inline *flow.RetryConfig) *flow.RetryConfig {
	merged := *base
	merged.Name = ""
	merged.Use = inline.Use

	if inline.Attempts != 0 {
		merged.Attempts = inline.Attempts
	}
	if inline.Delay != "" {
		merged.Delay = inline.Delay
	}
	if inline.MaxDelay != "" {
		merged.MaxDelay = inline.MaxDelay
	}
	if inline.Backoff != "" {
		merged.Backoff = inline.Backoff
	}
	return &merged
}

// mergeLock overlays an inline lock block on top of the named base it
// references. String attributes override when non-empty. The storage sub-block
// is replaced wholesale when the inline block defines one (no deep merge — see
// the v2.6 plan: sub-blocks replace, attributes overlay).
//
// Caveat for `wait`: a bool cannot distinguish "unset" from "false", so an
// inline `wait = true` overrides the base but an inline `wait = false` (or an
// omitted wait) inherits the base value. To force no-wait against a base that
// waits, define a separate named lock or inline the block fully.
func mergeLock(base, inline *flow.LockConfig) *flow.LockConfig {
	merged := *base
	merged.Name = ""
	merged.Use = inline.Use

	if inline.Key != "" {
		merged.Key = inline.Key
	}
	if inline.Timeout != "" {
		merged.Timeout = inline.Timeout
	}
	if inline.Retry != "" {
		merged.Retry = inline.Retry
	}
	if inline.Wait {
		merged.Wait = true
	}
	if inline.Storage != nil {
		merged.Storage = inline.Storage
	}
	return &merged
}

// mergeSemaphore overlays an inline semaphore block on top of the named base.
// Scalars override when non-zero; the storage sub-block is replaced wholesale.
func mergeSemaphore(base, inline *flow.SemaphoreConfig) *flow.SemaphoreConfig {
	merged := *base
	merged.Name = ""
	merged.Use = inline.Use

	if inline.Key != "" {
		merged.Key = inline.Key
	}
	if inline.MaxPermits != 0 {
		merged.MaxPermits = inline.MaxPermits
	}
	if inline.Timeout != "" {
		merged.Timeout = inline.Timeout
	}
	if inline.Lease != "" {
		merged.Lease = inline.Lease
	}
	if inline.Storage != nil {
		merged.Storage = inline.Storage
	}
	return &merged
}

// mergeCoordinate overlays an inline coordinate block on top of the named
// base. Scalars override when non-zero; each sub-block (storage/wait/signal/
// preflight) is replaced wholesale when the inline block defines it.
func mergeCoordinate(base, inline *flow.CoordinateConfig) *flow.CoordinateConfig {
	merged := *base
	merged.Name = ""
	merged.Use = inline.Use

	if inline.Timeout != "" {
		merged.Timeout = inline.Timeout
	}
	if inline.OnTimeout != "" {
		merged.OnTimeout = inline.OnTimeout
	}
	if inline.MaxRetries != 0 {
		merged.MaxRetries = inline.MaxRetries
	}
	if inline.MaxConcurrentWaits != 0 {
		merged.MaxConcurrentWaits = inline.MaxConcurrentWaits
	}
	if inline.Storage != nil {
		merged.Storage = inline.Storage
	}
	if inline.Wait != nil {
		merged.Wait = inline.Wait
	}
	if inline.Signal != nil {
		merged.Signal = inline.Signal
	}
	if inline.Preflight != nil {
		merged.Preflight = inline.Preflight
	}
	return &merged
}

// mergeTransaction overlays an inline transaction reference on top of the named
// base. A transaction has no scalar fields to overlay: if the referencing block
// lists its own statements they replace the named base's wholesale, otherwise
// the base's statements are used as-is.
func mergeTransaction(base, inline *flow.TransactionConfig) *flow.TransactionConfig {
	merged := *base
	merged.Name = ""
	merged.Use = inline.Use

	if len(inline.Statements) > 0 {
		merged.Statements = inline.Statements
	}
	return &merged
}

// mergeErrorHandling overlays an inline error_handling block on top of the
// named base. Every member is a sub-block, so each is replaced wholesale when
// the inline block defines it. A retry pulled in here that itself carries a
// `use` is resolved by the retry pass, which runs after this one (see the
// reusableKinds registry ordering).
func mergeErrorHandling(base, inline *flow.ErrorHandlingConfig) *flow.ErrorHandlingConfig {
	merged := *base
	merged.Name = ""
	merged.Use = inline.Use

	if inline.Retry != nil {
		merged.Retry = inline.Retry
	}
	if inline.Fallback != nil {
		merged.Fallback = inline.Fallback
	}
	if inline.ErrorResponse != nil {
		merged.ErrorResponse = inline.ErrorResponse
	}
	if inline.OnTimeout != nil {
		merged.OnTimeout = inline.OnTimeout
	}
	if inline.OnError != nil {
		merged.OnError = inline.OnError
	}
	return &merged
}

// mergeSequenceGuard overlays an inline sequence_guard block on top of the
// named base. Scalars override when non-empty; the storage sub-block is
// replaced wholesale.
func mergeSequenceGuard(base, inline *flow.SequenceGuardConfig) *flow.SequenceGuardConfig {
	merged := *base
	merged.Name = ""
	merged.Use = inline.Use

	if inline.Key != "" {
		merged.Key = inline.Key
	}
	if inline.Sequence != "" {
		merged.Sequence = inline.Sequence
	}
	if inline.OnOlder != "" {
		merged.OnOlder = inline.OnOlder
	}
	if inline.TTL != "" {
		merged.TTL = inline.TTL
	}
	if inline.Storage != nil {
		merged.Storage = inline.Storage
	}
	return &merged
}
