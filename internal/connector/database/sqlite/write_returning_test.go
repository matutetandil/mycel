package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A write that asks for rows gets them.
//
// The recogniser had a test and the path behind it had none: nothing ever ran
// a RETURNING statement through Write, which is how a flow gets back the row
// it just created — the shape a GraphQL mutation answering `User!` depends on,
// and the only way to read a generated key on a driver with no last insert id.
func TestAWriteThatReturnsRowsHandsThemBack(t *testing.T) {
	c := connected(t)
	ctx := context.Background()

	mustExec(t, c, `CREATE TABLE items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sku TEXT NOT NULL,
		quantity INTEGER
	)`)

	written, err := c.Write(ctx, &connector.Data{
		RawSQL: "INSERT INTO items (sku, quantity) VALUES (:sku, :quantity) RETURNING id, sku, quantity",
		Payload: map[string]interface{}{
			"sku": "SKU-1", "quantity": 4,
		},
	})
	if err != nil {
		t.Fatalf("write with RETURNING: %v", err)
	}
	if len(written.Rows) != 1 {
		t.Fatalf("%d rows came back from a RETURNING insert", len(written.Rows))
	}

	row := written.Rows[0]
	if row["sku"] != "SKU-1" {
		t.Errorf("sku = %#v", row["sku"])
	}
	if id, ok := row["id"].(int64); !ok || id == 0 {
		t.Errorf("id = %#v (%T), want the key the database assigned", row["id"], row["id"])
	}

	// An update can ask too.
	updated, err := c.Write(ctx, &connector.Data{
		RawSQL:  "UPDATE items SET quantity = :quantity WHERE sku = :sku RETURNING quantity",
		Payload: map[string]interface{}{"quantity": 9, "sku": "SKU-1"},
	})
	if err != nil {
		t.Fatalf("update with RETURNING: %v", err)
	}
	if len(updated.Rows) != 1 {
		t.Fatalf("%d rows came back from a RETURNING update", len(updated.Rows))
	}
	if quantity, _ := updated.Rows[0]["quantity"].(int64); quantity != 9 {
		t.Errorf("quantity = %#v", updated.Rows[0]["quantity"])
	}

	// And one that asks for nothing still says how many it touched.
	plain, err := c.Write(ctx, &connector.Data{
		RawSQL:  "UPDATE items SET quantity = :quantity WHERE sku = :sku",
		Payload: map[string]interface{}{"quantity": 1, "sku": "SKU-1"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if plain.Affected != 1 {
		t.Errorf("affected = %d, want 1", plain.Affected)
	}
	if len(plain.Rows) != 0 {
		t.Errorf("a statement that asked for no rows produced %d", len(plain.Rows))
	}
}

// What a colon means depends on where it is.
//
// `:sku` is a parameter, `::int` is a cast somebody wrote for another driver
// and left, and a colon inside quotes is part of the text. Getting any of them
// wrong turns a working query into a syntax error or, worse, into one that
// runs against the wrong value.
func TestWhatACounterColonMeansDependsOnWhereItIs(t *testing.T) {
	c := connected(t)
	ctx := context.Background()

	mustExec(t, c, `CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)
	mustExec(t, c, `INSERT INTO notes (id, body) VALUES (1, 'ratio 3:1'), (2, 'plain')`)

	// A colon inside a string literal is text.
	found, err := c.Read(ctx, connector.Query{
		RawSQL:  "SELECT id FROM notes WHERE body = 'ratio 3:1' AND id = :id",
		Filters: map[string]interface{}{"id": 1},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(found.Rows) != 1 {
		t.Errorf("%d rows, want the one whose text contains a colon", len(found.Rows))
	}

	// A name nothing supplies is left as written, so a cast survives.
	sql, args, err := c.parseNamedParams("SELECT :id, ':not_a_param', :missing", map[string]interface{}{"id": 1})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if strings.Count(sql, "?") != 1 {
		t.Errorf("sql = %q, want one bound parameter", sql)
	}
	if !strings.Contains(sql, ":missing") {
		t.Errorf("sql = %q, want the unsupplied name left alone", sql)
	}
	if !strings.Contains(sql, "':not_a_param'") {
		t.Errorf("sql = %q, want the quoted text untouched", sql)
	}
	if len(args) != 1 || args[0] != 1 {
		t.Errorf("args = %#v", args)
	}
}

// The lifecycle: what a connector says about itself, and that it can be closed
// and asked about its health.
func TestAConnectorSaysWhatItIs(t *testing.T) {
	c := connected(t)

	if c.Name() != "db" {
		t.Errorf("name = %q", c.Name())
	}
	if c.Type() != "database" {
		t.Errorf("type = %q", c.Type())
	}
	if c.DB() == nil {
		t.Error("no handle came back, so nothing that needs one can work")
	}
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("health: %v", err)
	}

	if err := c.Close(context.Background()); err != nil {
		t.Errorf("close: %v", err)
	}
	// Closing twice is what shutdown does when something already has.
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("second close: %v", err)
	}
	if err := c.Health(context.Background()); err == nil {
		t.Error("a closed connector reported itself healthy")
	}
}

func mustExec(t *testing.T, c *Connector, statement string) {
	t.Helper()
	if _, err := c.DB().ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}
