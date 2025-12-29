// Package rest provides a REST HTTP connector for exposing endpoints.
package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HandlerFunc is a function that handles a flow request.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Connector exposes HTTP endpoints as a REST API.
type Connector struct {
	name   string
	port   int
	server *http.Server
	mux    *http.ServeMux
	cors   *CORSConfig
	logger *slog.Logger

	mu       sync.Mutex
	handlers map[string]HandlerFunc
	started  bool
}

// CORSConfig holds CORS configuration.
type CORSConfig struct {
	Origins []string
	Methods []string
	Headers []string
}

// New creates a new REST connector.
func New(name string, port int, cors *CORSConfig, logger *slog.Logger) *Connector {
	if logger == nil {
		logger = slog.Default()
	}

	return &Connector{
		name:     name,
		port:     port,
		mux:      http.NewServeMux(),
		cors:     cors,
		logger:   logger,
		handlers: make(map[string]HandlerFunc),
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "rest"
}

// Connect is a no-op for REST connector (connection happens on Start).
func (c *Connector) Connect(ctx context.Context) error {
	return nil
}

// Close stops the HTTP server.
func (c *Connector) Close(ctx context.Context) error {
	if c.server != nil {
		return c.server.Shutdown(ctx)
	}
	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if !c.started {
		return fmt.Errorf("server not started")
	}
	return nil
}

// RegisterRoute registers a flow handler for an operation.
// Operation format: "METHOD /path" e.g., "GET /users", "POST /users", "GET /users/:id"
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlers[operation] = handler
}

// Start starts the HTTP server.
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("server already started")
	}

	// Setup routes
	c.setupRoutes()

	// Create server
	c.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", c.port),
		Handler:      c.corsMiddleware(c.mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.logger.Error("HTTP server error", slog.Any("error", err))
		}
	}()

	c.started = true
	return nil
}

// setupRoutes configures all registered routes on the mux.
func (c *Connector) setupRoutes() {
	// Group handlers by path to handle multiple methods
	pathHandlers := make(map[string]map[string]HandlerFunc)

	for operation, handler := range c.handlers {
		method, path := parseOperation(operation)

		// Convert :param to {param} for Go 1.22+ mux
		path = convertPathParams(path)

		if _, ok := pathHandlers[path]; !ok {
			pathHandlers[path] = make(map[string]HandlerFunc)
		}
		pathHandlers[path][method] = handler
	}

	// Register combined handlers for each path
	for path, methods := range pathHandlers {
		handlers := methods // capture for closure
		c.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			c.handleRequest(w, r, handlers)
		})
	}

	// Health check endpoint
	c.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

// handleRequest processes an HTTP request.
func (c *Connector) handleRequest(w http.ResponseWriter, r *http.Request, handlers map[string]HandlerFunc) {
	handler, ok := handlers[r.Method]
	if !ok {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Build input from request
	input := c.buildInput(r)

	// Execute flow handler
	result, err := handler(r.Context(), input)
	if err != nil {
		c.logger.Error("Flow handler error",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
		c.writeError(w, err)
		return
	}

	// Write response
	c.writeJSON(w, http.StatusOK, result)
}

// buildInput extracts input data from the HTTP request.
func (c *Connector) buildInput(r *http.Request) map[string]interface{} {
	input := make(map[string]interface{})

	// Path parameters (from Go 1.22+ pattern matching)
	// Note: Go 1.22 ServeMux supports {param} patterns
	// For now, we'll extract them manually from the path
	pathParams := extractPathParams(r)
	for k, v := range pathParams {
		input[k] = v
	}

	// Query parameters
	for key, values := range r.URL.Query() {
		if len(values) == 1 {
			input[key] = values[0]
		} else {
			input[key] = values
		}
	}

	// Body for POST/PUT/PATCH
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		if r.Header.Get("Content-Type") == "application/json" ||
		   strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				for k, v := range body {
					input[k] = v
				}
			}
		}
	}

	return input
}

// extractPathParams extracts path parameters from the request.
// This is a simple implementation that works with Go 1.22+ PathValue.
func extractPathParams(r *http.Request) map[string]interface{} {
	params := make(map[string]interface{})

	// Try to get 'id' from path (common pattern)
	if id := r.PathValue("id"); id != "" {
		params["id"] = id
	}

	return params
}

// writeJSON writes a JSON response.
func (c *Connector) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			c.logger.Error("Failed to encode response", slog.Any("error", err))
		}
	}
}

// writeError writes an error response.
func (c *Connector) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError

	// Check for specific error types
	errStr := err.Error()
	if strings.Contains(errStr, "validation") ||
		strings.Contains(errStr, "required") ||
		strings.Contains(errStr, "invalid") {
		status = http.StatusBadRequest
	}

	c.writeJSON(w, status, map[string]string{
		"error": errStr,
	})
}

// corsMiddleware adds CORS headers to responses.
func (c *Connector) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.cors != nil {
			// Set CORS headers
			origin := r.Header.Get("Origin")
			if c.isOriginAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			w.Header().Set("Access-Control-Allow-Methods", strings.Join(c.cors.Methods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			// Handle preflight
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks if the origin is allowed by CORS config.
func (c *Connector) isOriginAllowed(origin string) bool {
	if c.cors == nil || len(c.cors.Origins) == 0 {
		return false
	}

	for _, allowed := range c.cors.Origins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}

// parseOperation splits "METHOD /path" into method and path.
func parseOperation(op string) (method, path string) {
	parts := strings.SplitN(op, " ", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "GET", op
}

// convertPathParams converts :param to {param} for Go 1.22+ mux.
func convertPathParams(path string) string {
	// Replace :param with {param}
	result := strings.Builder{}
	i := 0
	for i < len(path) {
		if path[i] == ':' {
			// Find end of param name
			j := i + 1
			for j < len(path) && path[j] != '/' {
				j++
			}
			paramName := path[i+1 : j]
			result.WriteString("{")
			result.WriteString(paramName)
			result.WriteString("}")
			i = j
		} else {
			result.WriteByte(path[i])
			i++
		}
	}
	return result.String()
}
