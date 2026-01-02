package aspect

import (
	"github.com/matutetandil/mycel/internal/connector"
)

// When defines when an aspect executes relative to the flow.
type When string

const (
	// Before executes before the flow, can modify input or abort.
	Before When = "before"
	// After executes after the flow, has access to result.
	After When = "after"
	// Around wraps the flow (for caching, retry, circuit breaker, etc.).
	Around When = "around"
)

// Config represents an aspect configuration.
type Config struct {
	// Name is the aspect identifier.
	Name string

	// On is a list of glob patterns matching flow files.
	// Example: ["flows/**/create_*.hcl", "flows/**/delete_*.hcl"]
	On []string

	// When defines when the aspect executes: before, after, around.
	When When

	// If is an optional CEL condition that must be true for the aspect to execute.
	// For "after" aspects, has access to "result" variable.
	If string

	// Priority determines execution order (lower = earlier).
	// Default is 0. Aspects with same priority execute in definition order.
	Priority int

	// Action defines what the aspect does (for before/after).
	Action *ActionConfig

	// Cache defines caching behavior (for around aspects).
	Cache *CacheConfig

	// Invalidate defines cache invalidation (for after aspects).
	Invalidate *InvalidateConfig

	// RateLimit defines rate limiting (for before aspects).
	RateLimit *RateLimitConfig

	// CircuitBreaker defines circuit breaker behavior (for around aspects).
	CircuitBreaker *CircuitBreakerConfig
}

// ActionConfig defines an action to perform.
type ActionConfig struct {
	// Connector is the target connector name.
	Connector string

	// Operation is the operation to perform (e.g., "POST /audit").
	Operation string

	// Target is the target table/resource.
	Target string

	// Transform defines field mappings using CEL expressions.
	Transform map[string]string
}

// CacheConfig defines caching behavior for around aspects.
type CacheConfig struct {
	// Storage is the cache connector name.
	Storage string

	// TTL is the cache time-to-live (e.g., "5m", "1h").
	TTL string

	// Key is a CEL expression for the cache key.
	// Available variables: input, input._flow, input._operation
	Key string
}

// InvalidateConfig defines cache invalidation for after aspects.
type InvalidateConfig struct {
	// Storage is the cache connector name.
	Storage string

	// Keys is a list of specific keys to invalidate.
	// Supports variable interpolation: "products:${input.id}"
	Keys []string

	// Patterns is a list of glob patterns to invalidate.
	// Supports wildcards: "products:*", "users:${input.user_id}:*"
	Patterns []string
}

// RateLimitConfig defines rate limiting for before aspects.
type RateLimitConfig struct {
	// Key is a CEL expression for the rate limit key.
	// Example: "input.user_id" or "input._client_ip"
	Key string

	// RequestsPerSecond is the allowed rate.
	RequestsPerSecond float64

	// Burst is the maximum burst size.
	Burst int
}

// CircuitBreakerConfig defines circuit breaker behavior.
type CircuitBreakerConfig struct {
	// Name is the circuit breaker identifier.
	Name string

	// FailureThreshold is the number of failures before opening.
	FailureThreshold int

	// SuccessThreshold is the number of successes before closing.
	SuccessThreshold int

	// Timeout is the time to wait before trying again.
	Timeout string
}

// Context provides runtime information to aspects.
type Context struct {
	// FlowName is the name of the flow being executed.
	FlowName string

	// Operation is the operation (e.g., "GET /users", "POST /orders").
	Operation string

	// Target is the target connector/table.
	Target string

	// Input is the flow input data.
	Input map[string]interface{}

	// Result is the flow result (only available in After aspects).
	Result *connector.Result

	// Error is the error if the flow failed (only in After).
	Error error

	// Connector registry for executing actions.
	Connectors *connector.Registry
}

// Validate validates the aspect configuration.
func (c *Config) Validate() error {
	if c.Name == "" {
		return &ValidationError{Field: "name", Message: "aspect name is required"}
	}

	if len(c.On) == 0 {
		return &ValidationError{Field: "on", Message: "at least one pattern is required"}
	}

	if c.When == "" {
		return &ValidationError{Field: "when", Message: "when is required (before, after, around)"}
	}

	switch c.When {
	case Before, After, Around:
		// Valid
	default:
		return &ValidationError{Field: "when", Message: "must be 'before', 'after', or 'around'"}
	}

	// Validate that appropriate config is provided for the aspect type
	hasAction := c.Action != nil
	hasCache := c.Cache != nil
	hasInvalidate := c.Invalidate != nil
	hasRateLimit := c.RateLimit != nil
	hasCircuitBreaker := c.CircuitBreaker != nil

	if !hasAction && !hasCache && !hasInvalidate && !hasRateLimit && !hasCircuitBreaker {
		return &ValidationError{Field: "action", Message: "aspect must have at least one action type"}
	}

	// Around aspects typically use cache or circuit breaker
	if c.When == Around && !hasCache && !hasCircuitBreaker {
		// This is a warning, not an error - around can be used for other purposes
	}

	return nil
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "aspect validation error: " + e.Field + ": " + e.Message
}
