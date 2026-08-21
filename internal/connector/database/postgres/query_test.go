package postgres

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// The SQL a flow ends up sending is built before any connection is needed, so
// it can be read here without a server. Postgres numbers its placeholders, and
// a number that does not line up with the value behind it is the kind of defect
// that shows as the wrong row being updated rather than as an error.

func builder() *Connector {
	return New("db", "localhost", 5432, "app", "user", "secret", "disable")
}

func TestAFilteredSelectNumbersItsPlaceholdersInOrder(t *testing.T) {
	sql, args := builder().buildSelectQuery(connector.Query{
		Target:  "users",
		Filters: map[string]interface{}{"name": "Ada", "age": 36},
	})

	// Sorted, so the text is the same on every call.
	if !strings.Contains(sql, "age = $1") || !strings.Contains(sql, "name = $2") {
		t.Fatalf("sql = %q", sql)
	}
	if len(args) != 2 || args[0] != 36 || args[1] != "Ada" {
		t.Errorf("args = %v, want each value behind its own placeholder", args)
	}
}

func TestAnInsertNamesItsColumnsAndCarriesItsValues(t *testing.T) {
	sql, args := builder().buildInsertQuery(&connector.Data{
		Target:  "users",
		Payload: map[string]interface{}{"name": "Ada", "email": "ada@example.com"},
	})

	if !strings.Contains(sql, "INSERT INTO users (email, name)") {
		t.Errorf("sql = %q", sql)
	}
	// Postgres can hand the row back, which is how a flow captures the
	// identifier the database generated — there is no LastInsertId here.
	if !strings.Contains(sql, "RETURNING") {
		t.Errorf("sql = %q, want it to return the row it wrote", sql)
	}
	if len(args) != 2 || args[0] != "ada@example.com" || args[1] != "Ada" {
		t.Errorf("args = %v", args)
	}
}

func TestAnUpdateNumbersTheValuesBeforeTheConditions(t *testing.T) {
	// The clauses share one numbering, and getting it wrong swaps a value
	// somebody is setting with one they are matching on.
	sql, args := builder().buildUpdateQuery(&connector.Data{
		Target:  "users",
		Payload: map[string]interface{}{"email": "new@example.com", "active": false},
		Filters: map[string]interface{}{"id": 7},
	})

	if !strings.Contains(sql, "SET active = $1, email = $2") {
		t.Fatalf("sql = %q", sql)
	}
	if !strings.Contains(sql, "WHERE id = $3") {
		t.Fatalf("sql = %q, want the condition numbered after the values", sql)
	}
	if len(args) != 3 || args[0] != false || args[1] != "new@example.com" || args[2] != 7 {
		t.Errorf("args = %v", args)
	}
}

func TestADeleteCarriesItsConditions(t *testing.T) {
	sql, args := builder().buildDeleteQuery(&connector.Data{
		Target:  "users",
		Filters: map[string]interface{}{"tenant": "acme", "id": 7},
	})
	if !strings.Contains(sql, "DELETE FROM users WHERE id = $1 AND tenant = $2") {
		t.Errorf("sql = %q", sql)
	}
	if len(args) != 2 || args[0] != 7 || args[1] != "acme" {
		t.Errorf("args = %v", args)
	}
}

func TestTheSameQueryProducesTheSameSQL(t *testing.T) {
	// The driver caches prepared statements by the text of the query, and this
	// was built by walking a map: the same logical query came out differently
	// on each call, so the cache missed every time.
	c := builder()
	query := connector.Query{
		Target: "users",
		Filters: map[string]interface{}{
			"name": "Ada", "email": "ada@example.com", "age": 36, "tenant": "acme",
		},
	}
	data := &connector.Data{
		Target:  "users",
		Payload: map[string]interface{}{"name": "Ada", "email": "ada@example.com", "age": 36},
		Filters: map[string]interface{}{"id": 7, "tenant": "acme"},
	}

	firstSelect, _ := c.buildSelectQuery(query)
	firstInsert, _ := c.buildInsertQuery(data)
	firstUpdate, _ := c.buildUpdateQuery(data)
	firstDelete, _ := c.buildDeleteQuery(data)

	for i := 0; i < 25; i++ {
		if got, _ := c.buildSelectQuery(query); got != firstSelect {
			t.Fatalf("select built as\n  %s\nand then\n  %s", firstSelect, got)
		}
		if got, _ := c.buildInsertQuery(data); got != firstInsert {
			t.Fatalf("insert built as\n  %s\nand then\n  %s", firstInsert, got)
		}
		if got, _ := c.buildUpdateQuery(data); got != firstUpdate {
			t.Fatalf("update built as\n  %s\nand then\n  %s", firstUpdate, got)
		}
		if got, _ := c.buildDeleteQuery(data); got != firstDelete {
			t.Fatalf("delete built as\n  %s\nand then\n  %s", firstDelete, got)
		}
	}
}

func TestOrderAndPaginationReachTheQuery(t *testing.T) {
	sql, _ := builder().buildSelectQuery(connector.Query{
		Target:     "users",
		Fields:     []string{"id", "name"},
		OrderBy:    []connector.OrderClause{{Field: "name", Desc: true}},
		Pagination: &connector.Pagination{Limit: 10, Offset: 20},
	})
	for _, want := range []string{"SELECT id, name", "ORDER BY name DESC", "LIMIT 10", "OFFSET 20"} {
		if !strings.Contains(sql, want) {
			t.Errorf("sql = %q, want it to contain %q", sql, want)
		}
	}
}
