package saga

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

// Executor runs saga steps and handles compensation on failure.
type Executor struct {
	connectors  ConnectorGetter
	transformer *transform.CELTransformer
}

// NewExecutor creates a new saga executor.
func NewExecutor(connectors ConnectorGetter) *Executor {
	t, _ := transform.NewCELTransformer()
	return &Executor{
		connectors:  connectors,
		transformer: t,
	}
}

// Execute runs the saga: execute steps in order, compensate in reverse on failure.
func (e *Executor) Execute(ctx context.Context, config *Config, input map[string]interface{}) (*Result, error) {
	result := &Result{
		SagaName: config.Name,
		Status:   "running",
		Steps:    make(map[string]interface{}),
	}

	// Execute steps in order
	var failedStepIdx int
	var stepErr error

	for i, step := range config.Steps {
		stepResult, err := e.executeAction(ctx, step.Action, input, result.Steps)
		if err != nil {
			if step.OnError == "skip" {
				result.Steps[step.Name] = nil
				continue
			}
			failedStepIdx = i
			stepErr = fmt.Errorf("step %q failed: %w", step.Name, err)
			break
		}
		result.Steps[step.Name] = stepResult
	}

	// All steps succeeded
	if stepErr == nil {
		// Execute on_complete
		if config.OnComplete != nil {
			_, err := e.executeAction(ctx, config.OnComplete, input, result.Steps)
			if err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("on_complete failed: %v", err)
				return result, nil
			}
		}
		result.Status = "completed"
		return result, nil
	}

	// Compensation: run compensate blocks in reverse order for completed steps
	result.Error = stepErr.Error()
	var compErrors []string

	for i := failedStepIdx - 1; i >= 0; i-- {
		step := config.Steps[i]
		if step.Compensate == nil {
			continue
		}
		_, err := e.executeAction(ctx, step.Compensate, input, result.Steps)
		if err != nil {
			compErrors = append(compErrors, fmt.Sprintf("compensate %q: %v", step.Name, err))
		}
	}

	// Execute on_failure
	if config.OnFailure != nil {
		_, _ = e.executeAction(ctx, config.OnFailure, input, result.Steps)
	}

	result.CompErrors = compErrors
	if len(compErrors) > 0 {
		result.Status = "failed"
	} else {
		result.Status = "compensated"
	}

	return result, nil
}

// ExecuteStep executes a single saga step's action.
// This is used by the workflow engine to run individual steps with persistence.
func (e *Executor) ExecuteStep(ctx context.Context, step *StepConfig, input map[string]interface{}, stepResults map[string]interface{}) (interface{}, error) {
	if step.Action == nil {
		return nil, nil
	}
	return e.executeAction(ctx, step.Action, input, stepResults)
}

// ExecuteAction executes a single action (on_complete, on_failure, compensate).
// This is used by the workflow engine for callbacks and compensations.
func (e *Executor) ExecuteAction(ctx context.Context, action *ActionConfig, input map[string]interface{}, stepResults map[string]interface{}) (interface{}, error) {
	return e.executeAction(ctx, action, input, stepResults)
}

// executeAction dispatches an action to the appropriate connector.
func (e *Executor) executeAction(ctx context.Context, action *ActionConfig, input map[string]interface{}, stepResults map[string]interface{}) (interface{}, error) {
	if action.Connector == "" {
		return nil, nil
	}

	conn, err := e.connectors.Get(action.Connector)
	if err != nil {
		return nil, fmt.Errorf("connector %q not found: %w", action.Connector, err)
	}

	// Resolve CEL expressions in data/body/where/set/params
	resolvedData := e.resolveMap(ctx, action.Data, input, stepResults)
	resolvedBody := e.resolveMap(ctx, action.Body, input, stepResults)
	resolvedWhere := e.resolveMap(ctx, action.Where, input, stepResults)
	resolvedSet := e.resolveMap(ctx, action.Set, input, stepResults)
	resolvedParams := e.resolveMap(ctx, action.Params, input, stepResults)

	// Dispatch based on operation type
	op := strings.ToUpper(action.Operation)

	// Try Caller interface first (HTTP client, TCP, gRPC)
	if caller, ok := conn.(Caller); ok && isCallOperation(action.Operation) {
		params := resolvedBody
		if params == nil {
			params = resolvedParams
		}
		return caller.Call(ctx, action.Operation, params)
	}

	// Database operations via Writer
	if writer, ok := conn.(connector.Writer); ok && isWriteOperation(op) {
		payload := resolvedData
		if payload == nil {
			payload = resolvedBody
		}

		// For UPDATE operations, merge set and where into the payload
		if op == "UPDATE" && resolvedSet != nil {
			if payload == nil {
				payload = make(map[string]interface{})
			}
			for k, v := range resolvedSet {
				payload[k] = v
			}
		}

		data := &connector.Data{
			Target:    action.Target,
			Operation: op,
			Payload:   payload,
			Filters:   resolvedWhere,
		}
		writeResult, err := writer.Write(ctx, data)
		if err != nil {
			return nil, err
		}
		if len(writeResult.Rows) > 0 {
			if len(writeResult.Rows) == 1 {
				return writeResult.Rows[0], nil
			}
			return writeResult.Rows, nil
		}
		return map[string]interface{}{
			"affected": writeResult.Affected,
			"id":       writeResult.LastID,
		}, nil
	}

	// Database read via Reader
	if reader, ok := conn.(connector.Reader); ok {
		query := connector.Query{
			Target:    action.Target,
			Operation: op,
			RawSQL:    action.Query,
			Filters:   resolvedParams,
		}
		readResult, err := reader.Read(ctx, query)
		if err != nil {
			return nil, err
		}
		if len(readResult.Rows) == 1 {
			return readResult.Rows[0], nil
		}
		return readResult.Rows, nil
	}

	return nil, fmt.Errorf("connector %q does not support operation %q", action.Connector, action.Operation)
}

// resolveMap evaluates CEL expressions in map values.
func (e *Executor) resolveMap(ctx context.Context, m map[string]interface{}, input map[string]interface{}, stepResults map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}

	resolved := make(map[string]interface{}, len(m))
	for k, v := range m {
		if strVal, ok := v.(string); ok && isCELExpression(strVal) {
			if e.transformer != nil {
				result, err := e.transformer.EvaluateExpressionWithSteps(ctx, input, stepResults, strVal)
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

// isCELExpression checks if a string looks like a CEL expression.
func isCELExpression(s string) bool {
	return strings.Contains(s, "input.") || strings.Contains(s, "step.")
}

// isWriteOperation checks if an operation is a database write.
func isWriteOperation(op string) bool {
	switch op {
	case "INSERT", "UPDATE", "DELETE", "INSERT_ONE", "UPDATE_ONE", "DELETE_ONE",
		"UPDATE_MANY", "DELETE_MANY", "PUBLISH":
		return true
	}
	return false
}

// isCallOperation checks if an operation is an HTTP/RPC call (contains a path or method).
func isCallOperation(op string) bool {
	return strings.Contains(op, "/") || strings.Contains(op, " ")
}
