// Package saga implements the Saga pattern for distributed transactions
// with automatic compensation (rollback) on failure.
package saga

// Config holds saga configuration from HCL.
type Config struct {
	// Name is the saga identifier.
	Name string

	// From defines the trigger for this saga (optional).
	From *FromConfig

	// Steps are the saga steps executed in order.
	Steps []*StepConfig

	// OnComplete is executed when all steps succeed.
	OnComplete *ActionConfig

	// OnFailure is executed after compensations complete.
	OnFailure *ActionConfig

	// Timeout is the maximum duration for the entire saga (e.g., "30s").
	Timeout string
}

// FromConfig defines the saga trigger.
type FromConfig struct {
	// Connector is the source connector name.
	Connector string

	// Operation is the trigger operation (e.g., "POST /orders").
	Operation string

	// Filter is a CEL expression to filter incoming requests.
	Filter string
}

// StepConfig defines a single step in the saga.
type StepConfig struct {
	// Name is the step identifier (used as step.<name> in expressions).
	Name string

	// Action is the forward action to execute.
	Action *ActionConfig

	// Compensate is the rollback action executed on failure.
	Compensate *ActionConfig

	// Timeout is the maximum duration for this step.
	Timeout string

	// OnError defines behavior on failure: "fail" (default) or "skip".
	OnError string
}

// ActionConfig defines a connector action (used for action, compensate, on_complete, on_failure).
type ActionConfig struct {
	// Connector is the target connector name.
	Connector string

	// Operation is the operation to perform (e.g., "INSERT", "POST /charges").
	Operation string

	// Target is the target resource (table, endpoint).
	Target string

	// Query is a raw SQL query for database connectors.
	Query string

	// Data is a map of key-value pairs for INSERT/UPDATE operations.
	// Values can be CEL expressions referencing input.* or step.*.
	Data map[string]interface{}

	// Body is the request body for HTTP connectors.
	Body map[string]interface{}

	// Set is a map of fields to update (for UPDATE operations).
	Set map[string]interface{}

	// Where is a map of conditions for UPDATE/DELETE operations.
	Where map[string]interface{}

	// Params are additional parameters.
	Params map[string]interface{}

	// Template is a notification template name.
	Template string

	// To is the destination (email, user, etc.).
	To string
}

// Result holds the outcome of a saga execution.
type Result struct {
	// SagaName is the name of the saga that was executed.
	SagaName string `json:"saga_name"`

	// InstanceID is a unique identifier for this execution.
	InstanceID string `json:"instance_id"`

	// Status is the saga outcome: "completed", "compensated", or "failed".
	Status string `json:"status"`

	// Steps contains the results of each step keyed by step name.
	Steps map[string]interface{} `json:"steps"`

	// Error contains the error message if the saga failed.
	Error string `json:"error,omitempty"`

	// CompErrors contains errors from compensation steps.
	CompErrors []string `json:"compensation_errors,omitempty"`
}
