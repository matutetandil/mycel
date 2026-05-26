package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/database/sqlite"
	"github.com/matutetandil/mycel/internal/flow"
)

// newTxHandler wires a FlowHandler whose destination is a real sqlite connector
// (a temp-file DB so every pooled connection sees the same tables) with the
// given transaction config. It returns the handler and the underlying *sql.DB
// for assertions.
func newTxHandler(t *testing.T, txCfg *flow.TransactionConfig) (*FlowHandler, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "tx.db")
	conn := sqlite.New("db", dbPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	db := conn.DB()
	schema := []string{
		`CREATE TABLE parent (id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id INTEGER, name TEXT)`,
		`CREATE TABLE child (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER, label TEXT, position INTEGER)`,
		`CREATE TABLE child_value (id INTEGER PRIMARY KEY AUTOINCREMENT, child_id INTEGER, store_id INTEGER, val TEXT)`,
		`CREATE TABLE lookup (option_id INTEGER, code TEXT, label TEXT)`,
	}
	for _, s := range schema {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema %q: %v", s, err)
		}
	}

	registry := connector.NewRegistry()
	registry.Replace("db", conn)

	h := &FlowHandler{
		Config: &flow.Config{
			Name: "tx_flow",
			From: &flow.FromConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"operation": "INSERT"},
			},
			To: &flow.ToConfig{Connector: "db", Transaction: txCfg},
		},
		Dest:       conn,
		Connectors: registry,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, db
}

func mkExec(query, capture, when string, params map[string]string) flow.TxStatement {
	return flow.TxStatement{Exec: &flow.TxExec{Query: query, Capture: capture, When: when, Params: params}}
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestTransactionOrderCaptureEach covers the common aggregate write: a clean
// step, a parent insert capturing its autoincrement id, then a per-child insert
// referencing the captured id and the each index — all atomic.
func TestTransactionOrderCaptureEach(t *testing.T) {
	txCfg := &flow.TransactionConfig{Statements: []flow.TxStatement{
		mkExec(`DELETE FROM child WHERE parent_id IN (SELECT id FROM parent WHERE owner_id = :owner)`, "", "output.owner_id > 0",
			map[string]string{"owner": "output.owner_id"}),
		mkExec(`INSERT INTO parent (owner_id, name) VALUES (:owner, :name)`, "parent_id", "",
			map[string]string{"owner": "output.owner_id", "name": "output.name"}),
		{Each: &flow.TxEach{
			Var: "child", In: "output.children",
			Body: []flow.TxStatement{
				mkExec(`INSERT INTO child (parent_id, label, position) VALUES (:pid, :label, :pos)`, "child_id", "",
					map[string]string{"pid": "captured.parent_id", "label": "child.label", "pos": "child_index"}),
			},
		}},
	}}

	h, db := newTxHandler(t, txCfg)
	input := map[string]interface{}{
		"owner_id": 7,
		"name":     "agg",
		"children": []interface{}{
			map[string]interface{}{"label": "a"},
			map[string]interface{}{"label": "b"},
			map[string]interface{}{"label": "c"},
		},
	}

	if _, err := h.executeFlowCore(context.Background(), input); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := count(t, db, "parent"); got != 1 {
		t.Fatalf("parent rows = %d, want 1", got)
	}
	if got := count(t, db, "child"); got != 3 {
		t.Fatalf("child rows = %d, want 3", got)
	}

	// position must equal the 0-based each index, and all children must point
	// at the single parent's captured id.
	rows, err := db.Query(`SELECT c.label, c.position, c.parent_id, p.owner_id FROM child c JOIN parent p ON p.id = c.parent_id ORDER BY c.position`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	wantLabels := []string{"a", "b", "c"}
	i := 0
	for rows.Next() {
		var label string
		var pos, parentID, ownerID int
		if err := rows.Scan(&label, &pos, &parentID, &ownerID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if label != wantLabels[i] || pos != i {
			t.Fatalf("row %d: label=%q pos=%d, want label=%q pos=%d", i, label, pos, wantLabels[i], i)
		}
		if ownerID != 7 {
			t.Fatalf("row %d: owner_id=%d, want 7 (captured parent link broken)", i, ownerID)
		}
		i++
	}
	if i != 3 {
		t.Fatalf("iterated %d child rows, want 3", i)
	}
}

// TestTransactionNestedEach covers each inside each.
func TestTransactionNestedEach(t *testing.T) {
	txCfg := &flow.TransactionConfig{Statements: []flow.TxStatement{
		mkExec(`INSERT INTO parent (name) VALUES (:name)`, "parent_id", "", map[string]string{"name": "output.name"}),
		{Each: &flow.TxEach{
			Var: "child", In: "output.children",
			Body: []flow.TxStatement{
				mkExec(`INSERT INTO child (parent_id, label) VALUES (:pid, :label)`, "child_id", "",
					map[string]string{"pid": "captured.parent_id", "label": "child.label"}),
				{Each: &flow.TxEach{
					Var: "store", In: "child.stores",
					Body: []flow.TxStatement{
						mkExec(`INSERT INTO child_value (child_id, store_id, val) VALUES (:cid, :sid, :v)`, "", "",
							map[string]string{"cid": "captured.child_id", "sid": "store.id", "v": "store.value"}),
					},
				}},
			},
		}},
	}}

	h, db := newTxHandler(t, txCfg)
	input := map[string]interface{}{
		"name": "agg",
		"children": []interface{}{
			map[string]interface{}{"label": "a", "stores": []interface{}{
				map[string]interface{}{"id": 1, "value": "x"},
				map[string]interface{}{"id": 2, "value": "y"},
			}},
			map[string]interface{}{"label": "b", "stores": []interface{}{
				map[string]interface{}{"id": 3, "value": "z"},
			}},
		},
	}

	if _, err := h.executeFlowCore(context.Background(), input); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := count(t, db, "child"); got != 2 {
		t.Fatalf("child rows = %d, want 2", got)
	}
	if got := count(t, db, "child_value"); got != 3 {
		t.Fatalf("child_value rows = %d, want 3 (nested each)", got)
	}
}

// TestTransactionCaptureScalarSelect covers capturing a scalar from a SELECT,
// the 0-rows -> null case, and a `when` gate testing the captured null.
func TestTransactionCaptureScalarSelect(t *testing.T) {
	h, db := newTxHandler(t, nil) // build DB first to seed lookup
	if _, err := db.Exec(`INSERT INTO lookup (option_id, code, label) VALUES (42, 'C', 'L')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h.Config.To.Transaction = &flow.TransactionConfig{Statements: []flow.TxStatement{
		// Existing row -> captures 42, then inserts a parent named "42".
		mkExec(`SELECT option_id FROM lookup WHERE code = :c AND label = :l LIMIT 1`, "found", "",
			map[string]string{"c": "output.code", "l": "output.label"}),
		mkExec(`INSERT INTO parent (owner_id, name) VALUES (:oid, 'hit')`, "", "captured.found != null",
			map[string]string{"oid": "captured.found"}),
		// Missing row -> captures null, dependent insert is skipped.
		mkExec(`SELECT option_id FROM lookup WHERE code = :c LIMIT 1`, "missing", "",
			map[string]string{"c": "output.absent"}),
		mkExec(`INSERT INTO parent (name) VALUES ('miss')`, "", "captured.missing != null", nil),
	}}

	input := map[string]interface{}{"code": "C", "label": "L", "absent": "ZZZ"}
	if _, err := h.executeFlowCore(context.Background(), input); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Only the "hit" row should exist (owner_id captured from SELECT = 42).
	var name string
	var ownerID int
	if err := db.QueryRow(`SELECT name, owner_id FROM parent`).Scan(&name, &ownerID); err != nil {
		t.Fatalf("query parent: %v", err)
	}
	if name != "hit" || ownerID != 42 {
		t.Fatalf("parent = (%q, %d), want (\"hit\", 42)", name, ownerID)
	}
	if got := count(t, db, "parent"); got != 1 {
		t.Fatalf("parent rows = %d, want 1 (the null-capture insert must be skipped)", got)
	}
}

// TestTransactionWhenFalseSkips confirms a false when gate skips the statement
// without error.
func TestTransactionWhenFalseSkips(t *testing.T) {
	txCfg := &flow.TransactionConfig{Statements: []flow.TxStatement{
		mkExec(`INSERT INTO parent (name) VALUES ('skipped')`, "", "output.go == true", nil),
		mkExec(`INSERT INTO parent (name) VALUES ('ran')`, "", "", nil),
	}}
	h, db := newTxHandler(t, txCfg)

	if _, err := h.executeFlowCore(context.Background(), map[string]interface{}{"go": false}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := count(t, db, "parent"); got != 1 {
		t.Fatalf("parent rows = %d, want 1 (gated insert must be skipped)", got)
	}
	var name string
	_ = db.QueryRow(`SELECT name FROM parent`).Scan(&name)
	if name != "ran" {
		t.Fatalf("name = %q, want \"ran\"", name)
	}
}

// TestTransactionRollbackOnError confirms a failing statement rolls back every
// prior write in the same transaction and surfaces the error.
func TestTransactionRollbackOnError(t *testing.T) {
	txCfg := &flow.TransactionConfig{Statements: []flow.TxStatement{
		mkExec(`INSERT INTO parent (name) VALUES ('before-error')`, "parent_id", "", nil),
		mkExec(`INSERT INTO child (parent_id, label) VALUES (:pid, :label)`, "", "",
			map[string]string{"pid": "captured.parent_id", "label": "output.label"}),
		mkExec(`INSERT INTO nonexistent_table (x) VALUES (1)`, "", "", nil), // boom
	}}
	h, db := newTxHandler(t, txCfg)

	_, err := h.executeFlowCore(context.Background(), map[string]interface{}{"label": "a"})
	if err == nil {
		t.Fatal("expected error from failing statement, got nil")
	}
	if got := count(t, db, "parent"); got != 0 {
		t.Fatalf("parent rows = %d, want 0 (rollback must undo the parent insert)", got)
	}
	if got := count(t, db, "child"); got != 0 {
		t.Fatalf("child rows = %d, want 0 (rollback must undo the child insert)", got)
	}
}

// --- error_handling envelope: a mock TxRunner that fails then succeeds ---

type mockTxConn struct {
	name      string
	failFirst int // number of leading RunInTx calls to fail
	calls     int
}

func (m *mockTxConn) Name() string                      { return m.name }
func (m *mockTxConn) Type() string                      { return "database" }
func (m *mockTxConn) Connect(ctx context.Context) error { return nil }
func (m *mockTxConn) Close(ctx context.Context) error   { return nil }
func (m *mockTxConn) Health(ctx context.Context) error  { return nil }
func (m *mockTxConn) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	return &connector.Result{}, nil
}

func (m *mockTxConn) RunInTx(ctx context.Context, fn func(connector.TxOps) error) error {
	m.calls++
	if m.calls <= m.failFirst {
		return fmt.Errorf("simulated transient failure %d", m.calls)
	}
	return fn(noopTxOps{})
}

type noopTxOps struct{}

func (noopTxOps) Exec(ctx context.Context, q string, p map[string]interface{}) (int64, int64, error) {
	return 1, 1, nil
}
func (noopTxOps) QueryScalar(ctx context.Context, q string, p map[string]interface{}) (interface{}, error) {
	return nil, nil
}

// TestTransactionWrappedByRetry confirms the transaction runs inside the flow's
// error_handling/retry envelope: a transient failure is retried and the second
// attempt commits.
func TestTransactionWrappedByRetry(t *testing.T) {
	conn := &mockTxConn{name: "db", failFirst: 1}
	registry := connector.NewRegistry()
	registry.Replace("db", conn)

	h := &FlowHandler{
		Config: &flow.Config{
			Name: "tx_retry",
			From: &flow.FromConfig{Connector: "db", ConnectorParams: map[string]interface{}{"operation": "INSERT"}},
			To: &flow.ToConfig{Connector: "db", Transaction: &flow.TransactionConfig{Statements: []flow.TxStatement{
				mkExec(`INSERT INTO parent (name) VALUES ('x')`, "", "", nil),
			}}},
			ErrorHandling: &flow.ErrorHandlingConfig{Retry: &flow.RetryConfig{Attempts: 3, Delay: "1ms", Backoff: "constant"}},
		},
		Dest:       conn,
		Connectors: registry,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if _, err := h.HandleRequest(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if conn.calls != 2 {
		t.Fatalf("RunInTx calls = %d, want 2 (one failure + one retry success)", conn.calls)
	}
}

// TestTransactionBackwardCompatClassicWrite confirms a classic to.query write
// (no transaction) still works through the same handler/connector.
func TestTransactionBackwardCompatClassicWrite(t *testing.T) {
	h, db := newTxHandler(t, nil)
	h.Config.To.Transaction = nil
	h.Config.To.ConnectorParams = map[string]interface{}{
		"query":     "INSERT INTO parent (name) VALUES (:name)",
		"operation": "INSERT",
	}

	if _, err := h.executeFlowCore(context.Background(), map[string]interface{}{"name": "classic"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := count(t, db, "parent"); got != 1 {
		t.Fatalf("parent rows = %d, want 1 (classic write must still work)", got)
	}
}
