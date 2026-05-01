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
	// Used for duplicate name detection and error reporting.
	SourceFile string

	// When defines the flow trigger schedule.
	// Values: "always" (default), cron expression, or "@every X"
	When string

	// From defines the source of the flow.
	From *FromConfig

	// Accept defines a business-level gate that runs after filter but before transform.
	// Unlike filter (which determines if a message belongs to this flow),
	// accept determines if this flow should process the message.
	// When the condition is false, on_reject controls the disposition (ack/reject/requeue).
	Accept *AcceptConfig

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

	// SequenceGuard rejects messages whose sequence number is not strictly
	// greater than the last one observed for the same key. Used to prevent
	// out-of-order delivery from regressing per-resource state (e.g. an old
	// product update overwriting a newer one).
	SequenceGuard *SequenceGuardConfig

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

	// Transform defines transformation rules applied to input BEFORE sending to destination.
	Transform *TransformConfig

	// Response defines transformation rules applied to the result AFTER receiving from destination.
	// Available variables: input (original request), output (destination result).
	// For echo flows (no "to" block), only input is available.
	Response map[string]string

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

	// Idempotency defines idempotency key configuration.
	// When set, duplicate requests with the same key return the cached result.
	Idempotency *IdempotencyConfig

	// Async makes the flow return 202 Accepted immediately.
	// The flow executes in the background and stores the result.
	// A status endpoint is auto-registered at GET /jobs/:job_id.
	Async *AsyncConfig
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

	// When is a CEL expression that determines if this step should execute.
	// If empty or evaluates to true, the step executes.
	// Example: "input.include_prices == true"
	When string

	// Timeout is the maximum time to wait for this step (e.g., "30s").
	Timeout string

	// OnError defines what to do if this step fails: "fail", "skip", "default".
	OnError string

	// Default is the default value to use if OnError is "default".
	Default interface{}

	// Envelope wraps the outgoing payload under a single root key before it
	// reaches the connector. Same semantics as ToConfig.Envelope. Empty means
	// no wrapping.
	Envelope string

	// ConnectorParams holds all connector-specific parameters.
	// Populated by the parser from the HCL block attributes.
	// Access via getter methods (GetOperation, GetTarget, etc.).
	ConnectorParams map[string]interface{}
}

// GetOperation returns the operation from ConnectorParams.
func (s *StepConfig) GetOperation() string {
	return getStringParam(s.ConnectorParams, "operation", "")
}

// GetFormat returns the format from ConnectorParams.
func (s *StepConfig) GetFormat() string {
	return getStringParam(s.ConnectorParams, "format", "")
}

// GetTarget returns the target from ConnectorParams.
func (s *StepConfig) GetTarget() string {
	return getStringParam(s.ConnectorParams, "target", "")
}

// GetQuery returns the query from ConnectorParams.
func (s *StepConfig) GetQuery() string {
	return getStringParam(s.ConnectorParams, "query", "")
}

// GetBody returns the body from ConnectorParams.
func (s *StepConfig) GetBody() map[string]interface{} {
	return getMapParam(s.ConnectorParams, "body", nil)
}

// GetParams returns the params from ConnectorParams.
func (s *StepConfig) GetParams() map[string]interface{} {
	return getMapParam(s.ConnectorParams, "params", nil)
}

// FromConfig defines the flow source.
type FromConfig struct {
	// Connector is the source connector name.
	Connector string

	// Filter is a CEL expression to filter incoming requests/messages (legacy string syntax).
	// If the expression evaluates to false, the request is skipped.
	// Example: "input.metadata.origin != 'internal'"
	Filter string

	// FilterConfig holds the extended filter configuration (block syntax).
	// When set, takes precedence over Filter string.
	FilterConfig *FilterConfig

	// ConnectorParams holds all connector-specific parameters.
	// Populated by the parser from the HCL block attributes.
	// Access via getter methods (GetOperation, GetFormat).
	ConnectorParams map[string]interface{}
}

// GetOperation returns the operation from ConnectorParams.
func (f *FromConfig) GetOperation() string {
	return getStringParam(f.ConnectorParams, "operation", "")
}

// GetFormat returns the format from ConnectorParams.
func (f *FromConfig) GetFormat() string {
	return getStringParam(f.ConnectorParams, "format", "")
}

// FilterCondition returns the active filter condition expression.
// Returns empty string if no filter is configured.
func (f *FromConfig) FilterCondition() string {
	if f.FilterConfig != nil {
		return f.FilterConfig.Condition
	}
	return f.Filter
}

// AcceptConfig holds the accept gate configuration.
// Accept runs after filter but before transform, for business-level decisions.
// Example: "this message is my type, but is it specifically for me?"
type AcceptConfig struct {
	// When is the CEL expression to evaluate. Must return true to proceed.
	When string

	// OnReject defines what to do with messages that don't pass the accept gate.
	// Values: "ack" (default), "reject", "requeue"
	OnReject string
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

// WrapPayload nests payload under a single root key when key is non-empty.
// Used by ToConfig.Envelope and StepConfig.Envelope to satisfy SOAP-derived
// REST frameworks (Magento webapi, Spring @RequestBody) that expect bodies
// shaped like `{ "<paramName>": { ...body... } }`.
//
// A nil payload becomes `{ "<key>": nil }` rather than being passed through —
// the framework on the other end is the one that knows whether nil is OK,
// and our job is to be predictable about the wrapper.
func WrapPayload(payload map[string]interface{}, key string) map[string]interface{} {
	if key == "" {
		return payload
	}
	return map[string]interface{}{key: payload}
}

// FilteredResultWithPolicy is returned when a message is filtered out,
// carrying the rejection policy so MQ consumers can handle it appropriately.
type FilteredResultWithPolicy struct {
	Filtered   bool
	Policy     string // "ack", "reject", "requeue"
	MessageID  string
	MaxRequeue int

	// Reason names the gate that produced the drop. Surfaced to
	// `on_drop` aspects via the `drop.reason` CEL binding so a single
	// alerter can route per-disposition without writing one aspect per
	// gate. Stable values:
	//   - "filter"             — from { filter { } } rejected
	//   - "accept"             — accept { } rejected
	//   - "coordinate_timeout" — coordinate { on_timeout = "ack" } fired
	//   - "sequence_older"     — sequence_guard { } saw current <= stored
	Reason string
}

// FilteredDropError wraps a FilteredResultWithPolicy when the value crosses
// a layer (the aspect executor) that wants to short-circuit aspect dispatch
// for it. The sentinel makes the disposition look like an error to layers
// that gate on errors, while still carrying the original FilteredResultWithPolicy
// for the layers that need it (the MQ consumer's ack/nack/requeue branch).
//
// Used for filter/accept rejections AND coordinate.on_timeout="ack". In
// both cases the flow body did not run its main path; firing
// after/on_error aspects would produce misleading notifications.
type FilteredDropError struct {
	Result *FilteredResultWithPolicy
}

// Error makes FilteredDropError satisfy the error interface.
func (e *FilteredDropError) Error() string {
	if e == nil || e.Result == nil {
		return "filtered drop"
	}
	return "filtered drop (policy=" + e.Result.Policy + ")"
}

// ToConfig defines the flow destination.
type ToConfig struct {
	// Connector is the destination connector name.
	Connector string

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

	// Envelope wraps the outgoing payload under a single root key before it
	// reaches the connector. Required by Magento webapi, Spring's
	// @RequestBody, and other SOAP-derived REST frameworks that expect
	// `{ "<paramName>": { ...body... } }`. Empty means no wrapping.
	Envelope string

	// ConnectorParams holds all connector-specific parameters.
	// Populated by the parser from the HCL block attributes.
	// Access via getter methods (GetTarget, GetOperation, etc.).
	ConnectorParams map[string]interface{}
}

// GetTarget returns the target from ConnectorParams.
func (t *ToConfig) GetTarget() string {
	return getStringParam(t.ConnectorParams, "target", "")
}

// GetOperation returns the operation from ConnectorParams.
func (t *ToConfig) GetOperation() string {
	return getStringParam(t.ConnectorParams, "operation", "")
}

// GetFormat returns the format from ConnectorParams.
func (t *ToConfig) GetFormat() string {
	return getStringParam(t.ConnectorParams, "format", "")
}

// GetFilter returns the filter from ConnectorParams.
func (t *ToConfig) GetFilter() string {
	return getStringParam(t.ConnectorParams, "filter", "")
}

// GetQuery returns the query from ConnectorParams.
func (t *ToConfig) GetQuery() string {
	return getStringParam(t.ConnectorParams, "query", "")
}

// GetQueryFilter returns the query_filter from ConnectorParams.
func (t *ToConfig) GetQueryFilter() map[string]interface{} {
	return getMapParam(t.ConnectorParams, "query_filter", nil)
}

// GetUpdate returns the update from ConnectorParams.
func (t *ToConfig) GetUpdate() map[string]interface{} {
	return getMapParam(t.ConnectorParams, "update", nil)
}

// GetParams returns the params from ConnectorParams.
func (t *ToConfig) GetParams() map[string]interface{} {
	return getMapParam(t.ConnectorParams, "params", nil)
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

	// Params are the parameters to pass to the operation.
	// Keys are parameter names, values are CEL expressions.
	Params map[string]string

	// ConnectorParams holds all connector-specific parameters.
	// Populated by the parser from the HCL block attributes.
	// Access via getter methods (GetOperation).
	ConnectorParams map[string]interface{}
}

// GetOperation returns the operation from ConnectorParams.
func (e *EnrichConfig) GetOperation() string {
	return getStringParam(e.ConnectorParams, "operation", "")
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

// AsyncConfig defines async execution for a flow.
type AsyncConfig struct {
	// Storage is the cache connector for storing job results (e.g., "redis", "memory_cache").
	Storage string

	// TTL is how long to keep job results (e.g., "1h", "24h").
	TTL string
}

// IdempotencyConfig defines idempotency key behavior for a flow.
// Unlike dedupe (which discards duplicates), idempotency returns the cached result.
type IdempotencyConfig struct {
	// Storage is the cache connector name (e.g., "redis", "memory_cache").
	Storage string

	// Key is a CEL expression that extracts the idempotency key from input.
	// Example: "input.headers['X-Idempotency-Key']"
	Key string

	// TTL is how long to keep cached results (e.g., "24h").
	// After TTL expires, the same key triggers a new execution.
	TTL string
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

// SyncStorageConfig defines inline storage configuration for sync primitives.
type SyncStorageConfig struct {
	// Driver is the storage backend: "redis" or "memory".
	Driver string

	// URL is the full Redis connection URL (e.g., "redis://localhost:6379/0").
	URL string

	// Host is the Redis host (used if URL is empty).
	Host string

	// Port is the Redis port (used if URL is empty, default 6379).
	Port int

	// Password is the Redis password.
	Password string

	// DB is the Redis database number.
	DB int
}

// LockConfig holds mutex lock configuration for a flow.
type LockConfig struct {
	// Storage defines the storage backend for this lock.
	Storage *SyncStorageConfig

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
	// Storage defines the storage backend for this semaphore.
	Storage *SyncStorageConfig

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

// SequenceGuardConfig holds monotonic sequence-number deduplication for a
// flow. When a message arrives, the runtime reads the last sequence stored
// for the resolved key and only proceeds when the current sequence is
// strictly greater. Older or equal sequences are rejected with the
// configured policy. After a successful flow execution the stored sequence
// is bumped to the current value.
//
// Designed to compose with Lock — wrap the same key in both blocks and the
// outer lock guarantees the read-decide-write pattern is atomic across
// concurrent workers without explicit CAS.
type SequenceGuardConfig struct {
	// Storage defines the storage backend (Redis or in-memory). Same shape
	// as LockConfig / CoordinateConfig — accepts either a `url` or
	// `host`/`port`/`password`/`db`.
	Storage *SyncStorageConfig

	// Key is a CEL expression for the sequence key (per-resource scope).
	// Example: "'sku:' + input.body.payload.styleNumber"
	Key string

	// Sequence is a CEL expression yielding the current monotonic value.
	// Must evaluate to a number. Example: "input.body.payload.jobId"
	Sequence string

	// OnOlder defines what to do when the current sequence is <= the stored
	// sequence: "ack", "reject", or "requeue". Defaults to "ack" — the
	// most common case (older message superseded by a newer one already
	// processed).
	OnOlder string

	// TTL is how long to keep the stored sequence after the last update
	// (e.g., "30d"). Empty means no expiry. Long-tail keys can leak; a
	// 30-day TTL is the recommended baseline.
	TTL string
}

// CoordinateConfig holds coordination configuration for a flow.
type CoordinateConfig struct {
	// Storage defines the storage backend for this coordinator.
	Storage *SyncStorageConfig

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

// getStringParam reads a string from ConnectorParams, falling back to the typed field.
func getStringParam(params map[string]interface{}, key string, fallback string) string {
	if params != nil {
		if v, ok := params[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return fallback
}

// getMapParam reads a map from ConnectorParams, falling back to the typed field.
func getMapParam(params map[string]interface{}, key string, fallback map[string]interface{}) map[string]interface{} {
	if params != nil {
		if v, ok := params[key]; ok {
			if m, ok := v.(map[string]interface{}); ok {
				return m
			}
		}
	}
	return fallback
}
