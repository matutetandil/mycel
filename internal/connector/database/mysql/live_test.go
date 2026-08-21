package mysql

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Reading and writing against a real MySQL.
//
// Read and Write are the two methods every flow goes through and they were
// covered at six and eight per cent — not because nothing exercises them, but
// because what does is the integration suite, which runs the binary and sees
// none of this from Go's side. What a black-box suite cannot check is the part
// that goes wrong quietly: what a column's type comes back as, whether a named
// parameter binds, whether a filter reaches the WHERE clause.
func liveMySQLConnector(t *testing.T) *Connector {
	t.Helper()

	dsn := os.Getenv("MYCEL_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set MYCEL_TEST_MYSQL_DSN to run this against a real database")
	}

	// user:password@tcp(host:port)/database
	parts := regexp.MustCompile(`^([^:]+):([^@]*)@tcp\(([^:]+):(\d+)\)/([^?]+)`).FindStringSubmatch(dsn)
	if parts == nil {
		t.Fatalf("MYCEL_TEST_MYSQL_DSN is not the shape this expects: %s", dsn)
	}
	port, _ := strconv.Atoi(parts[4])

	c := New("db", parts[3], port, parts[5], parts[1], parts[2], "utf8mb4")
	if err := c.Connect(context.Background()); err != nil {
		t.Skipf("no database at %s:%d: %v", parts[3], port, err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestAMySQLRoundTrip(t *testing.T) {
	c := liveMySQLConnector(t)
	ctx := context.Background()

	table := "mycel_live_" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	exec(t, c, `CREATE TABLE `+table+` (
		id INT AUTO_INCREMENT PRIMARY KEY,
		sku VARCHAR(64) NOT NULL,
		quantity INT,
		price DECIMAL(10,2),
		active BOOLEAN,
		note TEXT
	)`)
	t.Cleanup(func() { exec(t, c, `DROP TABLE IF EXISTS `+table) })

	// A write with no SQL of its own: the column list is built from what the
	// flow produced.
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
	if written.LastID == 0 {
		t.Error("no insert id came back, so a flow cannot answer with one")
	}

	// A read by filter, which is what a flow without its own query does.
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
	// What the types come back as is the thing a flow does arithmetic on.
	if row["sku"] != "SKU-1" {
		t.Errorf("sku = %#v", row["sku"])
	}
	if quantity, ok := row["quantity"].(int64); !ok || quantity != 3 {
		t.Errorf("quantity = %#v (%T), want a number", row["quantity"], row["quantity"])
	}
	if row["note"] != nil {
		t.Errorf("a NULL column came back as %#v, want nothing", row["note"])
	}

	// A query of its own, with a named parameter — the form every guide shows.
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

	// An update through the same door.
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

	after, err := c.Read(ctx, connector.Query{
		RawSQL:  "SELECT quantity FROM " + table + " WHERE sku = :sku",
		Filters: map[string]interface{}{"sku": "SKU-1"},
	})
	if err != nil {
		t.Fatalf("read after update: %v", err)
	}
	if quantity, _ := after.Rows[0]["quantity"].(int64); quantity != 10 {
		t.Errorf("quantity after update = %#v", after.Rows[0]["quantity"])
	}

	// And a delete.
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

// A statement that returns rows is asked as a query, and one that does not is
// executed — asking the wrong way is how an INSERT comes back as "no rows".
func TestAWriteWithItsOwnSQL(t *testing.T) {
	c := liveMySQLConnector(t)
	ctx := context.Background()

	table := "mycel_sql_" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	exec(t, c, `CREATE TABLE `+table+` (id INT AUTO_INCREMENT PRIMARY KEY, sku VARCHAR(64))`)
	t.Cleanup(func() { exec(t, c, `DROP TABLE IF EXISTS `+table) })

	written, err := c.Write(ctx, &connector.Data{
		RawSQL:  "INSERT INTO " + table + " (sku) VALUES (:sku)",
		Payload: map[string]interface{}{"sku": "SKU-2"},
	})
	if err != nil {
		t.Fatalf("write with raw SQL: %v", err)
	}
	if written.Affected != 1 {
		t.Errorf("affected = %d", written.Affected)
	}
}

func exec(t *testing.T, c *Connector, statement string) {
	t.Helper()
	if _, err := c.DB().ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}
