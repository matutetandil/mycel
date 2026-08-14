package mysql

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// The SQL a flow ends up sending is built before any connection is needed, so
// it can be read here without a server. MySQL's placeholders are positional,
// so the order the values are collected in is the order they are bound in.

func builder() *Connector {
	return New("db", "localhost", 3306, "app", "user", "secret", "utf8mb4")
}

func TestAFilteredSelectBindsEachValueToItsColumn(t *testing.T) {
	sql, args := builder().buildSelectQuery(connector.Query{
		Target:  "users",
		Filters: map[string]interface{}{"name": "Ada", "age": 36},
	})

	if !strings.Contains(sql, "WHERE age = ? AND name = ?") {
		t.Fatalf("sql = %q", sql)
	}
	if len(args) != 2 || args[0] != 36 || args[1] != "Ada" {
		t.Errorf("args = %v, want them in the order the columns appear", args)
	}
}

func TestAnInsertNamesItsColumnsAndCarriesItsValues(t *testing.T) {
	sql, args := builder().buildInsertQuery(&connector.Data{
		Target:  "users",
		Payload: map[string]interface{}{"name": "Ada", "email": "ada@example.com"},
	})

	if !strings.Contains(sql, "INSERT INTO users (email, name) VALUES (?, ?)") {
		t.Errorf("sql = %q", sql)
	}
	if len(args) != 2 || args[0] != "ada@example.com" || args[1] != "Ada" {
		t.Errorf("args = %v", args)
	}
}

func TestAnUpdateBindsTheValuesBeforeTheConditions(t *testing.T) {
	sql, args := builder().buildUpdateQuery(&connector.Data{
		Target:  "users",
		Payload: map[string]interface{}{"email": "new@example.com", "active": false},
		Filters: map[string]interface{}{"id": 7},
	})

	if !strings.Contains(sql, "SET active = ?, email = ?") || !strings.Contains(sql, "WHERE id = ?") {
		t.Fatalf("sql = %q", sql)
	}
	// Positional binding: the conditions come last, so their values must too.
	if len(args) != 3 || args[0] != false || args[1] != "new@example.com" || args[2] != 7 {
		t.Errorf("args = %v", args)
	}
}

func TestADeleteCarriesItsConditions(t *testing.T) {
	sql, args := builder().buildDeleteQuery(&connector.Data{
		Target:  "users",
		Filters: map[string]interface{}{"tenant": "acme", "id": 7},
	})
	if !strings.Contains(sql, "DELETE FROM users WHERE id = ? AND tenant = ?") {
		t.Errorf("sql = %q", sql)
	}
	if len(args) != 2 || args[0] != 7 || args[1] != "acme" {
		t.Errorf("args = %v", args)
	}
}

func TestTheSameQueryProducesTheSameSQL(t *testing.T) {
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
