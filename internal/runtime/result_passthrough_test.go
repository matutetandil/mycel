package runtime

import (
	"testing"
)

// What a flow answers when it goes through the aspect executor.
//
// A flow with aspects in play is executed through them, and its result is
// carried across that boundary as a connector result. Anything that was not
// already a row or a map was turned into an empty one, so a saga's outcome and
// a state transition's — both structs — arrived as nothing, and the response
// builder rendered {"affected":0,"id":null}. It depended on whether the service
// had any aspect configured at all, which is not something the flow that
// answers should depend on.

type transitionAnswer struct {
	EntityID      string `json:"entity_id"`
	PreviousState string `json:"previous_state"`
	CurrentState  string `json:"current_state"`
}

func TestAResultThatIsNotARowSurvivesTheAspectBoundary(t *testing.T) {
	h := &FlowHandler{}

	result := h.resultToConnectorResult(&transitionAnswer{
		EntityID: "order-1", PreviousState: "pending", CurrentState: "paid",
	})

	if len(result.Rows) != 1 {
		t.Fatalf("%d rows, want the answer carried through", len(result.Rows))
	}
	if result.Rows[0]["current_state"] != "paid" {
		t.Errorf("row = %v, want the state the transition reached", result.Rows[0])
	}
	// Under its published name, not its Go one: this is what a caller reads.
	if _, ok := result.Rows[0]["CurrentState"]; ok {
		t.Error("the answer carries Go field names")
	}
}

func TestTheShapesAFlowAlreadyAnsweredWithAreUnchanged(t *testing.T) {
	h := &FlowHandler{}

	rows := h.resultToConnectorResult([]map[string]interface{}{{"id": 1}, {"id": 2}})
	if len(rows.Rows) != 2 {
		t.Errorf("%d rows, want both", len(rows.Rows))
	}

	one := h.resultToConnectorResult(map[string]interface{}{"id": 1})
	if len(one.Rows) != 1 {
		t.Errorf("%d rows, want one", len(one.Rows))
	}

	// A write result stays a write result rather than becoming a row saying
	// how many rows it wrote.
	write := h.resultToConnectorResult(map[string]interface{}{"affected": 3, "id": int64(7)})
	if write.Affected != 3 || write.LastID != int64(7) {
		t.Errorf("affected = %d, id = %v", write.Affected, write.LastID)
	}
	if len(write.Rows) != 0 {
		t.Errorf("a write result grew %d rows", len(write.Rows))
	}

	if got := h.resultToConnectorResult(nil); got == nil || len(got.Rows) != 0 {
		t.Errorf("nothing became %v", got)
	}
}

func TestAnAnswerWithNothingInItStaysEmpty(t *testing.T) {
	// A bare value carries no fields to name, so there is nothing to answer
	// with — and inventing a row for it would be worse than the empty result.
	h := &FlowHandler{}

	for name, value := range map[string]interface{}{
		"a number": 42,
		"a string": "done",
		"a list":   []int{1, 2},
	} {
		t.Run(name, func(t *testing.T) {
			if got := h.resultToConnectorResult(value); len(got.Rows) != 0 {
				t.Errorf("rows = %v", got.Rows)
			}
		})
	}
}
