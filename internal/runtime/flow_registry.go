package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/aspect"
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/cache"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/functions"
	msync "github.com/matutetandil/mycel/internal/sync"
	"github.com/matutetandil/mycel/internal/transform"
	"github.com/matutetandil/mycel/internal/validate"
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

	// FlowPath is the path to the flow file (used for aspect matching).
	// Example: "flows/users/create_user.hcl"
	FlowPath string

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
}

// FilteredResult is returned when a request is filtered out by the from.filter expression.
var FilteredResult = &struct{ Filtered bool }{Filtered: true}

// HandleRequest processes an incoming request through the flow.
func (h *FlowHandler) HandleRequest(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	// Check filter condition first (before any processing)
	if h.Config.From != nil && h.Config.From.Filter != "" {
		shouldProcess, err := h.evaluateFilter(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("filter evaluation error: %w", err)
		}
		if !shouldProcess {
			// Request filtered out - return special result
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
	if err := h.validateInput(ctx, input); err != nil {
		return nil, err
	}

	// Core execution function
	executeFn := func() (interface{}, error) {
		// If aspect executor is configured, wrap execution with aspects
		if h.AspectExecutor != nil && h.FlowPath != "" {
			return h.handleRequestWithAspects(ctx, input)
		}
		// Execute without aspects
		return h.executeFlowCore(ctx, input)
	}

	// If error handling is configured, wrap with retry logic
	if h.Config.ErrorHandling != nil {
		return h.executeWithRetry(ctx, input, executeFn)
	}

	// Execute without error handling wrapper
	return executeFn()
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

// evaluateFilter evaluates the from.filter CEL expression.
// Returns true if the request should be processed, false if filtered out.
func (h *FlowHandler) evaluateFilter(ctx context.Context, input map[string]interface{}) (bool, error) {
	if h.Config.From.Filter == "" {
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

	return h.Transformer.EvaluateCondition(ctx, data, h.Config.From.Filter)
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

	// Execute with aspects
	result, err := h.AspectExecutor.Execute(
		ctx,
		h.FlowPath,
		h.Config.Name,
		h.Config.From.Operation,
		h.Config.To.Target,
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

	var result interface{}
	var err error

	// Check for multi-destination writes
	if len(h.Config.MultiTo) > 0 && operation.Method != "GET" {
		result, err = h.handleMultiDestWrite(ctx, input, operation)
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

// handleRead handles GET requests.
func (h *FlowHandler) handleRead(ctx context.Context, input map[string]interface{}, dest connector.Reader) (interface{}, error) {
	query := connector.Query{
		Target:    h.Config.To.Target,
		Operation: "SELECT",
		Filters:   make(map[string]interface{}),
	}

	// Use raw SQL query if configured
	if h.Config.To.Query != "" {
		query.RawSQL = h.Config.To.Query
		// Pass all input as filters/params for named parameter substitution
		for key, val := range input {
			query.Filters[key] = val
		}
	} else if isGraphQLOperation(h.Config.From.Operation) {
		// For GraphQL, use all input arguments as filters
		// This supports queries like Query.user(id: 1) -> filters by id
		for key, val := range input {
			// Skip special keys that aren't filters
			if key == "parent_id" || hasPrefix(key, "parent_") {
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

	result, err := dest.Read(ctx, query)
	if err != nil {
		return nil, err
	}

	return result.Rows, nil
}

// isGraphQLOperation checks if an operation string is a GraphQL operation.
func isGraphQLOperation(op string) bool {
	return hasPrefix(op, "Query.") || hasPrefix(op, "Mutation.") || hasPrefix(op, "Subscription.")
}

// handleCreate handles POST requests.
func (h *FlowHandler) handleCreate(ctx context.Context, input map[string]interface{}, dest connector.Writer) (interface{}, error) {
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

	// Use raw SQL query if configured
	if h.Config.To.Query != "" {
		data.RawSQL = h.Config.To.Query
	}

	result, err := dest.Write(ctx, data)
	if err != nil {
		return nil, err
	}

	// If raw SQL returned rows (e.g., INSERT ... RETURNING), return those
	if len(result.Rows) > 0 {
		if len(result.Rows) == 1 {
			return result.Rows[0], nil
		}
		return result.Rows, nil
	}

	// For GraphQL operations, return the created object instead of {id, affected}
	// This allows mutations like `createUser(input: {...}) { id email name }` to work
	if isGraphQLOperation(h.Config.From.Operation) && result.LastID != 0 {
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

	stepResults := make(map[string]interface{})

	for _, step := range h.Config.Steps {
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
					continue
				}
				return nil, fmt.Errorf("step %s: failed to evaluate condition: %w", step.Name, err)
			}
			if !shouldExecute {
				// Condition is false, skip this step
				if step.Default != nil {
					stepResults[step.Name] = step.Default
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
			evalCtx := map[string]interface{}{
				"input": input,
				"step":  stepResults,
			}
			for key, val := range step.Params {
				// If value is a string that looks like an expression, evaluate it
				if strVal, ok := val.(string); ok {
					if strings.Contains(strVal, "input.") || strings.Contains(strVal, "step.") {
						result, err := h.Transformer.EvaluateExpression(ctx, evalCtx, nil, strVal)
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
	return h.Transformer.TransformWithSteps(ctx, input, enriched, stepResults, rules)
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

// ===== Cache Helper Methods =====

// hasCacheConfig returns true if the flow has cache configuration.
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
