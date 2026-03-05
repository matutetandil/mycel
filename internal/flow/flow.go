// Package flow defines the core interfaces for data flows.
// Flows represent the movement of data from source to destination.
package flow

import (
	"context"
	"fmt"
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

	// SourceFile is the path to the HCL file that defined this flow.
	// Used for aspect pattern matching (e.g., "flows/users/create_user.hcl").
	SourceFile string

	// When defines the flow trigger schedule.
	// Values: "always" (default), cron expression, or "@every X"
	When string

	// From defines the source of the flow.
	From *FromConfig

	// To defines the destination of the flow (single destination).
	To *ToConfig

	// MultiTo defines multiple destinations for the flow (fan-out pattern).
	// When specified, writes to all destinations (in parallel by default).
	MultiTo []*ToConfig

	// Returns specifies the GraphQL return type for this flow.
	// Used in HCL-first mode to define typed responses.
	// Supports formats: "User", "User[]", "[User!]!", "User!"
	Returns string

	// Lock defines mutex locking for this flow.
	Lock *LockConfig

	// Semaphore defines concurrency limiting for this flow.
	Semaphore *SemaphoreConfig

	// Coordinate defines signal/wait coordination for this flow.
	Coordinate *CoordinateConfig

	// Cache defines caching behavior for this flow.
	Cache *CacheConfig

	// Validate defines validation rules.
	Validate *ValidateConfig

	// Steps are intermediate connector calls executed before transform.
	// Results are available as step.<name>.* in transform expressions.
	Steps []*StepConfig

	// Enrichments are data lookups from other connectors (flow-level).
	// These are executed before transform and results are available as enriched.*
	Enrichments []*EnrichConfig

	// Transform defines transformation rules.
	Transform *TransformConfig

	// Require defines authorization requirements.
	Require *RequireConfig

	// After defines post-execution actions (cache invalidation, notifications, etc.)
	After *AfterConfig

	// ErrorHandling defines error handling behavior.
	ErrorHandling *ErrorHandlingConfig

	// Dedupe defines deduplication behavior for the flow.
	Dedupe *DedupeConfig

	// Entity marks this flow as a federated entity resolver.
	// The value is the entity type name (e.g., "Product").
	// When set, this flow resolves _entities queries for the given type.
	Entity string

	// Batch defines batch processing configuration for chunked data operations.
	// When set, the flow reads from the source in pages and writes to a target.
	Batch *BatchConfig

	// StateTransition triggers a state machine transition during flow execution.
	StateTransition *StateTransitionConfig
}

// StateTransitionConfig defines a state machine transition within a flow.
type StateTransitionConfig struct {
	// Machine is the state machine name to use.
	Machine string

	// Entity is the table/entity name where state is stored.
	Entity string

	// ID is a CEL expression that resolves to the entity ID.
	ID string

	// Event is a CEL expression that resolves to the event name.
	Event string

	// Data is a CEL expression that resolves to additional transition data.
	Data string
}

// StepConfig defines an intermediate connector call within a flow.
// Steps allow calling connectors and using their results in subsequent
// steps or transforms.
type StepConfig struct {
	// Name is the step identifier (used as step.<name> in expressions).
	Name string

	// Connector is the connector to call.
	Connector string

	// Operation is the operation to perform on the connector.
	// Examples: "query", "GET /users", "POST /api/calculate"
	Operation string

	// Format overrides the connector's default format for this step (e.g., "xml", "json").
	Format string

	// When is a CEL expression that determines if this step should execute.
	// If empty or evaluates to true, the step executes.
	// Example: "input.include_prices == true"
	When string

	// Params are parameters to pass to the connector operation.
	// Values can be CEL expressions referencing input.* or previous step.* results.
	Params map[string]interface{}

	// Query is an optional SQL query for database connectors.
	Query string

	// Body is the request body for HTTP connectors.
	// Can be a map or a CEL expression.
	Body map[string]interface{}

	// Target is the target resource (table, collection, endpoint).
	Target string

	// Timeout is the maximum time to wait for this step (e.g., "30s").
	Timeout string

	// OnError defines what to do if this step fails: "fail", "skip", "default".
	OnError string

	// Default is the default value to use if OnError is "default".
	Default interface{}
}

// FromConfig defines the flow source.
type FromConfig struct {
	// Connector is the source connector name.
	Connector string

	// Operation is the trigger operation (e.g., "GET /users", "topic:orders").
	Operation string

	// Format overrides the connector's default format for incoming data (e.g., "xml", "json").
	Format string

	// Filter is a CEL expression to filter incoming requests/messages (legacy string syntax).
	// If the expression evaluates to false, the request is skipped.
	// Example: "input.metadata.origin != 'internal'"
	Filter string

	// FilterConfig holds the extended filter configuration (block syntax).
	// When set, takes precedence over Filter string.
	FilterConfig *FilterConfig
}

// FilterCondition returns the active filter condition expression.
// Returns empty string if no filter is configured.
func (f *FromConfig) FilterCondition() string {
	if f.FilterConfig != nil {
		return f.FilterConfig.Condition
	}
	return f.Filter
}

// FilterConfig holds extended filter configuration with rejection policy.
type FilterConfig struct {
	// Condition is the CEL expression to evaluate.
	Condition string

	// OnReject defines what to do with messages that don't match the filter.
	// Values: "ack" (default), "reject", "requeue"
	OnReject string

	// IDField is a CEL expression to extract a message ID for requeue deduplication.
	// Example: "input.properties.message_id"
	IDField string

	// MaxRequeue is the maximum number of requeue attempts before giving up.
	// Default: 3
	MaxRequeue int
}

// FilteredResultWithPolicy is returned when a message is filtered out,
// carrying the rejection policy so MQ consumers can handle it appropriately.
type FilteredResultWithPolicy struct {
	Filtered   bool
	Policy     string // "ack", "reject", "requeue"
	MessageID  string
	MaxRequeue int
}

// ToConfig defines the flow destination.
type ToConfig struct {
	// Connector is the destination connector name.
	Connector string

	// Target is the destination target (e.g., table name, collection, endpoint).
	Target string

	// Operation is the type of write operation to perform.
	// Examples: INSERT_ONE, UPDATE_ONE, DELETE_ONE, UPDATE_MANY, DELETE_MANY,
	// WRITE (files/S3), READ, DELETE, LIST, PRESIGN, COPY
	Operation string

	// Format overrides the connector's default format for outgoing data (e.g., "xml", "json").
	Format string

	// Filter is an optional filter expression for the destination.
	Filter string

	// Query is an optional raw SQL query for SQL database connectors.
	// Example: "SELECT * FROM users WHERE id = :id"
	Query string

	// QueryFilter is an optional query filter for NoSQL database connectors (MongoDB).
	// Example: {"status": "active", "age": {"$gte": 18}}
	QueryFilter map[string]interface{}

	// Update is an optional update document for NoSQL UPDATE operations (MongoDB).
	// Example: {"$set": {"status": "active"}, "$inc": {"count": 1}}
	Update map[string]interface{}

	// Params contains additional parameters for the operation.
	// Example: S3 COPY operation uses {source: "...", dest: "..."}
	Params map[string]interface{}

	// When is a CEL expression that determines if this destination should be written to.
	// If empty or evaluates to true, the write executes.
	// Example: "output.total > 1000"
	When string

	// Transform is an optional per-destination transformation.
	// If specified, this transform is applied instead of the flow's main transform.
	Transform map[string]string

	// Parallel indicates if this destination should be written in parallel with others.
	// Default is true. Set to false for sequential writes.
	Parallel bool
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
	// Retry settings for automatic retries on failure.
	Retry *RetryConfig

	// Fallback defines where to send failed messages (DLQ).
	Fallback *FallbackConfig

	// ErrorResponse defines a custom error response for HTTP connectors.
	ErrorResponse *ErrorResponseConfig
}

// ErrorResponseConfig defines a custom HTTP error response.
type ErrorResponseConfig struct {
	// Status is the HTTP status code (e.g., 422, 503).
	Status int

	// Headers are custom response headers.
	Headers map[string]string

	// Body is a map of CEL expressions that build the response body.
	Body map[string]string
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	// Attempts is the maximum number of retry attempts.
	Attempts int

	// Delay is the initial delay between retries (e.g., "1s", "500ms").
	Delay string

	// MaxDelay is the maximum delay between retries (e.g., "30s").
	// Used with exponential backoff to cap the delay.
	MaxDelay string

	// Backoff is the backoff strategy: "linear", "exponential", or "constant".
	Backoff string
}

// FallbackConfig holds fallback/DLQ settings.
type FallbackConfig struct {
	// Connector is the fallback connector name (e.g., a queue for DLQ).
	Connector string

	// Target is the destination (exchange, topic, table, etc.).
	Target string

	// IncludeError indicates whether to include error details in the fallback message.
	IncludeError bool

	// Transform is an optional transformation to apply before sending to fallback.
	Transform map[string]string
}

// FlowError wraps an error with custom response configuration.
// When a flow has error_response configured and the flow fails,
// the error is wrapped in FlowError so HTTP connectors can use it.
type FlowError struct {
	// Err is the original error.
	Err error

	// Status is the HTTP status code to return.
	Status int

	// Body is the custom response body.
	Body map[string]interface{}

	// Headers are custom response headers.
	Headers map[string]string
}

func (e *FlowError) Error() string {
	return e.Err.Error()
}

func (e *FlowError) Unwrap() error {
	return e.Err
}

// NewFlowError creates a FlowError with the given status, body, and headers.
func NewFlowError(err error, status int, body map[string]interface{}, headers map[string]string) *FlowError {
	if status == 0 {
		status = 500
	}
	return &FlowError{
		Err:     fmt.Errorf("%w", err),
		Status:  status,
		Body:    body,
		Headers: headers,
	}
}

// DedupeConfig holds deduplication configuration for a flow.
// Used to prevent processing duplicate messages (e.g., from message queues).
type DedupeConfig struct {
	// Storage is the connector for storing dedup state (typically Redis or cache).
	Storage string

	// Key is a CEL expression that generates the deduplication key.
	// Example: "input.message_id" or "'order:' + input.order_id"
	Key string

	// TTL is how long to remember seen keys (e.g., "1h", "24h").
	// After TTL expires, the same key can be processed again.
	TTL string

	// OnDuplicate defines behavior when duplicate is found: "skip" or "fail".
	// - "skip": Silently skip the duplicate (default)
	// - "fail": Return an error for duplicates
	OnDuplicate string
}

// DuplicateResult is returned when a message is identified as duplicate.
var DuplicateResult = &struct {
	Duplicate bool   `json:"duplicate"`
	Reason    string `json:"reason"`
}{Duplicate: true, Reason: "duplicate message"}

// CacheConfig holds caching configuration for a flow.
type CacheConfig struct {
	// Storage is the cache connector name (e.g., "redis_cache", "memory_cache").
	Storage string

	// TTL is the time-to-live for cached entries (e.g., "5m", "1h").
	TTL string

	// Key is the cache key template with variable interpolation.
	// Supports ${input.params.id}, ${input.query.page}, etc.
	Key string

	// InvalidateOn is a list of event patterns that invalidate this cache entry.
	// Example: ["products:updated:${input.params.id}"]
	InvalidateOn []string

	// Use references a named cache definition (cache.name).
	Use string
}

// AfterConfig holds post-execution actions.
type AfterConfig struct {
	// Invalidate defines cache invalidation rules to run after the flow.
	Invalidate *InvalidateConfig
}

// InvalidateConfig holds cache invalidation settings.
type InvalidateConfig struct {
	// Storage is the cache connector name.
	Storage string

	// Keys are specific keys to invalidate.
	// Supports variable interpolation: ${input.params.id}
	Keys []string

	// Patterns are key patterns to invalidate (with * wildcards).
	// Example: ["products:*", "categories:${input.data.category_id}:*"]
	Patterns []string
}

// NamedCacheConfig holds a reusable cache definition.
type NamedCacheConfig struct {
	// Name is the cache definition name.
	Name string

	// Storage is the cache connector name.
	Storage string

	// TTL is the default TTL for this cache.
	TTL string

	// Prefix is prepended to all cache keys.
	Prefix string

	// InvalidateOn is a list of events that invalidate entries in this cache.
	InvalidateOn []string
}

// LockConfig holds mutex lock configuration for a flow.
type LockConfig struct {
	// Storage is the lock connector name (typically Redis).
	Storage string

	// Key is a CEL expression for the lock key.
	// Example: "'user:' + input.body.user_id"
	Key string

	// Timeout is the maximum time to hold the lock (e.g., "30s").
	Timeout string

	// Wait indicates whether to wait for the lock or fail immediately.
	Wait bool

	// Retry is the interval between retry attempts (e.g., "100ms").
	Retry string
}

// SemaphoreConfig holds semaphore configuration for a flow.
type SemaphoreConfig struct {
	// Storage is the semaphore connector name (typically Redis).
	Storage string

	// Key is a CEL expression for the semaphore key.
	// Example: "'external_api'"
	Key string

	// MaxPermits is the maximum number of concurrent permits.
	MaxPermits int

	// Timeout is the maximum time to wait for a permit (e.g., "30s").
	Timeout string

	// Lease is the maximum time to hold a permit before auto-release (e.g., "60s").
	Lease string
}

// CoordinateConfig holds coordination configuration for a flow.
type CoordinateConfig struct {
	// Storage is the coordinator connector name (typically Redis).
	Storage string

	// Wait defines when and what to wait for.
	Wait *WaitConfig

	// Signal defines when and what to signal.
	Signal *SignalConfig

	// Preflight defines a check to run before waiting.
	Preflight *PreflightConfig

	// Timeout is the maximum time to wait (e.g., "60s").
	Timeout string

	// OnTimeout defines what to do on timeout: "fail", "retry", "skip", "pass".
	OnTimeout string

	// MaxRetries is the maximum number of retries when OnTimeout is "retry".
	MaxRetries int

	// MaxConcurrentWaits limits the number of simultaneous waits (0 = unlimited).
	MaxConcurrentWaits int
}

// WaitConfig defines when and what to wait for.
type WaitConfig struct {
	// When is a CEL expression that determines if this flow should wait.
	// Example: "input.headers.type == 'child'"
	When string

	// For is a CEL expression for the signal to wait for.
	// Example: "'parent:' + input.headers.parent_id + ':ready'"
	For string
}

// SignalConfig defines when and what to signal.
type SignalConfig struct {
	// When is a CEL expression that determines if this flow should emit a signal.
	// Example: "input.headers.type == 'parent'"
	When string

	// Emit is a CEL expression for the signal to emit.
	// Example: "'parent:' + input.body.id + ':ready'"
	Emit string

	// TTL is the signal's time-to-live (e.g., "5m").
	TTL string
}

// PreflightConfig defines a check to run before waiting.
type PreflightConfig struct {
	// Connector is the connector name for the preflight check.
	Connector string

	// Query is the query to execute (e.g., "SELECT 1 FROM entities WHERE id = :parent_id").
	Query string

	// Params are the parameters for the query.
	// Keys are parameter names, values are CEL expressions.
	Params map[string]string

	// IfExists defines what to do if the query returns results: "pass" or "fail".
	IfExists string
}

// BatchConfig defines batch processing configuration.
// When configured, the flow reads from a source connector in chunks,
// optionally applies a per-item transform, and writes each chunk to a target.
type BatchConfig struct {
	// Source is the name of the source connector to read from.
	Source string

	// Query is the SQL query or operation to read data.
	Query string

	// Params are query parameters (values can be CEL expressions).
	Params map[string]interface{}

	// ChunkSize is the number of records per chunk (default 100).
	ChunkSize int

	// OnError defines behavior on chunk failure: "continue" or "stop" (default "stop").
	OnError string

	// Transform is an optional per-item transformation.
	Transform *TransformConfig

	// To defines the target connector and operation for each chunk.
	To *ToConfig
}

// BatchResult holds the outcome of a batch processing operation.
type BatchResult struct {
	// Processed is the total number of records successfully processed.
	Processed int `json:"processed"`

	// Failed is the number of records that failed.
	Failed int `json:"failed"`

	// Chunks is the number of chunks processed.
	Chunks int `json:"chunks"`

	// Errors contains error messages from failed chunks.
	Errors []string `json:"errors,omitempty"`
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
