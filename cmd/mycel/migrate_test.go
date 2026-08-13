package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector/database"
)

// `mycel migrate` runs SQL against a live database, which is as consequential
// as this binary gets, and it had never run at all: it read a `dsn` property
// and a `path` property that no database connector has, so the address was
// always empty, and it opened the drivers under names — "pgx", "sqlite3" —
// that nothing registers. Every invocation ended at the first line with
// `sql: unknown driver`.

func migrationProject(t *testing.T, config string, migrations map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.mycel"), []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if len(migrations) > 0 {
		migrationsDir := filepath.Join(dir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		for name, body := range migrations {
			if err := os.WriteFile(filepath.Join(migrationsDir, name), []byte(body), 0o644); err != nil {
				t.Fatalf("WriteFile %s: %v", name, err)
			}
		}
	}
	return dir
}

func withConfigDir(t *testing.T, dir string) {
	t.Helper()
	previous := configDir
	configDir = dir
	t.Cleanup(func() { configDir = previous })
}

func sqliteProject(t *testing.T, migrations map[string]string) (dir, dbPath string) {
	t.Helper()
	dir = t.TempDir()
	dbPath = filepath.Join(dir, "app.db")
	config := `connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "` + dbPath + `"
}`
	project := migrationProject(t, config, migrations)
	// The config and the migrations have to sit together.
	if err := os.WriteFile(filepath.Join(project, "config.mycel"), []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return project, dbPath
}

func TestMigrationsAreAppliedInOrderAndRecorded(t *testing.T) {
	dir, dbPath := sqliteProject(t, map[string]string{
		"002_add_orders.sql":   "CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES customers(id));",
		"001_create_users.sql": "CREATE TABLE customers (id INTEGER PRIMARY KEY, email TEXT);",
	})
	withConfigDir(t, dir)

	if err := runMigrate(nil, nil); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening the database the migration wrote to: %v", err)
	}
	defer db.Close()

	// The order matters: the second migration references the table the first
	// one creates, so an alphabetical run is what makes it work.
	for _, table := range []string{"customers", "orders"} {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s was not created: %v", table, err)
		}
	}

	rows, err := db.Query("SELECT name FROM _mycel_migrations ORDER BY name")
	if err != nil {
		t.Fatalf("the tracking table was not created: %v", err)
	}
	defer rows.Close()
	var recorded []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		recorded = append(recorded, name)
	}
	if len(recorded) != 2 {
		t.Fatalf("recorded = %v, want both migrations", recorded)
	}
}

func TestAnAppliedMigrationIsNotRunAgain(t *testing.T) {
	// The recording is the whole point of the tracking table: a second run that
	// re-applied a CREATE TABLE would fail, and one that re-applied an INSERT
	// would duplicate data.
	dir, _ := sqliteProject(t, map[string]string{
		"001_create_users.sql": "CREATE TABLE customers (id INTEGER PRIMARY KEY);",
	})
	withConfigDir(t, dir)

	if err := runMigrate(nil, nil); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := runMigrate(nil, nil); err != nil {
		t.Fatalf("second run re-applied a migration: %v", err)
	}
	if err := runMigrate(nil, nil); err != nil {
		t.Fatalf("third run: %v", err)
	}
}

func TestAMigrationAddedLaterIsTheOnlyOneRun(t *testing.T) {
	dir, dbPath := sqliteProject(t, map[string]string{
		"001_create_users.sql": "CREATE TABLE customers (id INTEGER PRIMARY KEY);",
	})
	withConfigDir(t, dir)

	if err := runMigrate(nil, nil); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// The second migration would fail if the first were run again.
	err := os.WriteFile(filepath.Join(dir, "migrations", "002_add_orders.sql"),
		[]byte("CREATE TABLE orders (id INTEGER PRIMARY KEY);"), 0o644)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runMigrate(nil, nil); err != nil {
		t.Fatalf("second run: %v", err)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()
	var name string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE name='orders'").Scan(&name); err != nil {
		t.Errorf("the migration added later did not run: %v", err)
	}
}

func TestAFailingMigrationIsReportedAndNotRecorded(t *testing.T) {
	// Recording one that failed would skip it for ever, leaving the schema
	// permanently one step behind with nothing to show for it.
	dir, dbPath := sqliteProject(t, map[string]string{
		"001_broken.sql": "CREATE TABLE ;",
	})
	withConfigDir(t, dir)

	err := runMigrate(nil, nil)
	if err == nil {
		t.Fatal("a migration that is not valid SQL was reported as applied")
	}
	if !strings.Contains(err.Error(), "001_broken.sql") {
		t.Errorf("error = %q, want it to name the file", err)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM _mycel_migrations").Scan(&count); err != nil {
		t.Fatalf("querying the tracking table: %v", err)
	}
	if count != 0 {
		t.Errorf("%d migrations recorded, want none — the one that ran failed", count)
	}
}

func TestNoMigrationsIsNotAnError(t *testing.T) {
	dir, _ := sqliteProject(t, nil)
	withConfigDir(t, dir)
	if err := runMigrate(nil, nil); err != nil {
		t.Errorf("a project with no migrations directory: %v", err)
	}
	if err := runMigrateStatus(nil, nil); err != nil {
		t.Errorf("status on a project with no migrations: %v", err)
	}
}

func TestOnlySQLFilesAreRun(t *testing.T) {
	// A README or an editor's backup file sitting in the directory must not be
	// executed against the database.
	dir, dbPath := sqliteProject(t, map[string]string{
		"001_create.sql":  "CREATE TABLE customers (id INTEGER PRIMARY KEY);",
		"README.md":       "# how to add a migration",
		"001_create.sql~": "DROP TABLE customers;",
	})
	withConfigDir(t, dir)

	if err := runMigrate(nil, nil); err != nil {
		t.Fatalf("runMigrate: %v", err)
	}

	db, _ := sql.Open("sqlite", dbPath)
	defer db.Close()
	var name string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE name='customers'").Scan(&name); err != nil {
		t.Errorf("the table was dropped by something that is not a migration: %v", err)
	}
}

func TestAConfigurationWithNoDatabaseIsReported(t *testing.T) {
	dir := migrationProject(t, `connector "api" {
  type = "rest"
  port = 18291
}`, nil)
	withConfigDir(t, dir)

	err := runMigrate(nil, nil)
	if err == nil {
		t.Fatal("migrations ran against a configuration with no database")
	}
	if !strings.Contains(err.Error(), "no database connector") {
		t.Errorf("error = %q", err)
	}
}

func TestNamingAConnectorThatDoesNotExistListsTheOnesThatDo(t *testing.T) {
	dir, _ := sqliteProject(t, nil)
	withConfigDir(t, dir)

	previous := migrateConnector
	migrateConnector = "typo"
	t.Cleanup(func() { migrateConnector = previous })

	err := runMigrate(nil, nil)
	if err == nil {
		t.Fatal("a connector that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "typo") || !strings.Contains(err.Error(), "db") {
		t.Errorf("error = %q, want it to name both what was asked for and what exists", err)
	}
}

// The addressing itself, which is what had never worked. These hold the CLI to
// the same names and properties the connectors use, since a second copy of that
// knowledge is how it broke in the first place.

func TestTheDriverNamesAreOnesThatAreActuallyRegistered(t *testing.T) {
	// The check that would have caught this: a driver name nothing registers
	// cannot open anything. The connector packages register these by importing
	// their drivers, and this binary links them.
	registered := map[string]bool{}
	for _, name := range sql.Drivers() {
		registered[name] = true
	}

	for _, configured := range []string{"postgres", "postgresql", "mysql", "mariadb", "sqlite", "sqlite3"} {
		driver, err := database.SQLDriver(configured)
		if err != nil {
			t.Errorf("%q: %v", configured, err)
			continue
		}
		if !registered[driver] {
			t.Errorf("%q maps to driver %q, which nothing registers — every migration would fail with \"unknown driver\"", configured, driver)
		}
	}
}

func TestAnAddressIsBuiltFromThePropertiesConnectorsUse(t *testing.T) {
	for name, tc := range map[string]struct {
		driver string
		props  map[string]interface{}
		want   []string
	}{
		"sqlite addresses the file in database": {
			driver: "sqlite",
			props:  map[string]interface{}{"database": "/var/lib/mycel/app.db"},
			want:   []string{"/var/lib/mycel/app.db"},
		},
		"postgres from discrete fields": {
			driver: "postgres",
			props: map[string]interface{}{
				"host": "db.internal", "port": 5433, "user": "mycel",
				"password": "s3cret", "database": "orders", "sslmode": "require",
			},
			want: []string{"host=db.internal", "port=5433", "dbname=orders", "sslmode=require"},
		},
		"postgres from a whole url, which is what a platform hands over": {
			driver: "postgres",
			props:  map[string]interface{}{"url": "postgres://mycel:s3cret@db.internal:5433/orders"},
			want:   []string{"host=db.internal", "port=5433", "dbname=orders", "user=mycel"},
		},
		"mysql from discrete fields": {
			driver: "mysql",
			props: map[string]interface{}{
				"host": "db.internal", "port": 3307, "user": "mycel",
				"password": "s3cret", "database": "orders",
			},
			want: []string{"mycel:s3cret@tcp(db.internal:3307)/orders", "parseTime=True"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			dsn, err := database.DSN(tc.driver, tc.props)
			if err != nil {
				t.Fatalf("DSN: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(dsn, want) {
					t.Errorf("address = %q, want it to contain %q", dsn, want)
				}
			}
		})
	}
}

func TestAConnectorWithNoDatabaseIsReportedRatherThanAddressedEmptily(t *testing.T) {
	// The original failure: an empty address that sql.Open accepts and that
	// reaches either nothing or the wrong database.
	if _, err := database.DSN("postgres", map[string]interface{}{"host": "db.internal"}); err == nil {
		t.Error("a connector naming no database produced an address")
	}
}

func TestADriverWithNoMigrationsSupportSaysWhichHaveIt(t *testing.T) {
	_, err := database.SQLDriver("mongodb")
	if err == nil {
		t.Fatal("a driver with no migrations support was accepted")
	}
	for _, named := range []string{"postgres", "mysql", "sqlite"} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("error = %q, want it to name %q as one that has them", err, named)
		}
	}
}

func TestEachDriverSpellsTheTrackingTableItsOwnWay(t *testing.T) {
	// One statement did not serve all three, and the fallback keyed on the
	// error text — so MySQL got PostgreSQL's, which it also rejects.
	for driver, want := range map[string]string{
		"postgres": "SERIAL",
		"mysql":    "AUTO_INCREMENT",
		"sqlite":   "AUTOINCREMENT",
	} {
		ddl, err := database.MigrationsTableDDL(driver, "_mycel_migrations")
		if err != nil {
			t.Errorf("%s: %v", driver, err)
			continue
		}
		if !strings.Contains(ddl, want) {
			t.Errorf("%s table = %q, want %q", driver, ddl, want)
		}
		if !strings.Contains(ddl, "IF NOT EXISTS") {
			t.Errorf("%s table is created unconditionally, so a second run fails", driver)
		}
	}
}

func TestTheParameterIsSpelledTheWayEachDriverExpects(t *testing.T) {
	// The insert that records an applied migration used $1 everywhere. On
	// MySQL and SQLite that is not a parameter, so it failed after the schema
	// change had already been made — leaving it applied and unrecorded, to be
	// applied again on the next run.
	if got := database.Placeholder("postgres", 1); got != "$1" {
		t.Errorf("postgres placeholder = %q", got)
	}
	for _, driver := range []string{"mysql", "sqlite"} {
		if got := database.Placeholder(driver, 1); got != "?" {
			t.Errorf("%s placeholder = %q, want ?", driver, got)
		}
	}
}
