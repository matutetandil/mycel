package sqlite

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A transaction block is the promise that a set of writes either all happened
// or none did — an order and its lines, an inventory decrement beside them.
// The whole point is what the database looks like after something goes wrong
// halfway through, which is exactly what nobody exercises by hand.

func withOrders(t *testing.T) *Connector {
	t.Helper()
	conn := New("db", ":memory:", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	for _, ddl := range []string{
		`CREATE TABLE orders (id INTEGER PRIMARY KEY AUTOINCREMENT, reference TEXT NOT NULL UNIQUE, total REAL)`,
		`CREATE TABLE order_items (id INTEGER PRIMARY KEY AUTOINCREMENT, order_id INTEGER NOT NULL, sku TEXT NOT NULL, qty INTEGER)`,
		`CREATE TABLE inventory (sku TEXT PRIMARY KEY, on_hand INTEGER NOT NULL)`,
	} {
		if err := conn.Exec(context.Background(), ddl); err != nil {
			t.Fatalf("creating the tables: %v", err)
		}
	}
	if err := conn.Exec(context.Background(), `INSERT INTO inventory (sku, on_hand) VALUES ('A-1', 10)`); err != nil {
		t.Fatalf("seeding inventory: %v", err)
	}
	return conn
}

func count(t *testing.T, conn *Connector, table string) int {
	t.Helper()
	result, err := conn.Read(context.Background(), connector.Query{
		Target: table, RawSQL: "SELECT COUNT(*) AS n FROM " + table,
	})
	if err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	switch n := result.Rows[0]["n"].(type) {
	case int64:
		return int(n)
	case int:
		return n
	}
	t.Fatalf("count came back as %#v", result.Rows[0]["n"])
	return -1
}

func TestEverythingInATransactionLandsTogether(t *testing.T) {
	conn := withOrders(t)

	err := conn.RunInTx(context.Background(), func(ops connector.TxOps) error {
		orderID, _, err := ops.Exec(context.Background(),
			"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
			map[string]interface{}{"reference": "ORD-1", "total": 42.5})
		if err != nil {
			return err
		}

		// The identifier the database generated is what the lines hang off,
		// and it has to be available inside the same transaction.
		for _, sku := range []string{"A-1", "B-2"} {
			if _, _, err := ops.Exec(context.Background(),
				"INSERT INTO order_items (order_id, sku, qty) VALUES (:order_id, :sku, :qty)",
				map[string]interface{}{"order_id": orderID, "sku": sku, "qty": 1}); err != nil {
				return err
			}
		}

		_, affected, err := ops.Exec(context.Background(),
			"UPDATE inventory SET on_hand = on_hand - :qty WHERE sku = :sku",
			map[string]interface{}{"qty": 1, "sku": "A-1"})
		if err != nil {
			return err
		}
		if affected != 1 {
			t.Errorf("the inventory update touched %d rows", affected)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInTx: %v", err)
	}

	if got := count(t, conn, "orders"); got != 1 {
		t.Errorf("orders = %d", got)
	}
	if got := count(t, conn, "order_items"); got != 2 {
		t.Errorf("order items = %d", got)
	}
}

func TestNothingIsLeftBehindWhenAStepFails(t *testing.T) {
	// The parent row was written before the failure. If it survives, a service
	// has an order with no lines and an inventory that never moved — the state
	// a transaction exists to prevent.
	conn := withOrders(t)

	failure := errors.New("the third statement failed")
	err := conn.RunInTx(context.Background(), func(ops connector.TxOps) error {
		orderID, _, err := ops.Exec(context.Background(),
			"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
			map[string]interface{}{"reference": "ORD-2", "total": 10})
		if err != nil {
			return err
		}
		if _, _, err := ops.Exec(context.Background(),
			"INSERT INTO order_items (order_id, sku, qty) VALUES (:order_id, :sku, :qty)",
			map[string]interface{}{"order_id": orderID, "sku": "A-1", "qty": 1}); err != nil {
			return err
		}
		return failure
	})

	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the failure that stopped it", err)
	}
	if got := count(t, conn, "orders"); got != 0 {
		t.Errorf("%d orders survived a rolled back transaction", got)
	}
	if got := count(t, conn, "order_items"); got != 0 {
		t.Errorf("%d order lines survived a rolled back transaction", got)
	}
}

func TestAStatementTheDatabaseRefusesRollsBackTheRest(t *testing.T) {
	// Not an error the flow returned — one the database raised. The unique
	// constraint on reference is the everyday version: the same order arriving
	// twice.
	conn := withOrders(t)
	if err := conn.Exec(context.Background(),
		`INSERT INTO orders (reference, total) VALUES ('ORD-3', 1)`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	err := conn.RunInTx(context.Background(), func(ops connector.TxOps) error {
		if _, _, err := ops.Exec(context.Background(),
			"UPDATE inventory SET on_hand = on_hand - :qty WHERE sku = :sku",
			map[string]interface{}{"qty": 5, "sku": "A-1"}); err != nil {
			return err
		}
		_, _, err := ops.Exec(context.Background(),
			"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
			map[string]interface{}{"reference": "ORD-3", "total": 2})
		return err
	})
	if err == nil {
		t.Fatal("a duplicate order was accepted")
	}

	// The inventory decrement, which succeeded, must be gone too.
	result, _ := conn.Read(context.Background(), connector.Query{
		Target: "inventory", Filters: map[string]interface{}{"sku": "A-1"},
	})
	if onHand := result.Rows[0]["on_hand"]; onHand != int64(10) {
		t.Errorf("on hand = %v, want the 10 it started with", onHand)
	}
}

func TestAValueCanBeReadBackInsideTheTransaction(t *testing.T) {
	// How a flow captures something to use in a later statement — the pattern
	// Postgres needs, since it has no last inserted id.
	conn := withOrders(t)

	var captured interface{}
	err := conn.RunInTx(context.Background(), func(ops connector.TxOps) error {
		if _, _, err := ops.Exec(context.Background(),
			"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
			map[string]interface{}{"reference": "ORD-4", "total": 99}); err != nil {
			return err
		}
		value, err := ops.QueryScalar(context.Background(),
			"SELECT id FROM orders WHERE reference = :reference",
			map[string]interface{}{"reference": "ORD-4"})
		captured = value
		return err
	})
	if err != nil {
		t.Fatalf("RunInTx: %v", err)
	}
	if captured == nil {
		t.Error("nothing was captured from inside the transaction")
	}
}

func TestReadingSomethingThatIsNotThereIsNotAFailure(t *testing.T) {
	// An empty result is an answer — nil — so a flow can decide what to do
	// rather than having the whole transaction fail.
	conn := withOrders(t)

	err := conn.RunInTx(context.Background(), func(ops connector.TxOps) error {
		value, err := ops.QueryScalar(context.Background(),
			"SELECT id FROM orders WHERE reference = :reference",
			map[string]interface{}{"reference": "nobody"})
		if err != nil {
			return err
		}
		if value != nil {
			t.Errorf("value = %#v, want nothing", value)
		}
		return nil
	})
	if err != nil {
		t.Errorf("RunInTx: %v", err)
	}
}

func TestWorkDoneBeforeAPanicIsRolledBack(t *testing.T) {
	// A panic inside a transaction must not leave it open, holding its
	// connection and its locks until the process ends.
	conn := withOrders(t)

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("the panic was swallowed")
			}
		}()
		_ = conn.RunInTx(context.Background(), func(ops connector.TxOps) error {
			if _, _, err := ops.Exec(context.Background(),
				"INSERT INTO orders (reference, total) VALUES (:reference, :total)",
				map[string]interface{}{"reference": "ORD-5", "total": 1}); err != nil {
				return err
			}
			panic("something in the flow went wrong")
		})
	}()

	if got := count(t, conn, "orders"); got != 0 {
		t.Errorf("%d orders survived a panic", got)
	}
	// And the database is still usable, rather than stuck behind an open
	// transaction.
	if _, err := conn.Write(context.Background(), &connector.Data{
		Target: "orders", Operation: "INSERT",
		Payload: map[string]interface{}{"reference": "ORD-6", "total": 1},
	}); err != nil {
		t.Errorf("the database was left unusable: %v", err)
	}
}

func TestATransactionOnADatabaseThatIsNotConnectedIsReported(t *testing.T) {
	conn := New("db", ":memory:", slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := conn.RunInTx(context.Background(), func(ops connector.TxOps) error { return nil })
	if err == nil {
		t.Error("a transaction ran on a database nobody connected to")
	}
}
