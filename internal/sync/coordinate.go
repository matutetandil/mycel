package sync

import (
	"context"
	"errors"
	"time"
)

// Coordinate errors.
var (
	ErrCoordinateTimeout     = errors.New("coordinate wait timed out")
	ErrMaxRetriesExceeded    = errors.New("maximum retries exceeded")
	ErrMaxConcurrentWaits    = errors.New("maximum concurrent waits reached")
	ErrCoordinateSkip        = errors.New("coordinate skip requested")
	ErrCoordinateRetry       = errors.New("coordinate retry requested")
	ErrPreflightCheckFailed  = errors.New("preflight check failed")
)

// OnTimeoutAction defines what happens when coordinate wait times out.
type OnTimeoutAction string

const (
	OnTimeoutFail  OnTimeoutAction = "fail"  // Return error, do not process
	OnTimeoutRetry OnTimeoutAction = "retry" // Requeue message for retry
	OnTimeoutSkip  OnTimeoutAction = "skip"  // Skip silently (ack without processing)
	OnTimeoutPass  OnTimeoutAction = "pass"  // Process anyway despite timeout
)

// Coordinator represents the signal/wait coordination interface.
type Coordinator interface {
	// Signal emits a signal that waiting processes can receive.
	Signal(ctx context.Context, signal string, ttl time.Duration) error

	// Wait waits for a signal to be emitted.
	// Returns true if signal was received, false if timed out.
	Wait(ctx context.Context, signal string, timeout time.Duration) (bool, error)

	// Exists checks if a signal has been emitted and is still valid.
	Exists(ctx context.Context, signal string) (bool, error)

	// Close cleans up any resources.
	Close() error
}

// CoordinateConfig holds configuration for coordinate block in a flow.
type CoordinateConfig struct {
	// Wait configuration (who waits)
	Wait *WaitConfig `json:"wait,omitempty"`

	// Signal configuration (who signals)
	Signal *SignalConfig `json:"signal,omitempty"`

	// Preflight check configuration
	Preflight *PreflightConfig `json:"preflight,omitempty"`

	// Timeout is the maximum time to wait for a signal.
	Timeout time.Duration `json:"timeout"`

	// OnTimeout defines behavior when wait times out.
	OnTimeout OnTimeoutAction `json:"on_timeout"`

	// MaxRetries is the maximum number of retries when OnTimeout is "retry".
	MaxRetries int `json:"max_retries"`

	// MaxConcurrentWaits limits simultaneous waiting processes.
	// 0 means unlimited.
	MaxConcurrentWaits int `json:"max_concurrent_waits"`
}

// WaitConfig defines when and what to wait for.
type WaitConfig struct {
	// When is a CEL expression that evaluates to true if this message should wait.
	When string `json:"when"`

	// For is a CEL expression that evaluates to the signal name to wait for.
	For string `json:"for"`
}

// SignalConfig defines when and what to signal.
type SignalConfig struct {
	// When is a CEL expression that evaluates to true if this message should emit a signal.
	When string `json:"when"`

	// Emit is a CEL expression that evaluates to the signal name to emit.
	Emit string `json:"emit"`

	// TTL is how long the signal should remain valid.
	TTL time.Duration `json:"ttl"`
}

// PreflightConfig defines a check to run before waiting.
// If the check passes, waiting is skipped.
type PreflightConfig struct {
	// Connector is the name of the connector to use for the check.
	Connector string `json:"connector"`

	// Query is the SQL query or operation to execute.
	Query string `json:"query"`

	// Params maps parameter names to CEL expressions.
	Params map[string]string `json:"params"`

	// IfExists defines behavior if the check finds a result.
	// "pass" means skip waiting, "fail" means return error.
	IfExists string `json:"if_exists"`
}

// DefaultCoordinateConfig returns a CoordinateConfig with sensible defaults.
func DefaultCoordinateConfig() *CoordinateConfig {
	return &CoordinateConfig{
		Timeout:            60 * time.Second,
		OnTimeout:          OnTimeoutFail,
		MaxRetries:         3,
		MaxConcurrentWaits: 0, // unlimited
	}
}

// ParseOnTimeoutAction parses a string into OnTimeoutAction.
func ParseOnTimeoutAction(s string) OnTimeoutAction {
	switch s {
	case "fail":
		return OnTimeoutFail
	case "retry":
		return OnTimeoutRetry
	case "skip":
		return OnTimeoutSkip
	case "pass":
		return OnTimeoutPass
	default:
		return OnTimeoutFail
	}
}

// CoordinateResult represents the result of a coordinate operation.
type CoordinateResult struct {
	// Waited indicates if the process had to wait.
	Waited bool

	// SignalReceived indicates if the signal was received.
	SignalReceived bool

	// PreflightPassed indicates if preflight check passed.
	PreflightPassed bool

	// TimedOut indicates if the wait timed out.
	TimedOut bool

	// Action indicates the action taken after timeout.
	Action OnTimeoutAction
}
