package aspect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/circuitbreaker"
	"github.com/matutetandil/mycel/internal/connector"
	gqlconn "github.com/matutetandil/mycel/internal/connector/graphql"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/flow"
	myerrors "github.com/matutetandil/mycel/pkg/errors"
	"github.com/matutetandil/mycel/internal/ratelimit"
	"github.com/matutetandil/mycel/internal/transform"
)

// FlowFunc is a function that executes a flow and returns the result.
type FlowFunc func(ctx context.Context, input map[string]interface{}) (*connector.Result, error)

// FlowInvoker allows the aspect executor to invoke flows by name.
type FlowInvoker interface {
	// InvokeFlow executes a flow by name with the given input.
	InvokeFlow(ctx context.Context, flowName string, input map[string]interface{}) (interface{}, error)
}

// Executor executes aspects around flows.
type Executor struct {
	registry   *Registry
	cel        *transform.CELTransformer
	connectors *connector.Registry
	flows      FlowInvoker

	// Rate limiters by config key (rps:burst)
	rateLimiters   map[string]*ratelimit.Limiter
	rateLimitersMu sync.RWMutex

	// Circuit breakers by name
	circuitBreakers   map[string]*circuitbreaker.Breaker
	circuitBreakersMu sync.RWMutex
}

// NewExecutor creates a new aspect executor.
func NewExecutor(registry *Registry, connectors *connector.Registry) (*Executor, error) {
	cel, err := transform.NewCELTransformer()
	if err != nil {
		return nil, fmt.Errorf("creating CEL transformer for aspects: %w", err)
	}

	return &Executor{
		registry:        registry,
		cel:             cel,
		connectors:      connectors,
		rateLimiters:    make(map[string]*ratelimit.Limiter),
		circuitBreakers: make(map[string]*circuitbreaker.Breaker),
	}, nil
}

// SetFlowInvoker sets the flow invoker for executing flows from aspect actions.
// Called after the flow registry is populated (avoids circular dependency).
func (e *Executor) SetFlowInvoker(invoker FlowInvoker) {
	e.flows = invoker
}

// Execute executes a flow with all matching aspects applied.
// flowName is the flow identifier used to match aspects (e.g., "create_user")
// operation is the operation being performed (e.g., "POST /users")
// target is the target connector/table
func (e *Executor) Execute(
	ctx context.Context,
	flowName string,
	operation string,
	target string,
	input map[string]interface{},
	flowFn FlowFunc,
) (*connector.Result, error) {
	// Get matching aspects by flow name
	beforeAspects := e.registry.GetBefore(flowName)
	aroundAspects := e.registry.GetAround(flowName)
	afterAspects := e.registry.GetAfter(flowName)
	onErrorAspects := e.registry.GetOnError(flowName)
	onDropAspects := e.registry.GetOnDrop(flowName)

	// Add metadata to input
	enrichedInput := e.enrichInput(input, flowName, operation, target)

	// Execute before aspects
	var err error
	enrichedInput, err = e.executeBefore(ctx, beforeAspects, enrichedInput)
	if err != nil {
		return nil, fmt.Errorf("before aspect error: %w", err)
	}

	// Build the execution chain with around aspects
	execFn := flowFn
	for i := len(aroundAspects) - 1; i >= 0; i-- {
		aspect := aroundAspects[i]
		execFn = e.wrapAround(ctx, aspect, enrichedInput, execFn)
	}

	// Strip aspect metadata before passing to the flow core — these fields
	// are only for aspect expressions, not for the downstream connector.
	flowInput := make(map[string]interface{}, len(enrichedInput))
	for k, v := range enrichedInput {
		if k == "_flow" || k == "_operation" || k == "_target" || k == "_timestamp" {
			continue
		}
		flowInput[k] = v
	}

	// Execute the flow (with around wrappers)
	result, flowErr := execFn(ctx, flowInput)

	// FilteredDropError signals "the flow body emitted a documented
	// disposition (filter / accept rejection, coordinate on_timeout=ack,
	// sequence_guard older-than-stored) rather than running its main
	// path". Skip both after AND on_error — the message was deflected,
	// not succeeded or failed. Run on_drop aspects so operators can
	// notify on orphans / drops without writing one aspect per gate;
	// `drop.reason` and `drop.policy` are bound for routing.
	var dropErr *flow.FilteredDropError
	if errors.As(flowErr, &dropErr) {
		if len(onDropAspects) > 0 {
			if onDropErr := e.executeOnDrop(ctx, onDropAspects, enrichedInput, dropErr.Result); onDropErr != nil {
				slog.Warn("on_drop aspect error",
					"flow", flowName,
					"error", onDropErr)
			}
		}
		return result, flowErr
	}

	// Execute after aspects only when the flow succeeded. Per the
	// documented contract (`docs/guides/extending.md` — "after: Run after
	// the flow succeeds"), `after` is for success notifications / cache
	// writes / response enrichment. Failures take the on_error branch
	// instead.
	if flowErr == nil {
		var afterErr error
		result, afterErr = e.executeAfter(ctx, afterAspects, enrichedInput, result, flowErr)
		if afterErr != nil {
			slog.Warn("after aspect error",
				"flow", flowName,
				"error", afterErr)
		}
	}

	// Execute on_error aspects (only when flow failed)
	if flowErr != nil && len(onErrorAspects) > 0 {
		onErrErr := e.executeOnError(ctx, onErrorAspects, enrichedInput, result, flowErr)
		if onErrErr != nil {
			slog.Warn("on_error aspect error",
				"flow", flowName,
				"error", onErrErr)
		}
	}

	return result, flowErr
}

// enrichInput adds metadata to the input for use in aspect expressions.
func (e *Executor) enrichInput(input map[string]interface{}, flowName, operation, target string) map[string]interface{} {
	enriched := make(map[string]interface{})
	for k, v := range input {
		enriched[k] = v
	}

	// Add flow metadata
	enriched["_flow"] = flowName
	enriched["_operation"] = operation
	enriched["_target"] = target
	enriched["_timestamp"] = time.Now().Unix()

	return enriched
}

// executeBefore executes all before aspects.
// Returns the potentially modified input.
func (e *Executor) executeBefore(ctx context.Context, aspects []*Config, input map[string]interface{}) (map[string]interface{}, error) {
	current := input

	for _, aspect := range aspects {
		// Check condition
		if !e.evaluateCondition(ctx, aspect, current, nil, nil) {
			continue
		}

		slog.Debug("executing before aspect",
			"aspect", aspect.Name,
			"flow", input["_flow"])

		// Execute action if present
		if aspect.Action != nil {
			if err := e.executeAction(ctx, aspect.Action, current, nil); err != nil {
				return nil, fmt.Errorf("aspect %s action error: %w", aspect.Name, err)
			}
		}

		// Execute rate limit if present
		if aspect.RateLimit != nil {
			if err := e.executeRateLimit(ctx, aspect.RateLimit, current); err != nil {
				return nil, fmt.Errorf("aspect %s rate limit error: %w", aspect.Name, err)
			}
		}
	}

	return current, nil
}

// wrapAround creates a wrapper function for around aspects.
func (e *Executor) wrapAround(ctx context.Context, aspect *Config, input map[string]interface{}, next FlowFunc) FlowFunc {
	return func(execCtx context.Context, execInput map[string]interface{}) (*connector.Result, error) {
		// Check condition
		if !e.evaluateCondition(execCtx, aspect, execInput, nil, nil) {
			return next(execCtx, execInput)
		}

		slog.Debug("executing around aspect",
			"aspect", aspect.Name,
			"flow", execInput["_flow"])

		// Handle cache aspect
		if aspect.Cache != nil {
			return e.executeCache(execCtx, aspect.Cache, execInput, next)
		}

		// Handle circuit breaker aspect
		if aspect.CircuitBreaker != nil {
			return e.executeCircuitBreaker(execCtx, aspect.CircuitBreaker, execInput, next)
		}

		// Default: just execute next
		return next(execCtx, execInput)
	}
}

// executeAfter executes all after aspects.
func (e *Executor) executeAfter(ctx context.Context, aspects []*Config, input map[string]interface{}, result *connector.Result, flowErr error) (*connector.Result, error) {
	// Reverse order for after aspects
	for i := len(aspects) - 1; i >= 0; i-- {
		asp := aspects[i]

		// Check condition
		if !e.evaluateCondition(ctx, asp, input, result, flowErr) {
			continue
		}

		slog.Debug("executing after aspect",
			"aspect", asp.Name,
			"flow", input["_flow"])

		// Execute action if present
		if asp.Action != nil {
			if err := e.executeAction(ctx, asp.Action, input, result); err != nil {
				slog.Warn("after aspect action error",
					"aspect", asp.Name,
					"error", err)
			}
		}

		// Execute invalidate if present
		if asp.Invalidate != nil {
			if err := e.executeInvalidate(ctx, asp.Invalidate, input, result); err != nil {
				slog.Warn("after aspect invalidate error",
					"aspect", asp.Name,
					"error", err)
			}
		}

		// Apply response enrichment if present
		if asp.Response != nil {
			result = e.applyResponseEnrichment(ctx, asp, input, result)
		}
	}

	return result, nil
}

// executeOnError executes all on_error aspects when a flow fails.
func (e *Executor) executeOnError(ctx context.Context, aspects []*Config, input map[string]interface{}, result *connector.Result, flowErr error) error {
	// Build structured error info once for all aspects
	errorInfo := buildErrorInfo(flowErr)

	for _, aspect := range aspects {
		// Check condition (error is always available for on_error aspects)
		if !e.evaluateCondition(ctx, aspect, input, result, flowErr) {
			continue
		}

		slog.Debug("executing on_error aspect",
			"aspect", aspect.Name,
			"flow", input["_flow"],
			"error", flowErr)

		// Execute action if present
		if aspect.Action != nil {
			// Add error info to input for transform expressions
			errorInput := make(map[string]interface{})
			for k, v := range input {
				errorInput[k] = v
			}
			errorInput["error"] = errorInfo

			if err := e.executeAction(ctx, aspect.Action, errorInput, result); err != nil {
				slog.Warn("on_error aspect action error",
					"aspect", aspect.Name,
					"error", err)
			}
		}
	}

	return nil
}

// executeOnDrop runs all on_drop aspects when the flow body emitted a
// documented filter disposition. The aspect's transform / `if` sees the
// `drop` variable with `.reason` (e.g. "coordinate_timeout",
// "sequence_older", "filter", "accept") and `.policy` ("ack", "reject",
// "requeue"); the connector + payload of the action come from the
// aspect's own `action { connector = ... transform { ... } }` block, same
// as `on_error`. Errors from individual aspect actions are logged and
// swallowed — a broken alerter shouldn't make the disposition worse.
func (e *Executor) executeOnDrop(ctx context.Context, aspects []*Config, input map[string]interface{}, dropResult *flow.FilteredResultWithPolicy) error {
	dropInfo := buildDropInfo(dropResult)

	for _, aspect := range aspects {
		if !e.evaluateCondition(ctx, aspect, input, nil, nil) {
			continue
		}

		slog.Debug("executing on_drop aspect",
			"aspect", aspect.Name,
			"flow", input["_flow"],
			"reason", dropInfo["reason"],
			"policy", dropInfo["policy"])

		if aspect.Action != nil {
			dropInput := make(map[string]interface{}, len(input)+1)
			for k, v := range input {
				dropInput[k] = v
			}
			dropInput["drop"] = dropInfo

			if err := e.executeAction(ctx, aspect.Action, dropInput, nil); err != nil {
				slog.Warn("on_drop aspect action error",
					"aspect", aspect.Name,
					"error", err)
			}
		}
	}

	return nil
}

// buildDropInfo converts a FilteredResultWithPolicy into the map shape
// `on_drop` aspects use as the `drop` CEL binding.
func buildDropInfo(result *flow.FilteredResultWithPolicy) map[string]interface{} {
	info := map[string]interface{}{
		"reason":     "",
		"policy":     "",
		"message_id": "",
	}
	if result == nil {
		return info
	}
	info["reason"] = result.Reason
	info["policy"] = result.Policy
	info["message_id"] = result.MessageID
	return info
}

// buildErrorInfo extracts structured error information from an error.
// Returns a map with: message (string), code (int), type (string).
func buildErrorInfo(err error) map[string]interface{} {
	info := map[string]interface{}{
		"message": err.Error(),
		"code":    int64(0),
		"type":    "unknown",
	}

	// Check for HTTP errors (from HTTP client connector)
	var httpErr *httpconn.HTTPError
	if errors.As(err, &httpErr) {
		info["code"] = int64(httpErr.StatusCode)
		info["type"] = "http"
		info["body"] = httpErr.Body
		return info
	}

	// Check for GraphQL HTTP errors
	var gqlErr *gqlconn.HTTPError
	if errors.As(err, &gqlErr) {
		info["code"] = int64(gqlErr.StatusCode)
		info["type"] = "http"
		info["body"] = gqlErr.Body
		return info
	}

	// Check for FlowError (from error_response block)
	var flowErr *flow.FlowError
	if errors.As(err, &flowErr) {
		info["code"] = int64(flowErr.Status)
		info["type"] = "flow"
		if flowErr.Err != nil {
			info["message"] = flowErr.Err.Error()
		}
		return info
	}

	// Check for validation errors
	var valErr *myerrors.ValidationError
	if errors.As(err, &valErr) {
		info["code"] = int64(400)
		info["type"] = "validation"
		return info
	}

	// Heuristic: check for common error patterns in the message
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		info["code"] = int64(404)
		info["type"] = "not_found"
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "connection reset"):
		info["code"] = int64(503)
		info["type"] = "connection"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out"):
		info["code"] = int64(504)
		info["type"] = "timeout"
	case strings.Contains(msg, "permission denied") || strings.Contains(msg, "unauthorized"):
		info["code"] = int64(403)
		info["type"] = "auth"
	}

	return info
}

// evaluateCondition evaluates the aspect's if condition.
func (e *Executor) evaluateCondition(ctx context.Context, aspect *Config, input map[string]interface{}, result *connector.Result, flowErr error) bool {
	if aspect.If == "" {
		return true
	}

	// Build context for CEL evaluation
	// CEL variables are at the top level, not nested in input
	evalInput := make(map[string]interface{})

	// Add input data (original input)
	evalInput["input"] = input

	// Add result if available
	if result != nil {
		resultMap := map[string]interface{}{
			"affected": result.Affected,
		}
		if len(result.Rows) > 0 {
			resultMap["data"] = result.Rows
		}
		evalInput["result"] = resultMap
	} else {
		// Provide empty result to avoid undeclared variable errors
		evalInput["result"] = map[string]interface{}{}
	}

	// Add error if available (structured object with code, message, type)
	if flowErr != nil {
		evalInput["error"] = buildErrorInfo(flowErr)
	} else {
		evalInput["error"] = map[string]interface{}{
			"message": "",
			"code":    int64(0),
			"type":    "",
		}
	}

	// Add flow metadata (from enriched input)
	evalInput["_flow"] = getStringValue(input, "_flow")
	evalInput["_operation"] = getStringValue(input, "_operation")
	evalInput["_target"] = getStringValue(input, "_target")
	evalInput["_timestamp"] = getIntValue(input, "_timestamp")

	// Empty optional variables
	evalInput["output"] = map[string]interface{}{}
	evalInput["ctx"] = map[string]interface{}{}
	evalInput["enriched"] = map[string]interface{}{}

	// Evaluate condition
	val, err := e.cel.EvaluateExpression(ctx, evalInput, nil, aspect.If)
	if err != nil {
		slog.Warn("aspect condition evaluation error",
			"aspect", aspect.Name,
			"condition", aspect.If,
			"error", err)
		return false
	}

	if boolVal, ok := val.(bool); ok {
		return boolVal
	}

	return false
}

// getStringValue safely extracts a string value from a map.
func getStringValue(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getIntValue safely extracts an int64 value from a map.
func getIntValue(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int64:
			return n
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
	}
	return 0
}

// applyResponseEnrichment evaluates CEL expressions in the response block
// and merges the results into each row of the connector.Result.
// Headers are stored in a special _response_headers field for connectors to pick up.
func (e *Executor) applyResponseEnrichment(ctx context.Context, asp *Config, input map[string]interface{}, result *connector.Result) *connector.Result {
	if result == nil {
		result = &connector.Result{}
	}

	resp := asp.Response

	// Build evaluation context with result data
	evalInput := make(map[string]interface{})
	for k, v := range input {
		evalInput[k] = v
	}
	evalInput["result"] = map[string]interface{}{
		"affected": result.Affected,
		"data":     result.Rows,
	}

	// Evaluate each response field (CEL expressions)
	enriched := make(map[string]interface{})
	for field, expr := range resp.Fields {
		val, err := e.cel.EvaluateExpression(ctx, evalInput, nil, expr)
		if err != nil {
			slog.Warn("response enrichment field error",
				"aspect", asp.Name,
				"field", field,
				"error", err)
			continue
		}
		enriched[field] = val
	}

	// Add response headers as a special field for connectors
	if len(resp.Headers) > 0 {
		// Merge with any existing response headers from other aspects
		existing := make(map[string]string)
		if result.Metadata == nil {
			result.Metadata = make(map[string]interface{})
		}
		if prev, ok := result.Metadata["_response_headers"].(map[string]string); ok {
			for k, v := range prev {
				existing[k] = v
			}
		}
		for k, v := range resp.Headers {
			existing[k] = v
		}
		result.Metadata["_response_headers"] = existing
	}

	if len(enriched) == 0 {
		return result
	}

	// Merge enriched fields into each row
	if len(result.Rows) > 0 {
		for i, row := range result.Rows {
			newRow := make(map[string]interface{})
			for k, v := range row {
				newRow[k] = v
			}
			for k, v := range enriched {
				newRow[k] = v
			}
			result.Rows[i] = newRow
		}
	} else {
		// No rows — create a single row with the enriched fields
		result.Rows = []map[string]interface{}{enriched}
	}

	return result
}

// executeAction executes an aspect action (connector write or flow invocation).
func (e *Executor) executeAction(ctx context.Context, action *ActionConfig, input map[string]interface{}, result *connector.Result) error {
	// Build data from transform
	data := make(map[string]interface{})

	// Add result to context for transform evaluation
	evalInput := make(map[string]interface{})
	for k, v := range input {
		evalInput[k] = v
	}
	if result != nil {
		evalInput["result"] = map[string]interface{}{
			"affected": result.Affected,
			"data":     result.Rows,
		}
	}

	// Apply transform
	if action.Transform != nil {
		for field, expr := range action.Transform {
			val, err := e.cel.EvaluateExpression(ctx, evalInput, nil, expr)
			if err != nil {
				return fmt.Errorf("transform field %s error: %w", field, err)
			}
			data[field] = val
		}
	}

	// Route to flow or connector
	if action.Flow != "" {
		return e.executeFlowAction(ctx, action.Flow, data)
	}

	return e.executeConnectorAction(ctx, action, data)
}

// executeFlowAction invokes a flow by name with the transformed data as input.
func (e *Executor) executeFlowAction(ctx context.Context, flowName string, data map[string]interface{}) error {
	if e.flows == nil {
		return fmt.Errorf("flow invocation not available (no flow invoker configured)")
	}

	slog.Debug("aspect invoking flow", "flow", flowName)

	_, err := e.flows.InvokeFlow(ctx, flowName, data)
	if err != nil {
		return fmt.Errorf("flow %s invocation error: %w", flowName, err)
	}

	return nil
}

// executeConnectorAction writes data to a connector.
func (e *Executor) executeConnectorAction(ctx context.Context, action *ActionConfig, data map[string]interface{}) error {
	// Get connector
	conn, err := e.connectors.Get(action.Connector)
	if err != nil {
		return fmt.Errorf("connector %s not found: %w", action.Connector, err)
	}

	// Write to connector
	writer, ok := conn.(connector.Writer)
	if !ok {
		return fmt.Errorf("connector %s does not support write operations", action.Connector)
	}

	// Default operation is INSERT for aspect actions (e.g., audit logs)
	operation := "INSERT"
	if action.Operation != "" {
		operation = action.Operation
	}

	_, writeErr := writer.Write(ctx, &connector.Data{
		Target:    action.Target,
		Operation: operation,
		Payload:   data,
	})

	return writeErr
}

// executeCache executes cache lookup/store around a flow.
func (e *Executor) executeCache(ctx context.Context, cache *CacheConfig, input map[string]interface{}, next FlowFunc) (*connector.Result, error) {
	// Build cache key
	key, err := e.interpolateString(ctx, cache.Key, input)
	if err != nil {
		slog.Warn("cache key interpolation error", "error", err)
		// Continue without cache
		return next(ctx, input)
	}

	// Get cache connector
	cacheConn, err := e.connectors.Get(cache.Storage)
	if err != nil {
		slog.Warn("cache connector not found", "storage", cache.Storage, "error", err)
		return next(ctx, input)
	}

	// Try to read from cache
	reader, ok := cacheConn.(connector.Reader)
	if ok {
		result, err := reader.Read(ctx, connector.Query{
			Target: key,
		})
		if err == nil && result != nil && len(result.Rows) > 0 {
			slog.Debug("cache hit", "key", key)
			return result, nil
		}
	}

	// Execute flow
	result, err := next(ctx, input)
	if err != nil {
		return result, err
	}

	// Store in cache
	writer, ok := cacheConn.(connector.Writer)
	if ok && result != nil && len(result.Rows) > 0 {
		ttl := parseDuration(cache.TTL)
		// Store the result data as payload
		payload := map[string]interface{}{
			"data": result.Rows,
			"ttl":  ttl.Seconds(),
		}
		_, writeErr := writer.Write(ctx, &connector.Data{
			Target:  key,
			Payload: payload,
		})
		if writeErr != nil {
			slog.Warn("cache write error", "key", key, "error", writeErr)
		} else {
			slog.Debug("cache store", "key", key, "ttl", cache.TTL)
		}
	}

	return result, nil
}

// executeInvalidate invalidates cache entries.
func (e *Executor) executeInvalidate(ctx context.Context, invalidate *InvalidateConfig, input map[string]interface{}, result *connector.Result) error {
	// Get cache connector
	cacheConn, err := e.connectors.Get(invalidate.Storage)
	if err != nil {
		return fmt.Errorf("cache connector %s not found: %w", invalidate.Storage, err)
	}

	// Add result to context
	evalInput := make(map[string]interface{})
	for k, v := range input {
		evalInput[k] = v
	}
	if result != nil {
		evalInput["result"] = map[string]interface{}{
			"affected": result.Affected,
			"data":     result.Rows,
		}
	}

	// Invalidate specific keys
	for _, keyTemplate := range invalidate.Keys {
		key, err := e.interpolateString(ctx, keyTemplate, evalInput)
		if err != nil {
			slog.Warn("invalidate key interpolation error", "template", keyTemplate, "error", err)
			continue
		}

		// Delete key via Call if available
		if caller, ok := cacheConn.(interface {
			Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error)
		}); ok {
			_, err := caller.Call(ctx, "delete", map[string]interface{}{"key": key})
			if err != nil {
				slog.Warn("cache invalidate error", "key", key, "error", err)
			} else {
				slog.Debug("cache invalidated", "key", key)
			}
		}
	}

	// Invalidate patterns
	for _, patternTemplate := range invalidate.Patterns {
		pattern, err := e.interpolateString(ctx, patternTemplate, evalInput)
		if err != nil {
			slog.Warn("invalidate pattern interpolation error", "template", patternTemplate, "error", err)
			continue
		}

		if caller, ok := cacheConn.(interface {
			Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error)
		}); ok {
			_, err := caller.Call(ctx, "delete_pattern", map[string]interface{}{"pattern": pattern})
			if err != nil {
				slog.Warn("cache pattern invalidate error", "pattern", pattern, "error", err)
			} else {
				slog.Debug("cache pattern invalidated", "pattern", pattern)
			}
		}
	}

	return nil
}

// executeRateLimit checks rate limit before flow execution.
func (e *Executor) executeRateLimit(ctx context.Context, rl *RateLimitConfig, input map[string]interface{}) error {
	// Evaluate key expression to get the rate limit key
	key, err := e.interpolateString(ctx, rl.Key, input)
	if err != nil {
		slog.Warn("rate limit key evaluation error", "error", err)
		// Continue without rate limiting on key error
		return nil
	}

	// Get or create rate limiter for this config
	limiter := e.getOrCreateRateLimiter(rl)

	// Check if request is allowed
	if !limiter.AllowKey(key) {
		slog.Debug("rate limit exceeded",
			"key", key,
			"rps", rl.RequestsPerSecond)
		return ratelimit.ErrRateLimited
	}

	slog.Debug("rate limit allowed",
		"key", key,
		"rps", rl.RequestsPerSecond)
	return nil
}

// getOrCreateRateLimiter gets or creates a rate limiter for the given config.
func (e *Executor) getOrCreateRateLimiter(rl *RateLimitConfig) *ratelimit.Limiter {
	// Create config key from RPS and burst
	configKey := fmt.Sprintf("%.2f:%d", rl.RequestsPerSecond, rl.Burst)

	e.rateLimitersMu.RLock()
	limiter, exists := e.rateLimiters[configKey]
	e.rateLimitersMu.RUnlock()

	if exists {
		return limiter
	}

	e.rateLimitersMu.Lock()
	defer e.rateLimitersMu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists = e.rateLimiters[configKey]; exists {
		return limiter
	}

	// Create new limiter
	burst := rl.Burst
	if burst == 0 {
		burst = int(rl.RequestsPerSecond * 2) // Default burst is 2x RPS
	}

	limiter = ratelimit.New(&ratelimit.Config{
		Enabled:           true,
		RequestsPerSecond: rl.RequestsPerSecond,
		Burst:             burst,
		KeyExtractor:      "custom", // We handle key extraction ourselves
	})

	e.rateLimiters[configKey] = limiter
	return limiter
}

// executeCircuitBreaker wraps flow with circuit breaker.
func (e *Executor) executeCircuitBreaker(ctx context.Context, cb *CircuitBreakerConfig, input map[string]interface{}, next FlowFunc) (*connector.Result, error) {
	breaker := e.getOrCreateCircuitBreaker(cb)

	// Execute through circuit breaker
	result, err := breaker.ExecuteWithResult(ctx, func() (interface{}, error) {
		return next(ctx, input)
	})

	if err != nil {
		// Check if it's a circuit breaker error
		if err == circuitbreaker.ErrCircuitOpen {
			slog.Debug("circuit breaker open",
				"name", cb.Name,
				"state", breaker.State().String())
			return nil, fmt.Errorf("circuit breaker %s is open: %w", cb.Name, err)
		}
		return nil, err
	}

	if connResult, ok := result.(*connector.Result); ok {
		return connResult, nil
	}

	return nil, nil
}

// getOrCreateCircuitBreaker gets or creates a circuit breaker for the given config.
func (e *Executor) getOrCreateCircuitBreaker(cb *CircuitBreakerConfig) *circuitbreaker.Breaker {
	e.circuitBreakersMu.RLock()
	breaker, exists := e.circuitBreakers[cb.Name]
	e.circuitBreakersMu.RUnlock()

	if exists {
		return breaker
	}

	e.circuitBreakersMu.Lock()
	defer e.circuitBreakersMu.Unlock()

	// Double-check after acquiring write lock
	if breaker, exists = e.circuitBreakers[cb.Name]; exists {
		return breaker
	}

	// Create new circuit breaker
	timeout := parseDuration(cb.Timeout)
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	failureThreshold := cb.FailureThreshold
	if failureThreshold == 0 {
		failureThreshold = 5
	}

	successThreshold := cb.SuccessThreshold
	if successThreshold == 0 {
		successThreshold = 2
	}

	breaker = circuitbreaker.New(&circuitbreaker.Config{
		Name:             cb.Name,
		FailureThreshold: failureThreshold,
		SuccessThreshold: successThreshold,
		Timeout:          timeout,
		OnStateChange: func(name string, from, to circuitbreaker.State) {
			slog.Info("circuit breaker state change",
				"name", name,
				"from", from.String(),
				"to", to.String())
		},
	})

	e.circuitBreakers[cb.Name] = breaker
	return breaker
}

// interpolateString interpolates ${...} expressions in a string.
func (e *Executor) interpolateString(ctx context.Context, template string, input map[string]interface{}) (string, error) {
	result := template

	// Find all ${...} patterns
	for {
		start := strings.Index(result, "${")
		if start < 0 {
			break
		}

		end := strings.Index(result[start:], "}")
		if end < 0 {
			break
		}

		expr := result[start+2 : start+end]
		val, err := e.cel.EvaluateExpression(ctx, input, nil, expr)
		if err != nil {
			return "", fmt.Errorf("interpolation error for %s: %w", expr, err)
		}

		result = result[:start] + fmt.Sprintf("%v", val) + result[start+end+1:]
	}

	return result, nil
}

// parseDuration parses a duration string.
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute // default
	}
	return d
}
