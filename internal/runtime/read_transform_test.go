package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
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

// A GraphQL mutation returns the record it created, found by the id the flow
// assigned.
//
// The read-back used only the driver's last insert id, which for a table keyed
// by anything but an autoincrementing integer is the row's position — so a
// mutation whose flow generates its own key read back nothing, and GraphQL
// answered "Cannot return null for non-nullable field User.email" for a record
// that had just been written.
func TestAGraphQLMutationReadsBackByTheIdItAssigned(t *testing.T) {
	created := map[string]interface{}{"id": "u-uuid", "email": "john@example.com"}
	dest := &recordingReadWriter{rows: []map[string]interface{}{created}}

	h := &FlowHandler{
		Config: &flow.Config{
			Name: "create_user",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "Mutation.createUser"},
			},
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"id": "'u-uuid'", "email": "input.email"},
				Order:    []string{"id", "email"},
			},
			To: &flow.ToConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"target": "users"},
			},
		},
		Dest:       dest,
		SourceType: "graphql",
	}

	result, err := h.executeFlowCoreInternal(context.Background(),
		map[string]interface{}{"email": "john@example.com"})
	if err != nil {
		t.Fatalf("mutation failed: %v", err)
	}
	row, ok := result.(map[string]interface{})
	if !ok || row["email"] != "john@example.com" {
		t.Fatalf("the mutation answered %#v, want the created record", result)
	}
	if got := dest.readFilters["id"]; got != "u-uuid" {
		t.Errorf("read back by %#v, want the id the flow assigned", got)
	}
}

// recordingReadWriter keeps the filters it was read with.
type recordingReadWriter struct {
	rows        []map[string]interface{}
	readFilters map[string]interface{}
}

func (c *recordingReadWriter) Name() string                      { return "db" }
func (c *recordingReadWriter) Type() string                      { return "database" }
func (c *recordingReadWriter) Connect(ctx context.Context) error { return nil }
func (c *recordingReadWriter) Close(ctx context.Context) error   { return nil }
func (c *recordingReadWriter) Health(ctx context.Context) error  { return nil }

func (c *recordingReadWriter) Read(_ context.Context, q connector.Query) (*connector.Result, error) {
	c.readFilters = q.Filters
	return &connector.Result{Rows: c.rows}, nil
}

func (c *recordingReadWriter) Write(_ context.Context, _ *connector.Data) (*connector.Result, error) {
	// A text primary key, so what the driver reports is the row's position.
	return &connector.Result{Affected: 1, LastID: 7}, nil
}

// A flow with no destination answers with what it computed.
//
// The dispatch said "return transformed input" and returned the input, so a
// flow without a `to` ignored its transform and its enrich blocks entirely —
// and a flow without a destination is usually a gateway, which calls somebody
// else's API and shapes the answer. It echoed the request back instead,
// headers and all.
func TestAFlowWithNoDestinationAnswersWithItsTransform(t *testing.T) {
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "get_products",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /products"},
			},
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"name": "upper(input.name)", "source": "'gateway'"},
				Order:    []string{"name", "source"},
			},
		},
		SourceType: "rest",
	}

	result, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{
		"name":    "widget",
		"headers": map[string]interface{}{"user-agent": "curl"},
	})
	if err != nil {
		t.Fatalf("flow failed: %v", err)
	}

	answer, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("answered %#v", result)
	}
	if answer["name"] != "WIDGET" || answer["source"] != "gateway" {
		t.Errorf("answered %#v, want what the transform built", answer)
	}
	// The request's headers are not part of the answer.
	if _, leaked := answer["headers"]; leaked {
		t.Errorf("the request's headers came back with the answer: %#v", answer)
	}
}

// One that computes nothing still echoes, which is what an echo flow is for.
func TestAFlowWithNoDestinationAndNoTransformStillEchoes(t *testing.T) {
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "echo",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "POST /echo"},
			},
		},
		SourceType: "rest",
	}

	result, err := h.executeFlowCoreInternal(context.Background(),
		map[string]interface{}{"message": "hello"})
	if err != nil {
		t.Fatalf("flow failed: %v", err)
	}
	answer, _ := result.(map[string]interface{})
	if answer["message"] != "hello" {
		t.Errorf("answered %#v, want the request back", result)
	}
}

// A read flow whose destination can only be written to renders its answer
// through it.
//
// A PDF connector is written to, not read from, so a flow that gathers an
// invoice and hands it over was refused with "destination connector does not
// support required operation" — or, with steps, answered with the gathered
// JSON, because a read flow with steps never touched its destination. The
// documented way to serve a generated document could not work either way.
func TestAReadFlowRendersThroughAWriteOnlyDestination(t *testing.T) {
	renderer := &writeOnlyConnector{
		rows: []map[string]interface{}{{
			"_binary":       "JVBERi0=",
			"_content_type": "application/pdf",
		}},
	}

	h := &FlowHandler{
		Config: &flow.Config{
			Name: "get_invoice_pdf",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /invoices/:id/pdf"},
			},
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"number": "input.id"},
				Order:    []string{"number"},
			},
			To: &flow.ToConfig{
				Connector:       "pdf",
				ConnectorParams: map[string]interface{}{"operation": "generate"},
			},
		},
		Dest:       renderer,
		SourceType: "rest",
	}

	result, err := h.executeFlowCoreInternal(context.Background(),
		map[string]interface{}{"id": "42"})
	if err != nil {
		t.Fatalf("flow failed: %v", err)
	}

	answer, ok := result.(map[string]interface{})
	if !ok || answer["_content_type"] != "application/pdf" {
		t.Fatalf("answered %#v, want what the renderer produced", result)
	}
	// And it was handed what the flow built, under the operation it named.
	if renderer.written == nil || renderer.written.Payload["number"] != "42" {
		t.Errorf("the renderer was handed %#v", renderer.written)
	}
	if renderer.written.Operation != "generate" {
		t.Errorf("operation = %q, want the one the to block names", renderer.written.Operation)
	}
}

// writeOnlyConnector can be written to and not read from, like the PDF and
// notification connectors.
type writeOnlyConnector struct {
	rows    []map[string]interface{}
	written *connector.Data
}

func (c *writeOnlyConnector) Name() string                      { return "pdf" }
func (c *writeOnlyConnector) Type() string                      { return "pdf" }
func (c *writeOnlyConnector) Connect(ctx context.Context) error { return nil }
func (c *writeOnlyConnector) Close(ctx context.Context) error   { return nil }
func (c *writeOnlyConnector) Health(ctx context.Context) error  { return nil }

func (c *writeOnlyConnector) Write(_ context.Context, data *connector.Data) (*connector.Result, error) {
	c.written = data
	return &connector.Result{Rows: c.rows, Affected: 1}, nil
}
