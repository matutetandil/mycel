// Package statemachine implements state machines for entity lifecycle management.
// States and transitions are defined declaratively in HCL.
package statemachine

// Config holds state machine configuration from HCL.
type Config struct {
	// Name is the state machine identifier.
	Name string

	// Initial is the initial state for new entities.
	Initial string

	// States is a map of state name to state configuration.
	States map[string]*StateConfig
}

// StateConfig defines a single state and its transitions.
type StateConfig struct {
	// Name is the state identifier.
	Name string

	// Final marks this as a terminal state (no transitions out).
	Final bool

	// Transitions maps event names to transition configurations.
	Transitions map[string]*TransitionConfig
}

// TransitionConfig defines a single state transition triggered by an event.
type TransitionConfig struct {
	// Event is the event name that triggers this transition.
	Event string

	// TransitionTo is the target state.
	TransitionTo string

	// Guard is a CEL expression that must evaluate to true for the transition to proceed.
	Guard string

	// Action is an optional connector action to execute during the transition.
	Action *ActionConfig
}

// ActionConfig defines a connector action executed during a transition.
type ActionConfig struct {
	// Connector is the target connector name.
	Connector string

	// Operation is the operation to perform.
	Operation string

	// Target is the target resource (table, endpoint).
	Target string

	// Data is a map of key-value pairs for the action.
	Data map[string]interface{}

	// Body is the request body for HTTP connectors.
	Body map[string]interface{}

	// Params are additional parameters.
	Params map[string]interface{}

	// Template is a notification template name.
	Template string

	// To is the destination (email, user, etc.).
	To string
}

// TransitionResult holds the outcome of a state transition.
type TransitionResult struct {
	// EntityID is the entity that was transitioned.
	EntityID string `json:"entity_id"`

	// Machine is the state machine name.
	Machine string `json:"machine"`

	// PreviousState is the state before the transition.
	PreviousState string `json:"previous_state"`

	// CurrentState is the state after the transition.
	CurrentState string `json:"current_state"`

	// Event is the event that triggered the transition.
	Event string `json:"event"`

	// ActionResult is the result of the transition action (if any).
	ActionResult interface{} `json:"action_result,omitempty"`
}
