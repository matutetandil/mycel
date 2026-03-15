package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/internal/parser"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Long: `Run SQL migration files from the migrations/ directory.

Migrations are plain .sql files executed in alphabetical order.
A migrations tracking table is created automatically to prevent
re-running already applied migrations.

Migration files should be named with a sortable prefix:
  001_create_users.sql
  002_add_email_index.sql
  003_create_orders.sql

Examples:
  mycel migrate                       # Run pending migrations
  mycel migrate --config ./my-service # From specific config directory
  mycel migrate status                # Show migration status`,
	RunE: runMigrate,
}

var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status",
	RunE:  runMigrateStatus,
}

var migrateConnector string

func init() {
	migrateCmd.PersistentFlags().StringVar(&migrateConnector, "connector", "", "Database connector name (auto-detected if only one)")
	migrateCmd.AddCommand(migrateStatusCmd)
	rootCmd.AddCommand(migrateCmd)
}

func runMigrate(cmd *cobra.Command, args []string) error {
	loadDotEnv()

	db, connName, err := getMigrationDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Ensure migrations table exists
	if err := ensureMigrationsTable(db); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get applied migrations
	applied, err := getAppliedMigrations(db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Find migration files
	migrationsDir := filepath.Join(configDir, "migrations")
	files, err := findMigrationFiles(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	if len(files) == 0 {
		fmt.Printf("No migration files found in %s/\n", migrationsDir)
		return nil
	}

	// Run pending migrations
	pending := 0
	for _, file := range files {
		name := filepath.Base(file)
		if applied[name] {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", name, err)
		}

		fmt.Printf("  Applying %s...", name)
		if _, err := db.ExecContext(context.Background(), string(content)); err != nil {
			fmt.Println(" FAILED")
			return fmt.Errorf("migration %s failed: %w", name, err)
		}

		// Record migration
		if _, err := db.ExecContext(context.Background(),
			"INSERT INTO _mycel_migrations (name) VALUES ($1)", name); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", name, err)
		}

		fmt.Println(" OK")
		pending++
	}

	if pending == 0 {
		fmt.Printf("✓ All %d migrations already applied (connector: %s)\n", len(files), connName)
	} else {
		fmt.Printf("\n✓ Applied %d migration(s)\n", pending)
	}

	return nil
}

func runMigrateStatus(cmd *cobra.Command, args []string) error {
	loadDotEnv()

	db, connName, err := getMigrationDB()
	if err != nil {
		return err
	}
	defer db.Close()

	if err := ensureMigrationsTable(db); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	applied, err := getAppliedMigrations(db)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	migrationsDir := filepath.Join(configDir, "migrations")
	files, err := findMigrationFiles(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	fmt.Printf("Migration status (connector: %s)\n\n", connName)
	for _, file := range files {
		name := filepath.Base(file)
		status := "pending"
		if applied[name] {
			status = "applied"
		}
		fmt.Printf("  [%s] %s\n", status, name)
	}

	if len(files) == 0 {
		fmt.Printf("  No migration files found in %s/\n", migrationsDir)
	}

	return nil
}

// getMigrationDB parses config, finds the database connector, and opens a *sql.DB.
func getMigrationDB() (*sql.DB, string, error) {
	p := parser.NewHCLParser()
	config, err := p.Parse(context.Background(), configDir)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Find database connector
	var dbConnector *struct {
		Name   string
		Driver string
		DSN    string
	}

	for _, c := range config.Connectors {
		if c.Type != "database" {
			continue
		}
		if migrateConnector != "" && c.Name != migrateConnector {
			continue
		}

		driver, _ := c.Properties["driver"].(string)
		dsn, _ := c.Properties["dsn"].(string)

		if driver == "sqlite" {
			if path, ok := c.Properties["path"].(string); ok {
				dsn = path
			}
		}

		dbConnector = &struct {
			Name   string
			Driver string
			DSN    string
		}{
			Name:   c.Name,
			Driver: driver,
			DSN:    dsn,
		}
		break
	}

	if dbConnector == nil {
		if migrateConnector != "" {
			return nil, "", fmt.Errorf("database connector %q not found", migrateConnector)
		}
		return nil, "", fmt.Errorf("no database connector found in configuration")
	}

	// Map driver names to database/sql drivers
	sqlDriver := dbConnector.Driver
	switch sqlDriver {
	case "postgres", "postgresql":
		sqlDriver = "pgx"
	case "mysql":
		sqlDriver = "mysql"
	case "sqlite":
		sqlDriver = "sqlite3"
	}

	db, err := sql.Open(sqlDriver, dbConnector.DSN)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to %s: %w", dbConnector.Name, err)
	}

	return db, dbConnector.Name, nil
}

func ensureMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _mycel_migrations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	// PostgreSQL uses SERIAL instead of AUTOINCREMENT
	if err != nil && strings.Contains(err.Error(), "AUTOINCREMENT") {
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS _mycel_migrations (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`)
	}
	return err
}

func getAppliedMigrations(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT name FROM _mycel_migrations ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

func findMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
