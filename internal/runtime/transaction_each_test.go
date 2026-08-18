package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// What an `each` does when its expression is not a list.
//
// Every value that was not a slice made the loop a no-op that reported
// success, so a transaction writing a parent and its children wrote the parent,
// wrote no children, committed, and the flow acknowledged its message. The
// commonest way to arrive there is a queue message whose array came through as
// a string — the same shape that catches people writing CEL against these
// payloads — and nothing anywhere said so.

func eachOver(in string) *flow.TransactionConfig {
	return &flow.TransactionConfig{Statements: []flow.TxStatement{
		mkExec(`INSERT INTO parent (owner_id, name) VALUES (:owner, :name)`, "parent_id", "",
			map[string]string{"owner": "output.owner_id", "name": "output.name"}),
		{Each: &flow.TxEach{
			Var: "child", In: in,
			Body: []flow.TxStatement{
				mkExec(`INSERT INTO child (parent_id, label, position) VALUES (:pid, :label, :pos)`, "", "",
					map[string]string{"pid": "captured.parent_id", "label": "child.label", "pos": "child_index"}),
			},
		}},
	}}
}

func TestAnEachOverSomethingThatIsNotAListIsRefused(t *testing.T) {
	// Each of these is a real way to get here, and each used to write the
	// parent and quietly skip every child.
	for name, children := range map[string]interface{}{
		"a list that arrived as a string": `[{"label":"a"},{"label":"b"}]`,
		"a single object, not wrapped":    map[string]interface{}{"label": "a"},
		"a number":                        42,
		"a bare string":                   "a",
	} {
		t.Run(name, func(t *testing.T) {
			h, db := newTxHandler(t, eachOver("output.children"))

			_, err := h.executeFlowCore(context.Background(), map[string]interface{}{
				"owner_id": 7, "name": "agg", "children": children,
			})
			if err == nil {
				t.Fatal("the transaction reported success having written no children")
			}
			// The message has to name the expression and what it found, or
			// somebody is looking at a payload wondering which field is wrong.
			if !strings.Contains(err.Error(), "output.children") {
				t.Errorf("the error does not name the expression: %v", err)
			}

			// And nothing was committed: a parent without its children is
			// worse than no parent at all.
			if got := count(t, db, "parent"); got != 0 {
				t.Errorf("parent rows = %d after a failed transaction, want 0", got)
			}
		})
	}
}

func TestAnEachOverNothingIsStillANoOp(t *testing.T) {
	// A field that is absent, or an empty list, is not a mistake: an order
	// with no discounts should write the order.
	for name, input := range map[string]map[string]interface{}{
		"the field is not there": {"owner_id": 7, "name": "agg"},
		"an empty list":          {"owner_id": 7, "name": "agg", "children": []interface{}{}},
		"an explicit null":       {"owner_id": 7, "name": "agg", "children": nil},
	} {
		t.Run(name, func(t *testing.T) {
			h, db := newTxHandler(t, eachOver("output.children"))

			if _, err := h.executeFlowCore(context.Background(), input); err != nil {
				t.Fatalf("a transaction with nothing to loop over failed: %v", err)
			}
			if got := count(t, db, "parent"); got != 1 {
				t.Errorf("parent rows = %d, want the parent written", got)
			}
			if got := count(t, db, "child"); got != 0 {
				t.Errorf("child rows = %d, want none", got)
			}
		})
	}
}

func TestAnEachOverATypedSliceWorks(t *testing.T) {
	// What a database step hands back is not []interface{} — it is a slice of
	// maps — and an each over the result of one is the ordinary case.
	h, db := newTxHandler(t, eachOver("output.children"))

	_, err := h.executeFlowCore(context.Background(), map[string]interface{}{
		"owner_id": 7, "name": "agg",
		"children": []map[string]interface{}{
			{"label": "a"}, {"label": "b"},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := count(t, db, "child"); got != 2 {
		t.Errorf("child rows = %d, want 2 from a typed slice", got)
	}
}
