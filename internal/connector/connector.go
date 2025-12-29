// Package connector defines the core interfaces for data connectors.
// All connectors (database, REST, queue, etc.) implement these interfaces.
package connector

import (
	"context"
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
	// Target is the resource to query (table name, endpoint, topic, etc.).
	Target string

	// Operation is the type of read operation (SELECT, GET, CONSUME, etc.).
	Operation string

	// Filters are conditions to apply (WHERE clauses, query params, etc.).
	Filters map[string]interface{}

	// Fields are specific fields to retrieve (empty means all).
	Fields []string

	// Pagination settings for paginated results.
	Pagination *Pagination

	// OrderBy clauses for sorting results.
	OrderBy []OrderClause

	// Params are additional operation-specific parameters.
	Params map[string]interface{}
}

// Data represents a write operation specification.
type Data struct {
	// Target is the resource to write to (table name, endpoint, topic, etc.).
	Target string

	// Operation is the type of write operation (INSERT, POST, PUBLISH, etc.).
	Operation string

	// Payload is the data to write.
	Payload map[string]interface{}

	// Filters are conditions for UPDATE/DELETE operations.
	Filters map[string]interface{}

	// Params are additional operation-specific parameters.
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
