package postgres

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Reading and writing against a real PostgreSQL.
//
// The same reason as the MySQL one beside it: Read and Write are what every
// flow goes through, and the suite that exercises them runs the binary, so
// none of it is visible from Go. What that suite cannot see is the part that
// goes wrong quietly — what a column's type comes back as, whether a named
// parameter becomes $1, whether an insert can answer with the id it assigned,
// which on this driver is not LastInsertId at all.
func livePostgresConnector(t *testing.T) *Connector {
	t.Helper()

	host, port := liveDSN(t)
	c := New("db", host, port, "mycel_test", "mycel", "mycel", "disable")
	if err := c.Connect(context.Background()); err != nil {
		t.Skipf("no database at %s:%d: %v", host, port, err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestAPostgresRoundTrip(t *testing.T) {
	c := livePostgresConnector(t)
	ctx := context.Background()

	table := "mycel_live_" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	exec(t, c, `CREATE TABLE `+table+` (
		id SERIAL PRIMARY KEY,
		sku TEXT NOT NULL,
		quantity INT,
		price NUMERIC(10,2),
		active BOOLEAN,
		note TEXT
	)`)
	t.Cleanup(func() { exec(t, c, `DROP TABLE IF EXISTS `+table) })

	written, err := c.Write(ctx, &connector.Data{
		Target:    table,
		Operation: "INSERT",
		Payload: map[string]interface{}{
			"sku": "SKU-1", "quantity": 3, "price": 9.99, "active": true, "note": nil,
		},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if written.Affected != 1 {
		t.Errorf("affected = %d, want 1", written.Affected)
	}
	// lib/pq has no LastInsertId, so an insert that wants to answer with the
	// id has to ask for it — RETURNING, which the connector adds.
	if written.LastID == 0 {
		t.Error("no id came back from an insert into a table with a serial key")
	}

	found, err := c.Read(ctx, connector.Query{
		Target:  table,
		Filters: map[string]interface{}{"sku": "SKU-1"},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(found.Rows) != 1 {
		t.Fatalf("%d rows came back, want the one written", len(found.Rows))
	}

	row := found.Rows[0]
	if row["sku"] != "SKU-1" {
		t.Errorf("sku = %#v", row["sku"])
	}
	if quantity, ok := row["quantity"].(int64); !ok || quantity != 3 {
		t.Errorf("quantity = %#v (%T), want a number", row["quantity"], row["quantity"])
	}
	if active, ok := row["active"].(bool); !ok || !active {
		t.Errorf("active = %#v (%T), want a boolean", row["active"], row["active"])
	}
	if row["note"] != nil {
		t.Errorf("a NULL column came back as %#v, want nothing", row["note"])
	}

	// A named parameter, which on this driver has to become $1.
	byName, err := c.Read(ctx, connector.Query{
		RawSQL:  "SELECT sku, quantity FROM " + table + " WHERE sku = :sku AND quantity >= :least",
		Filters: map[string]interface{}{"sku": "SKU-1", "least": 1},
	})
	if err != nil {
		t.Fatalf("read with named parameters: %v", err)
	}
	if len(byName.Rows) != 1 {
		t.Errorf("%d rows, want the one that matches", len(byName.Rows))
	}

	updated, err := c.Write(ctx, &connector.Data{
		Target:    table,
		Operation: "UPDATE",
		Payload:   map[string]interface{}{"quantity": 10},
		Filters:   map[string]interface{}{"sku": "SKU-1"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Affected != 1 {
		t.Errorf("update affected %d rows", updated.Affected)
	}

	deleted, err := c.Write(ctx, &connector.Data{
		Target:    table,
		Operation: "DELETE",
		Filters:   map[string]interface{}{"sku": "SKU-1"},
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted.Affected != 1 {
		t.Errorf("delete affected %d rows", deleted.Affected)
	}
}

// A cast written with two colons is not a parameter.
//
// `:name` binds and `::int` is PostgreSQL's cast — telling them apart is this
// driver's own problem, and getting it wrong turns a working query into a
// syntax error at the colon.
func TestACastIsNotAParameter(t *testing.T) {
	c := livePostgresConnector(t)

	found, err := c.Read(context.Background(), connector.Query{
		RawSQL:  "SELECT (:number)::int AS n",
		Filters: map[string]interface{}{"number": "42"},
	})
	if err != nil {
		t.Fatalf("a query with a cast and a parameter: %v", err)
	}
	if len(found.Rows) != 1 {
		t.Fatalf("%d rows", len(found.Rows))
	}
	if n, ok := found.Rows[0]["n"].(int64); !ok || n != 42 {
		t.Errorf("n = %#v (%T)", found.Rows[0]["n"], found.Rows[0]["n"])
	}
}

func exec(t *testing.T, c *Connector, statement string) {
	t.Helper()
	if _, err := c.DB().ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}
