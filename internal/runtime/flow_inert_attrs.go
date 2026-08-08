package runtime

import (
	"fmt"
	"sort"

	"github.com/matutetandil/mycel/v2/internal/parser"
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

		// `params` on a `to` block is never read. ToConfig.GetParams() exists
		// but has no call site: a write takes its payload from the transform
		// output (or the raw input when there is no transform). The attribute
		// is real on `step`, on `enrich`, and on `exec` inside a
		// `transaction`, which is exactly why it gets copied onto `to`.
		if f.To != nil {
			if _, ok := f.To.ConnectorParams["params"]; ok {
				warnings = append(warnings, fmt.Sprintf(
					"flow %q: `params` on a to block is ignored — a write sends the transform "+
						"output (or the raw input when the flow has no transform). Shape the "+
						"payload in transform {}, or use step {} / transaction { exec {} }, "+
						"where params is read",
					f.Name))
			}
		}
		for i, to := range f.MultiTo {
			if to == nil {
				continue
			}
			if _, ok := to.ConnectorParams["params"]; ok {
				warnings = append(warnings, fmt.Sprintf(
					"flow %q: `params` on to block #%d (connector %q) is ignored — a write sends "+
						"the transform output (or the raw input when the flow has no transform). "+
						"Shape the payload in transform {}, or use step {} / "+
						"transaction { exec {} }, where params is read",
					f.Name, i+1, to.Connector))
			}
		}
	}

	sort.Strings(warnings)
	return warnings
}
