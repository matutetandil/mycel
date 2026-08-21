package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A sqlite connector with no database is a mistake, not a default.
//
// An empty `database` used to become ./data/mycel.db, so a connector whose
// attribute was misspelled — `path` instead of `database`, which the
// integration-patterns guide showed and two tests in this repository copied —
// opened a file holding none of your tables. Every request then answered "no
// such table" for as long as the service ran, and nothing pointed at the
// cause. `mycel migrate` refused the same configuration outright.
func TestASQLiteConnectorWithoutADatabaseIsRefused(t *testing.T) {
	_, err := (&Factory{}).Create(context.Background(), &connector.Config{
		Name: "db",
		Type: "database",
		Properties: map[string]interface{}{
			// The misspelling that started it: swept into the property bag,
			// read by nothing.
			"path": "./data/orders.db",
		},
	})
	if err == nil {
		t.Fatal("a connector with no database was created")
	}
	if !strings.Contains(err.Error(), "database") || !strings.Contains(err.Error(), "db") {
		t.Errorf("the refusal reads %q; it should name the connector and the attribute", err)
	}
}

func TestASQLiteConnectorWithADatabaseIsCreated(t *testing.T) {
	conn, err := (&Factory{}).Create(context.Background(), &connector.Config{
		Name:       "db",
		Type:       "database",
		Properties: map[string]interface{}{"database": "./data/app.db"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if conn == nil {
		t.Fatal("no connector")
	}
}
