package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/mycel-labs/mycel/internal/connector"
	"github.com/mycel-labs/mycel/internal/flow"
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
}

// HandleRequest processes an incoming request through the flow.
func (h *FlowHandler) HandleRequest(ctx context.Context, input map[string]interface{}) (interface{}, error) {
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
	data := &connector.Data{
		Target:    h.Config.To.Target,
		Operation: "INSERT",
		Payload:   input,
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
	data := &connector.Data{
		Target:    h.Config.To.Target,
		Operation: "UPDATE",
		Payload:   input,
		Filters:   make(map[string]interface{}),
	}

	// Get ID from input for filter
	if id, ok := input["id"]; ok {
		data.Filters["id"] = id
		delete(input, "id") // Remove ID from payload
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
