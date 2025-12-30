// Package flow defines the core interfaces for data flows.
// Flows represent the movement of data from source to destination.
package flow

import (
	"context"
)

// Flow represents a data flow from source to destination.
// Single Responsibility Principle - each flow handles ONE data movement.
type Flow interface {
	// Name returns the flow identifier as defined in HCL.
	Name() string

	// Execute runs the flow with the given input and returns output.
	Execute(ctx context.Context, input *Input) (*Output, error)
}

// Input represents the input data for a flow execution.
type Input struct {
	// Data is the primary input payload (e.g., request body).
	Data map[string]interface{}

	// Headers are HTTP headers or equivalent metadata.
	Headers map[string]string

	// Params are URL path parameters (e.g., :id).
	Params map[string]string

	// Query are query string parameters.
	Query map[string]string
}

// NewInput creates a new empty input.
func NewInput() *Input {
	return &Input{
		Data:    make(map[string]interface{}),
		Headers: make(map[string]string),
		Params:  make(map[string]string),
		Query:   make(map[string]string),
	}
}

// Output represents the output of a flow execution.
type Output struct {
	// Data is the response payload.
	Data interface{}

	// StatusCode is the HTTP status code (or equivalent).
	StatusCode int

	// Headers are response headers.
	Headers map[string]string

	// Error contains any error message.
	Error string
}

// NewOutput creates a successful output with data.
func NewOutput(data interface{}) *Output {
	return &Output{
		Data:       data,
		StatusCode: 200,
		Headers:    make(map[string]string),
	}
}

// NewErrorOutput creates an error output.
func NewErrorOutput(statusCode int, message string) *Output {
	return &Output{
		StatusCode: statusCode,
		Error:      message,
		Headers:    make(map[string]string),
	}
}

// Config holds flow configuration from HCL.
type Config struct {
	// Name is the flow identifier.
	Name string

	// From defines the source of the flow.
	From *FromConfig

	// To defines the destination of the flow.
	To *ToConfig

	// Validate defines validation rules.
	Validate *ValidateConfig

	// Enrichments are data lookups from other connectors (flow-level).
	// These are executed before transform and results are available as enriched.*
	Enrichments []*EnrichConfig

	// Transform defines transformation rules.
	Transform *TransformConfig

	// Require defines authorization requirements.
	Require *RequireConfig

	// ErrorHandling defines error handling behavior.
	ErrorHandling *ErrorHandlingConfig
}

// FromConfig defines the flow source.
type FromConfig struct {
	// Connector is the source connector name.
	Connector string

	// Operation is the trigger operation (e.g., "GET /users", "topic:orders").
	Operation string
}

// ToConfig defines the flow destination.
type ToConfig struct {
	// Connector is the destination connector name.
	Connector string

	// Target is the destination target (e.g., table name, endpoint).
	Target string

	// Filter is an optional filter expression for the destination.
	Filter string
}

// ValidateConfig holds validation configuration.
type ValidateConfig struct {
	// Input is the type name for input validation.
	Input string

	// Output is the type name for output validation.
	Output string
}

// TransformConfig holds transformation configuration.
type TransformConfig struct {
	// Use is a reference to a named transform (or list of transforms).
	Use string

	// Mappings are inline transformation rules.
	// Keys are output field paths, values are expressions.
	Mappings map[string]string

	// Enrichments are data lookups from other connectors.
	// These are executed before mappings and results are available as enriched.*
	Enrichments []*EnrichConfig
}

// EnrichConfig holds configuration for enriching data from external sources.
type EnrichConfig struct {
	// Name is the identifier for this enrichment (used as enriched.<name>).
	Name string

	// Connector is the connector to use for the lookup.
	Connector string

	// Operation is the operation to perform on the connector.
	Operation string

	// Params are the parameters to pass to the operation.
	// Keys are parameter names, values are CEL expressions.
	Params map[string]string
}

// RequireConfig holds authorization requirements.
type RequireConfig struct {
	// Roles are required roles.
	Roles []string

	// Permissions are required permissions.
	Permissions []string
}

// ErrorHandlingConfig holds error handling settings.
type ErrorHandlingConfig struct {
	// Retry settings.
	Retry *RetryConfig
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	// Attempts is the maximum number of retry attempts.
	Attempts int

	// Delay is the initial delay between retries.
	Delay string

	// Backoff is the backoff strategy (linear, exponential).
	Backoff string
}

// Context holds runtime context for flow execution.
type Context struct {
	// FlowName is the name of the executing flow.
	FlowName string

	// UserID is the authenticated user ID (from auth context).
	UserID string

	// Roles are the user's roles.
	Roles []string

	// TenantID is the tenant identifier for multi-tenant systems.
	TenantID string

	// TraceID is the distributed tracing ID.
	TraceID string

	// RequestID is a unique identifier for this request.
	RequestID string

	// Values holds arbitrary context values.
	Values map[string]interface{}
}

// NewContext creates a new flow context.
func NewContext(flowName string) *Context {
	return &Context{
		FlowName: flowName,
		Values:   make(map[string]interface{}),
	}
}

// Set stores a value in the context.
func (c *Context) Set(key string, value interface{}) {
	c.Values[key] = value
}

// Get retrieves a value from the context.
func (c *Context) Get(key string) (interface{}, bool) {
	v, ok := c.Values[key]
	return v, ok
}
