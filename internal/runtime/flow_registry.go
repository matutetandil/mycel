package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/mycel-labs/mycel/internal/connector"
	"github.com/mycel-labs/mycel/internal/flow"
	"github.com/mycel-labs/mycel/internal/transform"
	"github.com/mycel-labs/mycel/internal/validate"
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
}

// HandleRequest processes an incoming request through the flow.
func (h *FlowHandler) HandleRequest(ctx context.Context, input map[string]interface{}) (interface{}, error) {
	// Validate input if schema is configured
	if err := h.validateInput(ctx, input); err != nil {
		return nil, err
	}

	// Get the destination as a reader/writer
	dest, ok := h.Dest.(connector.ReadWriter)
	if !ok {
		// Try just reader or writer based on operation
		return h.handleSimpleRequest(ctx, input)
	}

	// Determine operation type from the flow config
	operation := parseOperation(h.Config.From.Operation)

	switch operation.Method {
	case "GET":
		return h.handleRead(ctx, input, dest)
	case "POST":
		return h.handleCreate(ctx, input, dest)
	case "PUT", "PATCH":
		return h.handleUpdate(ctx, input, dest)
	case "DELETE":
		return h.handleDelete(ctx, input, dest)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation.Method)
	}
}

// handleRead handles GET requests.
func (h *FlowHandler) handleRead(ctx context.Context, input map[string]interface{}, dest connector.Reader) (interface{}, error) {
	query := connector.Query{
		Target:    h.Config.To.Target,
		Operation: "SELECT",
		Filters:   make(map[string]interface{}),
	}

	// Check if this is a GraphQL operation (Query.fieldName or Mutation.fieldName)
	if isGraphQLOperation(h.Config.From.Operation) {
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

	result, err := dest.Write(ctx, data)
	if err != nil {
		return nil, err
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

	result, err := dest.Write(ctx, data)
	if err != nil {
		return nil, err
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

	result, err := dest.Write(ctx, data)
	if err != nil {
		return nil, err
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
	// No transform configured - return input as-is
	if h.Config.Transform == nil && len(h.Config.Enrichments) == 0 {
		return input, nil
	}

	// Initialize CEL transformer if needed
	if h.Transformer == nil {
		var err error
		h.Transformer, err = transform.NewCELTransformer()
		if err != nil {
			return nil, fmt.Errorf("failed to create CEL transformer: %w", err)
		}
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

	// No transform configured, just return input (enrichments were for side effects)
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

	// Apply transforms using CEL with enriched data
	return h.Transformer.TransformWithEnriched(ctx, input, enriched, rules)
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
