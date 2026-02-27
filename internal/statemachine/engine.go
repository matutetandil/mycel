package statemachine

import (
	"context"
	"fmt"
	"strings"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/transform"
)

// ConnectorGetter retrieves connectors by name.
type ConnectorGetter interface {
	Get(name string) (connector.Connector, error)
}

// Caller invokes an operation on a connector.
type Caller interface {
	Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error)
}

// Engine executes state machine transitions.
type Engine struct {
	machines    map[string]*Config
	connectors  ConnectorGetter
	transformer *transform.CELTransformer
}

// NewEngine creates a new state machine engine.
func NewEngine(connectors ConnectorGetter) *Engine {
	t, _ := transform.NewCELTransformer()
	return &Engine{
		machines:    make(map[string]*Config),
		connectors:  connectors,
		transformer: t,
	}
}

// Register adds a state machine configuration.
func (e *Engine) Register(config *Config) {
	e.machines[config.Name] = config
}

// Transition executes a state transition for an entity.
func (e *Engine) Transition(ctx context.Context, machineName, entity, entityID, event string, data map[string]interface{}) (*TransitionResult, error) {
	machine, ok := e.machines[machineName]
	if !ok {
		return nil, fmt.Errorf("state machine %q not found", machineName)
	}

	// Read current state from entity table
	currentState, err := e.readCurrentState(ctx, entity, entityID, machine.Initial)
	if err != nil {
		return nil, fmt.Errorf("failed to read current state: %w", err)
	}

	// Find state config
	stateConfig, ok := machine.States[currentState]
	if !ok {
		return nil, fmt.Errorf("unknown state %q in machine %q", currentState, machineName)
	}

	// Check if state is final
	if stateConfig.Final {
		return nil, fmt.Errorf("cannot transition from final state %q", currentState)
	}

	// Find transition for event
	transition, ok := stateConfig.Transitions[event]
	if !ok {
		validEvents := make([]string, 0, len(stateConfig.Transitions))
		for e := range stateConfig.Transitions {
			validEvents = append(validEvents, e)
		}
		return nil, fmt.Errorf("invalid event %q for state %q (valid events: %s)", event, currentState, strings.Join(validEvents, ", "))
	}

	// Evaluate guard if present
	if transition.Guard != "" && e.transformer != nil {
		evalCtx := map[string]interface{}{
			"input": data,
		}
		allowed, err := e.transformer.EvaluateCondition(ctx, evalCtx, transition.Guard)
		if err != nil {
			return nil, fmt.Errorf("guard evaluation failed: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("guard rejected transition %q -> %q (guard: %s)", currentState, transition.TransitionTo, transition.Guard)
		}
	}

	// Execute transition action if present
	var actionResult interface{}
	if transition.Action != nil {
		actionResult, err = e.executeAction(ctx, transition.Action, data)
		if err != nil {
			return nil, fmt.Errorf("transition action failed: %w", err)
		}
	}

	// Persist new state
	if err := e.writeNewState(ctx, entity, entityID, transition.TransitionTo); err != nil {
		return nil, fmt.Errorf("failed to persist new state: %w", err)
	}

	return &TransitionResult{
		EntityID:      entityID,
		Machine:       machineName,
		PreviousState: currentState,
		CurrentState:  transition.TransitionTo,
		Event:         event,
		ActionResult:  actionResult,
	}, nil
}

// readCurrentState reads the current state of an entity from the database.
// Falls back to the initial state if entity is not found or has no status.
func (e *Engine) readCurrentState(ctx context.Context, entity, entityID, initial string) (string, error) {
	// Find a connector that holds this entity (by convention, the entity name is the table)
	// We iterate connectors looking for a reader that can serve this entity.
	// In practice, the flow's "to" connector is used.
	// For simplicity, we try all connectors that support reading.

	// Try to find the entity in any registered reader connector
	for _, name := range e.listConnectors() {
		conn, err := e.connectors.Get(name)
		if err != nil {
			continue
		}
		reader, ok := conn.(connector.Reader)
		if !ok {
			continue
		}

		result, err := reader.Read(ctx, connector.Query{
			Target:    entity,
			Operation: "SELECT",
			Filters: map[string]interface{}{
				"id": entityID,
			},
		})
		if err != nil {
			continue
		}
		if len(result.Rows) > 0 {
			if status, ok := result.Rows[0]["status"]; ok {
				if s, ok := status.(string); ok && s != "" {
					return s, nil
				}
			}
		}
	}

	// Entity not found or no status column — use initial state
	return initial, nil
}

// writeNewState persists the new state to the entity's status column.
func (e *Engine) writeNewState(ctx context.Context, entity, entityID, newState string) error {
	for _, name := range e.listConnectors() {
		conn, err := e.connectors.Get(name)
		if err != nil {
			continue
		}
		writer, ok := conn.(connector.Writer)
		if !ok {
			continue
		}

		_, err = writer.Write(ctx, &connector.Data{
			Target:    entity,
			Operation: "UPDATE",
			Payload: map[string]interface{}{
				"status": newState,
			},
			Filters: map[string]interface{}{
				"id": entityID,
			},
		})
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("no writable connector found for entity %q", entity)
}

// executeAction dispatches a transition action to the appropriate connector.
func (e *Engine) executeAction(ctx context.Context, action *ActionConfig, data map[string]interface{}) (interface{}, error) {
	if action.Connector == "" {
		return nil, nil
	}

	conn, err := e.connectors.Get(action.Connector)
	if err != nil {
		return nil, fmt.Errorf("connector %q not found: %w", action.Connector, err)
	}

	// Resolve CEL expressions in maps
	resolvedData := e.resolveMap(ctx, action.Data, data)
	resolvedBody := e.resolveMap(ctx, action.Body, data)
	resolvedParams := e.resolveMap(ctx, action.Params, data)

	// Try Caller interface first (HTTP, gRPC)
	if caller, ok := conn.(Caller); ok && isCallOperation(action.Operation) {
		params := resolvedBody
		if params == nil {
			params = resolvedParams
		}
		return caller.Call(ctx, action.Operation, params)
	}

	// Writer interface
	if writer, ok := conn.(connector.Writer); ok {
		payload := resolvedData
		if payload == nil {
			payload = resolvedBody
		}
		writeResult, err := writer.Write(ctx, &connector.Data{
			Target:    action.Target,
			Operation: action.Operation,
			Payload:   payload,
			Filters:   resolvedParams,
		})
		if err != nil {
			return nil, err
		}
		if len(writeResult.Rows) > 0 {
			return writeResult.Rows, nil
		}
		return map[string]interface{}{"affected": writeResult.Affected}, nil
	}

	return nil, fmt.Errorf("connector %q does not support operation %q", action.Connector, action.Operation)
}

// resolveMap evaluates CEL expressions in map values.
func (e *Engine) resolveMap(ctx context.Context, m map[string]interface{}, data map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}

	resolved := make(map[string]interface{}, len(m))
	for k, v := range m {
		if strVal, ok := v.(string); ok && strings.Contains(strVal, "input.") {
			if e.transformer != nil {
				result, err := e.transformer.EvaluateExpression(ctx, data, nil, strVal)
				if err == nil {
					resolved[k] = result
					continue
				}
			}
		}
		resolved[k] = v
	}
	return resolved
}

// listConnectors returns all connector names from the registry.
func (e *Engine) listConnectors() []string {
	if lister, ok := e.connectors.(interface{ List() []string }); ok {
		return lister.List()
	}
	return nil
}

// isCallOperation checks if an operation is an HTTP/RPC call.
func isCallOperation(op string) bool {
	return strings.Contains(op, "/") || strings.Contains(op, " ")
}
