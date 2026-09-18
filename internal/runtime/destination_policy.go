package runtime

import (
	"context"
	"fmt"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
)

// What a destination says about the record it writes, beyond its contents:
// which fields identify it, what to do when the store already holds it, and
// how long it stays.
//
// This is read in one place because the write intent used to be derived in
// two, and the second one went on deriving it from the source alone long
// after the first learned better. A destination's policy is asked for by
// every write path — a single `to`, one of several, an aspect's action — and
// they have to answer the same.
func applyDestinationPolicy(data *connector.Data, to *flow.ToConfig) error {
	if data == nil || to == nil {
		return nil
	}

	data.ConflictKey = to.GetConflictKey()
	data.OnConflict = to.GetOnConflict()

	if written := to.GetTTL(); written != "" {
		ttl, err := flow.ParseDuration(written)
		if err != nil {
			// Reported rather than defaulted to zero: a ttl that silently
			// means "no expiry" is the one failure nobody notices, because
			// everything keeps working and the store keeps growing.
			return fmt.Errorf("ttl %q: %w", written, err)
		}
		data.TTL = ttl
	}

	return nil
}

// applyDestinationParams hands a destination the extra parameters it declares.
//
// These are the connector's own vocabulary — the sheet of a spreadsheet, the
// exchange of a publish, whether a file is appended to — and they were read on
// exactly one write path. A flow with several destinations honoured them; the
// same block on a flow with one destination was swept up and ignored, so the
// attribute worked or did nothing depending on how many places the flow wrote
// to, which is not a distinction anybody would think to make.
//
// Values are resolved the way a filter document is: `input.x` is evaluated,
// ":name" is the path parameter of that name, and anything else is the literal
// it looks like.
func (h *FlowHandler) applyDestinationParams(
	ctx context.Context,
	data *connector.Data,
	to *flow.ToConfig,
	input map[string]interface{},
) error {
	if data == nil || to == nil || len(to.GetParams()) == 0 {
		return nil
	}

	params, err := h.resolveFilterDocument(ctx, to.GetParams(), input)
	if err != nil {
		return fmt.Errorf("params: %w", err)
	}
	data.Params = params
	return nil
}
