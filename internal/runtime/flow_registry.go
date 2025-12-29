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

	// Extract path parameters from operation and use as filters
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

	result, err := dest.Read(ctx, query)
	if err != nil {
		return nil, err
	}

	return result.Rows, nil
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

// parseOperation parses an operation string like "GET /users/:id".
func parseOperation(op string) Operation {
	// Split by first space
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

// applyTransforms applies configured transformations to the input data.
func (h *FlowHandler) applyTransforms(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	// No transform configured - return input as-is
	if h.Config.Transform == nil {
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

	// Apply transforms using CEL
	return h.Transformer.Transform(ctx, input, rules)
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
