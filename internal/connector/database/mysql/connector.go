// Package mysql provides a MySQL database connector.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL driver

	"github.com/matutetandil/mycel/internal/connector"
)

// ReplicaConfig holds configuration for a read replica.
type ReplicaConfig struct {
	Host     string
	Port     int
	Weight   int // For weighted round-robin (default: 1)
	MaxConns int // Max connections for this replica
}

// Connector implements a MySQL database connector.
type Connector struct {
	name     string
	host     string
	port     int
	database string
	user     string
	password string
	charset  string
	db       *sql.DB // Primary connection

	// Connection pool settings
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration

	// Read replicas
	replicas       []ReplicaConfig
	replicaDBs     []*sql.DB
	replicaMu      sync.RWMutex
	replicaCounter uint64 // For round-robin selection
	useReplicas    bool   // Whether to route reads to replicas
}

// New creates a new MySQL connector.
func New(name, host string, port int, database, user, password, charset string) *Connector {
	if port == 0 {
		port = 3306
	}
	if charset == "" {
		charset = "utf8mb4"
	}

	return &Connector{
		name:            name,
		host:            host,
		port:            port,
		database:        database,
		user:            user,
		password:        password,
		charset:         charset,
		maxOpenConns:    25,
		maxIdleConns:    5,
		connMaxLifetime: 5 * time.Minute,
	}
}

// SetPoolConfig sets connection pool configuration.
func (c *Connector) SetPoolConfig(maxOpen, maxIdle int, maxLifetime time.Duration) {
	if maxOpen > 0 {
		c.maxOpenConns = maxOpen
	}
	if maxIdle > 0 {
		c.maxIdleConns = maxIdle
	}
	if maxLifetime > 0 {
		c.connMaxLifetime = maxLifetime
	}
}

// AddReplica adds a read replica configuration.
func (c *Connector) AddReplica(replica ReplicaConfig) {
	if replica.Port == 0 {
		replica.Port = 3306
	}
	if replica.Weight <= 0 {
		replica.Weight = 1
	}
	c.replicas = append(c.replicas, replica)
	c.useReplicas = true
}

// SetUseReplicas enables or disables routing reads to replicas.
func (c *Connector) SetUseReplicas(use bool) {
	c.useReplicas = use
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
	// Connect to primary
	// MySQL DSN format: user:password@tcp(host:port)/database?charset=utf8mb4&parseTime=True
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.user, c.password, c.host, c.port, c.database, c.charset,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open mysql connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(c.maxOpenConns)
	db.SetMaxIdleConns(c.maxIdleConns)
	db.SetConnMaxLifetime(c.connMaxLifetime)

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping mysql primary: %w", err)
	}

	c.db = db

	// Connect to replicas if configured
	if len(c.replicas) > 0 {
		if err := c.connectReplicas(ctx); err != nil {
			// Log warning but don't fail - primary is available
			fmt.Printf("warning: failed to connect to some replicas: %v\n", err)
		}
	}

	return nil
}

// connectReplicas connects to all configured read replicas.
func (c *Connector) connectReplicas(ctx context.Context) error {
	c.replicaMu.Lock()
	defer c.replicaMu.Unlock()

	var lastErr error
	for _, replica := range c.replicas {
		dsn := fmt.Sprintf(
			"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
			c.user, c.password, replica.Host, replica.Port, c.database, c.charset,
		)

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			lastErr = fmt.Errorf("failed to open replica %s:%d: %w", replica.Host, replica.Port, err)
			continue
		}

		// Configure replica connection pool
		maxConns := replica.MaxConns
		if maxConns <= 0 {
			maxConns = c.maxOpenConns
		}
		db.SetMaxOpenConns(maxConns)
		db.SetMaxIdleConns(c.maxIdleConns)
		db.SetConnMaxLifetime(c.connMaxLifetime)

		// Verify connection
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			lastErr = fmt.Errorf("failed to ping replica %s:%d: %w", replica.Host, replica.Port, err)
			continue
		}

		c.replicaDBs = append(c.replicaDBs, db)
	}

	return lastErr
}

// getReplicaDB returns a replica connection using round-robin selection.
// Falls back to primary if no replicas are available.
func (c *Connector) getReplicaDB() *sql.DB {
	if !c.useReplicas {
		return c.db
	}

	c.replicaMu.RLock()
	defer c.replicaMu.RUnlock()

	if len(c.replicaDBs) == 0 {
		return c.db // Fallback to primary
	}

	// Round-robin selection
	counter := atomic.AddUint64(&c.replicaCounter, 1)
	idx := int(counter % uint64(len(c.replicaDBs)))
	return c.replicaDBs[idx]
}

// Close closes the database connection.
func (c *Connector) Close(ctx context.Context) error {
	var lastErr error

	// Close replica connections
	c.replicaMu.Lock()
	for _, db := range c.replicaDBs {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}
	c.replicaDBs = nil
	c.replicaMu.Unlock()

	// Close primary connection
	if c.db != nil {
		if err := c.db.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("database not connected")
	}
	return c.db.PingContext(ctx)
}

// Read executes a query and returns results (implements connector.Reader).
// If replicas are configured, reads are routed to replicas using round-robin.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	var sqlQuery string
	var args []interface{}

	// Use raw SQL if provided, otherwise build query automatically
	if query.RawSQL != "" {
		sqlQuery, args = c.parseNamedParams(query.RawSQL, query.Filters)
	} else {
		sqlQuery, args = c.buildSelectQuery(query)
	}

	// Use replica for reads if available
	db := c.getReplicaDB()
	rows, err := db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Scan results
	var results []map[string]interface{}
	for rows.Next() {
		// Create a slice of interface{} to hold the values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Create a map for this row
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string for better JSON serialization
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
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

// Write executes an insert, update, or delete operation (implements connector.Writer).
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

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

	// Check if the query is a SELECT (for queries that return data)
	if isSelectQuery(sqlQuery) {
		return c.executeQueryWithResults(ctx, sqlQuery, args)
	}

	result, err := c.db.ExecContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("execute failed: %w", err)
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
	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)
	return strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH")
}

// executeQueryWithResults executes a query that returns rows.
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
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
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

// buildSelectQuery builds a SELECT query from the query specification.
func (c *Connector) buildSelectQuery(query connector.Query) (string, []interface{}) {
	var args []interface{}

	// Build SELECT with columns or *
	columns := "*"
	if len(query.Fields) > 0 {
		columns = strings.Join(query.Fields, ", ")
	}

	sql := fmt.Sprintf("SELECT %s FROM %s", columns, query.Target)

	// Add WHERE clause
	if len(query.Filters) > 0 {
		var conditions []string
		for col, val := range query.Filters {
			conditions = append(conditions, fmt.Sprintf("%s = ?", col))
			args = append(args, val)
		}
		sql += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Add ORDER BY
	if len(query.OrderBy) > 0 {
		var orderClauses []string
		for _, o := range query.OrderBy {
			if o.Desc {
				orderClauses = append(orderClauses, o.Field+" DESC")
			} else {
				orderClauses = append(orderClauses, o.Field+" ASC")
			}
		}
		sql += " ORDER BY " + strings.Join(orderClauses, ", ")
	}

	// Add LIMIT and OFFSET from Pagination
	if query.Pagination != nil {
		if query.Pagination.Limit > 0 {
			sql += fmt.Sprintf(" LIMIT %d", query.Pagination.Limit)
		}
		if query.Pagination.Offset > 0 {
			sql += fmt.Sprintf(" OFFSET %d", query.Pagination.Offset)
		}
	}

	return sql, args
}

// buildInsertQuery builds an INSERT query from the data specification.
func (c *Connector) buildInsertQuery(data *connector.Data) (string, []interface{}) {
	var columns []string
	var placeholders []string
	var args []interface{}

	if data.Payload == nil {
		return "", nil
	}

	for col, val := range data.Payload {
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		args = append(args, val)
	}

	sql := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		data.Target,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	return sql, args
}

// buildUpdateQuery builds an UPDATE query from the data specification.
func (c *Connector) buildUpdateQuery(data *connector.Data) (string, []interface{}) {
	var setClauses []string
	var args []interface{}

	if data.Payload == nil {
		return "", nil
	}

	for col, val := range data.Payload {
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", col))
		args = append(args, val)
	}

	sql := fmt.Sprintf("UPDATE %s SET %s", data.Target, strings.Join(setClauses, ", "))

	// Add WHERE clause
	if len(data.Filters) > 0 {
		var conditions []string
		for col, val := range data.Filters {
			conditions = append(conditions, fmt.Sprintf("%s = ?", col))
			args = append(args, val)
		}
		sql += " WHERE " + strings.Join(conditions, " AND ")
	}

	return sql, args
}

// buildDeleteQuery builds a DELETE query from the data specification.
func (c *Connector) buildDeleteQuery(data *connector.Data) (string, []interface{}) {
	var args []interface{}

	sql := fmt.Sprintf("DELETE FROM %s", data.Target)

	// Add WHERE clause
	if len(data.Filters) > 0 {
		var conditions []string
		for col, val := range data.Filters {
			conditions = append(conditions, fmt.Sprintf("%s = ?", col))
			args = append(args, val)
		}
		sql += " WHERE " + strings.Join(conditions, " AND ")
	}

	return sql, args
}

// parseNamedParams replaces named parameters (:name) with MySQL positional parameters (?)
// and returns the modified SQL along with the ordered argument values.
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
