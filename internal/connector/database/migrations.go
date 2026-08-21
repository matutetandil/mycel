package database

import (
	"fmt"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Everything a tool outside the runtime needs in order to reach the same
// database a connector reaches.
//
// `mycel migrate` had its own copy of this and it was wrong in three ways at
// once: it read a `dsn` property and a `path` property, neither of which any
// database connector has, so the address was always empty; and it opened the
// drivers under the names "pgx" and "sqlite3", while the connectors register
// "postgres" and "sqlite". The command failed on the first line for every
// driver — `sql: unknown driver "sqlite3"` — which is to say it had never run.
//
// Keeping it here, beside ParseURL and ApplyURL, is what stops the copy
// drifting again: a connector that changes how it addresses its database
// changes this with it.

// SQLDriver is the name a driver is registered under by the connector that
// imports it. These are the names database/sql knows; the names written in
// configuration are the ones below.
func SQLDriver(configured string) (string, error) {
	switch configured {
	case "postgres", "postgresql":
		return "postgres", nil
	case "mysql", "mariadb":
		return "mysql", nil
	case "sqlite", "sqlite3":
		return "sqlite", nil
	default:
		return "", fmt.Errorf("no migrations support for driver %q (postgres, mysql and sqlite have it)", configured)
	}
}

// DSN builds the address for a driver out of a connector's properties — the
// same properties the connector itself reads, including a whole `url` where
// one is given.
func DSN(configured string, props map[string]interface{}) (string, error) {
	if err := ApplyURL(props); err != nil {
		return "", err
	}

	get := func(key, fallback string) string {
		if v, ok := props[key].(string); ok && v != "" {
			return v
		}
		return fallback
	}
	port := func(fallback int) int {
		return connector.IntFromProps(props, "port", fallback)
	}

	database := get("database", "")
	if database == "" {
		return "", fmt.Errorf("connector has no database")
	}

	switch configured {
	case "postgres", "postgresql":
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			get("host", "localhost"), port(5432), get("user", ""), get("password", ""),
			database, get("sslmode", "disable")), nil

	case "mysql", "mariadb":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
			get("user", ""), get("password", ""), get("host", "localhost"), port(3306),
			database, get("charset", "utf8mb4")), nil

	case "sqlite", "sqlite3":
		// SQLite addresses a file, which the connector takes from the same
		// `database` property rather than a separate one.
		return database, nil
	}

	return "", fmt.Errorf("no migrations support for driver %q", configured)
}

// MigrationsTableDDL is the tracking table, in the dialect of the driver.
//
// One statement did not serve all three: AUTOINCREMENT is SQLite's spelling,
// MySQL wants AUTO_INCREMENT and a length on a unique text column, and
// PostgreSQL wants SERIAL.
func MigrationsTableDDL(configured, table string) (string, error) {
	switch configured {
	case "postgres", "postgresql":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id SERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`, table), nil

	case "mysql", "mariadb":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id INT AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL UNIQUE,
	applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`, table), nil

	case "sqlite", "sqlite3":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`, table), nil
	}

	return "", fmt.Errorf("no migrations support for driver %q", configured)
}

// Placeholder is how a driver spells a bound parameter.
//
// The recorded-migration insert used $1 unconditionally. On MySQL and SQLite
// that is not a parameter, so the insert failed after the migration itself had
// already been applied — leaving a schema change made and unrecorded, which the
// next run would try to make again.
func Placeholder(configured string, n int) string {
	switch configured {
	case "postgres", "postgresql":
		return fmt.Sprintf("$%d", n)
	default:
		return "?"
	}
}

// MigrationDialects lists the drivers migrations can run against, for an error
// message that names them.
func MigrationDialects() string {
	return strings.Join([]string{"postgres", "mysql", "sqlite"}, ", ")
}
