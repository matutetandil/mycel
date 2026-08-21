package runtime

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/transform"
)

// A step that writes.
//
// Which of a connector's abilities a step uses was decided by which interface
// the connector happened to satisfy first, and the order asked for a reader
// before a writer. A database satisfies both, so a step naming INSERT was
// dispatched as a read: the branch that writes was unreachable for every
// connector that can also read, which is every database. The insert quietly
// became a select, nothing was stored, and the id a later step wanted was never
// there.
//
// And what a step sends was passed through as written. `body = { name =
// "input.name" }` — the form the guide shows — stored the words "input.name" in
// the column, and sent that text over the wire to an HTTP service.

// readWriteConnector answers both, and records which one was asked.
type readWriteConnector struct {
	name string

	mu      sync.Mutex
	reads   []connector.Query
	writes  []*connector.Data
	written int64
}

func (c *readWriteConnector) Name() string                  { return c.name }
func (c *readWriteConnector) Type() string                  { return "database" }
func (c *readWriteConnector) Connect(context.Context) error { return nil }
func (c *readWriteConnector) Close(context.Context) error   { return nil }
func (c *readWriteConnector) Health(context.Context) error  { return nil }

func (c *readWriteConnector) Read(_ context.Context, q connector.Query) (*connector.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads = append(c.reads, q)
	return &connector.Result{Rows: []map[string]interface{}{{"read": true}}}, nil
}

func (c *readWriteConnector) Write(_ context.Context, d *connector.Data) (*connector.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, d)
	c.written++
	return &connector.Result{Affected: 1, LastID: c.written}, nil
}

func (c *readWriteConnector) seen() ([]connector.Query, []*connector.Data) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads, c.writes
}

func writingHandler(t *testing.T, steps []*flow.StepConfig, conn connector.Connector) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	registry.Replace("db", conn)
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	return &FlowHandler{
		Config:      &flow.Config{Name: "save", Steps: steps},
		Connectors:  registry,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestAStepThatSaysInsertWrites(t *testing.T) {
	db := &readWriteConnector{name: "db"}
	handler := writingHandler(t, []*flow.StepConfig{{
		Name: "save", Connector: "db",
		ConnectorParams: map[string]interface{}{
			"target":    "products",
			"operation": "INSERT",
			"body":      map[string]interface{}{"name": "input.name"},
		},
	}}, db)

	results, err := handler.executeSteps(context.Background(),
		map[string]interface{}{"name": "Widget"})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	reads, writes := db.seen()
	if len(writes) != 1 {
		t.Fatalf("the step made %d writes and %d reads; INSERT has to write", len(writes), len(reads))
	}
	if len(reads) != 0 {
		t.Errorf("the step also read: %v", reads)
	}
	if got := writes[0].Payload["name"]; got != "Widget" {
		t.Errorf("the row holds %v; the body was not evaluated", got)
	}

	saved, _ := results["save"].(map[string]interface{})
	if saved["id"] != int64(1) {
		t.Errorf("the step reported %v; a later step asks it for the id", results["save"])
	}
}

func TestAStepWithNoOperationStillReads(t *testing.T) {
	// The other direction: choosing by operation must not turn every step into
	// a write.
	db := &readWriteConnector{name: "db"}
	handler := writingHandler(t, []*flow.StepConfig{{
		Name: "look_up", Connector: "db",
		ConnectorParams: map[string]interface{}{
			"target": "products",
		},
	}}, db)

	if _, err := handler.executeSteps(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	reads, writes := db.seen()
	if len(reads) != 1 || len(writes) != 0 {
		t.Errorf("a step with no operation made %d reads and %d writes", len(reads), len(writes))
	}
}

func TestAStepsBodyIsEvaluated(t *testing.T) {
	// The guide writes body = { items = "step.cart.items" }, referring to an
	// earlier step. That was sent as those words.
	db := &readWriteConnector{name: "db"}
	handler := writingHandler(t, []*flow.StepConfig{
		{
			Name: "cart", Connector: "db",
			ConnectorParams: map[string]interface{}{"target": "carts"},
		},
		{
			Name: "save", Connector: "db",
			ConnectorParams: map[string]interface{}{
				"target":    "orders",
				"operation": "INSERT",
				"body": map[string]interface{}{
					"from_earlier": "step.cart.read",
					"from_message": "input.customer",
					"a_constant":   "unchanged",
				},
			},
		},
	}, db)

	_, err := handler.executeSteps(context.Background(),
		map[string]interface{}{"customer": "alice"})
	if err != nil {
		t.Fatalf("executeSteps: %v", err)
	}

	_, writes := db.seen()
	if len(writes) != 1 {
		t.Fatalf("writes = %d", len(writes))
	}
	payload := writes[0].Payload
	if payload["from_earlier"] != true {
		t.Errorf("a body field naming an earlier step arrived as %#v", payload["from_earlier"])
	}
	if payload["from_message"] != "alice" {
		t.Errorf("a body field naming the message arrived as %#v", payload["from_message"])
	}
	if payload["a_constant"] != "unchanged" {
		t.Errorf("a constant was changed to %#v", payload["a_constant"])
	}
}
