package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// Every place a connector is named, checked against the connectors that exist.
//
// A flow's `from` and `to` were checked when the flow was registered; nothing
// else was, and the twenty-odd other places behaved three different ways. A
// dedupe pointing at a connector nobody declared was refused at parse time. An
// idempotency block pointing at one started happily and failed on the first
// request that carried a key — in production rather than at deploy. A cache
// block pointing at one started happily and cached nothing, for ever, without
// a word: `getCacheConnector` looks the name up, gets an error, and returns
// nil, which every caller reads as "no cache configured".
//
// A name that is not there is a typo, and a typo should stop a deployment.

// connectorRefRule is one reference: where it was written and what it points at.
type connectorRefRule struct {
	where string // flow "x": cache.storage
	name  string
	// kind, when the reference only makes sense for one sort of connector. A
	// cache that caches into a database does nothing either.
	kind string
}

// ValidateConnectorReferences reports every named connector that does not
// exist, and every one that exists but cannot do the job it was named for.
func ValidateConnectorReferences(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	declared := make(map[string]string, len(config.Connectors)) // name -> type
	for _, c := range config.Connectors {
		if c != nil {
			declared[c.Name] = c.Type
		}
	}

	var errs []error
	for _, ref := range collectConnectorRefs(config) {
		if ref.name == "" {
			continue
		}
		// A reference carrying an expression is resolved when the flow runs.
		if strings.ContainsAny(ref.name, "${") {
			continue
		}

		actual, exists := declared[ref.name]
		if !exists {
			errs = append(errs, fmt.Errorf("%s names connector %q, which no connector block declares%s",
				ref.where, ref.name, nearestName(ref.name, declared)))
			continue
		}
		if ref.kind != "" && actual != ref.kind {
			errs = append(errs, fmt.Errorf("%s names connector %q, which is a %s connector and not a %s one",
				ref.where, ref.name, actual, ref.kind))
		}
	}
	return errs
}

// nearestName offers the declared name closest to what was written, which is
// usually the one that was meant.
func nearestName(written string, declared map[string]string) string {
	best, bestScore := "", 0
	for name := range declared {
		score := commonPrefixLen(written, name)
		if score > bestScore {
			best, bestScore = name, score
		}
	}
	// Only when there is something to go on: an unrelated name is worse than
	// no suggestion.
	if bestScore >= 3 {
		return fmt.Sprintf(" (did you mean %q?)", best)
	}

	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return fmt.Sprintf(" (declared: %s)", strings.Join(names, ", "))
}

func commonPrefixLen(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// collectConnectorRefs walks the configuration for every connector name in it.
func collectConnectorRefs(config *parser.Configuration) []connectorRefRule {
	var refs []connectorRefRule

	add := func(where, name, kind string) {
		refs = append(refs, connectorRefRule{where: where, name: name, kind: kind})
	}

	for _, f := range config.Flows {
		if f == nil {
			continue
		}
		at := func(what string) string { return fmt.Sprintf("flow %q: %s", f.Name, what) }

		if f.From != nil {
			add(at("from.connector"), f.From.GetConnector(), "")
		}
		if f.To != nil {
			add(at("to.connector"), f.To.Connector, "")
		}
		for i, to := range f.MultiTo {
			if to != nil {
				add(at(fmt.Sprintf("to[%d].connector", i)), to.Connector, "")
			}
		}
		for i, step := range f.Steps {
			if step != nil {
				add(at(fmt.Sprintf("step %q.connector", stepName(step, i))), step.Connector, "")
			}
		}
		for i, e := range f.Enrichments {
			if e != nil {
				add(at(fmt.Sprintf("enrich[%d].connector", i)), e.Connector, "")
			}
		}
		// The ones that cache: a database here does nothing at all.
		if f.Cache != nil {
			add(at("cache.storage"), f.Cache.Storage, "cache")
		}
		if f.Dedupe != nil {
			add(at("dedupe.cache"), f.Dedupe.Cache, "cache")
		}
		if f.Idempotency != nil {
			add(at("idempotency.storage"), f.Idempotency.Storage, "cache")
		}
		if f.Async != nil {
			add(at("async.storage"), f.Async.Storage, "cache")
		}
		if f.After != nil && f.After.Invalidate != nil {
			add(at("after.invalidate.storage"), f.After.Invalidate.Storage, "cache")
		}
		if f.ErrorHandling != nil && f.ErrorHandling.Fallback != nil {
			add(at("error_handling.fallback.connector"), f.ErrorHandling.Fallback.Connector, "")
		}
		if f.Coordinate != nil && f.Coordinate.Preflight != nil {
			add(at("coordinate.preflight.connector"), f.Coordinate.Preflight.Connector, "")
		}
		if f.Batch != nil {
			add(at("batch.source"), f.Batch.Source, "")
		}
	}

	for _, c := range config.NamedCaches {
		if c != nil {
			add(fmt.Sprintf("cache %q: storage", c.Name), c.Storage, "cache")
		}
	}

	for _, a := range config.Aspects {
		if a == nil || a.Action == nil {
			continue
		}
		add(fmt.Sprintf("aspect %q: action.connector", a.Name), a.Action.Connector, "")
	}

	return refs
}

// stepName is what to call a step in a message: its own name, or its position.
func stepName(step *flow.StepConfig, i int) string {
	if step.Name != "" {
		return step.Name
	}
	return fmt.Sprintf("%d", i)
}
