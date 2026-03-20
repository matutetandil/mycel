// Package connector defines the core interfaces for data connectors.
// All connectors (database, REST, queue, etc.) implement these interfaces.
package connector

import (
	"context"
	"database/sql"
)

// Connector represents any data source or sink.
// All connectors must implement this base interface.
// Follows Liskov Substitution Principle - all connectors are substitutable.
type Connector interface {
	// Name returns the connector identifier as defined in HCL.
	Name() string

	// Type returns the connector type (database, rest, queue, etc.).
	Type() string

	// Connect establishes the connection to the external system.
	Connect(ctx context.Context) error

	// Close terminates the connection gracefully.
	Close(ctx context.Context) error

	// Health checks if the connector is healthy and responsive.
	Health(ctx context.Context) error
}

// DebugThrottler is implemented by event-driven connectors that support
// studio-controlled message processing. When enabled, the connector's
// DebugGate blocks all messages until the IDE sends debug.consume,
// which calls AllowOne() to let exactly one message through.
type DebugThrottler interface {
	SetDebugMode(enabled bool)

	// AllowOne permits exactly one message through the debug gate.
	// Called when the IDE sends debug.consume.
	AllowOne()

	// SourceInfo returns the connector type and source identifier
	// (e.g., queue name for RabbitMQ, topic for Kafka) for IDE display.
	SourceInfo() (connectorType string, source string)
}

// DBAccessor is an optional interface for database connectors that expose
// their underlying *sql.DB for direct access (e.g., workflow persistence).
type DBAccessor interface {
	DB() *sql.DB
}

// SourceValidator is implemented by connectors that validate flow "from" block parameters.
// When implemented, the runtime calls ValidateSourceParams at startup for each flow
// that uses this connector as a source. The connector decides what's required, optional,
// and what defaults to apply.
type SourceValidator interface {
	ValidateSourceParams(params map[string]interface{}) error
}

// TargetValidator is implemented by connectors that validate flow "to"/"step" block parameters.
// When implemented, the runtime calls ValidateTargetParams at startup for each flow
// that uses this connector as a target. The connector decides what's required, optional,
// and what defaults to apply.
type TargetValidator interface {
	ValidateTargetParams(params map[string]interface{}) error
}

// Reader interface for connectors that can read data.
// Interface Segregation Principle - not all connectors need to write.
type Reader interface {
	Connector
	Read(ctx context.Context, query Query) (*Result, error)
}

// Writer interface for connectors that can write data.
// Interface Segregation Principle - not all connectors need to read.
type Writer interface {
	Connector
	Write(ctx context.Context, data *Data) (*Result, error)
}

// ReadWriter combines Reader and Writer for connectors that support both.
type ReadWriter interface {
	Reader
	Writer
}

// Query represents a read operation specification.
type Query struct {
	// Target is the resource to query (table name, collection, endpoint, topic, etc.).
	Target string

	// Operation is the type of read operation (SELECT, GET, CONSUME, FIND, etc.).
	Operation string

	// RawSQL is a raw SQL query string for SQL databases.
	// When set, overrides automatic query building.
	// Supports named parameters like :id, :user_id that are replaced from Filters.
	RawSQL string

	// RawQuery is a query document for NoSQL databases (MongoDB, etc.).
	// When set, overrides automatic query building.
	// Example for MongoDB: {"status": "active", "age": {"$gte": 18}}
	RawQuery map[string]interface{}

	// Filters are conditions to apply (WHERE clauses, query params, MongoDB filter, etc.).
	// Also used to provide values for named parameters in RawSQL.
	Filters map[string]interface{}

	// Fields are specific fields to retrieve (empty means all).
	// For MongoDB, this becomes the projection.
	Fields []string

	// Pagination settings for paginated results.
	Pagination *Pagination

	// OrderBy clauses for sorting results.
	// For MongoDB, this becomes the sort document.
	OrderBy []OrderClause

	// Params are additional operation-specific parameters.
	Params map[string]interface{}
}

// Data represents a write operation specification.
type Data struct {
	// Target is the resource to write to (table name, collection, endpoint, topic, etc.).
	Target string

	// Operation is the type of write operation (INSERT, POST, PUBLISH, INSERT_ONE, UPDATE_ONE, etc.).
	Operation string

	// RawSQL is a raw SQL query string for SQL databases.
	// When set, overrides automatic query building.
	// Supports named parameters like :id, :name that are replaced from Payload and Filters.
	RawSQL string

	// Update is an update document for NoSQL databases (MongoDB, etc.).
	// Example for MongoDB: {"$set": {"status": "active"}, "$inc": {"count": 1}}
	Update map[string]interface{}

	// Payload is the data to write (document for NoSQL, row for SQL).
	Payload map[string]interface{}

	// Filters are conditions for UPDATE/DELETE operations.
	// For MongoDB, this is the filter document.
	Filters map[string]interface{}

	// Params are additional operation-specific parameters.
	// For MongoDB: upsert, arrayFilters, etc.
	Params map[string]interface{}
}

// Result represents the outcome of a connector operation.
type Result struct {
	// Rows contains the returned data for read operations.
	Rows []map[string]interface{}

	// Affected is the number of rows/records affected by write operations.
	Affected int64

	// LastID is the last inserted ID for insert operations (if applicable).
	LastID interface{}

	// Metadata contains additional operation-specific information.
	Metadata map[string]interface{}
}

// Pagination holds pagination settings.
type Pagination struct {
	// Limit is the maximum number of results to return.
	Limit int

	// Offset is the number of results to skip.
	Offset int
}

// OrderClause represents a single ordering directive.
type OrderClause struct {
	// Field is the field to order by.
	Field string

	// Desc indicates descending order when true.
	Desc bool
}

// Config holds connector configuration from HCL.
type Config struct {
	// Name is the connector identifier.
	Name string

	// Type is the connector type (database, rest, queue, etc.).
	Type string

	// Driver is the specific driver (postgres, mysql, kafka, etc.).
	Driver string

	// Properties contains all connector-specific settings.
	Properties map[string]interface{}

	// Operations are named operations defined on this connector.
	Operations []*OperationDef

	// Environment is the runtime environment (development, staging, production).
	// Injected by the runtime for environment-aware defaults.
	Environment string
}

// GetOperation finds an operation by name.
func (c *Config) GetOperation(name string) *OperationDef {
	for _, op := range c.Operations {
		if op.Name == name {
			return op
		}
	}
	return nil
}

// HasOperation checks if an operation exists.
func (c *Config) HasOperation(name string) bool {
	return c.GetOperation(name) != nil
}

// ListOperations returns all defined operations.
func (c *Config) ListOperations() []*OperationDef {
	return c.Operations
}

// GetString retrieves a string property from the config.
func (c *Config) GetString(key string) string {
	if v, ok := c.Properties[key].(string); ok {
		return v
	}
	return ""
}

// GetInt retrieves an integer property from the config.
func (c *Config) GetInt(key string) int {
	switch v := c.Properties[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// GetBool retrieves a boolean property from the config.
func (c *Config) GetBool(key string) bool {
	if v, ok := c.Properties[key].(bool); ok {
		return v
	}
	return false
}

// GetMap retrieves a map property from the config.
func (c *Config) GetMap(key string) map[string]interface{} {
	if v, ok := c.Properties[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// ExtractStatusCode extracts a status code from a result map by key name,
// removes it from the map, and returns the integer value.
// Used by connectors to support response block _status overrides
// (e.g., http_status_code, grpc_status_code).
func ExtractStatusCode(result map[string]interface{}, key string) (int, bool) {
	v, exists := result[key]
	if !exists {
		return 0, false
	}
	delete(result, key)
	switch val := v.(type) {
	case string:
		code := 0
		for _, c := range val {
			if c < '0' || c > '9' {
				return 0, false
			}
			code = code*10 + int(c-'0')
		}
		return code, code > 0
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case int:
		return val, true
	}
	return 0, false
}
