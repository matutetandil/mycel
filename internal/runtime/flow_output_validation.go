package runtime

import (
	"context"
	"fmt"
)

// validateResult checks a flow's answer against the type its `validate` block
// names for the output.
//
// validateOutput was written, complete, and called by nothing. A flow that
// declared an output contract had it checked nowhere, so a transform producing
// the wrong shape — a required field missing, a number where a name belongs —
// reached the destination or the caller with the contract meaning nothing.
//
// The asymmetry is what made it worse than a missing feature: the input side
// was always checked, so a `validate` block that refuses a bad request looks
// like it is doing both halves of its job. The debugger even carries a stage
// for the output check, which never fired.
//
// An answer is not always one record. A read returns rows, and the type
// describes a record, so each row is checked against it; anything that is
// neither is left alone, since there is nothing to check it against.
func (h *FlowHandler) validateResult(ctx context.Context, result interface{}) error {
	if h.Config.Validate == nil || h.Config.Validate.Output == "" {
		return nil
	}

	switch value := result.(type) {
	case nil:
		return nil

	case map[string]interface{}:
		return h.validateOutput(ctx, value)

	case []map[string]interface{}:
		for i, row := range value {
			if err := h.validateOutput(ctx, row); err != nil {
				return fmt.Errorf("record %d: %w", i, err)
			}
		}
		return nil

	case []interface{}:
		for i, item := range value {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if err := h.validateOutput(ctx, row); err != nil {
				return fmt.Errorf("record %d: %w", i, err)
			}
		}
		return nil
	}

	return nil
}
