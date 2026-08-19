package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Which database the migrations are applied to is not a thing to guess at.
//
// The flag's help says the connector is auto-detected when there is only one,
// and with several the first one declared was migrated silently — first by file
// order, so the tables could land in a database nobody meant to touch. The
// aspects example has two, and running migrate there created its products table
// in whichever file the parser happened to read first.

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.mycel"), []byte(strings.TrimSpace(body)), 0o644); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}
	return dir
}

func TestMigrateRefusesToChooseBetweenDatabases(t *testing.T) {
	dir := writeConfig(t, `
connector "main" {
  type     = "database"
  driver   = "sqlite"
  database = "./main.db"
}

connector "audit" {
  type     = "database"
  driver   = "sqlite"
  database = "./audit.db"
}`)

	migrateConnector = ""
	configDir = dir
	t.Cleanup(func() { migrateConnector = ""; configDir = "." })

	_, _, _, err := getMigrationDB()
	if err == nil {
		t.Fatal("migrate picked one of two databases without being told which")
	}
	for _, want := range []string{"main", "audit", "--connector"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

func TestMigrateTakesTheOnlyDatabase(t *testing.T) {
	dir := writeConfig(t, `
connector "main" {
  type     = "database"
  driver   = "sqlite"
  database = "`+filepath.Join(t.TempDir(), "main.db")+`"
}`)

	migrateConnector = ""
	configDir = dir
	t.Cleanup(func() { migrateConnector = ""; configDir = "." })

	db, name, _, err := getMigrationDB()
	if err != nil {
		t.Fatalf("a configuration with one database was refused: %v", err)
	}
	defer db.Close()
	if name != "main" {
		t.Errorf("migrated connector %q, want main", name)
	}
}

func TestMigrateTakesTheNamedDatabase(t *testing.T) {
	dir := writeConfig(t, `
connector "main" {
  type     = "database"
  driver   = "sqlite"
  database = "`+filepath.Join(t.TempDir(), "main.db")+`"
}

connector "audit" {
  type     = "database"
  driver   = "sqlite"
  database = "`+filepath.Join(t.TempDir(), "audit.db")+`"
}`)

	migrateConnector = "audit"
	configDir = dir
	t.Cleanup(func() { migrateConnector = ""; configDir = "." })

	db, name, _, err := getMigrationDB()
	if err != nil {
		t.Fatalf("naming the connector was refused: %v", err)
	}
	defer db.Close()
	if name != "audit" {
		t.Errorf("migrated connector %q, want audit", name)
	}
}
