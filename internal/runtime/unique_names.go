package runtime

import (
	"fmt"
	"sort"

	"github.com/matutetandil/mycel/v2/internal/parser"
)

// Names that have to be unique because something is keyed by them.
//
// Top-level names — connectors, flows, types, transforms, aspects, validators —
// were checked. The names inside a flow were not, and every one of them is a
// map key at run time: a step's result is stored under `stepResults[name]`, an
// enrichment's under `enriched[name]`, a saga step's under `Steps[name]`.
//
// So two steps called the same thing both ran, the second overwrote the first,
// and `step.detail` in a transform meant whichever came last. The service
// worked, answered, and was wrong — while paying for a query whose result was
// thrown away on every request. That is a copy-paste away in any flow with
// more than one step.

// ValidateUniqueInnerNames reports names repeated where something is keyed by
// them.
func ValidateUniqueInnerNames(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	var errs []error

	for _, f := range config.Flows {
		if f == nil {
			continue
		}

		errs = append(errs, duplicatesIn(
			fmt.Sprintf("flow %q", f.Name), "step",
			namesOf(len(f.Steps), func(i int) string { return f.Steps[i].Name }),
			"a step's result is stored under its name, so the second overwrites the first "+
				"and every reference to it means the last one")...)

		errs = append(errs, duplicatesIn(
			fmt.Sprintf("flow %q", f.Name), "enrich",
			namesOf(len(f.Enrichments), func(i int) string { return f.Enrichments[i].Name }),
			"an enrichment's result is stored under its name")...)
	}

	for _, s := range config.Sagas {
		if s == nil {
			continue
		}
		errs = append(errs, duplicatesIn(
			fmt.Sprintf("saga %q", s.Name), "step",
			namesOf(len(s.Steps), func(i int) string { return s.Steps[i].Name }),
			"a saga step's result is stored under its name, and its compensation is found by it")...)
	}

	return errs
}

// namesOf collects n names through an accessor, which keeps the caller from
// repeating a loop per kind.
func namesOf(n int, at func(int) string) []string {
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		names = append(names, at(i))
	}
	return names
}

// duplicatesIn reports each name used more than once.
func duplicatesIn(where, kind string, names []string, why string) []error {
	seen := make(map[string]int, len(names))
	for _, name := range names {
		if name != "" {
			seen[name]++
		}
	}

	repeated := make([]string, 0)
	for name, n := range seen {
		if n > 1 {
			repeated = append(repeated, name)
		}
	}
	if len(repeated) == 0 {
		return nil
	}
	sort.Strings(repeated)

	errs := make([]error, 0, len(repeated))
	for _, name := range repeated {
		errs = append(errs, fmt.Errorf("%s: %s %q is declared %d times, and %s",
			where, kind, name, seen[name], why))
	}
	return errs
}
