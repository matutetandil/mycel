package sqlite

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// SQLite is the connector every example and every quick start uses, and it runs
// entirely in this process — so what a flow does to a database can be checked
// here rather than only against a container.

func connected(t *testing.T) *Connector {
	t.Helper()
	conn := New("db", ":memory:", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	if err := conn.Exec(context.Background(), `CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT,
		age INTEGER,
		active BOOLEAN DEFAULT 1
	)`); err != nil {
		t.Fatalf("creating the table: %v", err)
	}
	return conn
}

func insert(t *testing.T, conn *Connector, values map[string]interface{}) *connector.Result {
	t.Helper()
	result, err := conn.Write(context.Background(), &connector.Data{
		Target: "users", Operation: "INSERT", Payload: values,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return result
}

func TestARowGoesInAndComesBack(t *testing.T) {
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada", "email": "ada@example.com", "age": 36})

	result, err := conn.Read(context.Background(), connector.Query{Target: "users"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("%d rows came back", len(result.Rows))
	}

	row := result.Rows[0]
	// Text has to arrive as text: read as bytes it would reach a caller as
	// base64 in the JSON response, which is what happens when a driver's
	// values are passed along without looking at them.
	if name, ok := row["name"].(string); !ok || name != "Ada" {
		t.Errorf("name = %#v, want the string \"Ada\"", row["name"])
	}
	if age := row["age"]; age != int64(36) {
		t.Errorf("age = %#v (%T), want a number", age, age)
	}
}

func TestTheIdentifierOfANewRowComesBack(t *testing.T) {
	// A flow writes the parent row and then its children, and the children
	// need the identifier the database gave the parent.
	conn := connected(t)
	result := insert(t, conn, map[string]interface{}{"name": "Ada"})

	firstID := asInt64(t, result.LastID)
	if firstID == 0 {
		t.Fatalf("no identifier came back: %+v", result)
	}
	if result.Affected != 1 {
		t.Errorf("rows affected = %d", result.Affected)
	}

	second := insert(t, conn, map[string]interface{}{"name": "Grace"})
	if secondID := asInt64(t, second.LastID); secondID <= firstID {
		t.Errorf("the second row got identifier %d after %d", secondID, firstID)
	}
}

func asInt64(t *testing.T, v interface{}) int64 {
	t.Helper()
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	}
	t.Fatalf("the identifier came back as %#v (%T), which a flow cannot use as one", v, v)
	return 0
}

func TestOnlyTheRowsThatMatchComeBack(t *testing.T) {
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada", "age": 36})
	insert(t, conn, map[string]interface{}{"name": "Grace", "age": 45})

	result, err := conn.Read(context.Background(), connector.Query{
		Target:  "users",
		Filters: map[string]interface{}{"name": "Grace"},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "Grace" {
		t.Errorf("rows = %v", result.Rows)
	}
}

func TestSeveralFiltersAllHaveToHold(t *testing.T) {
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada", "age": 36})
	insert(t, conn, map[string]interface{}{"name": "Ada", "age": 45})

	result, err := conn.Read(context.Background(), connector.Query{
		Target:  "users",
		Filters: map[string]interface{}{"name": "Ada", "age": 45},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("%d rows matched both filters, want 1", len(result.Rows))
	}
}

func TestOnlyTheFieldsAskedForComeBack(t *testing.T) {
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada", "email": "ada@example.com"})

	result, err := conn.Read(context.Background(), connector.Query{
		Target: "users", Fields: []string{"name"},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, present := result.Rows[0]["email"]; present {
		t.Errorf("a field nobody asked for came back: %v", result.Rows[0])
	}
}

func TestOrderAndPaginationAreWhatTheQuerySaid(t *testing.T) {
	conn := connected(t)
	for _, name := range []string{"Ada", "Grace", "Katherine"} {
		insert(t, conn, map[string]interface{}{"name": name})
	}

	result, err := conn.Read(context.Background(), connector.Query{
		Target:     "users",
		OrderBy:    []connector.OrderClause{{Field: "name", Desc: true}},
		Pagination: &connector.Pagination{Limit: 2, Offset: 1},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("%d rows came back, want the page of 2", len(result.Rows))
	}
	if result.Rows[0]["name"] != "Grace" {
		t.Errorf("the page starts at %v, want the second name descending", result.Rows[0]["name"])
	}
}

func TestAnUpdateTouchesOnlyWhatItMatched(t *testing.T) {
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada", "email": "old@example.com"})
	insert(t, conn, map[string]interface{}{"name": "Grace", "email": "grace@example.com"})

	result, err := conn.Write(context.Background(), &connector.Data{
		Target: "users", Operation: "UPDATE",
		Payload: map[string]interface{}{"email": "new@example.com"},
		Filters: map[string]interface{}{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("rows affected = %d, want the one that matched", result.Affected)
	}

	rows, _ := conn.Read(context.Background(), connector.Query{
		Target: "users", Filters: map[string]interface{}{"name": "Grace"},
	})
	if rows.Rows[0]["email"] != "grace@example.com" {
		t.Error("a row nobody matched was changed")
	}
}

func TestADeleteRemovesWhatItMatched(t *testing.T) {
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada"})
	insert(t, conn, map[string]interface{}{"name": "Grace"})

	if _, err := conn.Write(context.Background(), &connector.Data{
		Target: "users", Operation: "DELETE",
		Filters: map[string]interface{}{"name": "Ada"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, _ := conn.Read(context.Background(), connector.Query{Target: "users"})
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "Grace" {
		t.Errorf("rows left = %v", result.Rows)
	}
}

func TestRawSQLCarriesItsParametersByName(t *testing.T) {
	// The form a flow writes when the generated query is not enough. Values
	// travel as parameters, so a name holding a quote is a name, not an
	// injection.
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada", "age": 36})
	insert(t, conn, map[string]interface{}{"name": "Grace", "age": 45})

	result, err := conn.Read(context.Background(), connector.Query{
		Target: "users",
		RawSQL: "SELECT name FROM users WHERE age > :min_age ORDER BY age",
		Filters: map[string]interface{}{
			"min_age": 40,
		},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["name"] != "Grace" {
		t.Errorf("rows = %v", result.Rows)
	}
}

func TestAValueIsAValueEvenWhenItLooksLikeSQL(t *testing.T) {
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Robert'); DROP TABLE users;--"})

	result, err := conn.Read(context.Background(), connector.Query{
		Target:  "users",
		Filters: map[string]interface{}{"name": "Robert'); DROP TABLE users;--"},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("the row was not found by its own name: %v", result.Rows)
	}
	// And the table is still there.
	if _, err := conn.Read(context.Background(), connector.Query{Target: "users"}); err != nil {
		t.Errorf("the table is gone: %v", err)
	}
}

func TestTheSameQueryProducesTheSameSQL(t *testing.T) {
	// The driver caches prepared statements by the text of the query. Built by
	// ranging over the filters, the same logical query produced a different
	// string on each call, so the cache missed every time and the same query
	// appeared under several forms in logs and traces.
	conn := connected(t)
	query := connector.Query{
		Target: "users",
		Filters: map[string]interface{}{
			"name": "Ada", "email": "ada@example.com", "age": 36, "active": true,
		},
	}

	first, _ := conn.buildSelectQuery(query)
	for i := 0; i < 25; i++ {
		if got, _ := conn.buildSelectQuery(query); got != first {
			t.Fatalf("the same query built as\n  %s\nand then\n  %s", first, got)
		}
	}
}

func TestTheFiltersLineUpWithTheirValues(t *testing.T) {
	// Whatever the order, each condition has to carry its own value — this is
	// the property that makes the ordering safe to change.
	conn := connected(t)
	insert(t, conn, map[string]interface{}{"name": "Ada", "email": "ada@example.com", "age": 36})
	insert(t, conn, map[string]interface{}{"name": "Ada", "email": "other@example.com", "age": 36})

	result, err := conn.Read(context.Background(), connector.Query{
		Target: "users",
		Filters: map[string]interface{}{
			"name": "Ada", "email": "ada@example.com", "age": 36,
		},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("%d rows matched, want the one row that matches all three", len(result.Rows))
	}
}

func TestTheSameWriteProducesTheSameSQL(t *testing.T) {
	conn := connected(t)
	data := &connector.Data{
		Target:  "users",
		Payload: map[string]interface{}{"name": "Ada", "email": "ada@example.com", "age": 36},
	}

	firstInsert, _ := conn.buildInsertQuery(data)
	firstUpdate, _ := conn.buildUpdateQuery(&connector.Data{
		Target: "users", Payload: data.Payload,
		Filters: map[string]interface{}{"id": 1, "active": true},
	})
	for i := 0; i < 25; i++ {
		if got, _ := conn.buildInsertQuery(data); got != firstInsert {
			t.Fatalf("insert built as\n  %s\nand then\n  %s", firstInsert, got)
		}
		if got, _ := conn.buildUpdateQuery(&connector.Data{
			Target: "users", Payload: data.Payload,
			Filters: map[string]interface{}{"id": 1, "active": true},
		}); got != firstUpdate {
			t.Fatalf("update built as\n  %s\nand then\n  %s", firstUpdate, got)
		}
	}
}

func TestAQueryAgainstATableThatIsNotThereIsReported(t *testing.T) {
	conn := connected(t)
	_, err := conn.Read(context.Background(), connector.Query{Target: "absent"})
	if err == nil {
		t.Fatal("a query against a table that does not exist succeeded")
	}
	if !strings.Contains(err.Error(), "absent") {
		t.Errorf("error = %q, want the table named", err)
	}
}

func TestAWriteThatBreaksARuleIsReported(t *testing.T) {
	// name is NOT NULL, and a flow needs to hear about it rather than believe
	// the row was written.
	conn := connected(t)
	_, err := conn.Write(context.Background(), &connector.Data{
		Target: "users", Operation: "INSERT",
		Payload: map[string]interface{}{"email": "nobody@example.com"},
	})
	if err == nil {
		t.Error("a row that breaks a constraint was reported as written")
	}
}

func TestHealthNoticesWhenTheDatabaseIsGone(t *testing.T) {
	conn := connected(t)
	if err := conn.Health(context.Background()); err != nil {
		t.Fatalf("a connected database reported itself unhealthy: %v", err)
	}
	if err := conn.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := conn.Health(context.Background()); err == nil {
		t.Error("a closed database reported itself healthy")
	}
}
