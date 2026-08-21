package runtime

import (
	"log/slog"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
)

// reportIgnoredDestinations says which `to` blocks are not going to be used.
//
// A read flow with steps answers out of its steps: the destination is neither
// read nor written, and nothing said so. The use-cases guide had a recipe built
// on the opposite belief — read the user, then a step fetches the weather for
// their city — which answered "no such key: name" for every field it was
// supposed to fill, because the read never happened.
//
// The combination is not refused, because a `to` in a steps flow is often just
// left over, and refusing would stop services that work. It is said out loud
// at startup instead, once, naming the flow.
func reportIgnoredDestinations(logger *slog.Logger, flows []*flow.Config, connectors *connector.Registry) {
	if logger == nil {
		return
	}

	for _, f := range flows {
		if f == nil || len(f.Steps) == 0 || f.To == nil || f.To.Connector == "" {
			continue
		}
		if f.From == nil {
			continue
		}
		if !parseOperation(f.From.GetOperation()).IsRead() {
			continue
		}
		// A destination that can only be written to is used: the flow renders
		// its answer through it, which is what the PDF connector is for.
		if connectors != nil {
			if dest, err := connectors.Get(f.To.Connector); err == nil && rendersTheAnswer(dest) {
				continue
			}
		}

		logger.Warn("a read flow with steps does not use its destination",
			"flow", f.Name,
			"destination", f.To.Connector,
			"meaning", "the answer is built from the steps and the transform; the to block is neither read nor written",
			"hint", "to read the destination and add to what came back, use an enrich block instead of a step")
	}
}
