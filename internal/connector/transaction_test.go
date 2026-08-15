package connector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// All of it or none of it.
//
// This is the machinery behind `to { transaction { } }`: an ordered list of
// statements on one pinned connection inside a single BEGIN/COMMIT. The point
// is the guarantee — an order row without its lines, or stock taken off
// without the order that took it, is worse than the write failing — and the
// guarantee itself had no test. Every SQL connector delegates to this, so it
// is one copy of the rollback for all of them.

func memoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, reference TEXT, total REAL)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE order_items (id INTEGER PRIMARY KEY, order_id INTEGER, sku TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	return db
}

// positional turns :name placeholders into ? in the order they appear, which
// is what each SQL connector's own parser does.
func positional(query string, params map[string]interface{}) (string, []interface{}) {
	var args []interface{}
	out := query
	for {
		start := strings.Index(out, ":")
		if start < 0 {
			break
		}
		end := start + 1
		for end < len(out) && (out[end] == '_' ||
			(out[end] >= 'a' && out[end] <= 'z') ||
			(out[end] >= 'A' && out[end] <= 'Z') ||
			(out[end] >= '0' && out[end] <= '9')) {
			end++
		}
		args = append(args, params[out[start+1:end]])
		out = out[:start] + "?" + out[end:]
	}
	return out, args
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestEveryStatementOrNone(t *testing.T) {
	db := memoryDB(t)
	ctx := context.Background()

	err := RunInSQLTx(ctx, db, positional, func(ops TxOps) error {
		id, _, err := ops.Exec(ctx,
			"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
			map[string]interface{}{"reference": "order-1", "total": 42.5})
		if err != nil {
			return err
		}
		// The parent's identifier flowing into its children is the whole
		// reason these run together.
		_, affected, err := ops.Exec(ctx,
			"INSERT INTO order_items (order_id, sku) VALUES (:order_id, :sku)",
			map[string]interface{}{"order_id": id, "sku": "SKU-1"})
		if err != nil {
			return err
		}
		if affected != 1 {
			t.Errorf("affected = %d", affected)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInSQLTx: %v", err)
	}

	if countRows(t, db, "orders") != 1 || countRows(t, db, "order_items") != 1 {
		t.Errorf("orders = %d, items = %d", countRows(t, db, "orders"), countRows(t, db, "order_items"))
	}

	var orderID int
	if err := db.QueryRow("SELECT order_id FROM order_items").Scan(&orderID); err != nil {
		t.Fatalf("read: %v", err)
	}
	if orderID != 1 {
		t.Errorf("the line was attached to order %d", orderID)
	}
}

func TestAStatementThatFailsTakesTheOthersWithIt(t *testing.T) {
	// The failure this exists to prevent: an order row with no lines under
	// it, which nothing downstream can tell from an order of nothing.
	db := memoryDB(t)
	ctx := context.Background()

	refused := errors.New("that product is not in the catalogue")
	err := RunInSQLTx(ctx, db, positional, func(ops TxOps) error {
		if _, _, err := ops.Exec(ctx,
			"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
			map[string]interface{}{"reference": "order-1", "total": 42.5}); err != nil {
			return err
		}
		return refused
	})

	if !errors.Is(err, refused) {
		t.Fatalf("error = %v, want the one the statement gave", err)
	}
	if countRows(t, db, "orders") != 0 {
		t.Error("the order stayed behind after the transaction failed")
	}
}

func TestAStatementTheDatabaseRefuses(t *testing.T) {
	// A constraint, a column that is not there: the error has to come back
	// as it is, so error_handling can tell a duplicate from a deadlock.
	db := memoryDB(t)
	ctx := context.Background()

	err := RunInSQLTx(ctx, db, positional, func(ops TxOps) error {
		_, _, execErr := ops.Exec(ctx, "INSERT INTO orders (nonexistent) VALUES (:x)",
			map[string]interface{}{"x": 1})
		return execErr
	})

	if err == nil {
		t.Fatal("a statement the database refused was reported as done")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("the error does not say what the database objected to: %v", err)
	}
	if countRows(t, db, "orders") != 0 {
		t.Error("rows were left behind")
	}
}

func TestAPanicRollsBackAndIsNotSwallowed(t *testing.T) {
	// A bug in the executor must not leave a connection sitting inside an
	// open transaction — that connection is then unusable and the rows it
	// holds are locked against everyone else.
	db := memoryDB(t)
	ctx := context.Background()

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("the panic was swallowed, so a bug would go unnoticed")
			}
		}()
		_ = RunInSQLTx(ctx, db, positional, func(ops TxOps) error {
			if _, _, err := ops.Exec(ctx,
				"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
				map[string]interface{}{"reference": "order-1", "total": 1}); err != nil {
				return err
			}
			panic("a bug in the executor")
		})
	}()

	if countRows(t, db, "orders") != 0 {
		t.Error("the row stayed behind after a panic")
	}
	// And the database is still usable, which it would not be if the
	// transaction were still open.
	if _, err := db.Exec("INSERT INTO orders (reference, total) VALUES ('order-2', 1)"); err != nil {
		t.Errorf("the database was left unusable: %v", err)
	}
}

func TestReadingOneValueBackMidTransaction(t *testing.T) {
	// What `capture` does: a statement reads something the later ones use,
	// inside the same transaction, so it sees what has just been written.
	db := memoryDB(t)
	ctx := context.Background()

	err := RunInSQLTx(ctx, db, positional, func(ops TxOps) error {
		if _, _, err := ops.Exec(ctx,
			"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
			map[string]interface{}{"reference": "order-1", "total": 42.5}); err != nil {
			return err
		}

		reference, err := ops.QueryScalar(ctx,
			"SELECT reference FROM orders WHERE reference = :reference",
			map[string]interface{}{"reference": "order-1"})
		if err != nil {
			return err
		}
		// Text comes back from database/sql as bytes; captured as bytes it
		// renders as a list of numbers wherever the flow uses it.
		if text, ok := reference.(string); !ok || text != "order-1" {
			t.Errorf("captured %#v, want the text", reference)
		}

		// Nothing found is nothing, not an error: a `when` gate later can
		// test for it, which is how "insert only if absent" is written.
		missing, err := ops.QueryScalar(ctx,
			"SELECT reference FROM orders WHERE reference = :reference",
			map[string]interface{}{"reference": "order-none"})
		if err != nil {
			return err
		}
		if missing != nil {
			t.Errorf("a row that is not there came back as %#v", missing)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInSQLTx: %v", err)
	}
}

func TestAQueryThatDoesNotRun(t *testing.T) {
	db := memoryDB(t)
	ctx := context.Background()

	err := RunInSQLTx(ctx, db, positional, func(ops TxOps) error {
		_, err := ops.QueryScalar(ctx, "SELECT nonexistent FROM orders", nil)
		return err
	})
	if err == nil {
		t.Error("a query the database refused came back with a value")
	}
}

func TestATransactionOnADatabaseThatIsNotConnected(t *testing.T) {
	// Said plainly rather than as a nil dereference: this is reachable when a
	// connector is used before start-up finished.
	err := RunInSQLTx(context.Background(), nil, positional, func(ops TxOps) error {
		return fmt.Errorf("should never run")
	})
	if err == nil {
		t.Fatal("a transaction ran on a database that is not there")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("error = %v", err)
	}
}

func TestAConnectorWithNoParameterParser(t *testing.T) {
	// A driver that binds nothing still has to run the statement rather than
	// panic on the missing parser.
	db := memoryDB(t)
	ctx := context.Background()

	err := RunInSQLTx(ctx, db, nil, func(ops TxOps) error {
		_, _, err := ops.Exec(ctx, "INSERT INTO orders (reference, total) VALUES ('order-1', 1)", nil)
		return err
	})
	if err != nil {
		t.Fatalf("RunInSQLTx: %v", err)
	}
	if countRows(t, db, "orders") != 1 {
		t.Error("the statement did not run")
	}
}
