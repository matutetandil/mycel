// Package sqlite provides a SQLite database connector.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mycel-labs/mycel/internal/connector"

	_ "modernc.org/sqlite" // SQLite driver (pure Go)
)

// Connector provides SQLite database connectivity.
type Connector struct {
	name   string
	path   string
	db     *sql.DB
	logger *slog.Logger
}

// New creates a new SQLite connector.
func New(name, path string, logger *slog.Logger) *Connector {
	if logger == nil {
		logger = slog.Default()
	}

	return &Connector{
		name:   name,
		path:   path,
		logger: logger,
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "database"
}

// Connect establishes the database connection.
func (c *Connector) Connect(ctx context.Context) error {
	// Ensure directory exists
	dir := filepath.Dir(c.path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Open database
	db, err := sql.Open("sqlite", c.path)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		c.logger.Warn("Failed to enable foreign keys", slog.Any("error", err))
	}

	c.db = db
	return nil
}

// Close closes the database connection.
func (c *Connector) Close(ctx context.Context) error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// Health checks if the database is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("database not connected")
	}
	return c.db.PingContext(ctx)
}

// Read executes a SELECT query.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	// Build SELECT query
	sql, args := c.buildSelectQuery(query)

	c.logger.Debug("Executing query",
		slog.String("sql", sql),
		slog.Any("args", args),
	)

	// Execute query
	rows, err := c.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Collect results
	var results []map[string]interface{}
	for rows.Next() {
		// Create slice of interface{} to hold values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Build result map
		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &connector.Result{
		Rows: results,
	}, nil
}

// Write executes INSERT, UPDATE, or DELETE operations.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	var sql string
	var args []interface{}

	switch data.Operation {
	case "INSERT":
		sql, args = c.buildInsertQuery(data)
	case "UPDATE":
		sql, args = c.buildUpdateQuery(data)
	case "DELETE":
		sql, args = c.buildDeleteQuery(data)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", data.Operation)
	}

	c.logger.Debug("Executing write",
		slog.String("sql", sql),
		slog.Any("args", args),
	)

	result, err := c.db.ExecContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}

	affected, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()

	return &connector.Result{
		Affected: affected,
		LastID:   lastID,
	}, nil
}

// buildSelectQuery builds a SELECT SQL query.
func (c *Connector) buildSelectQuery(query connector.Query) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}

	// SELECT
	sb.WriteString("SELECT ")
	if len(query.Fields) == 0 {
		sb.WriteString("*")
	} else {
		sb.WriteString(strings.Join(query.Fields, ", "))
	}

	// FROM
	sb.WriteString(" FROM ")
	sb.WriteString(query.Target)

	// WHERE
	if len(query.Filters) > 0 {
		sb.WriteString(" WHERE ")
		conditions := make([]string, 0, len(query.Filters))
		for k, v := range query.Filters {
			conditions = append(conditions, fmt.Sprintf("%s = ?", k))
			args = append(args, v)
		}
		sb.WriteString(strings.Join(conditions, " AND "))
	}

	// ORDER BY
	if len(query.OrderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		orders := make([]string, 0, len(query.OrderBy))
		for _, o := range query.OrderBy {
			if o.Desc {
				orders = append(orders, fmt.Sprintf("%s DESC", o.Field))
			} else {
				orders = append(orders, o.Field)
			}
		}
		sb.WriteString(strings.Join(orders, ", "))
	}

	// LIMIT & OFFSET
	if query.Pagination != nil {
		if query.Pagination.Limit > 0 {
			sb.WriteString(fmt.Sprintf(" LIMIT %d", query.Pagination.Limit))
		}
		if query.Pagination.Offset > 0 {
			sb.WriteString(fmt.Sprintf(" OFFSET %d", query.Pagination.Offset))
		}
	}

	return sb.String(), args
}

// buildInsertQuery builds an INSERT SQL query.
func (c *Connector) buildInsertQuery(data *connector.Data) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}

	columns := make([]string, 0, len(data.Payload))
	placeholders := make([]string, 0, len(data.Payload))

	for k, v := range data.Payload {
		columns = append(columns, k)
		placeholders = append(placeholders, "?")
		args = append(args, v)
	}

	sb.WriteString("INSERT INTO ")
	sb.WriteString(data.Target)
	sb.WriteString(" (")
	sb.WriteString(strings.Join(columns, ", "))
	sb.WriteString(") VALUES (")
	sb.WriteString(strings.Join(placeholders, ", "))
	sb.WriteString(")")

	return sb.String(), args
}

// buildUpdateQuery builds an UPDATE SQL query.
func (c *Connector) buildUpdateQuery(data *connector.Data) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}

	sb.WriteString("UPDATE ")
	sb.WriteString(data.Target)
	sb.WriteString(" SET ")

	sets := make([]string, 0, len(data.Payload))
	for k, v := range data.Payload {
		sets = append(sets, fmt.Sprintf("%s = ?", k))
		args = append(args, v)
	}
	sb.WriteString(strings.Join(sets, ", "))

	// WHERE
	if len(data.Filters) > 0 {
		sb.WriteString(" WHERE ")
		conditions := make([]string, 0, len(data.Filters))
		for k, v := range data.Filters {
			conditions = append(conditions, fmt.Sprintf("%s = ?", k))
			args = append(args, v)
		}
		sb.WriteString(strings.Join(conditions, " AND "))
	}

	return sb.String(), args
}

// buildDeleteQuery builds a DELETE SQL query.
func (c *Connector) buildDeleteQuery(data *connector.Data) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}

	sb.WriteString("DELETE FROM ")
	sb.WriteString(data.Target)

	// WHERE
	if len(data.Filters) > 0 {
		sb.WriteString(" WHERE ")
		conditions := make([]string, 0, len(data.Filters))
		for k, v := range data.Filters {
			conditions = append(conditions, fmt.Sprintf("%s = ?", k))
			args = append(args, v)
		}
		sb.WriteString(strings.Join(conditions, " AND "))
	}

	return sb.String(), args
}

// Exec executes a raw SQL query (for migrations, etc.).
func (c *Connector) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := c.db.ExecContext(ctx, sql, args...)
	return err
}

// DB returns the underlying database connection for advanced use cases.
func (c *Connector) DB() *sql.DB {
	return c.db
}
