package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// What `mycel migrate` builds out of a connector's configuration.
//
// None of this was covered, and all of it decides where a migration is applied
// and what it writes. A DSN assembled wrongly points the command at a database
// nobody meant — the worst kind of mistake here, because it succeeds.

func TestTheDSNIsBuiltFromTheSamePropertiesTheConnectorReads(t *testing.T) {
	for _, c := range []struct {
		name     string
		driver   string
		props    map[string]interface{}
		contains []string
	}{
		{
			name:   "postgres",
			driver: "postgres",
			props: map[string]interface{}{
				"host": "db.internal", "port": 6432, "user": "mycel",
				"password": "secret", "database": "orders", "sslmode": "require",
			},
			contains: []string{"host=db.internal", "port=6432", "user=mycel", "dbname=orders", "sslmode=require"},
		},
		{
			name:     "postgres takes the defaults it documents",
			driver:   "postgres",
			props:    map[string]interface{}{"database": "orders"},
			contains: []string{"host=localhost", "port=5432", "sslmode=disable"},
		},
		{
			name:   "mysql",
			driver: "mysql",
			props: map[string]interface{}{
				"host": "db.internal", "port": 3307, "user": "mycel",
				"password": "secret", "database": "orders",
			},
			contains: []string{"mycel:secret@tcp(db.internal:3307)/orders", "parseTime=True"},
		},
		{
			name:     "mariadb is mysql",
			driver:   "mariadb",
			props:    map[string]interface{}{"database": "orders", "user": "root"},
			contains: []string{"tcp(localhost:3306)/orders"},
		},
		{
			// A port read from the environment arrives as a string, which is
			// how every example writes it.
			name:     "a port written as a string",
			driver:   "postgres",
			props:    map[string]interface{}{"database": "orders", "port": "55432"},
			contains: []string{"port=55432"},
		},
		{
			name:     "sqlite addresses a file",
			driver:   "sqlite",
			props:    map[string]interface{}{"database": "./data/app.db"},
			contains: []string{"./data/app.db"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			dsn, err := DSN(c.driver, c.props)
			if err != nil {
				t.Fatalf("DSN: %v", err)
			}
			for _, want := range c.contains {
				if !strings.Contains(dsn, want) {
					t.Errorf("DSN %q does not contain %q", dsn, want)
				}
			}
		})
	}
}

// A whole `url` is read the same way the connector reads it, so migrating a
// service configured that way reaches the same database it does.
func TestTheDSNReadsAWholeURL(t *testing.T) {
	dsn, err := DSN("postgres", map[string]interface{}{
		"url": "postgres://mycel:secret@db.internal:6432/orders?sslmode=require",
	})
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	for _, want := range []string{"host=db.internal", "port=6432", "user=mycel", "dbname=orders"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN %q does not contain %q", dsn, want)
		}
	}
}

// A connector with no database is refused rather than pointed at an empty one.
func TestADSNWithoutADatabaseIsRefused(t *testing.T) {
	if _, err := DSN("postgres", map[string]interface{}{"host": "db.internal"}); err == nil {
		t.Error("a connector with no database produced a DSN")
	}
}

// The drivers migrations run against, and the message when it is another one.
func TestOnlyTheDriversWithMigrationsAreAccepted(t *testing.T) {
	for configured, want := range map[string]string{
		"postgres":   "postgres",
		"postgresql": "postgres",
		"mysql":      "mysql",
		"mariadb":    "mysql",
		"sqlite":     "sqlite",
		"sqlite3":    "sqlite",
	} {
		got, err := SQLDriver(configured)
		if err != nil {
			t.Errorf("%s: %v", configured, err)
			continue
		}
		if got != want {
			t.Errorf("%s registered as %q, want %q", configured, got, want)
		}
	}

	_, err := SQLDriver("mongodb")
	if err == nil {
		t.Fatal("a driver with no migrations support was accepted")
	}
	for _, named := range []string{"postgres", "mysql", "sqlite"} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the refusal does not name %s: %v", named, err)
		}
	}
	if MigrationDialects() != "postgres, mysql, sqlite" {
		t.Errorf("the list reads %q", MigrationDialects())
	}
}

// A bound parameter, spelled the way the driver spells it.
//
// The recorded-migration insert used $1 for everybody. On MySQL and SQLite
// that is not a parameter, so the insert failed *after* the migration had been
// applied — a schema change made and unrecorded, which the next run makes
// again.
func TestAParameterIsSpelledTheWayTheDriverSpellsIt(t *testing.T) {
	if got := Placeholder("postgres", 1); got != "$1" {
		t.Errorf("postgres = %q", got)
	}
	if got := Placeholder("postgres", 3); got != "$3" {
		t.Errorf("postgres = %q", got)
	}
	for _, driver := range []string{"mysql", "mariadb", "sqlite", "sqlite3"} {
		if got := Placeholder(driver, 1); got != "?" {
			t.Errorf("%s = %q", driver, got)
		}
	}
}

// The tracking table, in each dialect — checked by creating it.
//
// AUTOINCREMENT is SQLite's spelling, MySQL wants AUTO_INCREMENT and a length
// on a unique text column, PostgreSQL wants SERIAL. A statement that only one
// of them accepts is a migration command that works on a laptop and fails on
// the server.
func TestTheTrackingTableIsValidInEachDialect(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql", "sqlite"} {
		ddl, err := MigrationsTableDDL(driver, "mycel_migrations")
		if err != nil {
			t.Fatalf("%s: %v", driver, err)
		}
		if !strings.Contains(ddl, "mycel_migrations") {
			t.Errorf("%s DDL does not name the table: %s", driver, ddl)
		}
		if !strings.Contains(ddl, "IF NOT EXISTS") {
			t.Errorf("%s DDL is not repeatable: %s", driver, ddl)
		}
	}

	// The one that can be executed here, executed: twice, because a migration
	// command runs it on every invocation.
	ddl, err := MigrationsTableDDL("sqlite", "mycel_migrations")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("run %d: %v\n%s", i+1, err, ddl)
		}
	}

	// And it records what a migration command records.
	if _, err := db.Exec(
		`INSERT INTO mycel_migrations (name) VALUES (`+Placeholder("sqlite", 1)+`)`,
		"001_create_items.sql"); err != nil {
		t.Fatalf("recording a migration: %v", err)
	}

	if _, err := MigrationsTableDDL("mongodb", "x"); err == nil {
		t.Error("a driver with no migrations support produced a DDL")
	}
}
