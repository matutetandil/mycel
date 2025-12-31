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

	"github.com/matutetandil/mycel/internal/connector"

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
	var sqlQuery string
	var args []interface{}

	// Use raw SQL if provided, otherwise build query automatically
	if query.RawSQL != "" {
		sqlQuery, args = c.parseNamedParams(query.RawSQL, query.Filters)
	} else {
		sqlQuery, args = c.buildSelectQuery(query)
	}

	c.logger.Debug("Executing query",
		slog.String("sql", sqlQuery),
		slog.Any("args", args),
	)

	// Execute query
	rows, err := c.db.QueryContext(ctx, sqlQuery, args...)
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
	var sqlQuery string
	var args []interface{}

	// Use raw SQL if provided
	if data.RawSQL != "" {
		// Merge payload and filters for parameter substitution
		params := make(map[string]interface{})
		for k, v := range data.Filters {
			params[k] = v
		}
		for k, v := range data.Payload {
			params[k] = v
		}
		sqlQuery, args = c.parseNamedParams(data.RawSQL, params)
	} else {
		// Build query automatically based on operation
		switch data.Operation {
		case "INSERT":
			sqlQuery, args = c.buildInsertQuery(data)
		case "UPDATE":
			sqlQuery, args = c.buildUpdateQuery(data)
		case "DELETE":
			sqlQuery, args = c.buildDeleteQuery(data)
		default:
			return nil, fmt.Errorf("unsupported operation: %s", data.Operation)
		}
	}

	c.logger.Debug("Executing write",
		slog.String("sql", sqlQuery),
		slog.Any("args", args),
	)

	// Check if the query is a SELECT (for RETURNING clauses or raw SELECT in write context)
	if isSelectQuery(sqlQuery) {
		return c.executeQueryWithResults(ctx, sqlQuery, args)
	}

	result, err := c.db.ExecContext(ctx, sqlQuery, args...)
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

// isSelectQuery checks if a SQL query is a SELECT statement.
func isSelectQuery(sql string) bool {
	// Trim whitespace and check first word
	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)
	return strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")
}

// executeQueryWithResults executes a query that returns rows (for raw SQL SELECT or RETURNING).
func (c *Connector) executeQueryWithResults(ctx context.Context, sqlQuery string, args []interface{}) (*connector.Result, error) {
	rows, err := c.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

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
		Rows:     results,
		Affected: int64(len(results)),
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

// parseNamedParams replaces named parameters (:name) with positional parameters (?)
// and returns the modified SQL along with the ordered argument values.
// Example: "SELECT * FROM users WHERE id = :id AND status = :status"
// With params {"id": 1, "status": "active"}
// Returns: "SELECT * FROM users WHERE id = ? AND status = ?", [1, "active"]
func (c *Connector) parseNamedParams(sql string, params map[string]interface{}) (string, []interface{}) {
	if params == nil || len(params) == 0 {
		return sql, nil
	}

	var result strings.Builder
	var args []interface{}
	i := 0
	sqlBytes := []byte(sql)
	n := len(sqlBytes)

	for i < n {
		// Check for named parameter starting with :
		if sqlBytes[i] == ':' {
			// Find the end of the parameter name
			j := i + 1
			for j < n && isParamChar(sqlBytes[j]) {
				j++
			}

			if j > i+1 {
				// Extract parameter name (without the colon)
				paramName := string(sqlBytes[i+1 : j])

				// Look up the value in params
				if val, ok := params[paramName]; ok {
					result.WriteByte('?')
					args = append(args, val)
				} else {
					// Parameter not found - keep the original text
					// This allows for PostgreSQL-style casts like ::int
					result.Write(sqlBytes[i:j])
				}
				i = j
				continue
			}
		}

		// Check for string literals - don't replace inside them
		if sqlBytes[i] == '\'' {
			// Copy until the closing quote
			result.WriteByte(sqlBytes[i])
			i++
			for i < n {
				result.WriteByte(sqlBytes[i])
				if sqlBytes[i] == '\'' {
					// Check for escaped quote
					if i+1 < n && sqlBytes[i+1] == '\'' {
						i++
						result.WriteByte(sqlBytes[i])
					} else {
						i++
						break
					}
				}
				i++
			}
			continue
		}

		// Regular character - just copy it
		result.WriteByte(sqlBytes[i])
		i++
	}

	return result.String(), args
}

// isParamChar returns true if the character is valid in a parameter name.
func isParamChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}
