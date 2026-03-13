package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/aspect"
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/cache"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/functions"
	"github.com/matutetandil/mycel/internal/sanitize"
	"github.com/matutetandil/mycel/internal/trace"
	"github.com/matutetandil/mycel/internal/graphql/optimizer"
	"github.com/matutetandil/mycel/internal/saga"
	"github.com/matutetandil/mycel/internal/statemachine"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
	"github.com/matutetandil/mycel/internal/validate"
	"github.com/matutetandil/mycel/internal/validator"
	"github.com/matutetandil/mycel/internal/workflow"
)

// FlowRegistry manages flow handlers.
type FlowRegistry struct {
	mu       sync.RWMutex
	handlers map[string]*FlowHandler
}

// NewFlowRegistry creates a new flow registry.
func NewFlowRegistry() *FlowRegistry {
	return &FlowRegistry{
		handlers: make(map[string]*FlowHandler),
	}
}

// Register adds a flow handler to the registry.
func (r *FlowRegistry) Register(name string, handler *FlowHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = handler
}

// Get retrieves a flow handler by name.
func (r *FlowRegistry) Get(name string) (*FlowHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}

// InvokeFlow executes a flow by name with the given input.
// Implements aspect.FlowInvoker interface.
func (r *FlowRegistry) InvokeFlow(ctx context.Context, flowName string, input map[string]interface{}) (interface{}, error) {
	handler, ok := r.Get(flowName)
	if !ok {
		return nil, fmt.Errorf("flow %q not found", flowName)
	}
	return handler.HandleRequest(ctx, input)
}

// List returns all registered flow names.
func (r *FlowRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	return names
}

// FlowHandler handles execution of a single flow.
type FlowHandler struct {
	// Config is the flow configuration from HCL.
	Config *flow.Config

	// Source is the source connector (where data comes from).
	Source connector.Connector

	// Dest is the destination connector (where data goes to).
	Dest connector.Connector

	// Executor is the flow pipeline executor.
	Executor *flow.Executor

	// Transformer handles data transformations for this flow (CEL-based).
	Transformer *transform.CELTransformer

	// NamedTransforms allows lookup of reusable transforms.
	NamedTransforms map[string]*transform.Config

	// Types allows lookup of type schemas for validation.
	Types map[string]*validate.TypeSchema

	// Validator handles input/output validation.
	Validator *validate.TypeValidator

	// Connectors registry for enrichment lookups.
	Connectors *connector.Registry

	// OperationResolver resolves named operations to inline format.
	OperationResolver *connector.OperationResolver

	// CacheConnector is the cache connector for this flow (if configured).
	CacheConnector cache.Cache

	// NamedCaches allows lookup of named cache definitions.
	NamedCaches map[string]*flow.NamedCacheConfig

	// AspectExecutor handles cross-cutting concerns (AOP).
	AspectExecutor *aspect.Executor

	// FunctionsRegistry provides access to WASM functions for CEL expressions.
	FunctionsRegistry *functions.Registry

	// SyncManager provides distributed locks, semaphores, and coordination.
	SyncManager *msync.Manager

	// SagaExecutor handles saga pattern execution (distributed transactions).
	SagaExecutor *saga.Executor

	// SagaConfig holds the saga configuration when this handler wraps a saga.
	SagaConfig *saga.Config

	// WorkflowEngine handles long-running sagas with persistence.
	WorkflowEngine *workflow.Engine

	// StateMachineEngine handles state machine transitions.
	StateMachineEngine *statemachine.Engine

	// SourceType is the connector type of the source (e.g., "mq", "rest", "soap").
	// Used to determine how to interpret non-HTTP operations.
	SourceType string

	// Sanitizer is the input sanitization pipeline (always active).
	Sanitizer *sanitize.Pipeline

	// ValidatorRegistry provides access to custom validators (regex/CEL/WASM)
	// for type field validation via the `validator` attribute.
	ValidatorRegistry *validator.Registry

	// Logger for request logging.
	Logger *slog.Logger

	// VerboseFlow enables per-request trace logging via LogCollector.
	VerboseFlow bool
}

// FilteredResult is returned when a request is filtered out by the from.filter expression.
// Used for backwards compatibility when no rejection policy is configured.
var FilteredResult = &struct{ Filtered bool }{Filtered: true}

// HandleRequest processes an incoming request through the flow.
func (h *FlowHandler) HandleRequest(ctx context.Context, input map[string]interface{}) (result interface{}, err error) {
	// Attach trace context for verbose flow logging (when no trace is already active)
	if h.VerboseFlow && !trace.IsTracing(ctx) && h.Logger != nil {
		tc := &trace.Context{
			FlowName:  h.Config.Name,
			Collector: trace.NewLogCollector(h.Logger),
		}
		ctx = trace.WithTrace(ctx, tc)
	}

	start := time.Now()
	defer func() {
		if h.Logger == nil {
			return
		}
		duration := time.Since(start)
		attrs := []slog.Attr{
			slog.String("flow", h.Config.Name),
			slog.String("source", h.Config.From.Connector),
			slog.Duration("duration", duration),
		}
		if h.Config.From.Operation != "" {
			attrs = append(attrs, slog.String("operation", h.Config.From.Operation))
		}
		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
			h.Logger.LogAttrs(ctx, slog.LevelWarn, "request", attrs...)
		} else {
			h.Logger.LogAttrs(ctx, slog.LevelInfo, "request", attrs...)
		}
	}()

	// Record input for tracing
	trace.RecordSimple(ctx, trace.StageInput, "", input, "")

	// Sanitize input (always runs first, before any processing)
	if h.Sanitizer != nil && input != nil {
		var sanitizeErr error
		_, sanitizeErr = trace.RecordStage(ctx, trace.StageSanitize, "", input, func() (interface{}, error) {
			sanitized, err := h.Sanitizer.Sanitize(input)
			if err != nil {
				return nil, err
			}
			input = sanitized
			return input, nil
		})
		if sanitizeErr != nil {
			return nil, fmt.Errorf("input sanitization failed: %w", sanitizeErr)
		}
	}

	// Check filter condition first (before any processing)
	if h.Config.From != nil && h.Config.From.FilterCondition() != "" {
		shouldProcess, err := h.evaluateFilter(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("filter evaluation error: %w", err)
		}
		if !shouldProcess {
			trace.RecordSimple(ctx, trace.StageFilter, "", nil, "filtered out")

			// Return policy-aware result if FilterConfig is set
			if h.Config.From.FilterConfig != nil {
				result := &flow.FilteredResultWithPolicy{
					Filtered:   true,
					Policy:     h.Config.From.FilterConfig.OnReject,
					MaxRequeue: h.Config.From.FilterConfig.MaxRequeue,
				}
				// Evaluate ID field if configured (for requeue dedup)
				if h.Config.From.FilterConfig.IDField != "" && h.Config.From.FilterConfig.OnReject == "requeue" {
					msgID, _ := h.evaluateIDField(ctx, input)
					result.MessageID = msgID
				}
				return result, nil
			}
			return FilteredResult, nil
		}
	}

	// Check for duplicate messages (deduplication)
	if h.Config.Dedupe != nil {
		isDuplicate, err := h.checkDedupe(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("dedupe check error: %w", err)
		}
		if isDuplicate {
			if h.Config.Dedupe.OnDuplicate == "fail" {
				return nil, fmt.Errorf("duplicate message detected")
			}
			// Default: skip silently
			return flow.DuplicateResult, nil
		}
	}

	// Validate input if schema is configured
	_, valErr := trace.RecordStage(ctx, trace.StageValidateIn, "", input, func() (interface{}, error) {
		return nil, h.validateInput(ctx, input)
	})
	if valErr != nil {
		return nil, valErr
	}

	// Core execution function
	executeFn := func() (interface{}, error) {
		// If aspect executor is configured, wrap execution with aspects
		if h.AspectExecutor != nil && h.Config.Name != "" {
			return h.handleRequestWithAspects(ctx, input)
		}
		// Execute without aspects
		return h.executeFlowCore(ctx, input)
	}

	// If error handling is configured, wrap with retry logic
	if h.Config.ErrorHandling != nil {
		result, err := h.executeWithRetry(ctx, input, executeFn)
		if err != nil {
			return result, h.wrapErrorResponse(ctx, input, err)
		}
		return result, nil
	}

	// Execute without error handling wrapper
	result, err = executeFn()
	if err != nil && h.Config.ErrorHandling != nil {
		return result, h.wrapErrorResponse(ctx, input, err)
	}
	return result, err
}

// executeWithRetry executes the flow with retry and fallback handling.
func (h *FlowHandler) executeWithRetry(ctx context.Context, input map[string]interface{}, executeFn func() (interface{}, error)) (interface{}, error) {
	eh := h.Config.ErrorHandling
	maxAttempts := 1
	delay := time.Second
	maxDelay := 30 * time.Second
	backoff := "constant"

	if eh.Retry != nil {
		if eh.Retry.Attempts > 0 {
			maxAttempts = eh.Retry.Attempts
		}
		if eh.Retry.Delay != "" {
			if d, err := time.ParseDuration(eh.Retry.Delay); err == nil {
				delay = d
			}
		}
		if eh.Retry.MaxDelay != "" {
			if d, err := time.ParseDuration(eh.Retry.MaxDelay); err == nil {
				maxDelay = d
			}
		}
		if eh.Retry.Backoff != "" {
			backoff = eh.Retry.Backoff
		}
	}

	var lastErr error
	currentDelay := delay

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := executeFn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Don't retry if context is cancelled
		if ctx.Err() != nil {
			break
		}

		// Don't sleep after last attempt
		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(currentDelay):
			}

			// Calculate next delay based on backoff strategy
			switch backoff {
			case "exponential":
				currentDelay = currentDelay * 2
				if currentDelay > maxDelay {
					currentDelay = maxDelay
				}
			case "linear":
				currentDelay = currentDelay + delay
				if currentDelay > maxDelay {
					currentDelay = maxDelay
				}
			// "constant" - delay stays the same
			}
		}
	}

	// All retries exhausted, check for fallback
	if eh.Fallback != nil {
		if fallbackErr := h.sendToFallback(ctx, input, lastErr); fallbackErr != nil {
			// Return both errors
			return nil, fmt.Errorf("flow failed after %d attempts: %w (fallback also failed: %v)", maxAttempts, lastErr, fallbackErr)
		}
		// Fallback succeeded
		return nil, fmt.Errorf("flow failed after %d attempts, sent to fallback: %w", maxAttempts, lastErr)
	}

	return nil, fmt.Errorf("flow failed after %d attempts: %w", maxAttempts, lastErr)
}

// sendToFallback sends the failed message to the fallback connector (DLQ).
func (h *FlowHandler) sendToFallback(ctx context.Context, input map[string]interface{}, flowErr error) error {
	fb := h.Config.ErrorHandling.Fallback

	// Build fallback message
	message := make(map[string]interface{})

	// Include original input
	message["original_input"] = input

	// Include error details if configured
	if fb.IncludeError {
		message["error"] = map[string]interface{}{
			"message":   flowErr.Error(),
			"flow_name": h.Config.Name,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
	}

	// Apply transform if configured
	if len(fb.Transform) > 0 && h.Transformer != nil {
		rules := make([]transform.Rule, 0, len(fb.Transform))
		for target, expr := range fb.Transform {
			rules = append(rules, transform.Rule{Target: target, Expression: expr})
		}
		// Build context with input and error
		data := map[string]interface{}{
			"input": input,
			"error": map[string]interface{}{
				"message": flowErr.Error(),
			},
		}
		transformed, err := h.Transformer.Transform(ctx, data, rules)
		if err == nil {
			message = transformed
		}
	}

	// Get fallback connector from registry
	fallbackConn := h.getConnector(fb.Connector)
	if fallbackConn == nil {
		return fmt.Errorf("fallback connector '%s' not found", fb.Connector)
	}

	// Send to fallback
	writer, ok := fallbackConn.(connector.Writer)
	if !ok {
		return fmt.Errorf("fallback connector '%s' does not support writing", fb.Connector)
	}

	data := &connector.Data{
		Target:    fb.Target,
		Operation: "INSERT", // Default operation for DLQ
		Payload:   message,
	}

	_, err := writer.Write(ctx, data)
	return err
}

// getConnector gets a connector from the handler's connector registry.
func (h *FlowHandler) getConnector(name string) connector.Connector {
	if h.Connectors == nil {
		return nil
	}
	conn, err := h.Connectors.Get(name)
	if err != nil {
		return nil
	}
	return conn
}

// wrapErrorResponse wraps an error with custom response configuration if error_response is configured.
func (h *FlowHandler) wrapErrorResponse(ctx context.Context, input map[string]interface{}, err error) error {
	if h.Config.ErrorHandling == nil || h.Config.ErrorHandling.ErrorResponse == nil {
		return err
	}

	er := h.Config.ErrorHandling.ErrorResponse

	// Build response body using CEL transforms
	var body map[string]interface{}
	if len(er.Body) > 0 && h.Transformer != nil {
		rules := make([]transform.Rule, 0, len(er.Body))
		for target, expr := range er.Body {
			rules = append(rules, transform.Rule{Target: target, Expression: expr})
		}

		data := map[string]interface{}{
			"input": input,
			"error": map[string]interface{}{
				"message": err.Error(),
			},
		}

		transformed, transformErr := h.Transformer.Transform(ctx, data, rules)
		if transformErr == nil {
			body = transformed
		}
	}

	// If no body transform, use a simple error message
	if body == nil {
		body = map[string]interface{}{
			"error": err.Error(),
		}
	}

	return flow.NewFlowError(err, er.Status, body, er.Headers)
}

// evaluateFilter evaluates the from.filter CEL expression.
// Returns true if the request should be processed, false if filtered out.
func (h *FlowHandler) evaluateFilter(ctx context.Context, input map[string]interface{}) (bool, error) {
	condition := h.Config.From.FilterCondition()
	if condition == "" {
		return true, nil
	}

	// Initialize transformer if needed
	if h.Transformer == nil {
		var err error
		celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		h.Transformer, err = transform.NewCELTransformerWithOptions(celOptions...)
		if err != nil {
			return false, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
	}

	// Build context for filter evaluation
	data := map[string]interface{}{
		"input": input,
	}

	return h.Transformer.EvaluateCondition(ctx, data, condition)
}

// evaluateIDField evaluates the id_field CEL expression to extract a message ID.
func (h *FlowHandler) evaluateIDField(ctx context.Context, input map[string]interface{}) (string, error) {
	if h.Config.From.FilterConfig == nil || h.Config.From.FilterConfig.IDField == "" {
		return "", nil
	}

	// Initialize transformer if needed
	if h.Transformer == nil {
		var err error
		celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		h.Transformer, err = transform.NewCELTransformerWithOptions(celOptions...)
		if err != nil {
			return "", fmt.Errorf("failed to create CEL transformer: %w", err)
		}
	}

	data := map[string]interface{}{
		"input": input,
	}

	result, err := h.Transformer.EvaluateExpression(ctx, data, nil, h.Config.From.FilterConfig.IDField)
	if err != nil {
		return "", err
	}

	if s, ok := result.(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", result), nil
}

// checkDedupe checks if a message is a duplicate based on the dedupe configuration.
// Returns true if the message is a duplicate, false otherwise.
// If not a duplicate, stores the key in the cache with the configured TTL.
func (h *FlowHandler) checkDedupe(ctx context.Context, input map[string]interface{}) (bool, error) {
	dedupe := h.Config.Dedupe
	if dedupe == nil {
		return false, nil
	}

	// Initialize transformer if needed
	if h.Transformer == nil {
		var err error
		celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		h.Transformer, err = transform.NewCELTransformerWithOptions(celOptions...)
		if err != nil {
			return false, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
	}

	// Evaluate the key expression
	data := map[string]interface{}{
		"input": input,
	}

	keyResult, err := h.Transformer.EvaluateExpression(ctx, data, nil, dedupe.Key)
	if err != nil {
		return false, fmt.Errorf("dedupe key evaluation error: %w", err)
	}

	// Convert key to string
	var dedupeKey string
	switch v := keyResult.(type) {
	case string:
		dedupeKey = v
	default:
		dedupeKey = fmt.Sprintf("%v", v)
	}

	if dedupeKey == "" {
		return false, fmt.Errorf("dedupe key evaluated to empty string")
	}

	// Prefix the key to avoid collisions
	cacheKey := "dedupe:" + h.Config.Name + ":" + dedupeKey

	// Get the storage connector
	storageConn, err := h.Connectors.Get(dedupe.Storage)
	if err != nil {
		return false, fmt.Errorf("dedupe storage connector not found: %s: %w", dedupe.Storage, err)
	}

	// Check if key exists using the cache interface
	cacheStorage, ok := storageConn.(cache.Cache)
	if !ok {
		return false, fmt.Errorf("dedupe storage %s does not implement cache interface", dedupe.Storage)
	}

	// Try to get the key
	_, exists, err := cacheStorage.Get(ctx, cacheKey)
	if err != nil {
		// Log error but continue (fail open to avoid blocking messages on cache errors)
		// In production, you might want different behavior
		return false, nil
	}

	if exists {
		// Duplicate found
		return true, nil
	}

	// Not a duplicate - store the key with TTL
	ttl := time.Hour // Default TTL
	if dedupe.TTL != "" {
		if d, parseErr := time.ParseDuration(dedupe.TTL); parseErr == nil {
			ttl = d
		}
	}

	// Store a simple marker value
	if setErr := cacheStorage.Set(ctx, cacheKey, []byte("1"), ttl); setErr != nil {
		// Log error but continue (fail open)
		// The message will be processed even if we can't store the dedup key
	}

	return false, nil
}

// handleRequestWithAspects wraps flow execution with aspect executor.
func (h *FlowHandler) handleRequestWithAspects(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	operation := parseOperation(h.Config.From.Operation)

	// Create the flow function that the aspect executor will wrap
	flowFn := func(ctx context.Context, flowInput map[string]interface{}) (*connector.Result, error) {
		result, err := h.executeFlowCore(ctx, flowInput)
		if err != nil {
			return nil, err
		}

		// Convert result to connector.Result format
		return h.resultToConnectorResult(result), nil
	}

	// Execute with aspects (match by flow name)
	result, err := h.AspectExecutor.Execute(
		ctx,
		h.Config.Name,
		h.Config.From.Operation,
		h.toTarget(),
		input,
		flowFn,
	)

	if err != nil {
		return nil, err
	}

	// Convert back from connector.Result to interface{}
	if result == nil {
		return nil, nil
	}

	// For GET operations, return rows directly
	if operation.Method == "GET" {
		return result.Rows, nil
	}

	// For write operations, return appropriate format
	if len(result.Rows) > 0 {
		if len(result.Rows) == 1 {
			return result.Rows[0], nil
		}
		return result.Rows, nil
	}

	return map[string]interface{}{
		"affected": result.Affected,
		"id":       result.LastID,
	}, nil
}

// resultToConnectorResult converts an interface{} result to connector.Result.
func (h *FlowHandler) resultToConnectorResult(result interface{}) *connector.Result {
	if result == nil {
		return &connector.Result{}
	}

	switch v := result.(type) {
	case []map[string]interface{}:
		return &connector.Result{Rows: v}
	case map[string]interface{}:
		// Check if it's a write result
		if affected, ok := v["affected"]; ok {
			res := &connector.Result{}
			if a, ok := affected.(int64); ok {
				res.Affected = a
			} else if a, ok := affected.(int); ok {
				res.Affected = int64(a)
			}
			if id, ok := v["id"]; ok {
				if i, ok := id.(int64); ok {
					res.LastID = i
				} else if i, ok := id.(int); ok {
					res.LastID = int64(i)
				}
			}
			return res
		}
		return &connector.Result{Rows: []map[string]interface{}{v}}
	default:
		return &connector.Result{}
	}
}

// executeFlowCore executes the core flow logic without aspects.
func (h *FlowHandler) executeFlowCore(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	// Build the core execution function
	executeCore := func() (interface{}, error) {
		return h.executeFlowCoreInternal(ctx, input)
	}

	// Wrap with sync primitives if configured
	if h.SyncManager != nil {
		// Handle lock
		if h.Config.Lock != nil {
			lockKey := h.evaluateSyncKey(ctx, h.Config.Lock.Key, input)
			lockCfg := &msync.FlowLockConfig{
				Storage: h.Config.Lock.Storage,
				Key:     h.Config.Lock.Key,
				Timeout: h.Config.Lock.Timeout,
				Wait:    h.Config.Lock.Wait,
				Retry:   h.Config.Lock.Retry,
			}
			return h.SyncManager.ExecuteWithLock(ctx, lockCfg, lockKey, executeCore)
		}

		// Handle semaphore
		if h.Config.Semaphore != nil {
			semKey := h.evaluateSyncKey(ctx, h.Config.Semaphore.Key, input)
			semCfg := &msync.FlowSemaphoreConfig{
				Storage:    h.Config.Semaphore.Storage,
				Key:        h.Config.Semaphore.Key,
				MaxPermits: h.Config.Semaphore.MaxPermits,
				Timeout:    h.Config.Semaphore.Timeout,
				Lease:      h.Config.Semaphore.Lease,
			}
			return h.SyncManager.ExecuteWithSemaphore(ctx, semCfg, semKey, executeCore)
		}

		// Handle coordinate
		if h.Config.Coordinate != nil {
			var waitKey, signalKey string
			if h.Config.Coordinate.Wait != nil {
				waitKey = h.evaluateSyncKey(ctx, h.Config.Coordinate.Wait.For, input)
			}
			if h.Config.Coordinate.Signal != nil {
				signalKey = h.evaluateSyncKey(ctx, h.Config.Coordinate.Signal.Emit, input)
			}

			var waitCfg *msync.FlowWaitConfig
			if h.Config.Coordinate.Wait != nil {
				waitCfg = &msync.FlowWaitConfig{
					When: h.Config.Coordinate.Wait.When,
					For:  h.Config.Coordinate.Wait.For,
				}
			}
			var signalFlowCfg *msync.FlowSignalConfig
			if h.Config.Coordinate.Signal != nil {
				signalFlowCfg = &msync.FlowSignalConfig{
					When: h.Config.Coordinate.Signal.When,
					Emit: h.Config.Coordinate.Signal.Emit,
					TTL:  h.Config.Coordinate.Signal.TTL,
				}
			}

			coordCfg := &msync.FlowCoordinateConfig{
				Storage:            h.Config.Coordinate.Storage,
				Wait:               waitCfg,
				Signal:             signalFlowCfg,
				Timeout:            h.Config.Coordinate.Timeout,
				OnTimeout:          h.Config.Coordinate.OnTimeout,
				MaxRetries:         h.Config.Coordinate.MaxRetries,
				MaxConcurrentWaits: h.Config.Coordinate.MaxConcurrentWaits,
			}
			return h.SyncManager.ExecuteWithCoordinate(ctx, coordCfg, signalKey, waitKey, executeCore)
		}
	}

	// No sync primitives, execute directly
	return executeCore()
}

// evaluateSyncKey evaluates a CEL expression for sync key, or returns the key as-is if not a CEL expression.
func (h *FlowHandler) evaluateSyncKey(ctx context.Context, keyExpr string, input map[string]interface{}) string {
	if keyExpr == "" {
		return ""
	}

	// If it looks like a CEL expression (contains operators or function calls), evaluate it
	if h.Transformer != nil && (strings.Contains(keyExpr, "+") || strings.Contains(keyExpr, "(") || strings.Contains(keyExpr, "input.")) {
		result, err := h.Transformer.EvaluateExpression(ctx, input, nil, keyExpr)
		if err == nil {
			if s, ok := result.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", result)
		}
	}

	return keyExpr
}

// executeFlowCoreInternal contains the actual flow logic.
func (h *FlowHandler) executeFlowCoreInternal(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	// Determine operation type from the flow config
	operation := parseOperation(h.Config.From.Operation)

	// For event-driven sources (MQ consumers, CDC, file watchers), the operation
	// string is a queue name, table name, or glob pattern — not "METHOD /path".
	// These flows receive events and write to the destination.
	if operation.Method == "GET" && isEventDrivenSource(h.SourceType) {
		operation.Method = "POST"
	}

	// For non-REST sources (gRPC, SOAP, etc.) where the from.operation doesn't
	// contain an HTTP method, use to.Operation to determine the write intent.
	if operation.Method == "GET" && h.Config.To != nil && h.Config.To.Operation != "" {
		switch strings.ToUpper(h.Config.To.Operation) {
		case "INSERT":
			operation.Method = "POST"
		case "UPDATE":
			operation.Method = "PUT"
		case "DELETE":
			operation.Method = "DELETE"
		}
	}

	// For read operations, check cache first
	if operation.Method == "GET" && h.hasCacheConfig() {
		cacheKey := h.buildCacheKey(input)
		if cacheKey != "" {
			cached, hit, err := h.checkCache(ctx, cacheKey)
			if err == nil && hit {
				return cached, nil
			}
		}
	}

	// If this is a saga flow, dispatch to workflow engine (async) or saga executor (sync)
	if h.SagaExecutor != nil && h.SagaConfig != nil {
		if h.WorkflowEngine != nil && workflow.NeedsPersistence(h.SagaConfig) {
			instance, err := h.WorkflowEngine.Execute(ctx, h.SagaConfig.Name, input)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"workflow_id": instance.ID,
				"status":      string(instance.Status),
				"saga":        instance.SagaName,
			}, nil
		}
		sagaResult, err := h.SagaExecutor.Execute(ctx, h.SagaConfig, input)
		if err != nil {
			return nil, err
		}
		return sagaResult, nil
	}

	// If this flow has a state_transition block, execute it
	if h.Config.StateTransition != nil && h.StateMachineEngine != nil {
		return h.executeStateTransition(ctx, input)
	}

	var result interface{}
	var err error

	// If batch processing is configured, execute batch
	if h.Config.Batch != nil {
		return h.executeBatch(ctx, input)
	}

	// Check if the destination is a subscription publish target
	if h.Config.To != nil && isSubscriptionPublish(h.Config.To.Operation) {
		return h.handleSubscriptionPublish(ctx, input)
	}

	// For flows with steps, execute steps + transform instead of reading from destination
	// This supports orchestration flows where data comes from multiple sources
	if len(h.Config.Steps) > 0 && operation.Method == "GET" {
		result, err = h.handleStepsFlow(ctx, input)
	} else if len(h.Config.MultiTo) > 0 && operation.Method != "GET" {
		// Check for multi-destination writes
		result, err = h.handleMultiDestWrite(ctx, input, operation)
	} else if h.Dest == nil {
		// Echo flow (no "to" block) — return transformed input as-is
		result = input
	} else {
		// Single destination (original behavior)
		// Get the destination as a reader/writer
		dest, ok := h.Dest.(connector.ReadWriter)

		if !ok {
			// Try just reader or writer based on operation
			result, err = h.handleSimpleRequest(ctx, input)
		} else {
			switch operation.Method {
			case "GET":
				result, err = h.handleRead(ctx, input, dest)
			case "POST":
				result, err = h.handleCreate(ctx, input, dest)
			case "PUT", "PATCH":
				result, err = h.handleUpdate(ctx, input, dest)
			case "DELETE":
				result, err = h.handleDelete(ctx, input, dest)
			default:
				return nil, fmt.Errorf("unsupported operation: %s", operation.Method)
			}
		}
	}

	if err != nil {
		return nil, err
	}

	// Apply response transform if configured
	if len(h.Config.Response) > 0 {
		result, err = h.applyResponseTransform(ctx, input, result)
		if err != nil {
			return nil, fmt.Errorf("response transform error: %w", err)
		}
	}

	// For read operations, store result in cache
	if operation.Method == "GET" && h.hasCacheConfig() {
		cacheKey := h.buildCacheKey(input)
		if cacheKey != "" {
			_ = h.storeInCache(ctx, cacheKey, result)
		}
	}

	// For write operations, execute invalidation if configured
	if operation.Method != "GET" && h.Config.After != nil && h.Config.After.Invalidate != nil {
		_ = h.executeInvalidation(ctx, input, result)
	}

	return result, nil
}

// executeBatch processes data in chunks from a source connector to a target connector.
func (h *FlowHandler) executeBatch(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	batch := h.Config.Batch
	if batch == nil {
		return nil, fmt.Errorf("no batch configuration")
	}

	// Get source connector (Reader)
	sourceConn, err := h.Connectors.Get(batch.Source)
	if err != nil {
		return nil, fmt.Errorf("batch source connector %q not found: %w", batch.Source, err)
	}
	reader, ok := sourceConn.(connector.Reader)
	if !ok {
		return nil, fmt.Errorf("batch source connector %q does not support reading", batch.Source)
	}

	// Get target connector (Writer)
	targetConn, err := h.Connectors.Get(batch.To.Connector)
	if err != nil {
		return nil, fmt.Errorf("batch target connector %q not found: %w", batch.To.Connector, err)
	}
	writer, ok := targetConn.(connector.Writer)
	if !ok {
		return nil, fmt.Errorf("batch target connector %q does not support writing", batch.To.Connector)
	}

	chunkSize := batch.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 100
	}

	// Initialize transformer if needed
	if h.Transformer == nil {
		celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		t, err := transform.NewCELTransformerWithOptions(celOptions...)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
		h.Transformer = t
	}

	// Evaluate params if present
	params := batch.Params
	if len(params) > 0 {
		evaluated := make(map[string]interface{})
		for k, v := range params {
			if expr, ok := v.(string); ok {
				result, err := h.Transformer.EvaluateExpression(ctx, input, nil, expr)
				if err == nil {
					evaluated[k] = result
				} else {
					evaluated[k] = v
				}
			} else {
				evaluated[k] = v
			}
		}
		params = evaluated
	}

	// Build transform rules for per-item transforms if configured
	var itemRules []transform.Rule
	if batch.Transform != nil && batch.Transform.Mappings != nil {
		for target, expr := range batch.Transform.Mappings {
			itemRules = append(itemRules, transform.Rule{
				Target:     target,
				Expression: expr,
			})
		}
	}

	batchResult := &flow.BatchResult{}
	offset := 0

	for {
		// Build query for this chunk
		query := connector.Query{
			RawSQL:  batch.Query,
			Filters: make(map[string]interface{}),
			Pagination: &connector.Pagination{
				Limit:  chunkSize,
				Offset: offset,
			},
		}

		// Copy params as filters (for named params in SQL)
		for k, v := range params {
			query.Filters[k] = v
		}

		// Read a chunk
		readResult, err := reader.Read(ctx, query)
		if err != nil {
			if batch.OnError == "continue" {
				batchResult.Errors = append(batchResult.Errors, fmt.Sprintf("chunk at offset %d read error: %v", offset, err))
				batchResult.Chunks++
				break // Cannot continue reading after a read error
			}
			return nil, fmt.Errorf("batch read at offset %d failed: %w", offset, err)
		}

		// No more data
		if readResult == nil || len(readResult.Rows) == 0 {
			break
		}

		rows := readResult.Rows

		// Apply per-item transform if configured
		if len(itemRules) > 0 {
			transformed := make([]map[string]interface{}, 0, len(rows))
			for _, row := range rows {
				// Make item fields available as input.* (standard Mycel convention)
				// and batch_input.* for the original flow input
				itemInput := make(map[string]interface{})
				for k, v := range row {
					itemInput[k] = v
				}
				itemInput["_batch_input"] = input
				out, err := h.Transformer.Transform(ctx, itemInput, itemRules)
				if err != nil {
					if batch.OnError == "continue" {
						batchResult.Failed++
						continue
					}
					return nil, fmt.Errorf("batch transform error: %w", err)
				}
				transformed = append(transformed, out)
			}
			rows = transformed
		}

		// Write chunk to target
		for _, row := range rows {
			writeData := &connector.Data{
				Target:    batch.To.Target,
				Operation: batch.To.Operation,
				Payload:   row,
			}

			_, err := writer.Write(ctx, writeData)
			if err != nil {
				if batch.OnError == "continue" {
					batchResult.Failed++
					batchResult.Errors = append(batchResult.Errors, fmt.Sprintf("write error: %v", err))
					continue
				}
				return nil, fmt.Errorf("batch write failed: %w", err)
			}
			batchResult.Processed++
		}

		batchResult.Chunks++

		// If we got fewer rows than chunk_size, we're done
		if len(readResult.Rows) < chunkSize {
			break
		}

		offset += chunkSize
	}

	return batchResult, nil
}

// handleRead handles GET requests.
func (h *FlowHandler) handleRead(ctx context.Context, input map[string]interface{}, dest connector.Reader) (interface{}, error) {
	query := connector.Query{
		Target:    h.Config.To.Target,
		Operation: "SELECT",
		Filters:   make(map[string]interface{}),
	}

	// Override operation if specified in to block config
	if h.Config.To.Operation != "" {
		query.Operation = h.Config.To.Operation
	}

	// GraphQL Query Optimization: Extract requested fields from input
	// These fields are injected by the GraphQL resolver when field analysis is enabled
	if topFields := optimizer.TopFieldsFromInput(input); len(topFields) > 0 {
		// Convert GraphQL camelCase field names to snake_case column names
		columns := make([]string, len(topFields))
		for i, f := range topFields {
			columns[i] = optimizer.CamelToSnake(f)
		}
		query.Fields = columns
	}

	// Use raw SQL query if configured
	if h.Config.To.Query != "" {
		query.RawSQL = h.Config.To.Query
		// Pass all input as filters/params for named parameter substitution
		for key, val := range input {
			// Skip internal GraphQL optimization fields
			if isInternalField(key) {
				continue
			}
			query.Filters[key] = val
		}

		// GraphQL Query Optimization: Rewrite SELECT * to SELECT specific columns
		if allFields := optimizer.FieldsFromInput(input); len(allFields) > 0 {
			optimizedSQL, _ := optimizer.OptimizeQueryWithFields(query.RawSQL, allFields)
			query.RawSQL = optimizedSQL
		}
	} else if isGraphQLOperation(h.Config.From.Operation) {
		// For GraphQL, use all input arguments as filters
		// This supports queries like Query.user(id: 1) -> filters by id
		for key, val := range input {
			// Skip special keys that aren't filters
			if key == "parent_id" || hasPrefix(key, "parent_") || isInternalField(key) {
				continue
			}
			query.Filters[key] = val
		}
	} else if h.SourceType == "soap" || h.SourceType == "tcp" || h.SourceType == "grpc" {
		// For SOAP/TCP, the operation name is not a REST path — use all input as filters.
		// Example: "GetItem" with input {id: 1} → SELECT * FROM items WHERE id = 1
		for key, val := range input {
			if isInternalField(key) {
				continue
			}
			query.Filters[key] = val
		}
	} else {
		// For REST, extract path parameters from operation and use as filters
		// For operations like "GET /users/:id", extract :id as a filter
		operation := parseOperation(h.Config.From.Operation)
		pathParams := extractPathParams(operation.Path)

		for _, param := range pathParams {
			if val, ok := input[param]; ok {
				query.Filters[param] = val
			}
		}

		// Also apply explicit filter if present
		if h.Config.To.Filter != "" {
			// Parse filter expression and add to query
			// For now, we'll handle simple ID-based filters
			if id, ok := input["id"]; ok {
				query.Filters["id"] = id
			}
		}
	}

	readResult, readErr := trace.RecordStage(ctx, trace.StageRead, h.Config.To.Target, query.Filters, func() (interface{}, error) {
		result, err := dest.Read(ctx, query)
		if err != nil {
			return nil, err
		}
		return result.Rows, nil
	})
	if readErr != nil {
		return nil, readErr
	}

	return readResult, nil
}

// handleStepsFlow handles flows with steps where data comes from step execution + transform.
// This is used for orchestration flows where data is aggregated from multiple sources.
func (h *FlowHandler) handleStepsFlow(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	// Execute all steps and collect results
	stepResults, err := h.executeSteps(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("step execution failed: %w", err)
	}

	// If no transform is configured, return step results directly
	if h.Config.Transform == nil || len(h.Config.Transform.Mappings) == 0 {
		// Return the first step's result if available
		if len(stepResults) > 0 {
			for _, result := range stepResults {
				return result, nil
			}
		}
		return nil, nil
	}

	// Build transform rules from mappings
	var rules []transform.Rule
	for target, expr := range h.Config.Transform.Mappings {
		rules = append(rules, transform.Rule{
			Target:     target,
			Expression: expr,
		})
	}

	// Create CEL transformer if not already available
	celTransformer := h.Transformer
	if celTransformer == nil {
		celTransformer, err = transform.NewCELTransformer()
		if err != nil {
			return nil, fmt.Errorf("failed to create transformer: %w", err)
		}
	}

	// Apply transform with step results (no enriched data for steps-only flows)
	transformResult, err := celTransformer.TransformWithSteps(ctx, input, nil, stepResults, rules)
	if err != nil {
		return nil, fmt.Errorf("transform failed: %w", err)
	}

	return transformResult, nil
}

// isGraphQLOperation checks if an operation string is a GraphQL operation.
func isGraphQLOperation(op string) bool {
	return hasPrefix(op, "Query.") || hasPrefix(op, "Mutation.") || hasPrefix(op, "Subscription.")
}

// isSubscriptionPublish checks if a to.operation targets a GraphQL subscription.
func isSubscriptionPublish(op string) bool {
	return hasPrefix(op, "Subscription.")
}

// handleSubscriptionPublish applies transforms and publishes data to a subscription topic.
func (h *FlowHandler) handleSubscriptionPublish(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	// Apply transforms (handles steps and enrichments internally)
	payload, err := h.applyTransforms(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("transform failed: %w", err)
	}

	// Extract the subscription topic from the operation (e.g., "Subscription.orderUpdated" -> "orderUpdated")
	topic := strings.TrimPrefix(h.Config.To.Operation, "Subscription.")

	// Publish to the subscription topic via the destination connector
	type publisher interface {
		Publish(topic string, data interface{})
	}
	if pub, ok := h.Dest.(publisher); ok {
		pub.Publish(topic, payload)
		return payload, nil
	}

	return nil, fmt.Errorf("destination connector does not support subscription publishing")
}

// isInternalField checks if a key is an internal field used for query optimization.
func isInternalField(key string) bool {
	return key == "__requested_fields" || key == "__requested_top_fields"
}

// handleCreate handles POST requests.
func (h *FlowHandler) handleCreate(ctx context.Context, input map[string]interface{}, dest connector.Writer) (interface{}, error) {
	// Remove internal GraphQL optimization fields from input
	delete(input, "__requested_fields")
	delete(input, "__requested_top_fields")

	// Apply transforms if configured
	payload, err := h.applyTransforms(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("transform error: %w", err)
	}

	data := &connector.Data{
		Target:    h.Config.To.Target,
		Operation: "INSERT",
		Payload:   payload,
	}

	// Override operation if specified in to block config
	if h.Config.To.Operation != "" {
		data.Operation = h.Config.To.Operation
	}

	// Use raw SQL query if configured
	if h.Config.To.Query != "" {
		data.RawSQL = h.Config.To.Query
	}

	// Dry-run: record what would be written without executing
	if tc := trace.FromContext(ctx); tc != nil && tc.DryRun {
		tc.Record(trace.Event{
			Stage:  trace.StageWrite,
			Name:   data.Target,
			Input:  trace.Snapshot(data.Payload),
			DryRun: true,
			Detail: fmt.Sprintf("%s → %s", data.Operation, data.Target),
		})
		return map[string]interface{}{
			"dry_run":   true,
			"operation": data.Operation,
			"target":    data.Target,
			"payload":   payload,
		}, nil
	}

	result, err := dest.Write(ctx, data)
	if err != nil {
		trace.RecordSimple(ctx, trace.StageWrite, data.Target, nil, fmt.Sprintf("error: %s", err.Error()))
		return nil, err
	}
	trace.RecordSimple(ctx, trace.StageWrite, data.Target, map[string]interface{}{"affected": result.Affected, "last_id": result.LastID}, fmt.Sprintf("%s → %s", data.Operation, data.Target))

	// If raw SQL returned rows (e.g., INSERT ... RETURNING), return those
	if len(result.Rows) > 0 {
		if len(result.Rows) == 1 {
			return result.Rows[0], nil
		}
		return result.Rows, nil
	}

	// For GraphQL and gRPC operations, return the created object instead of {id, affected}
	// This allows mutations like `createUser(input: {...}) { id email name }` to work
	if (isGraphQLOperation(h.Config.From.Operation) || h.SourceType == "grpc") && result.LastID != 0 {
		// Try to read back the created record
		if reader, ok := dest.(connector.Reader); ok {
			query := connector.Query{
				Target:    h.Config.To.Target,
				Operation: "SELECT",
				Filters:   map[string]interface{}{"id": result.LastID},
			}
			readResult, err := reader.Read(ctx, query)
			if err == nil && len(readResult.Rows) > 0 {
				return readResult.Rows[0], nil
			}
		}
	}

	// If the connector returned metadata (e.g., FTP/SFTP path, MQTT topic), return it
	if len(result.Metadata) > 0 {
		return result.Metadata, nil
	}

	// Default: return insert metadata
	return map[string]interface{}{
		"id":       result.LastID,
		"affected": result.Affected,
	}, nil
}

// handleUpdate handles PUT/PATCH requests.
func (h *FlowHandler) handleUpdate(ctx context.Context, input map[string]interface{}, dest connector.Writer) (interface{}, error) {
	// Extract ID before transform
	var id interface{}
	if v, ok := input["id"]; ok {
		id = v
		delete(input, "id")
	}

	// Remove internal GraphQL optimization fields from input
	delete(input, "__requested_fields")
	delete(input, "__requested_top_fields")

	// Apply transforms if configured
	payload, err := h.applyTransforms(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("transform error: %w", err)
	}

	data := &connector.Data{
		Target:    h.Config.To.Target,
		Operation: "UPDATE",
		Payload:   payload,
		Filters:   make(map[string]interface{}),
	}

	// Set ID filter
	if id != nil {
		data.Filters["id"] = id
	}

	// Use raw SQL query if configured
	if h.Config.To.Query != "" {
		data.RawSQL = h.Config.To.Query
	}

	// Dry-run: record what would be written without executing
	if tc := trace.FromContext(ctx); tc != nil && tc.DryRun {
		tc.Record(trace.Event{
			Stage:  trace.StageWrite,
			Name:   data.Target,
			Input:  trace.Snapshot(data.Payload),
			DryRun: true,
			Detail: fmt.Sprintf("%s → %s (filters: %v)", data.Operation, data.Target, data.Filters),
		})
		return map[string]interface{}{
			"dry_run":   true,
			"operation": data.Operation,
			"target":    data.Target,
			"payload":   payload,
			"filters":   data.Filters,
		}, nil
	}

	result, err := dest.Write(ctx, data)
	if err != nil {
		return nil, err
	}

	// If raw SQL returned rows (e.g., UPDATE ... RETURNING), return those
	if len(result.Rows) > 0 {
		if len(result.Rows) == 1 {
			return result.Rows[0], nil
		}
		return result.Rows, nil
	}

	return map[string]interface{}{
		"affected": result.Affected,
	}, nil
}

// handleDelete handles DELETE requests.
func (h *FlowHandler) handleDelete(ctx context.Context, input map[string]interface{}, dest connector.Writer) (interface{}, error) {
	data := &connector.Data{
		Target:    h.Config.To.Target,
		Operation: "DELETE",
		Filters:   make(map[string]interface{}),
	}

	// Get ID from input for filter
	if id, ok := input["id"]; ok {
		data.Filters["id"] = id
	}

	// Use raw SQL query if configured
	if h.Config.To.Query != "" {
		data.RawSQL = h.Config.To.Query
		// Pass all input as params for named parameter substitution
		for key, val := range input {
			data.Filters[key] = val
		}
	}

	// Dry-run: record what would be deleted without executing
	if tc := trace.FromContext(ctx); tc != nil && tc.DryRun {
		tc.Record(trace.Event{
			Stage:  trace.StageWrite,
			Name:   data.Target,
			Input:  trace.Snapshot(data.Filters),
			DryRun: true,
			Detail: fmt.Sprintf("%s → %s (filters: %v)", data.Operation, data.Target, data.Filters),
		})
		return map[string]interface{}{
			"dry_run":   true,
			"operation": data.Operation,
			"target":    data.Target,
			"filters":   data.Filters,
		}, nil
	}

	result, err := dest.Write(ctx, data)
	if err != nil {
		return nil, err
	}

	// If raw SQL returned rows, return those
	if len(result.Rows) > 0 {
		if len(result.Rows) == 1 {
			return result.Rows[0], nil
		}
		return result.Rows, nil
	}

	return map[string]interface{}{
		"affected": result.Affected,
	}, nil
}

// handleSimpleRequest handles requests when dest only implements Reader or Writer.
func (h *FlowHandler) handleSimpleRequest(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	operation := parseOperation(h.Config.From.Operation)

	if operation.Method == "GET" {
		if reader, ok := h.Dest.(connector.Reader); ok {
			return h.handleRead(ctx, input, reader)
		}
	} else {
		if writer, ok := h.Dest.(connector.Writer); ok {
			switch operation.Method {
			case "POST":
				return h.handleCreate(ctx, input, writer)
			case "PUT", "PATCH":
				return h.handleUpdate(ctx, input, writer)
			case "DELETE":
				return h.handleDelete(ctx, input, writer)
			}
		}
	}

	return nil, fmt.Errorf("destination connector does not support required operation")
}

// MultiDestResult contains results from writing to multiple destinations.
type MultiDestResult struct {
	// Results contains the result from each destination, keyed by connector name.
	Results map[string]interface{} `json:"results"`
	// Errors contains any errors, keyed by connector name.
	Errors map[string]string `json:"errors,omitempty"`
	// Success indicates if all writes succeeded.
	Success bool `json:"success"`
}

// handleMultiDestWrite handles writing to multiple destinations (fan-out pattern).
func (h *FlowHandler) handleMultiDestWrite(ctx context.Context, input map[string]interface{}, operation Operation) (interface{}, error) {
	if len(h.Config.MultiTo) == 0 {
		return nil, fmt.Errorf("no destinations configured")
	}

	// Apply the main transform first to get the base payload
	basePayload, err := h.applyTransforms(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("transform error: %w", err)
	}

	// Determine which destinations should be written in parallel
	var parallelDests []*flow.ToConfig
	var sequentialDests []*flow.ToConfig

	for _, dest := range h.Config.MultiTo {
		if dest.Parallel {
			parallelDests = append(parallelDests, dest)
		} else {
			sequentialDests = append(sequentialDests, dest)
		}
	}

	result := &MultiDestResult{
		Results: make(map[string]interface{}),
		Errors:  make(map[string]string),
		Success: true,
	}

	// Execute parallel destinations concurrently
	if len(parallelDests) > 0 {
		var wg sync.WaitGroup
		var mu sync.Mutex

		for _, destConfig := range parallelDests {
			wg.Add(1)
			go func(dc *flow.ToConfig) {
				defer wg.Done()

				destResult, destErr := h.writeToDestination(ctx, input, basePayload, dc, operation)

				mu.Lock()
				defer mu.Unlock()
				if destErr != nil {
					result.Errors[dc.Connector] = destErr.Error()
					result.Success = false
				} else {
					result.Results[dc.Connector] = destResult
				}
			}(destConfig)
		}
		wg.Wait()
	}

	// Execute sequential destinations one by one
	for _, destConfig := range sequentialDests {
		destResult, destErr := h.writeToDestination(ctx, input, basePayload, destConfig, operation)
		if destErr != nil {
			result.Errors[destConfig.Connector] = destErr.Error()
			result.Success = false
		} else {
			result.Results[destConfig.Connector] = destResult
		}
	}

	// If all writes failed, return error
	if len(result.Results) == 0 && len(result.Errors) > 0 {
		return nil, fmt.Errorf("all destination writes failed: %v", result.Errors)
	}

	return result, nil
}

// writeToDestination writes data to a single destination.
func (h *FlowHandler) writeToDestination(ctx context.Context, input, basePayload map[string]interface{}, destConfig *flow.ToConfig, operation Operation) (interface{}, error) {
	// Check when condition if specified
	if destConfig.When != "" {
		// Build context with output (the transformed data)
		evalInput := make(map[string]interface{})
		for k, v := range input {
			evalInput[k] = v
		}
		evalInput["output"] = basePayload

		shouldWrite, err := h.Transformer.EvaluateCondition(ctx, evalInput, destConfig.When)
		if err != nil {
			return nil, fmt.Errorf("when condition error: %w", err)
		}
		if !shouldWrite {
			return map[string]interface{}{"skipped": true, "reason": "condition not met"}, nil
		}
	}

	// Get the destination connector
	destConn, err := h.Connectors.Get(destConfig.Connector)
	if err != nil {
		return nil, fmt.Errorf("connector not found: %s: %w", destConfig.Connector, err)
	}

	writer, ok := destConn.(connector.Writer)
	if !ok {
		return nil, fmt.Errorf("connector %s does not support write operations", destConfig.Connector)
	}

	// Determine payload: use per-destination transform or base payload
	var payload map[string]interface{}
	if len(destConfig.Transform) > 0 {
		// Apply per-destination transform
		// Build input context with access to input and output (base payload)
		transformInput := make(map[string]interface{})
		for k, v := range input {
			transformInput[k] = v
		}
		transformInput["output"] = basePayload

		// Convert map[string]string to []transform.Rule
		var rules []transform.Rule
		for target, expr := range destConfig.Transform {
			rules = append(rules, transform.Rule{
				Target:     target,
				Expression: expr,
			})
		}

		transformedPayload, err := h.Transformer.TransformWithSteps(ctx, transformInput, nil, nil, rules)
		if err != nil {
			return nil, fmt.Errorf("per-destination transform error: %w", err)
		}
		payload = transformedPayload
	} else {
		payload = basePayload
	}

	// Build data for write
	data := &connector.Data{
		Target:  destConfig.Target,
		Payload: payload,
	}

	// Set operation type
	switch operation.Method {
	case "POST":
		data.Operation = "INSERT"
	case "PUT", "PATCH":
		data.Operation = "UPDATE"
	case "DELETE":
		data.Operation = "DELETE"
	default:
		data.Operation = "INSERT"
	}

	// Set operation override if specified in config
	if destConfig.Operation != "" {
		data.Operation = destConfig.Operation
	}

	// Set raw SQL if configured
	if destConfig.Query != "" {
		data.RawSQL = destConfig.Query
		// Pass all input as params for named parameter substitution
		data.Filters = make(map[string]interface{})
		for key, val := range input {
			data.Filters[key] = val
		}
	}

	// Set query filter for NoSQL (MongoDB)
	if len(destConfig.QueryFilter) > 0 {
		data.Filters = destConfig.QueryFilter
	}

	// Set update document for NoSQL
	if len(destConfig.Update) > 0 {
		data.Update = destConfig.Update
	}

	// Dry-run: record what would be written without executing
	if tc := trace.FromContext(ctx); tc != nil && tc.DryRun {
		tc.Record(trace.Event{
			Stage:  trace.StageWrite,
			Name:   destConfig.Connector + ":" + data.Target,
			Input:  trace.Snapshot(data.Payload),
			DryRun: true,
			Detail: fmt.Sprintf("%s → %s.%s", data.Operation, destConfig.Connector, data.Target),
		})
		return map[string]interface{}{
			"dry_run":    true,
			"connector":  destConfig.Connector,
			"operation":  data.Operation,
			"target":     data.Target,
			"payload":    payload,
		}, nil
	}

	writeResult, err := writer.Write(ctx, data)
	if err != nil {
		return nil, err
	}

	// Return appropriate result
	if len(writeResult.Rows) > 0 {
		if len(writeResult.Rows) == 1 {
			return writeResult.Rows[0], nil
		}
		return writeResult.Rows, nil
	}

	return map[string]interface{}{
		"id":       writeResult.LastID,
		"affected": writeResult.Affected,
	}, nil
}

// Operation represents a parsed HTTP operation from flow config.
type Operation struct {
	Method string
	Path   string
}

// parseOperation parses an operation string like "GET /users/:id" or "Query.users".
// isEventDrivenSource returns true for connector types that receive events
// and write to the destination (message queues, CDC, file watchers).
func isEventDrivenSource(sourceType string) bool {
	switch sourceType {
	case "mq", "mqtt", "cdc", "file":
		return true
	}
	return false
}

func parseOperation(op string) Operation {
	// Check for GraphQL operation format: "Query.fieldName" or "Mutation.fieldName"
	if len(op) > 6 && (op[:6] == "Query." || (len(op) > 9 && op[:9] == "Mutation.")) {
		return parseGraphQLOperation(op)
	}

	// Split by first space for REST operations
	for i, c := range op {
		if c == ' ' {
			return Operation{
				Method: op[:i],
				Path:   op[i+1:],
			}
		}
	}
	// No space found, assume it's just the path
	return Operation{
		Method: "GET",
		Path:   op,
	}
}

// parseGraphQLOperation parses GraphQL operations like "Query.users" or "Mutation.createUser".
func parseGraphQLOperation(op string) Operation {
	// Query operations are read operations
	if len(op) > 6 && op[:6] == "Query." {
		return Operation{
			Method: "GET",
			Path:   op,
		}
	}

	// Mutation operations - determine method based on field name
	if len(op) > 9 && op[:9] == "Mutation." {
		fieldName := op[9:]
		lowerField := toLower(fieldName)

		// Create operations
		if hasPrefix(lowerField, "create") || hasPrefix(lowerField, "add") ||
			hasPrefix(lowerField, "insert") || hasPrefix(lowerField, "new") {
			return Operation{
				Method: "POST",
				Path:   op,
			}
		}

		// Update operations
		if hasPrefix(lowerField, "update") || hasPrefix(lowerField, "edit") ||
			hasPrefix(lowerField, "modify") || hasPrefix(lowerField, "set") {
			return Operation{
				Method: "PUT",
				Path:   op,
			}
		}

		// Delete operations
		if hasPrefix(lowerField, "delete") || hasPrefix(lowerField, "remove") ||
			hasPrefix(lowerField, "destroy") {
			return Operation{
				Method: "DELETE",
				Path:   op,
			}
		}

		// Default mutations to POST
		return Operation{
			Method: "POST",
			Path:   op,
		}
	}

	return Operation{
		Method: "GET",
		Path:   op,
	}
}

// toLower converts a string to lowercase without importing strings package.
func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// hasPrefix checks if s starts with prefix (case-insensitive already handled).
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// extractPathParams extracts parameter names from a path like "/users/:id".
// Returns a slice of parameter names without the colon prefix.
func extractPathParams(path string) []string {
	var params []string
	parts := splitPath(path)

	for _, part := range parts {
		if len(part) > 0 && part[0] == ':' {
			params = append(params, part[1:])
		}
	}

	return params
}

// splitPath splits a path into segments.
func splitPath(path string) []string {
	var parts []string
	start := 0

	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}

	if start < len(path) {
		parts = append(parts, path[start:])
	}

	return parts
}

// executeSteps executes intermediate connector calls and returns their results.
// Results are available as step.<name>.* in transform expressions.
func (h *FlowHandler) executeSteps(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	if len(h.Config.Steps) == 0 {
		return make(map[string]interface{}), nil
	}

	// Initialize CEL transformer if needed (for evaluating step params and conditions)
	if h.Transformer == nil {
		var err error
		celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		h.Transformer, err = transform.NewCELTransformerWithOptions(celOptions...)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
	}

	stepResults := make(map[string]interface{})

	// Analyze step dependencies for optimization (skip unused steps)
	neededSteps := h.analyzeNeededSteps(input)

	for _, step := range h.Config.Steps {
		// Step optimization: skip steps whose output isn't requested
		if neededSteps != nil && !neededSteps[step.Name] {
			// Step not needed based on requested fields - skip it
			// Always set a value (nil if no default) so CEL expressions can check "step.X != null"
			if step.Default != nil {
				stepResults[step.Name] = step.Default
			} else {
				stepResults[step.Name] = nil
			}
			continue
		}
		// Evaluate the "when" condition if present
		if step.When != "" && h.Transformer != nil {
			// Build context with input and previous step results
			evalCtx := map[string]interface{}{
				"input": input,
				"step":  stepResults,
			}
			shouldExecute, err := h.Transformer.EvaluateCondition(ctx, evalCtx, step.When)
			if err != nil {
				// If condition evaluation fails, skip the step (or fail based on on_error)
				if step.OnError == "skip" {
					stepResults[step.Name] = nil
					continue
				}
				return nil, fmt.Errorf("step %s: failed to evaluate condition: %w", step.Name, err)
			}
			if !shouldExecute {
				// Condition is false, skip this step
				// Always set a value (nil if no default) so CEL expressions can check "step.X != null"
				if step.Default != nil {
					stepResults[step.Name] = step.Default
				} else {
					stepResults[step.Name] = nil
				}
				continue
			}
		}

		// Get the connector for this step
		conn, err := h.Connectors.Get(step.Connector)
		if err != nil {
			if step.OnError == "skip" {
				if step.Default != nil {
					stepResults[step.Name] = step.Default
				} else {
					stepResults[step.Name] = nil
				}
				continue
			}
			if step.OnError == "default" && step.Default != nil {
				stepResults[step.Name] = step.Default
				continue
			}
			return nil, fmt.Errorf("step %s: connector not found: %w", step.Name, err)
		}

		// Build params by evaluating CEL expressions if needed
		params := make(map[string]interface{})
		if h.Transformer != nil && len(step.Params) > 0 {
			for key, val := range step.Params {
				// If value is a string that looks like an expression, evaluate it
				if strVal, ok := val.(string); ok {
					if strings.Contains(strVal, "input.") || strings.Contains(strVal, "step.") {
						result, err := h.Transformer.EvaluateExpressionWithSteps(ctx, input, stepResults, strVal)
						if err != nil {
							return nil, fmt.Errorf("step %s: failed to evaluate param %s: %w", step.Name, key, err)
						}
						params[key] = result
						continue
					}
				}
				params[key] = val
			}
		} else {
			for key, val := range step.Params {
				params[key] = val
			}
		}

		// Execute the step based on connector type and operation
		var result interface{}

		// Database query
		if step.Query != "" {
			if reader, ok := conn.(connector.Reader); ok {
				query := connector.Query{
					Target:    step.Target,
					Operation: "SELECT",
					RawSQL:    step.Query,
					Filters:   params,
				}
				readResult, err := reader.Read(ctx, query)
				if err != nil {
					if step.OnError == "skip" {
						if step.Default != nil {
							stepResults[step.Name] = step.Default
						} else {
							stepResults[step.Name] = nil
						}
						continue
					}
					if step.OnError == "default" && step.Default != nil {
						stepResults[step.Name] = step.Default
						continue
					}
					return nil, fmt.Errorf("step %s: query failed: %w", step.Name, err)
				}
				// Return single row if only one result
				if len(readResult.Rows) == 1 {
					result = readResult.Rows[0]
				} else {
					result = readResult.Rows
				}
			}
		} else if step.Operation != "" {
			// HTTP/REST or other operation-based connector
			if caller, ok := conn.(Caller); ok {
				// For Caller interface (TCP, HTTP client, gRPC)
				callParams := params
				if len(step.Body) > 0 {
					callParams = step.Body
				}
				callResult, err := caller.Call(ctx, step.Operation, callParams)
				if err != nil {
					if step.OnError == "skip" {
						if step.Default != nil {
							stepResults[step.Name] = step.Default
						} else {
							stepResults[step.Name] = nil
						}
						continue
					}
					if step.OnError == "default" && step.Default != nil {
						stepResults[step.Name] = step.Default
						continue
					}
					return nil, fmt.Errorf("step %s: call failed: %w", step.Name, err)
				}
				result = callResult
			} else if reader, ok := conn.(connector.Reader); ok {
				// For Reader interface (database SELECT)
				query := connector.Query{
					Target:    step.Target,
					Operation: step.Operation,
					Filters:   params,
				}
				readResult, err := reader.Read(ctx, query)
				if err != nil {
					if step.OnError == "skip" {
						if step.Default != nil {
							stepResults[step.Name] = step.Default
						} else {
							stepResults[step.Name] = nil
						}
						continue
					}
					if step.OnError == "default" && step.Default != nil {
						stepResults[step.Name] = step.Default
						continue
					}
					return nil, fmt.Errorf("step %s: read failed: %w", step.Name, err)
				}
				if len(readResult.Rows) == 1 {
					result = readResult.Rows[0]
				} else {
					result = readResult.Rows
				}
			} else if writer, ok := conn.(connector.Writer); ok {
				// For Writer interface (INSERT, UPDATE, DELETE)
				data := &connector.Data{
					Target:    step.Target,
					Operation: step.Operation,
					Payload:   step.Body,
					Filters:   params,
				}
				writeResult, err := writer.Write(ctx, data)
				if err != nil {
					if step.OnError == "skip" {
						if step.Default != nil {
							stepResults[step.Name] = step.Default
						} else {
							stepResults[step.Name] = nil
						}
						continue
					}
					if step.OnError == "default" && step.Default != nil {
						stepResults[step.Name] = step.Default
						continue
					}
					return nil, fmt.Errorf("step %s: write failed: %w", step.Name, err)
				}
				if len(writeResult.Rows) > 0 {
					result = writeResult.Rows
				} else {
					result = map[string]interface{}{
						"affected": writeResult.Affected,
						"id":       writeResult.LastID,
					}
				}
			}
		} else if step.Target != "" {
			// Simple target-based read
			if reader, ok := conn.(connector.Reader); ok {
				query := connector.Query{
					Target:    step.Target,
					Operation: "SELECT",
					Filters:   params,
				}
				readResult, err := reader.Read(ctx, query)
				if err != nil {
					if step.OnError == "skip" {
						if step.Default != nil {
							stepResults[step.Name] = step.Default
						} else {
							stepResults[step.Name] = nil
						}
						continue
					}
					if step.OnError == "default" && step.Default != nil {
						stepResults[step.Name] = step.Default
						continue
					}
					return nil, fmt.Errorf("step %s: read failed: %w", step.Name, err)
				}
				if len(readResult.Rows) == 1 {
					result = readResult.Rows[0]
				} else {
					result = readResult.Rows
				}
			}
		}

		stepResults[step.Name] = result
	}

	return stepResults, nil
}

// analyzeNeededSteps determines which steps are needed based on requested fields.
// Returns nil if no optimization is possible (execute all steps).
// Returns a map of step names to whether they should be executed.
func (h *FlowHandler) analyzeNeededSteps(input map[string]interface{}) map[string]bool {
	// Check if requested fields info is available
	requestedFields, ok := input["__requested_top_fields"].([]string)
	if !ok || len(requestedFields) == 0 {
		// No field info - execute all steps (no optimization)
		return nil
	}

	// Get transform expressions
	var transformExprs map[string]string
	if h.Config.Transform != nil && len(h.Config.Transform.Mappings) > 0 {
		transformExprs = optimizer.ExtractTransformExpressions(h.Config.Transform.Mappings)
	}

	if len(transformExprs) == 0 {
		// No transform mappings - execute all steps
		return nil
	}

	// Create step optimizer and analyze dependencies
	stepOptimizer := optimizer.NewStepOptimizer(h.Config.Steps, transformExprs, requestedFields)
	return stepOptimizer.AnalyzeDependencies()
}

// executeEnrichments fetches data from external connectors for enrichment.
// Returns a map of enrichment names to their fetched data.
func (h *FlowHandler) executeEnrichments(ctx context.Context, input map[string]interface{}, enrichments []*flow.EnrichConfig) (map[string]interface{}, error) {
	if len(enrichments) == 0 {
		return make(map[string]interface{}), nil
	}

	enriched := make(map[string]interface{})

	for _, enrich := range enrichments {
		// Get the connector for this enrichment
		conn, err := h.Connectors.Get(enrich.Connector)
		if err != nil {
			return nil, fmt.Errorf("enrich %s: connector not found: %w", enrich.Name, err)
		}

		// Build params by evaluating CEL expressions
		params := make(map[string]interface{})
		if h.Transformer != nil && len(enrich.Params) > 0 {
			for key, expr := range enrich.Params {
				// Evaluate the param expression using CEL
				result, err := h.Transformer.EvaluateExpression(ctx, input, nil, expr)
				if err != nil {
					return nil, fmt.Errorf("enrich %s: failed to evaluate param %s: %w", enrich.Name, key, err)
				}
				params[key] = result
			}
		} else {
			// Simple param copy without CEL evaluation
			for key, val := range enrich.Params {
				params[key] = val
			}
		}

		// Execute the enrichment based on connector capabilities
		var result interface{}

		// Try as a Reader first
		if reader, ok := conn.(connector.Reader); ok {
			query := connector.Query{
				Target:    enrich.Operation,
				Operation: "SELECT",
				Filters:   params,
			}
			readResult, err := reader.Read(ctx, query)
			if err != nil {
				return nil, fmt.Errorf("enrich %s: read failed: %w", enrich.Name, err)
			}
			// Return single row if only one result, otherwise return all
			if len(readResult.Rows) == 1 {
				result = readResult.Rows[0]
			} else {
				result = readResult.Rows
			}
		} else if caller, ok := conn.(Caller); ok {
			// Try as a Caller (for TCP, HTTP, etc.)
			callResult, err := caller.Call(ctx, enrich.Operation, params)
			if err != nil {
				return nil, fmt.Errorf("enrich %s: call failed: %w", enrich.Name, err)
			}
			result = callResult
		} else {
			return nil, fmt.Errorf("enrich %s: connector %s does not support read or call operations", enrich.Name, enrich.Connector)
		}

		enriched[enrich.Name] = result
	}

	return enriched, nil
}

// applyTransforms applies configured transformations to the input data.
// applyResponseTransform applies response transformation rules to the result.
// Available variables: input (original request), output (destination result).
func (h *FlowHandler) applyResponseTransform(ctx context.Context, input map[string]interface{}, result interface{}) (interface{}, error) {
	// Initialize CEL transformer if needed
	if h.Transformer == nil {
		var err error
		celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		h.Transformer, err = transform.NewCELTransformerWithOptions(celOptions...)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
	}

	// Build rules from response config
	rules := make([]transform.Rule, 0, len(h.Config.Response))
	for target, expr := range h.Config.Response {
		rules = append(rules, transform.Rule{Target: target, Expression: expr})
	}

	// Build context: input = original request, output = destination result
	output := make(map[string]interface{})
	switch v := result.(type) {
	case map[string]interface{}:
		output = v
	case []interface{}:
		// For array results, make the array available as output.items
		output["items"] = v
	}

	transformed, err := h.Transformer.TransformResponse(ctx, input, output, rules)
	if err != nil {
		return nil, err
	}

	return transformed, nil
}

func (h *FlowHandler) applyTransforms(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	// No transform configured and no steps - return input as-is
	if h.Config.Transform == nil && len(h.Config.Enrichments) == 0 && len(h.Config.Steps) == 0 {
		return input, nil
	}

	// Initialize CEL transformer if needed
	if h.Transformer == nil {
		var err error
		// Create CEL transformer with WASM functions if registry is available
		celOptions := transform.CreateWASMFunctionOptions(h.FunctionsRegistry)
		h.Transformer, err = transform.NewCELTransformerWithOptions(celOptions...)
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
	}

	// Execute steps first (their results are available in transforms)
	stepResults, err := h.executeSteps(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("step execution failed: %w", err)
	}

	// Collect all enrichments (flow-level + transform-level)
	var allEnrichments []*flow.EnrichConfig
	allEnrichments = append(allEnrichments, h.Config.Enrichments...)

	// Add enrichments from named transform if using one
	if h.Config.Transform != nil && h.Config.Transform.Use != "" {
		named, ok := h.NamedTransforms[h.Config.Transform.Use]
		if ok && len(named.Enrichments) > 0 {
			// Convert transform.EnrichConfig to flow.EnrichConfig
			for _, e := range named.Enrichments {
				allEnrichments = append(allEnrichments, &flow.EnrichConfig{
					Name:      e.Name,
					Connector: e.Connector,
					Operation: e.Operation,
					Params:    e.Params,
				})
			}
		}
	}

	// Add inline enrichments from transform block
	if h.Config.Transform != nil {
		allEnrichments = append(allEnrichments, h.Config.Transform.Enrichments...)
	}

	// Execute enrichments
	enriched, err := h.executeEnrichments(ctx, input, allEnrichments)
	if err != nil {
		return nil, fmt.Errorf("enrichment failed: %w", err)
	}

	// No transform configured, just return input (steps and enrichments were for side effects)
	if h.Config.Transform == nil {
		return input, nil
	}

	// Build transform rules from config
	var rules []transform.Rule

	// Check if using a named transform
	if h.Config.Transform.Use != "" {
		named, ok := h.NamedTransforms[h.Config.Transform.Use]
		if !ok {
			return nil, fmt.Errorf("named transform not found: %s", h.Config.Transform.Use)
		}
		for target, expr := range named.Mappings {
			rules = append(rules, transform.Rule{
				Target:     target,
				Expression: expr,
			})
		}
	}

	// Add inline mappings (can extend named transform)
	for target, expr := range h.Config.Transform.Mappings {
		rules = append(rules, transform.Rule{
			Target:     target,
			Expression: expr,
		})
	}

	// No rules to apply
	if len(rules) == 0 {
		return input, nil
	}

	// Apply transforms using CEL with enriched data and step results
	transformResult, transformErr := trace.RecordStage(ctx, trace.StageTransform, "", input, func() (interface{}, error) {
		return h.Transformer.TransformWithSteps(ctx, input, enriched, stepResults, rules)
	})
	if transformErr != nil {
		return nil, transformErr
	}
	if m, ok := transformResult.(map[string]interface{}); ok {
		return m, nil
	}
	return nil, fmt.Errorf("transform returned unexpected type")
}

// resolveValidatorRefs adds custom validator constraints to fields that reference
// a registered validator (regex/CEL/WASM) via the `validator` attribute.
// Safe to call multiple times — skips fields that already have the constraint.
func (h *FlowHandler) resolveValidatorRefs(schema *validate.TypeSchema) {
	if h.ValidatorRegistry == nil {
		return
	}
	for i := range schema.Fields {
		field := &schema.Fields[i]
		if field.ValidatorRef == "" {
			continue
		}
		// Skip if already resolved
		alreadyResolved := false
		for _, c := range field.Constraints {
			if c.Name() == "custom:"+field.ValidatorRef {
				alreadyResolved = true
				break
			}
		}
		if alreadyResolved {
			continue
		}
		v, ok := h.ValidatorRegistry.Get(field.ValidatorRef)
		if !ok {
			continue
		}
		validatorFn := v // capture for closure
		field.Constraints = append(field.Constraints, &validate.CustomValidatorConstraint{
			ValidatorName: field.ValidatorRef,
			ValidateFn:    validatorFn.Validate,
		})
	}
}

// validateInput validates input data against the configured input type schema.
func (h *FlowHandler) validateInput(ctx context.Context, input map[string]interface{}) error {
	// Skip if no validation configured
	if h.Config.Validate == nil || h.Config.Validate.Input == "" {
		return nil
	}

	// Get the type schema
	schema, ok := h.Types[h.Config.Validate.Input]
	if !ok {
		return fmt.Errorf("type schema not found: %s", h.Config.Validate.Input)
	}

	// Initialize validator if needed
	if h.Validator == nil {
		h.Validator = validate.NewTypeValidator(validate.NewConstraintRegistry())
	}

	// Resolve custom validator references on fields
	h.resolveValidatorRefs(schema)

	// Validate
	result := h.Validator.Validate(ctx, input, schema)
	if !result.Valid {
		// Build error message from all validation errors
		if len(result.Errors) > 0 {
			return &ValidationError{Errors: result.Errors}
		}
		return fmt.Errorf("validation failed")
	}

	return nil
}

// validateOutput validates output data against the configured output type schema.
func (h *FlowHandler) validateOutput(ctx context.Context, output map[string]interface{}) error {
	// Skip if no validation configured
	if h.Config.Validate == nil || h.Config.Validate.Output == "" {
		return nil
	}

	// Get the type schema
	schema, ok := h.Types[h.Config.Validate.Output]
	if !ok {
		return fmt.Errorf("output type schema not found: %s", h.Config.Validate.Output)
	}

	// Initialize validator if needed
	if h.Validator == nil {
		h.Validator = validate.NewTypeValidator(validate.NewConstraintRegistry())
	}

	// Resolve custom validator references on fields
	h.resolveValidatorRefs(schema)

	// Validate
	result := h.Validator.Validate(ctx, output, schema)
	if !result.Valid {
		if len(result.Errors) > 0 {
			return &ValidationError{Errors: result.Errors}
		}
		return fmt.Errorf("output validation failed")
	}

	return nil
}

// ValidationError represents validation failures.
type ValidationError struct {
	Errors []validate.Error
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "validation failed"
	}
	return e.Errors[0].Error()
}

// Caller is implemented by connectors that can make RPC-style calls.
// This is used for enrichments with TCP, HTTP client, gRPC, etc.
type Caller interface {
	// Call invokes an operation on the connector with the given parameters.
	Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error)
}

// ===== State Machine Methods =====

// executeStateTransition evaluates CEL expressions for the state_transition block
// and dispatches to the state machine engine.
func (h *FlowHandler) executeStateTransition(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	st := h.Config.StateTransition

	// Initialize transformer if needed
	if h.Transformer == nil {
		var err error
		h.Transformer, err = transform.NewCELTransformer()
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
	}

	// Evaluate ID expression
	entityID, err := h.Transformer.EvaluateExpression(ctx, input, nil, st.ID)
	if err != nil {
		return nil, fmt.Errorf("state_transition id evaluation error: %w", err)
	}
	idStr, ok := entityID.(string)
	if !ok {
		idStr = fmt.Sprintf("%v", entityID)
	}

	// Evaluate event expression
	eventVal, err := h.Transformer.EvaluateExpression(ctx, input, nil, st.Event)
	if err != nil {
		return nil, fmt.Errorf("state_transition event evaluation error: %w", err)
	}
	eventStr, ok := eventVal.(string)
	if !ok {
		return nil, fmt.Errorf("state_transition event must be a string, got %T", eventVal)
	}

	// Evaluate data expression (optional)
	var data map[string]interface{}
	if st.Data != "" {
		dataVal, err := h.Transformer.EvaluateExpression(ctx, input, nil, st.Data)
		if err == nil {
			if m, ok := dataVal.(map[string]interface{}); ok {
				data = m
			}
		}
	}
	// If no data expression or evaluation failed, use input as data
	if data == nil {
		data = input
	}

	return h.StateMachineEngine.Transition(ctx, st.Machine, st.Entity, idStr, eventStr, data)
}

// ===== Cache Helper Methods =====

// hasCacheConfig returns true if the flow has cache configuration.
// toTarget returns the destination target string, or empty string for echo flows.
func (h *FlowHandler) toTarget() string {
	if h.Config.To != nil {
		return h.Config.To.Target
	}
	return ""
}

func (h *FlowHandler) hasCacheConfig() bool {
	if h.Config.Cache == nil {
		return false
	}
	// Must have either storage or use reference
	return h.Config.Cache.Storage != "" || h.Config.Cache.Use != ""
}

// buildCacheKey builds the cache key by interpolating variables from input.
// Supports ${input.params.id}, ${input.query.page}, ${input.data.field}, etc.
func (h *FlowHandler) buildCacheKey(input map[string]interface{}) string {
	if h.Config.Cache == nil {
		return ""
	}

	// Get key template from cache config or named cache
	keyTemplate := h.Config.Cache.Key
	if keyTemplate == "" && h.Config.Cache.Use != "" {
		// If using named cache, build default key from flow name
		if named, ok := h.NamedCaches[h.Config.Cache.Use]; ok {
			if named.Prefix != "" {
				keyTemplate = named.Prefix + ":" + h.Config.Name
			} else {
				keyTemplate = h.Config.Name
			}
		}
	}

	if keyTemplate == "" {
		// Default key format: flow_name:param1=val1:param2=val2
		keyTemplate = h.Config.Name
		for k, v := range input {
			keyTemplate += fmt.Sprintf(":%s=%v", k, v)
		}
		return keyTemplate
	}

	// Interpolate variables in key template
	return h.interpolateKey(keyTemplate, input)
}

// interpolateKey replaces ${input.xxx} placeholders with actual values.
func (h *FlowHandler) interpolateKey(template string, input map[string]interface{}) string {
	result := template

	// Find and replace all ${...} patterns
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		placeholder := result[start : end+1]
		path := result[start+2 : end] // Remove ${ and }

		value := h.resolveInputPath(path, input)
		result = strings.Replace(result, placeholder, fmt.Sprintf("%v", value), 1)
	}

	return result
}

// resolveInputPath resolves a path like "input.params.id" from the input map.
func (h *FlowHandler) resolveInputPath(path string, input map[string]interface{}) interface{} {
	// Remove "input." prefix if present
	path = strings.TrimPrefix(path, "input.")

	// Handle nested paths like "params.id" or "query.page"
	parts := strings.Split(path, ".")
	var current interface{} = input

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		case map[string]string:
			current = v[part]
		default:
			return ""
		}
		if current == nil {
			return ""
		}
	}

	return current
}

// checkCache attempts to retrieve a value from the cache.
func (h *FlowHandler) checkCache(ctx context.Context, key string) (interface{}, bool, error) {
	cacheConn := h.getCacheConnector()
	if cacheConn == nil {
		return nil, false, nil
	}

	data, found, err := cacheConn.Get(ctx, key)
	if err != nil || !found {
		return nil, false, err
	}

	// Deserialize from JSON
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, err
	}

	return result, true, nil
}

// storeInCache stores a value in the cache with configured TTL.
func (h *FlowHandler) storeInCache(ctx context.Context, key string, value interface{}) error {
	cacheConn := h.getCacheConnector()
	if cacheConn == nil {
		return nil
	}

	// Serialize to JSON
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	// Get TTL
	ttl := h.getCacheTTL()

	return cacheConn.Set(ctx, key, data, ttl)
}

// getCacheConnector returns the cache connector for this flow.
func (h *FlowHandler) getCacheConnector() cache.Cache {
	// If already resolved, return it
	if h.CacheConnector != nil {
		return h.CacheConnector
	}

	if h.Config.Cache == nil {
		return nil
	}

	// Get storage name (from cache.storage or from named cache)
	storageName := h.Config.Cache.Storage
	if storageName == "" && h.Config.Cache.Use != "" {
		if named, ok := h.NamedCaches[h.Config.Cache.Use]; ok {
			storageName = named.Storage
		}
	}

	if storageName == "" || h.Connectors == nil {
		return nil
	}

	// Get connector from registry
	conn, err := h.Connectors.Get(storageName)
	if err != nil {
		return nil
	}

	// Cast to cache interface
	h.CacheConnector = cache.GetCache(conn)
	return h.CacheConnector
}

// getCacheTTL returns the TTL for cache entries.
func (h *FlowHandler) getCacheTTL() time.Duration {
	if h.Config.Cache == nil {
		return 0
	}

	// First check flow-level TTL
	if h.Config.Cache.TTL != "" {
		if ttl, err := time.ParseDuration(h.Config.Cache.TTL); err == nil {
			return ttl
		}
	}

	// Fall back to named cache TTL
	if h.Config.Cache.Use != "" {
		if named, ok := h.NamedCaches[h.Config.Cache.Use]; ok && named.TTL != "" {
			if ttl, err := time.ParseDuration(named.TTL); err == nil {
				return ttl
			}
		}
	}

	return 0 // Will use connector default
}

// executeInvalidation executes cache invalidation after write operations.
func (h *FlowHandler) executeInvalidation(ctx context.Context, input map[string]interface{}, result interface{}) error {
	if h.Config.After == nil || h.Config.After.Invalidate == nil {
		return nil
	}

	inv := h.Config.After.Invalidate

	// Get invalidation cache connector
	var cacheConn cache.Cache
	if inv.Storage != "" {
		conn, err := h.Connectors.Get(inv.Storage)
		if err != nil {
			return err
		}
		cacheConn = cache.GetCache(conn)
	} else {
		cacheConn = h.getCacheConnector()
	}

	if cacheConn == nil {
		return nil
	}

	// Build context for interpolation (merge input and result)
	interpolationCtx := make(map[string]interface{})
	for k, v := range input {
		interpolationCtx[k] = v
	}
	if resultMap, ok := result.(map[string]interface{}); ok {
		interpolationCtx["result"] = resultMap
	}

	// Invalidate specific keys
	if len(inv.Keys) > 0 {
		keys := make([]string, 0, len(inv.Keys))
		for _, keyTemplate := range inv.Keys {
			key := h.interpolateKey(keyTemplate, interpolationCtx)
			keys = append(keys, key)
		}
		if err := cacheConn.Delete(ctx, keys...); err != nil {
			return err
		}
	}

	// Invalidate patterns
	for _, patternTemplate := range inv.Patterns {
		pattern := h.interpolateKey(patternTemplate, interpolationCtx)
		if err := cacheConn.DeletePattern(ctx, pattern); err != nil {
			return err
		}
	}

	return nil
}
