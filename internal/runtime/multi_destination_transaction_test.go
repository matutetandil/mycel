package runtime

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/aspect"
	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/database/sqlite"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// A transaction is a destination like any other, however many destinations
// the flow has.
//
// With one `to` it ran as a transaction; with two, the parser put both in the
// list of destinations, and the code that writes each of those never looked at
// the transaction block: the database got a plain write of the payload, which
// ran none of the statements and failed — and the other destination's success
// acked the message. `mycel validate` called it "2 destinations" and moved on.

// txAmongDestinations is a flow writing a product through a transaction and an
// audit record through a second destination.
func txAmongDestinations(t *testing.T, statements []flow.TxStatement) (*FlowHandler, *recordingWriter, func(string) int) {
	t.Helper()

	conn := sqlite.New("db", filepath.Join(t.TempDir(), "multi.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.DB().Exec(`CREATE TABLE product (id INTEGER PRIMARY KEY AUTOINCREMENT, sku TEXT UNIQUE, name TEXT)`); err != nil {
		t.Fatalf("schema: %v", err)
	}

	audit := &recordingWriter{name: "audit"}
	h := multiDestHandler(t, []*flow.ToConfig{
		{Connector: "db", Transaction: &flow.TransactionConfig{Statements: statements}},
		{Connector: "audit", ConnectorParams: map[string]interface{}{"target": "audit.log"}},
	}, map[string]*recordingWriter{"audit": audit})
	h.Connectors.Replace("db", conn)
	h.Config.From = &flow.FromConfig{
		Connector:       "api",
		ConnectorParams: map[string]interface{}{"operation": "POST /products"},
	}

	rows := func(table string) int { return count(t, conn.DB(), table) }
	return h, audit, rows
}

func insertProduct() []flow.TxStatement {
	return []flow.TxStatement{
		mkExec(`INSERT INTO product (sku, name) VALUES (:sku, :name)`, "product_id", "",
			map[string]string{"sku": "input.sku", "name": "input.name"}),
	}
}

func TestATransactionAmongSeveralDestinationsRunsItsStatements(t *testing.T) {
	h, audit, rows := txAmongDestinations(t, insertProduct())

	got, err := h.HandleRequest(context.Background(), map[string]interface{}{"sku": "A1", "name": "T"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if n := rows("product"); n != 1 {
		t.Fatalf("product rows = %d: the transaction's statements did not run", n)
	}
	if len(audit.writes()) != 1 {
		t.Errorf("the other destination was not written")
	}

	result, ok := got.(*MultiDestResult)
	if !ok {
		t.Fatalf("answer = %#v", got)
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %v", result.Errors)
	}
	if result.Captured["product_id"] != int64(1) {
		t.Errorf("captured = %#v, want the inserted id", result.Captured)
	}
}

// A failing transaction is still a transaction: everything it did is rolled
// back, and it is reported as that destination's failure.
func TestAFailingTransactionAmongSeveralDestinationsRollsBack(t *testing.T) {
	h, _, rows := txAmongDestinations(t, []flow.TxStatement{
		mkExec(`INSERT INTO product (sku, name) VALUES (:sku, 'first')`, "", "", map[string]string{"sku": "input.sku"}),
		mkExec(`INSERT INTO product (sku, name) VALUES (:sku, 'duplicate')`, "", "", map[string]string{"sku": "input.sku"}),
	})

	got, err := h.HandleRequest(context.Background(), map[string]interface{}{"sku": "A1"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if n := rows("product"); n != 0 {
		t.Errorf("product rows = %d: the first insert survived the failed transaction", n)
	}
	result := got.(*MultiDestResult)
	if msg := result.Errors["db"]; !strings.Contains(strings.ToLower(msg), "unique") {
		t.Errorf("db error = %q, want the statement's own failure", msg)
	}
}

// What a transaction among several destinations captured reaches `after`
// aspects as result.captured, the same as a lone one.
func TestAnAfterAspectSeesWhatATransactionAmongSeveralDestinationsCaptured(t *testing.T) {
	h, _, _ := txAmongDestinations(t, insertProduct())

	sink := &sinkWriter{}
	h.Connectors.Replace("downstream", sink)
	aspects := aspect.NewRegistry()
	if err := aspects.Register(&aspect.Config{
		Name: "announce", On: []string{"distribute_*"}, When: aspect.After,
		If: "has(result.captured.product_id)",
		Action: &aspect.ActionConfig{
			Connector: "downstream",
			Transform: map[string]string{"id": "result.captured.product_id"},
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	executor, err := aspect.NewExecutor(aspects, h.Connectors)
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	h.AspectExecutor = executor

	if _, err := h.HandleRequest(context.Background(), map[string]interface{}{"sku": "A1", "name": "T"}); err != nil {
		t.Fatalf("request: %v", err)
	}
	calls := sink.calls()
	if len(calls) != 1 || calls[0]["id"] != int64(1) {
		t.Errorf("aspect calls = %#v, want one carrying the captured id", calls)
	}
}

// Two transactions capturing under the same name would overwrite each other
// in the merged result.captured, so validation refuses it.
func TestTheSameCaptureNameInTwoTransactionDestinationsIsRefused(t *testing.T) {
	capture := func(name string) *flow.TransactionConfig {
		return &flow.TransactionConfig{Statements: []flow.TxStatement{
			{Each: &flow.TxEach{Var: "line", In: "input.lines", Body: []flow.TxStatement{
				mkExec(`INSERT INTO t VALUES (1)`, name, "", nil),
			}}},
		}}
	}
	config := func(a, b string) *parser.Configuration {
		return &parser.Configuration{Flows: []*flow.Config{{
			Name: "save_order",
			MultiTo: []*flow.ToConfig{
				{Connector: "db", Transaction: capture(a)},
				{Connector: "archive", Transaction: capture(b)},
			},
		}}}
	}

	errs := ValidateUniqueInnerNames(config("row_id", "row_id"))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `capture "row_id"`) {
		t.Fatalf("errors = %v, want the repeated capture named", errs)
	}

	if errs := ValidateUniqueInnerNames(config("order_id", "archive_id")); len(errs) != 0 {
		t.Errorf("distinct names refused: %v", errs)
	}
}

// Recapturing a name inside one transaction is how an each loop works, and is
// not a duplicate.
func TestRecapturingInsideOneTransactionIsFine(t *testing.T) {
	tx := &flow.TransactionConfig{Statements: []flow.TxStatement{
		mkExec(`INSERT INTO a VALUES (1)`, "id", "", nil),
		mkExec(`INSERT INTO b VALUES (1)`, "id", "", nil),
	}}
	errs := ValidateUniqueInnerNames(&parser.Configuration{Flows: []*flow.Config{{
		Name:    "save",
		MultiTo: []*flow.ToConfig{{Connector: "db", Transaction: tx}, {Connector: "audit"}},
	}}})
	if len(errs) != 0 {
		t.Errorf("errors = %v", errs)
	}
}

var _ connector.TxRunner = (*sqlite.Connector)(nil)
