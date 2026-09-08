package runtime

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// A GraphQL field that returns a list, answered by a transform, must still run
// its steps.
//
// Step skipping matched the requested field names against the transform's
// mapping names. For a list field the client asks for the fields of the
// element and never for the mapping that holds the list, so nothing matched,
// every step was skipped, and the transform ran against nulls: an empty list,
// HTTP 200, nothing in the log. The request came back in a few milliseconds
// against a table whose query takes far longer, which is the only outward sign.

func listFieldHandler(t *testing.T, mappings map[string]string) (*FlowHandler, *readWriteConnector) {
	t.Helper()
	db := &readWriteConnector{name: "db"}
	h := writingHandler(t, []*flow.StepConfig{{
		Name: "rows", Connector: "db",
		ConnectorParams: map[string]interface{}{"query": "SELECT name FROM items"},
	}}, db)
	h.Config.Transform = &flow.TransformConfig{Mappings: mappings}
	return h, db
}

func TestAListFieldsStepsAreNotSkipped(t *testing.T) {
	h, db := listFieldHandler(t, map[string]string{
		"items": "as_list(step.rows).map(r, {'name': r.name})",
	})

	// What a GraphQL query for `{ items_field { name } }` puts on the input:
	// the element's fields, not the mapping's name.
	_, err := h.executeSteps(context.Background(), map[string]interface{}{
		"__requested_top_fields": []string{"name"},
		"__requested_fields":     []string{"name"},
	})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	reads, _ := db.seen()
	if len(reads) != 1 {
		t.Fatalf("the step ran %d times; its rows are what the field answers with", len(reads))
	}
}

// And the optimisation is still there for the shape it was written for.
func TestAnObjectFieldStillSkipsWhatNobodyAsked(t *testing.T) {
	db := &readWriteConnector{name: "db"}
	h := writingHandler(t, []*flow.StepConfig{
		{Name: "wanted", Connector: "db", ConnectorParams: map[string]interface{}{"query": "SELECT 1"}},
		{Name: "unwanted", Connector: "db", ConnectorParams: map[string]interface{}{"query": "SELECT 2"}},
	}, db)
	h.Config.Transform = &flow.TransformConfig{Mappings: map[string]string{
		"profile": "step.wanted.x",
		"orders":  "step.unwanted.y",
	}}

	results, err := h.executeSteps(context.Background(), map[string]interface{}{
		"__requested_top_fields": []string{"profile"},
	})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}
	reads, _ := db.seen()
	if len(reads) != 1 {
		t.Errorf("%d steps ran, want only the one the requested field reads", len(reads))
	}
	if results["unwanted"] != nil {
		t.Errorf("the skipped step left %#v behind", results["unwanted"])
	}
}
