package runtime

import (
	"fmt"
	"sort"

	"github.com/matutetandil/mycel/v3/internal/parser"
)

// InertFlowAttrs reports flow attributes that parse cleanly and then do
// nothing at all.
//
// These are worse than a syntax error: the config reads as if it configures
// something, `mycel validate` passes, the service starts, and the behaviour is
// simply absent. They are warnings rather than errors because rejecting them
// would break configs that run correctly today (the attribute is inert, not
// harmful), so this only makes the no-op visible.
//
// Returns one message per offending flow, sorted for stable output.
func InertFlowAttrs(config *parser.Configuration) []string {
	if config == nil {
		return nil
	}

	var warnings []string
	for _, f := range config.Flows {
		if f == nil {
			continue
		}

		// A transform with nowhere to send its output. A flow with no
		// destination answers its caller directly, and what it answers with is
		// the response block — the transform is read by nothing, so the caller
		// gets the raw request back, headers and all, while the file says the
		// fields were reshaped.
		if f.Transform != nil && len(f.Transform.Mappings) > 0 &&
			f.To == nil && len(f.MultiTo) == 0 && len(f.Steps) == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"flow %q: `transform` is ignored — a flow with no `to`, `step` or several "+
					"destinations answers its caller with the request as it arrived. Shape "+
					"what the caller receives in `response {}`",
				f.Name))
		}

		// `params` on a destination used to be reported here as inert, and
		// that warning was only ever half true: a flow with several
		// destinations honoured them and a flow with one did not, so the
		// attribute worked or did nothing depending on how many places the
		// flow wrote to. Both paths read them now, and the warning is gone
		// rather than corrected — there is nothing left to warn about.
	}

	sort.Strings(warnings)
	return warnings
}
