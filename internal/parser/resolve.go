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

	for _, f := range c.Flows {
		if f.Dedupe != nil && f.Dedupe.Use != "" {
			merged, err := resolveDedupe(f.Dedupe, namedDedupes)
			if err != nil {
				return fmt.Errorf("flow %q: %w", f.Name, err)
			}
			f.Dedupe = merged
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
