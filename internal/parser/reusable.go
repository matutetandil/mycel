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

// reusableKinds is the registry. Order is mostly cosmetic (it sets the order of
// rootSchema entries and validation passes), with ONE semantic constraint:
// error_handling must come before retry. A reused error_handling can pull in a
// retry that itself carries `use = "retry.<name>"`; ResolveReferences materializes
// the error_handling first so the later retry pass sees and folds that retry.
// Hence retry is intentionally last.
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
	{
		typeName: "semaphore",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			s, err := parseNamedSemaphoreBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("semaphore parse error: %w", err)
			}
			cfg.NamedSemaphores = append(cfg.NamedSemaphores, s)
			cfg.recordSource("semaphore", s.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("semaphore", cfg.NamedSemaphores, func(s *flow.SemaphoreConfig) string { return s.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedSemaphores, func(s *flow.SemaphoreConfig) string { return s.Name })
			return func(f *flow.Config) error {
				if f.Semaphore == nil || f.Semaphore.Use == "" {
					return nil
				}
				merged, err := resolveRef("semaphore", f.Semaphore.Use, f.Semaphore, idx, mergeSemaphore)
				if err != nil {
					return err
				}
				f.Semaphore = merged
				return nil
			}
		},
	},
	{
		typeName: "sequence_guard",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			sg, err := parseNamedSequenceGuardBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("sequence_guard parse error: %w", err)
			}
			cfg.NamedSequenceGuards = append(cfg.NamedSequenceGuards, sg)
			cfg.recordSource("sequence_guard", sg.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("sequence_guard", cfg.NamedSequenceGuards, func(sg *flow.SequenceGuardConfig) string { return sg.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedSequenceGuards, func(sg *flow.SequenceGuardConfig) string { return sg.Name })
			return func(f *flow.Config) error {
				if f.SequenceGuard == nil || f.SequenceGuard.Use == "" {
					return nil
				}
				merged, err := resolveRef("sequence_guard", f.SequenceGuard.Use, f.SequenceGuard, idx, mergeSequenceGuard)
				if err != nil {
					return err
				}
				f.SequenceGuard = merged
				return nil
			}
		},
	},
	{
		typeName: "coordinate",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			c, err := parseNamedCoordinateBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("coordinate parse error: %w", err)
			}
			cfg.NamedCoordinates = append(cfg.NamedCoordinates, c)
			cfg.recordSource("coordinate", c.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("coordinate", cfg.NamedCoordinates, func(c *flow.CoordinateConfig) string { return c.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedCoordinates, func(c *flow.CoordinateConfig) string { return c.Name })
			return func(f *flow.Config) error {
				if f.Coordinate == nil || f.Coordinate.Use == "" {
					return nil
				}
				merged, err := resolveRef("coordinate", f.Coordinate.Use, f.Coordinate, idx, mergeCoordinate)
				if err != nil {
					return err
				}
				f.Coordinate = merged
				return nil
			}
		},
	},
	{
		typeName: "transaction",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			tx, err := parseNamedTransactionBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("transaction parse error: %w", err)
			}
			cfg.NamedTransactions = append(cfg.NamedTransactions, tx)
			cfg.recordSource("transaction", tx.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("transaction", cfg.NamedTransactions, func(tx *flow.TransactionConfig) string { return tx.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedTransactions, func(tx *flow.TransactionConfig) string { return tx.Name })
			resolveTo := func(to *flow.ToConfig) error {
				if to == nil || to.Transaction == nil || to.Transaction.Use == "" {
					return nil
				}
				merged, err := resolveRef("transaction", to.Transaction.Use, to.Transaction, idx, mergeTransaction)
				if err != nil {
					return err
				}
				to.Transaction = merged
				return nil
			}
			return func(f *flow.Config) error {
				// Transactions can live in the single `to` and in every
				// fan-out `to` (MultiTo); resolve them all.
				if err := resolveTo(f.To); err != nil {
					return err
				}
				for _, to := range f.MultiTo {
					if err := resolveTo(to); err != nil {
						return err
					}
				}
				return nil
			}
		},
	},
	{
		typeName: "error_handling",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			eh, err := parseNamedErrorHandlingBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("error_handling parse error: %w", err)
			}
			cfg.NamedErrorHandlings = append(cfg.NamedErrorHandlings, eh)
			cfg.recordSource("error_handling", eh.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("error_handling", cfg.NamedErrorHandlings, func(eh *flow.ErrorHandlingConfig) string { return eh.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedErrorHandlings, func(eh *flow.ErrorHandlingConfig) string { return eh.Name })
			return func(f *flow.Config) error {
				if f.ErrorHandling == nil || f.ErrorHandling.Use == "" {
					return nil
				}
				merged, err := resolveRef("error_handling", f.ErrorHandling.Use, f.ErrorHandling, idx, mergeErrorHandling)
				if err != nil {
					return err
				}
				f.ErrorHandling = merged
				return nil
			}
		},
	},
	{
		typeName: "accept",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			a, err := parseNamedAcceptBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("accept parse error: %w", err)
			}
			cfg.NamedAccepts = append(cfg.NamedAccepts, a)
			cfg.recordSource("accept", a.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("accept", cfg.NamedAccepts, func(a *flow.AcceptConfig) string { return a.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedAccepts, func(a *flow.AcceptConfig) string { return a.Name })
			return func(f *flow.Config) error {
				if f.Accept == nil || f.Accept.Use == "" {
					return nil
				}
				merged, err := resolveRef("accept", f.Accept.Use, f.Accept, idx, mergeAccept)
				if err != nil {
					return err
				}
				f.Accept = merged
				return nil
			}
		},
	},
	{
		// response is special: the named form is a *ResponseConfig, but a flow's
		// inline response is a bare map (Flow.Config.Response) with the reference
		// carried in the ResponseUse marker. So the resolver is bespoke — it
		// folds the named mappings under the flow's inline overrides — rather
		// than going through resolveRef.
		typeName: "response",
		parseRegister: func(p *HCLParser, block *hcl.Block, cfg *Configuration, path string) error {
			r, err := parseNamedResponseBlock(block, p.evalCtx)
			if err != nil {
				return fmt.Errorf("response parse error: %w", err)
			}
			cfg.NamedResponses = append(cfg.NamedResponses, r)
			cfg.recordSource("response", r.Name, path)
			return nil
		},
		uniqueKeys: func(cfg *Configuration) []string {
			return nameKeys("response", cfg.NamedResponses, func(r *flow.ResponseConfig) string { return r.Name })
		},
		newResolver: func(cfg *Configuration) func(f *flow.Config) error {
			idx := indexByName(cfg.NamedResponses, func(r *flow.ResponseConfig) string { return r.Name })
			return func(f *flow.Config) error {
				if f.ResponseUse == "" {
					return nil
				}
				base, ok := idx[f.ResponseUse]
				if !ok {
					return fmt.Errorf("response references unknown name %q (available: %s)", f.ResponseUse, availableNames(idx))
				}
				// Named mappings form the base; the flow's inline entries (if
				// any) override key by key, same as a transform reference.
				merged := make(map[string]string, len(base.Mappings)+len(f.Response))
				for k, v := range base.Mappings {
					merged[k] = v
				}
				for k, v := range f.Response {
					merged[k] = v
				}
				f.Response = merged
				return nil
			}
		},
	},
	{
		// retry is intentionally LAST — see the ordering note above. A retry
		// materialized onto a flow by the error_handling pass (carrying its
		// own `use`) is folded here.
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
