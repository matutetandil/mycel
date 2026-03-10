// Package workflow implements long-running process support with persistence,
// delay/timer steps, await/signal mechanisms, and resume-on-restart.
package workflow

import (
	"time"
)

// Status represents the current state of a workflow instance.
type Status string

const (
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusTimeout   Status = "timeout"
	StatusCancelled Status = "cancelled"
)

// Instance represents a single execution of a workflow (saga).
type Instance struct {
	// ID is the unique identifier for this workflow instance.
	ID string `json:"id"`

	// SagaName is the name of the saga being executed.
	SagaName string `json:"saga_name"`

	// Status is the current workflow status.
	Status Status `json:"status"`

	// CurrentStep is the index of the next step to execute.
	CurrentStep int `json:"current_step"`

	// Input is the original input data that triggered the workflow.
	Input map[string]interface{} `json:"input"`

	// StepResults holds the accumulated results of completed steps, keyed by step name.
	StepResults map[string]interface{} `json:"step_results"`

	// SignalData holds data received from the most recent signal.
	SignalData map[string]interface{} `json:"signal_data,omitempty"`

	// ResumeAt is when a delayed workflow should resume (nil if not delayed).
	ResumeAt *time.Time `json:"resume_at,omitempty"`

	// AwaitEvent is the event name the workflow is waiting for (empty if not waiting).
	AwaitEvent string `json:"await_event,omitempty"`

	// ExpiresAt is the saga-level timeout deadline.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// StepExpiresAt is the step-level timeout deadline (for await steps).
	StepExpiresAt *time.Time `json:"step_expires_at,omitempty"`

	// Error holds the error message if the workflow failed.
	Error string `json:"error,omitempty"`

	// CreatedAt is when the workflow was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the workflow was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}
