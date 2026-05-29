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
// Mutates the receiver in place. Idempotent: running twice yields the same
// result because the second pass finds an empty `Use` (cleared after merge).
func (c *Configuration) ResolveReferences() error {
	namedDedupes := indexByName(c.NamedDedupes, func(d *flow.DedupeConfig) string { return d.Name })
	namedRetries := indexByName(c.NamedRetries, func(r *flow.RetryConfig) string { return r.Name })
	namedLocks := indexByName(c.NamedLocks, func(l *flow.LockConfig) string { return l.Name })

	for _, f := range c.Flows {
		if f.Dedupe != nil && f.Dedupe.Use != "" {
			merged, err := resolveDedupe(f.Dedupe, namedDedupes)
			if err != nil {
				return fmt.Errorf("flow %q: %w", f.Name, err)
			}
			f.Dedupe = merged
		}
		if f.ErrorHandling != nil && f.ErrorHandling.Retry != nil && f.ErrorHandling.Retry.Use != "" {
			merged, err := resolveRetry(f.ErrorHandling.Retry, namedRetries)
			if err != nil {
				return fmt.Errorf("flow %q: %w", f.Name, err)
			}
			f.ErrorHandling.Retry = merged
		}
		if f.Lock != nil && f.Lock.Use != "" {
			merged, err := resolveLock(f.Lock, namedLocks)
			if err != nil {
				return fmt.Errorf("flow %q: %w", f.Name, err)
			}
			f.Lock = merged
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

// resolveDedupe overlays an inline dedupe block (with `use` set) on top of
// the named base it references. Returns the merged config or an error if
// the reference does not exist.
func resolveDedupe(inline *flow.DedupeConfig, named map[string]*flow.DedupeConfig) (*flow.DedupeConfig, error) {
	base, ok := named[inline.Use]
	if !ok {
		return nil, fmt.Errorf("dedupe references unknown name %q (available: %s)", inline.Use, availableNames(named))
	}

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
	return &merged, nil
}

// resolveRetry overlays an inline retry block (with `use` set) on top of the
// named base it references. Inline non-zero attributes win, attribute by
// attribute; unset inline fields inherit from the named base. Returns the
// merged config or an error if the reference does not exist.
func resolveRetry(inline *flow.RetryConfig, named map[string]*flow.RetryConfig) (*flow.RetryConfig, error) {
	base, ok := named[inline.Use]
	if !ok {
		return nil, fmt.Errorf("retry references unknown name %q (available: %s)", inline.Use, availableNames(named))
	}

	merged := *base
	merged.Name = ""        // inline blocks have no name
	merged.Use = inline.Use // preserve the reference for tracing/debugging

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
	return &merged, nil
}

// resolveLock overlays an inline lock block (with `use` set) on top of the
// named base it references. String attributes override when non-empty. The
// storage sub-block is replaced wholesale when the inline block defines one
// (no deep merge — see the v2.6 plan: sub-blocks replace, attributes overlay).
//
// Caveat for `wait`: a bool cannot distinguish "unset" from "false", so an
// inline `wait = true` overrides the base but an inline `wait = false` (or an
// omitted wait) inherits the base value. To force no-wait against a base that
// waits, define a separate named lock or inline the block fully. (The upcoming
// table-driven refactor can resolve this generically via presence tracking.)
func resolveLock(inline *flow.LockConfig, named map[string]*flow.LockConfig) (*flow.LockConfig, error) {
	base, ok := named[inline.Use]
	if !ok {
		return nil, fmt.Errorf("lock references unknown name %q (available: %s)", inline.Use, availableNames(named))
	}

	merged := *base
	merged.Name = ""        // inline blocks have no name
	merged.Use = inline.Use // preserve the reference for tracing/debugging

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
	return &merged, nil
}
