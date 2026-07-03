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
	name  string
	reads int
	rows  []map[string]interface{}
}

func (m *mockQueryReader) Name() string                      { return m.name }
func (m *mockQueryReader) Type() string                      { return "mock" }
func (m *mockQueryReader) Connect(ctx context.Context) error { return nil }
func (m *mockQueryReader) Close(ctx context.Context) error   { return nil }
func (m *mockQueryReader) Health(ctx context.Context) error  { return nil }

func (m *mockQueryReader) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	m.reads++
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
