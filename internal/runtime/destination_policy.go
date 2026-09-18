package runtime

import (
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
