package runtime

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
)

// mockQueryReadWriter implements connector.ReadWriter and counts calls, so
// dispatch tests can assert QUERY lands on the read path, not the write path.
type mockQueryReadWriter struct {
	name   string
	reads  int
	writes int
	rows   []map[string]interface{}
}

func (m *mockQueryReadWriter) Name() string                      { return m.name }
func (m *mockQueryReadWriter) Type() string                      { return "mock" }
func (m *mockQueryReadWriter) Connect(ctx context.Context) error { return nil }
func (m *mockQueryReadWriter) Close(ctx context.Context) error   { return nil }
func (m *mockQueryReadWriter) Health(ctx context.Context) error  { return nil }

func (m *mockQueryReadWriter) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	m.reads++
	return &connector.Result{Rows: m.rows}, nil
}

func (m *mockQueryReadWriter) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	m.writes++
	return &connector.Result{Affected: 1}, nil
}

// mockQueryReader is a Reader-only variant (no Write method at all, so the
// ReadWriter type assertion fails) to exercise handleSimpleRequest.
type mockQueryReader struct {
	name      string
	reads     int
	rows      []map[string]interface{}
	lastQuery connector.Query
}

func (m *mockQueryReader) Name() string                      { return m.name }
func (m *mockQueryReader) Type() string                      { return "mock" }
func (m *mockQueryReader) Connect(ctx context.Context) error { return nil }
func (m *mockQueryReader) Close(ctx context.Context) error   { return nil }
func (m *mockQueryReader) Health(ctx context.Context) error  { return nil }

func (m *mockQueryReader) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	m.reads++
	m.lastQuery = query
	return &connector.Result{Rows: m.rows}, nil
}

func TestParseOperation_QueryMethod(t *testing.T) {
	op := parseOperation("QUERY /search")
	if op.Method != "QUERY" || op.Path != "/search" {
		t.Errorf("parseOperation(QUERY /search) = %+v, want Method=QUERY Path=/search", op)
	}
}

func TestOperationIsRead(t *testing.T) {
	cases := map[string]bool{
		"GET":    true,
		"QUERY":  true,
		"POST":   false,
		"PUT":    false,
		"PATCH":  false,
		"DELETE": false,
	}
	for method, want := range cases {
		if got := (Operation{Method: method}).IsRead(); got != want {
			t.Errorf("Operation{%s}.IsRead() = %v, want %v", method, got, want)
		}
	}
}

func newQueryFlowHandler(dest connector.Connector) *FlowHandler {
	return &FlowHandler{
		Config: &flow.Config{
			Name: "search_users",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "QUERY /search"},
			},
			To: &flow.ToConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"target": "users"},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}
}

// TestQueryDispatch_ReadWriter covers the main dispatch switch: a QUERY flow
// against a ReadWriter destination must execute the read path.
func TestQueryDispatch_ReadWriter(t *testing.T) {
	dest := &mockQueryReadWriter{
		name: "db",
		rows: []map[string]interface{}{{"id": 1, "name": "mat"}},
	}
	h := newQueryFlowHandler(dest)

	result, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{"name_like": "mat"})
	if err != nil {
		t.Fatalf("QUERY flow failed: %v", err)
	}
	if dest.reads != 1 {
		t.Errorf("reads = %d, want 1", dest.reads)
	}
	if dest.writes != 0 {
		t.Errorf("writes = %d, want 0 (QUERY must never write)", dest.writes)
	}
	rows, ok := result.([]map[string]interface{})
	if !ok || len(rows) != 1 {
		t.Errorf("result = %#v, want the reader's rows", result)
	}
}

// TestQueryDest_ForwardsInputAsFilters: when the destination targets the
// HTTP QUERY method, the whole input (inbound body + params) is forwarded as
// filters — the HTTP connector encodes filters as the outbound QUERY body.
// Headers and internal fields stay out.
func TestQueryDest_ForwardsInputAsFilters(t *testing.T) {
	dest := &mockQueryReader{name: "upstream", rows: []map[string]interface{}{{"id": 1}}}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "search_proxy",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "QUERY /search"},
			},
			To: &flow.ToConfig{
				Connector:       "upstream",
				ConnectorParams: map[string]interface{}{"target": "QUERY /search"},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	input := map[string]interface{}{
		"name_like": "%pro%",
		"max_price": 1500,
		"headers":   map[string]interface{}{"authorization": "secret"},
	}
	if _, err := h.executeFlowCoreInternal(context.Background(), input); err != nil {
		t.Fatalf("flow failed: %v", err)
	}

	f := dest.lastQuery.Filters
	if f["name_like"] != "%pro%" || f["max_price"] != 1500 {
		t.Errorf("filters = %#v, want inbound body fields forwarded", f)
	}
	if _, ok := f["headers"]; ok {
		t.Error("headers must not be forwarded in the outbound QUERY body")
	}
}

// TestQueryDest_SplitFormAlsoForwards: to { operation = "QUERY", target = "/search" }.
func TestQueryDest_SplitFormAlsoForwards(t *testing.T) {
	dest := &mockQueryReader{name: "upstream", rows: []map[string]interface{}{{"id": 1}}}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "search_proxy",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /search"},
			},
			To: &flow.ToConfig{
				Connector: "upstream",
				ConnectorParams: map[string]interface{}{
					"operation": "QUERY",
					"target":    "/search",
				},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	// a GET source with query params can still feed a QUERY destination
	input := map[string]interface{}{"q": "laptop"}
	if _, err := h.executeFlowCoreInternal(context.Background(), input); err != nil {
		t.Fatalf("flow failed: %v", err)
	}
	if dest.lastQuery.Filters["q"] != "laptop" {
		t.Errorf("filters = %#v, want query params forwarded", dest.lastQuery.Filters)
	}
}

// TestNonQueryDest_KeepsPathParamOnlyBehavior: a plain GET destination keeps
// the existing contract — only path params become filters.
func TestNonQueryDest_KeepsPathParamOnlyBehavior(t *testing.T) {
	dest := &mockQueryReader{name: "upstream", rows: []map[string]interface{}{{"id": 1}}}
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "plain_read",
			From: &flow.FromConfig{
				Connector:       "api",
				ConnectorParams: map[string]interface{}{"operation": "GET /users"},
			},
			To: &flow.ToConfig{
				Connector:       "upstream",
				ConnectorParams: map[string]interface{}{"target": "GET /users"},
			},
		},
		Dest:       dest,
		SourceType: "rest",
	}

	input := map[string]interface{}{"page": "2"}
	if _, err := h.executeFlowCoreInternal(context.Background(), input); err != nil {
		t.Fatalf("flow failed: %v", err)
	}
	if len(dest.lastQuery.Filters) != 0 {
		t.Errorf("filters = %#v, want empty (no path params in operation)", dest.lastQuery.Filters)
	}
}

// TestQueryDispatch_ReaderOnly covers handleSimpleRequest: a QUERY flow
// against a Reader-only destination must also execute the read path.
func TestQueryDispatch_ReaderOnly(t *testing.T) {
	dest := &mockQueryReader{
		name: "db",
		rows: []map[string]interface{}{{"id": 1}},
	}
	h := newQueryFlowHandler(dest)

	_, err := h.executeFlowCoreInternal(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("QUERY flow (reader-only dest) failed: %v", err)
	}
	if dest.reads != 1 {
		t.Errorf("reads = %d, want 1", dest.reads)
	}
}
