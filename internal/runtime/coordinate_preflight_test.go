package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
)

// stubReader is a connector that records every Read call and returns a
// canned set of rows. Used in preflight tests to simulate "SKU exists" /
// "SKU missing" cases without spinning up a real DB.
type stubReader struct {
	name      string
	rows      []map[string]interface{}
	readCalls atomic.Int32
	lastQuery connector.Query
}

func (s *stubReader) Name() string                    { return s.name }
func (s *stubReader) Type() string                    { return "fake-db" }
func (s *stubReader) Connect(_ context.Context) error { return nil }
func (s *stubReader) Close(_ context.Context) error   { return nil }
func (s *stubReader) Health(_ context.Context) error  { return nil }
func (s *stubReader) Read(_ context.Context, q connector.Query) (*connector.Result, error) {
	s.readCalls.Add(1)
	s.lastQuery = q
	return &connector.Result{Rows: s.rows}, nil
}

// preflightTestHandler builds a FlowHandler with a coordinate.preflight
// block, a stub reader registered as connector "db", and a destination
// httptest.Server that records hits.
func preflightTestHandler(t *testing.T, ifExists string, preflightRows []map[string]interface{}) (*FlowHandler, *stubReader, *atomic.Int32, func()) {
	t.Helper()

	var destHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destHits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())

	stub := &stubReader{name: "db", rows: preflightRows}

	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()

	cfg := &flow.Config{
		Name: "preflight_flow",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Coordinate: &flow.CoordinateConfig{
			Storage:   &flow.SyncStorageConfig{Driver: "memory"},
			Timeout:   "100ms",
			OnTimeout: "ack",
			Preflight: &flow.PreflightConfig{
				Connector: "db",
				Query:     "SELECT entity_id FROM catalog_product_entity WHERE sku = :sku",
				Params: map[string]string{
					"sku": "input.body.payload.sku",
				},
				IfExists: ifExists,
			},
			Wait: &flow.WaitConfig{
				When: "true",
				For:  "'never_emitted'",
			},
		},
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{"sku": "input.body.payload.sku"},
		},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
	}

	// Register the stub reader so h.getConnector("db") finds it.
	connRegistry := connector.NewRegistry()
	connRegistry.RegisterFactory(testConnectorFactory{name: "fake-db", conn: stub})
	if err := connRegistry.Register(context.Background(), &connector.Config{Name: "db", Type: "fake-db"}); err != nil {
		t.Fatalf("connRegistry.Register: %v", err)
	}

	h := &FlowHandler{
		Config:      cfg,
		SourceType:  "mq",
		Dest:        dest,
		Transformer: tr,
		SyncManager: mgr,
		Connectors:  connRegistry,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	return h, stub, &destHits, func() {
		srv.Close()
		_ = mgr.Close()
	}
}

// TestPreflight_SkipsWaitWhenResourceExists is the canonical case Mercury
// hit: SKU is already in the destination DB, the coordinate.wait was
// blocking for 5 minutes anyway because the parsed preflight block was
// being dropped on the floor at the runtime/sync boundary. Post-fix:
// preflight runs, returns 1 row, if_exists="pass" → wait is skipped, the
// flow proceeds to transform/to in well under the timeout.
func TestPreflight_SkipsWaitWhenResourceExists(t *testing.T) {
	rows := []map[string]interface{}{
		{"entity_id": int64(2896)},
	}
	h, stub, destHits, done := preflightTestHandler(t, "pass", rows)
	defer done()

	input := map[string]interface{}{
		"body": map[string]interface{}{"payload": map[string]interface{}{"sku": "TEST01"}},
	}

	start := time.Now()
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	elapsed := time.Since(start)

	if got := stub.readCalls.Load(); got != 1 {
		t.Errorf("preflight reader should be called once, got %d", got)
	}
	if stub.lastQuery.RawSQL == "" {
		t.Error("preflight should pass the SQL through to the reader")
	}
	if stub.lastQuery.Filters["sku"] != "TEST01" {
		t.Errorf("preflight params should resolve from input; got filters=%+v", stub.lastQuery.Filters)
	}
	if got := destHits.Load(); got != 1 {
		t.Errorf("destination should be hit (preflight passed → wait skipped → transform/to ran), got %d hits", got)
	}
	// 100ms timeout; a successful preflight skip + sub-millisecond stub
	// + sub-millisecond destination should comfortably finish under 50ms.
	if elapsed > 200*time.Millisecond {
		t.Errorf("preflight pass should not enter the wait; elapsed %s", elapsed)
	}
}

// TestPreflight_EntersWaitWhenResourceMissing: empty preflight → wait
// fires → on_timeout=ack short-circuits with FilteredResultWithPolicy.
func TestPreflight_EntersWaitWhenResourceMissing(t *testing.T) {
	h, stub, destHits, done := preflightTestHandler(t, "pass", nil)
	defer done()

	input := map[string]interface{}{
		"body": map[string]interface{}{"payload": map[string]interface{}{"sku": "ORPHAN"}},
	}

	result, err := h.HandleRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if got := stub.readCalls.Load(); got != 1 {
		t.Errorf("preflight reader should be called once, got %d", got)
	}
	// preflight returned 0 rows → wait fires → on_timeout=ack →
	// FilteredResultWithPolicy{Policy:"ack"}.
	filtered, ok := result.(*flow.FilteredResultWithPolicy)
	if !ok {
		t.Fatalf("expected FilteredResultWithPolicy after timeout, got %T", result)
	}
	if filtered.Policy != "ack" {
		t.Errorf("expected policy=ack, got %q", filtered.Policy)
	}
	if got := destHits.Load(); got != 0 {
		t.Errorf("destination must not be hit when wait timed out, got %d", got)
	}
}

// TestPreflight_IfExistsFailReturnsError: if_exists="fail" with rows
// returned must surface as an error so the flow takes the on_error
// branch instead of proceeding.
func TestPreflight_IfExistsFailReturnsError(t *testing.T) {
	rows := []map[string]interface{}{
		{"entity_id": int64(1)},
	}
	h, _, destHits, done := preflightTestHandler(t, "fail", rows)
	defer done()

	input := map[string]interface{}{
		"body": map[string]interface{}{"payload": map[string]interface{}{"sku": "AI02LT"}},
	}

	_, err := h.HandleRequest(context.Background(), input)
	if err == nil {
		t.Fatal("expected error when if_exists=fail and preflight finds rows")
	}
	if got := destHits.Load(); got != 0 {
		t.Errorf("destination must not be hit when preflight rejects, got %d", got)
	}
}

// TestPreflight_NotConfiguredFallsThroughToWait: without a preflight
// block the runtime must take the existing wait path unchanged. Pre-fix
// regression guard.
func TestPreflight_NotConfiguredFallsThroughToWait(t *testing.T) {
	// Mirror preflightTestHandler but with no Preflight on the config.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	dest := httpconn.New("api", srv.URL, 0, nil, nil, 1)
	_ = dest.Connect(context.Background())
	tr, _ := transform.NewCELTransformer()
	mgr := msync.NewManager()
	defer mgr.Close()

	cfg := &flow.Config{
		Name: "no_preflight",
		From: &flow.FromConfig{Connector: "rabbit", ConnectorParams: map[string]interface{}{"target": "q"}},
		Coordinate: &flow.CoordinateConfig{
			Storage:   &flow.SyncStorageConfig{Driver: "memory"},
			Timeout:   "100ms",
			OnTimeout: "ack",
			Wait: &flow.WaitConfig{
				When: "true",
				For:  "'never_emitted'",
			},
		},
		Transform: &flow.TransformConfig{Mappings: map[string]string{"x": "input.body.x"}},
		To: &flow.ToConfig{
			Connector:       "api",
			Parallel:        true,
			ConnectorParams: map[string]interface{}{"target": "/post", "operation": "POST"},
		},
	}
	h := &FlowHandler{
		Config: cfg, SourceType: "mq", Dest: dest, Transformer: tr, SyncManager: mgr,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"body": map[string]interface{}{"x": "v"}}
	result, _ := h.HandleRequest(context.Background(), input)
	filtered, ok := result.(*flow.FilteredResultWithPolicy)
	if !ok {
		t.Fatalf("expected FilteredResultWithPolicy after timeout, got %T", result)
	}
	if filtered.Policy != "ack" {
		t.Errorf("expected policy=ack from on_timeout=ack, got %q", filtered.Policy)
	}
}
