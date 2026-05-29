package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/matutetandil/mycel/internal/flow"
)

// reusableKind describes one nameable + referenceable inline block (dedupe,
// retry, lock, ...). A single entry drives every cross-cutting concern so the
// generic plumbing never has to grow per kind:
//
//   - rootSchema registers `<typeName> "<name>" {}` as a top-level block.
//   - ParseFile dispatches a top-level block of this type to parseRegister.
//   - ValidateUniqueNames feeds uniqueKeys into its duplicate-name check.
//   - ResolveReferences runs newResolver against every flow.
//
// Adding a new reusable block is then: embed flow.Reusable in its *Config, add
// the typed slice to Configuration (+ NewConfiguration + Merge), and append one
// entry here backed by a parseNamed*/merge* pair. No edits to the loops.
type reusableKind struct {
	// typeName is the HCL block type and the `use = "<typeName>.<name>"`
	// namespace (also the prefix used for SourceFiles and uniqueness keys).
	typeName string

	// parseRegister parses a top-level `<typeName> "<name>" { ... }` block,
	// appends it to the matching Configuration slice, and records its source
	// file for duplicate-name diagnostics.
	parseRegister func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error

	// uniqueKeys returns the "<typeName>:<name>" keys for the registered
	// blocks of this kind, consumed by ValidateUniqueNames.
	uniqueKeys func(cfg *Configuration) []string

	// newResolver builds a per-flow resolver. The index over named blocks is
	// built once (here, not per flow); the returned closure folds a flow's
	// reference of this kind into a self-contained block, or is a no-op when
	// the flow does not reference this kind.
	newResolver func(cfg *Configuration) func(f *flow.Config) error
}

// reusableKinds is the registry. Order only affects the deterministic order of
// rootSchema entries and validation passes; it has no semantic effect.
var reusableKinds = []reusableKind{
	{
		typeName: "dedupe",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			d, err := parseNamedDedupeBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("dedupe parse error: %w", err)
			}
			cfg.NamedDedupes = append(cfg.NamedDedupes, d)
			cfg.recordSource("dedupe", d.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("dedupe", cfg.NamedDedupes, func(d *flow.DedupeConfig) string { return d.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedDedupes, func(d *flow.DedupeConfig) string { return d.Name })
			return func(f *flow.Config) error {
				if f.Dedupe == nil || f.Dedupe.Use == "" {
					return nil
				}
				merged, err := resolveRef("dedupe", f.Dedupe.Use, f.Dedupe, idx, mergeDedupe)
				if err != nil {
					return err
				}
				f.Dedupe = merged
				return nil
			}
		},
	},
	{
		typeName: "retry",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			r, err := parseNamedRetryBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("retry parse error: %w", err)
			}
			cfg.NamedRetries = append(cfg.NamedRetries, r)
			cfg.recordSource("retry", r.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("retry", cfg.NamedRetries, func(r *flow.RetryConfig) string { return r.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedRetries, func(r *flow.RetryConfig) string { return r.Name })
			return func(f *flow.Config) error {
				if f.ErrorHandling == nil || f.ErrorHandling.Retry == nil || f.ErrorHandling.Retry.Use == "" {
					return nil
				}
				merged, err := resolveRef("retry", f.ErrorHandling.Retry.Use, f.ErrorHandling.Retry, idx, mergeRetry)
				if err != nil {
					return err
				}
				f.ErrorHandling.Retry = merged
				return nil
			}
		},
	},
	{
		typeName: "lock",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			l, err := parseNamedLockBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("lock parse error: %w", err)
			}
			cfg.NamedLocks = append(cfg.NamedLocks, l)
			cfg.recordSource("lock", l.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("lock", cfg.NamedLocks, func(l *flow.LockConfig) string { return l.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedLocks, func(l *flow.LockConfig) string { return l.Name })
			return func(f *flow.Config) error {
				if f.Lock == nil || f.Lock.Use == "" {
					return nil
				}
				merged, err := resolveRef("lock", f.Lock.Use, f.Lock, idx, mergeLock)
				if err != nil {
					return err
				}
				f.Lock = merged
				return nil
			}
		},
	},
}

// reusableKindByName indexes the registry by HCL block type for O(1) dispatch
// in ParseFile. Built once at init; depends on reusableKinds being initialized
// first, which Go guarantees by package-var dependency order.
var reusableKindByName = func() map[string]reusableKind {
	m := make(map[string]reusableKind, len(reusableKinds))
	for _, k := range reusableKinds {
		m[k.typeName] = k
	}
	return m
}()

// resolveRef looks up the named base an inline block references and folds the
// inline overrides on top of it via the kind-specific merge function. The
// lookup + "did you mean" error is shared; only merge differs per kind.
func resolveRef[T any](kind, use string, inline *T, named map[string]*T, merge func(base, inline *T) *T) (*T, error) {
	base, ok := named[use]
	if !ok {
		return nil, fmt.Errorf("%s references unknown name %q (available: %s)", kind, use, availableNames(named))
	}
	return merge(base, inline), nil
}

// nameKeys builds the "<kind>:<name>" uniqueness keys for a slice of named
// blocks. The name accessor lets one helper serve every kind without
// reflection.
func nameKeys[T any](kind string, items []*T, name func(*T) string) []string {
	keys := make([]string, len(items))
	for i, it := range items {
		keys[i] = kind + ":" + name(it)
	}
	return keys
}

// recordSource appends a source file under the "<kind>:<name>" key used by
// ValidateUniqueNames to report where each definition lives.
func (c *Configuration) recordSource(kind, name, path string) {
	key := kind + ":" + name
	c.SourceFiles[key] = append(c.SourceFiles[key], path)
}
