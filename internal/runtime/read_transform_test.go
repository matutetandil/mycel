package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// A read flow's transform is part of what it asks for.
//
// The transform block ran on every write path and on no read path: on a GET it
// was applied neither to the request nor to the answer. It parsed, the editor
// offered it, the documentation says `to` sees its result — and it did
// nothing. A flow whose destination query reads `WHERE id = :user_id` from a
// transform that computes user_id sent the parameter unbound, which Postgres
// reports as a syntax error at ":" and SQLite as a missing argument. Neither
// mentions the flow, and nothing points at the transform.
func TestAReadFlowsTransformReachesTheDestinationQuery(t *testing.T) {
	dest := &mockQueryReader{name: "db", rows: []map[string]interface{}{{"id": "u1"}}}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "get_me",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /me"},
			},
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"user_id": "'u1'"},
				Order:    []string{"user_id"},
			},
			To: &flow.ToConfig{
				Connector: "db",
				ConnectorParams: map[string]interface{}{
					"query": "SELECT * FROM users WHERE id = :user_id",
				},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	if _, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("read flow failed: %v", err)
	}
	if dest.reads != 1 {
		t.Fatalf("reads = %d, want 1", dest.reads)
	}
	if got := dest.lastQuery.Filters["user_id"]; got != "u1" {
		t.Errorf("the named parameter reached the driver as %#v; the transform computed \"u1\"", got)
	}
}

// The request is still there underneath. A path parameter is what a read flow
// filters on, and computing something else must not take it away.
func TestATransformDoesNotDisplaceThePathParameters(t *testing.T) {
	dest := &mockQueryReader{name: "db", rows: []map[string]interface{}{{"id": 7}}}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "get_user",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /users/:id"},
			},
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"requested_by": "'audit'"},
				Order:    []string{"requested_by"},
			},
			To: &flow.ToConfig{
				Connector: "db",
				ConnectorParams: map[string]interface{}{
					"query": "SELECT * FROM users WHERE id = :id AND note = :requested_by",
				},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	if _, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{"id": 7}); err != nil {
		t.Fatalf("read flow failed: %v", err)
	}
	if got := dest.lastQuery.Filters["id"]; got != 7 {
		t.Errorf("the path parameter reached the driver as %#v, want 7", got)
	}
	if got := dest.lastQuery.Filters["requested_by"]; got != "audit" {
		t.Errorf("the computed field reached the driver as %#v", got)
	}
}

// And a read flow with no transform is untouched.
func TestAReadFlowWithoutATransformIsUnchanged(t *testing.T) {
	dest := &mockQueryReader{name: "db", rows: []map[string]interface{}{{"id": 1}}}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "list_users",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /users"},
			},
			To: &flow.ToConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"target": "users"},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	if _, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("read flow failed: %v", err)
	}
	if len(dest.lastQuery.Filters) != 0 {
		t.Errorf("filters = %#v, want none", dest.lastQuery.Filters)
	}
}

// A destination named by table builds its criteria from the request, and is
// deliberately not given the computed fields: adding filters to reads that work
// today would be a change nobody asked for. Shaping the answer is what the
// response block is for.
func TestComputedFieldsDoNotBecomeFiltersOnATableRead(t *testing.T) {
	dest := &mockQueryReader{name: "db", rows: []map[string]interface{}{{"id": 7}}}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "get_user",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /users/:id"},
			},
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"note": "'audit'"},
				Order:    []string{"note"},
			},
			To: &flow.ToConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"target": "users"},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	if _, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{"id": 7}); err != nil {
		t.Fatalf("read flow failed: %v", err)
	}
	if _, present := dest.lastQuery.Filters["note"]; present {
		t.Errorf("a computed field became a filter: %#v", dest.lastQuery.Filters)
	}
	if got := dest.lastQuery.Filters["id"]; got != 7 {
		t.Errorf("the path parameter reached the driver as %#v, want 7", got)
	}
}

// Reading a table by name, there is nothing left to ask for, so what the
// transform describes is the answer.
//
// This is how every read flow in the examples is written and none of them
// worked: the transform was ignored, and with it the enrich blocks that fetch
// the price or the stock level the answer is being shaped to include.
func TestATableReadIsShapedByItsTransform(t *testing.T) {
	dest := &mockQueryReader{
		name: "db",
		rows: []map[string]interface{}{{"id": 7, "name": "Widget", "internal_cost": 3}},
	}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "get_product",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /products/:id"},
			},
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"id": "input.id", "label": "input.name"},
				Order:    []string{"id", "label"},
			},
			To: &flow.ToConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"target": "products"},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	result, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{"id": 7})
	if err != nil {
		t.Fatalf("read flow failed: %v", err)
	}

	rows, ok := result.([]interface{})
	if !ok || len(rows) != 1 {
		t.Fatalf("result = %#v, want one shaped row", result)
	}
	row, _ := rows[0].(map[string]interface{})
	if row["label"] != "Widget" {
		t.Errorf("label = %#v; the transform reads a column of the row", row["label"])
	}
	// What the transform did not name is not in the answer, which is the point
	// of shaping it.
	if _, leaked := row["internal_cost"]; leaked {
		t.Errorf("a column the transform never named came back: %#v", row)
	}
}

// A write with nothing in it says so.
//
// An empty payload became `INSERT INTO items () VALUES ()` and what came back
// was the driver's opinion of that — `SQL logic error: near ")"` — for a
// request that simply carried no fields. An empty body and a transform that
// produced nothing are both ordinary mistakes, and neither was named.
func TestAWriteWithNoFieldsIsRefusedByName(t *testing.T) {
	dest := &mockQueryReadWriter{name: "db"}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "create_item",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "POST /items"},
			},
			To: &flow.ToConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"target": "items"},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	_, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("an empty request was written")
	}
	if !strings.Contains(err.Error(), "create_item") || !strings.Contains(err.Error(), "nothing to write") {
		t.Errorf("the refusal reads %q; it should name the flow and the reason", err)
	}
	if dest.writes != 0 {
		t.Errorf("the destination was written to %d times", dest.writes)
	}
}
