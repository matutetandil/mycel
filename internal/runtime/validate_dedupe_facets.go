package runtime

import (
	"fmt"
	"sort"

	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// ValidateDedupeFacets checks that facets and the destinations naming them
// agree.
//
// Both mistakes it catches are the same shape: something written in the
// configuration that reads as if it were doing work and is not. A `to` naming
// a facet nobody declared would never run, because the set of changed facets
// can never contain a name that does not exist — the destination is skipped on
// every message, silently, for as long as the flow is deployed. A facet no
// destination names can never be committed, because commit is per facet and
// there is nothing to attribute success to — so it reads as changed for ever
// and the message is never dropped, which quietly disables deduplication.
//
// Neither fails at run time. Both would look like a flow that works.
func ValidateDedupeFacets(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	var errs []error
	for _, f := range config.Flows {
		if f == nil || f.Dedupe == nil || len(f.Dedupe.Facets) == 0 {
			continue
		}

		declared := make(map[string]bool, len(f.Dedupe.Facets))
		for _, facet := range f.Dedupe.Facets {
			declared[facet.Name] = true
		}

		named := make(map[string]bool, len(declared))
		for _, dest := range destinationsOf(f) {
			if dest.Facet == "" {
				continue
			}
			if !declared[dest.Facet] {
				errs = append(errs, fmt.Errorf(
					"flow %q: a destination names facet %q, which the dedupe block does not declare — "+
						"it would be skipped on every message. Declared: %s",
					f.Name, dest.Facet, listOf(declared)))
				continue
			}
			named[dest.Facet] = true
		}

		for _, facet := range f.Dedupe.Facets {
			if !named[facet.Name] {
				errs = append(errs, fmt.Errorf(
					"flow %q: dedupe declares facet %q and no destination names it — "+
						"nothing could commit it, so it would read as changed for ever and the flow would never "+
						"drop a duplicate. Add `facet = %q` to the destination it belongs to",
					f.Name, facet.Name, facet.Name))
			}
		}
	}
	return errs
}

// destinationsOf returns a flow's destinations whichever way it declares them.
func destinationsOf(f *flow.Config) []*flow.ToConfig {
	if len(f.MultiTo) > 0 {
		return f.MultiTo
	}
	if f.To != nil {
		return []*flow.ToConfig{f.To}
	}
	return nil
}

func listOf(names map[string]bool) string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, fmt.Sprintf("%q", name))
	}
	sort.Strings(out)
	if len(out) == 0 {
		return "none"
	}
	return joinWithCommas(out)
}

func joinWithCommas(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
