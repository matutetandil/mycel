// Package rest provides a REST HTTP connector for exposing endpoints.
package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/codec"
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/health"
	"github.com/matutetandil/mycel/internal/metrics"
	"github.com/matutetandil/mycel/internal/ratelimit"
)

// HandlerFunc is a function that handles a flow request.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Connector exposes HTTP endpoints as a REST API.
type Connector struct {
	name          string
	port          int
	server        *http.Server
	mux           *http.ServeMux
	cors          *CORSConfig
	authConfig    *AuthConfig
	logger        *slog.Logger
	health        *health.Manager
	metrics       *metrics.Registry
	rateLimiter   *ratelimit.Limiter
	defaultFormat string // default format for request/response ("json", "xml")
	environment   string // runtime environment (development, staging, production)

	mu         sync.Mutex
	handlers   map[string]HandlerFunc
	pathParams map[string][]string // maps path pattern to param names
	started    bool
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
		name:       name,
		port:       port,
		mux:        http.NewServeMux(),
		cors:       cors,
		logger:     logger,
		handlers:   make(map[string]HandlerFunc),
		pathParams: make(map[string][]string),
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

// SetHealthManager sets the health manager for this connector.
func (c *Connector) SetHealthManager(h *health.Manager) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.health = h
}

// SetMetrics sets the metrics registry for this connector.
func (c *Connector) SetMetrics(m *metrics.Registry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = m
}

// SetRateLimiter sets the rate limiter for this connector.
func (c *Connector) SetRateLimiter(rl *ratelimit.Limiter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rateLimiter = rl
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

	// Build middleware chain
	var handler http.Handler = c.mux

	// Apply rate limiting if configured
	if c.rateLimiter != nil {
		handler = c.rateLimiter.Middleware(handler)
		c.logger.Info("rate limiting enabled")
	}

	// Apply CORS
	handler = c.corsMiddleware(handler)

	// Apply authentication if configured
	if c.authConfig != nil {
		handler = c.authMiddleware(handler)
		c.logger.Info("authentication enabled", "type", c.authConfig.Type)
	}

	// Create server
	c.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", c.port),
		Handler:      handler,
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
		method, origPath := parseOperation(operation)

		// Extract param names from original path (e.g., :id, :user_id)
		paramNames := extractParamNames(origPath)

		// Convert :param to {param} for Go 1.22+ mux
		path := convertPathParams(origPath)

		// Store param names for this path
		c.pathParams[path] = paramNames

		if _, ok := pathHandlers[path]; !ok {
			pathHandlers[path] = make(map[string]HandlerFunc)
		}
		pathHandlers[path][method] = handler
	}

	// Register combined handlers for each path
	for path, methods := range pathHandlers {
		handlers := methods // capture for closure
		paramNames := c.pathParams[path]
		c.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			c.handleRequest(w, r, handlers, paramNames)
		})
	}

	// Health check endpoints
	if c.health != nil {
		// Use the health manager for full health checks
		c.health.RegisterHandlers(c.mux)
	} else {
		// Fallback to simple health check
		c.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		})
	}

	// Metrics endpoint
	if c.metrics != nil {
		c.mux.Handle("/metrics", c.metrics.Handler())
	}
}

// handleRequest processes an HTTP request.
func (c *Connector) handleRequest(w http.ResponseWriter, r *http.Request, handlers map[string]HandlerFunc, paramNames []string) {
	start := time.Now()
	path := r.URL.Path

	// Track in-flight requests
	if c.metrics != nil {
		c.metrics.IncRequestsInFlight(r.Method, path)
		defer c.metrics.DecRequestsInFlight(r.Method, path)
	}

	handler, ok := handlers[r.Method]
	if !ok {
		if c.metrics != nil {
			c.metrics.RecordRequest(r.Method, path, "405", time.Since(start))
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Build input from request
	input := c.buildInput(r, paramNames)

	// Execute flow handler
	result, err := handler(r.Context(), input)
	duration := time.Since(start)

	if err != nil {
		status := c.writeError(w, err)
		if c.metrics != nil {
			c.metrics.RecordRequest(r.Method, path, strconv.Itoa(status), duration)
		}
		return
	}

	// Check for http_status_code override in response (from response block)
	statusCode := http.StatusOK
	if resultMap, ok := result.(map[string]interface{}); ok {
		if code, found := connector.ExtractStatusCode(resultMap, "http_status_code"); found {
			statusCode = code
		}
	}

	// Extract and apply response headers from aspects
	result = c.applyResponseHeaders(w, result)

	// Check for binary response (e.g., PDF, images)
	if c.writeBinaryResponse(w, statusCode, result) {
		if c.metrics != nil {
			c.metrics.RecordRequest(r.Method, path, strconv.Itoa(statusCode), duration)
		}
		return
	}

	// Write response using format-aware codec
	c.writeResponse(w, r, statusCode, result)
	if c.metrics != nil {
		c.metrics.RecordRequest(r.Method, path, strconv.Itoa(statusCode), duration)
	}
}

// buildInput extracts input data from the HTTP request.
func (c *Connector) buildInput(r *http.Request, paramNames []string) map[string]interface{} {
	input := make(map[string]interface{})

	// Path parameters (from Go 1.22+ pattern matching)
	// Extract all named path parameters based on the route definition
	for _, name := range paramNames {
		if val := r.PathValue(name); val != "" {
			input[name] = val
		}
	}

	// Query parameters
	for key, values := range r.URL.Query() {
		if len(values) == 1 {
			input[key] = values[0]
		} else {
			input[key] = values
		}
	}

	// Request headers (available as input.headers in transforms/CEL)
	headers := make(map[string]interface{})
	for key := range r.Header {
		headers[strings.ToLower(key)] = r.Header.Get(key)
	}
	input["headers"] = headers

	// Body for POST/PUT/PATCH — auto-detect format from Content-Type
	if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
		ct := r.Header.Get("Content-Type")

		// Handle multipart/form-data (file uploads)
		if strings.HasPrefix(ct, "multipart/form-data") {
			c.parseMultipart(r, input)
		} else {
			// Determine codec: flow-level format override > Content-Type detection > connector default
			var bodyCodec codec.Codec
			if ctxFormat := codec.FormatFromContext(r.Context()); ctxFormat != "" {
				bodyCodec = codec.Get(ctxFormat)
			} else if ct != "" {
				bodyCodec = codec.DetectFromContentType(ct)
			} else if c.defaultFormat != "" {
				bodyCodec = codec.Get(c.defaultFormat)
			} else {
				bodyCodec = codec.Get("json")
			}

			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil && len(bodyBytes) > 0 {
				if decoded, err := bodyCodec.Decode(bodyBytes); err == nil {
					for k, v := range decoded {
						input[k] = v
					}
				}
			}
		}
	}

	return input
}

// extractParamNames extracts parameter names from a path pattern.
// Example: "/orders/:id/:user_id" returns ["id", "user_id"]
func extractParamNames(path string) []string {
	var names []string
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if len(part) > 0 && part[0] == ':' {
			names = append(names, part[1:])
		}
	}
	return names
}

// writeResponse writes a response using the appropriate codec.
// It checks for a flow-level format override in the context, then falls back
// to the connector's default format, and finally to JSON.
func (c *Connector) writeResponse(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	var respCodec codec.Codec
	if r != nil {
		if ctxFormat := codec.FormatFromContext(r.Context()); ctxFormat != "" {
			respCodec = codec.Get(ctxFormat)
		}
	}
	if respCodec == nil && c.defaultFormat != "" {
		respCodec = codec.Get(c.defaultFormat)
	}
	if respCodec == nil {
		respCodec = codec.Get("json")
	}

	w.Header().Set("Content-Type", respCodec.ContentType())
	w.WriteHeader(status)

	if data != nil {
		encoded, err := respCodec.Encode(data)
		if err != nil {
			c.logger.Error("Failed to encode response", slog.Any("error", err))
			return
		}
		w.Write(encoded)
	}
}

// parseMultipart extracts multipart/form-data fields and files into the input map.
// Form fields are added directly. Files are added as maps with metadata.
func (c *Connector) parseMultipart(r *http.Request, input map[string]interface{}) {
	// 32 MB max memory (rest spills to temp files)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return
	}

	// Regular form fields
	for key, values := range r.MultipartForm.Value {
		if len(values) == 1 {
			input[key] = values[0]
		} else {
			input[key] = values
		}
	}

	// File fields
	for key, fileHeaders := range r.MultipartForm.File {
		var files []map[string]interface{}
		for _, fh := range fileHeaders {
			file, err := fh.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				continue
			}

			files = append(files, map[string]interface{}{
				"filename":     fh.Filename,
				"size":         fh.Size,
				"content_type": fh.Header.Get("Content-Type"),
				"data":         base64.StdEncoding.EncodeToString(data),
			})
		}
		if len(files) == 1 {
			input[key] = files[0]
		} else if len(files) > 0 {
			input[key] = files
		}
	}
}

// applyResponseHeaders extracts _response_headers from the result wrapper
// (injected by aspect response enrichment) and sets them as HTTP headers.
// Returns the unwrapped data for normal response writing.
func (c *Connector) applyResponseHeaders(w http.ResponseWriter, data interface{}) interface{} {
	wrapper, ok := data.(map[string]interface{})
	if !ok {
		return data
	}

	headersRaw, hasHeaders := wrapper["_response_headers"]
	innerData, hasData := wrapper["_data"]
	if !hasHeaders || !hasData {
		return data
	}

	// Set HTTP headers
	if headers, ok := headersRaw.(map[string]string); ok {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
	}

	return innerData
}

// writeBinaryResponse checks if the result contains binary data (_binary + _content_type)
// and writes it as a raw binary HTTP response. Returns true if handled.
func (c *Connector) writeBinaryResponse(w http.ResponseWriter, status int, data interface{}) bool {
	resultMap, ok := data.(map[string]interface{})
	if !ok {
		return false
	}

	binaryData, hasBinary := resultMap["_binary"]
	contentType, hasContentType := resultMap["_content_type"]
	if !hasBinary || !hasContentType {
		return false
	}

	ct, _ := contentType.(string)
	if ct == "" {
		return false
	}

	var raw []byte
	switch v := binaryData.(type) {
	case []byte:
		raw = v
	case string:
		// Assume base64-encoded
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			c.logger.Error("Failed to decode binary response", slog.Any("error", err))
			return false
		}
		raw = decoded
	default:
		return false
	}

	w.Header().Set("Content-Type", ct)

	// Set Content-Disposition if filename is provided
	if filename, ok := resultMap["_filename"].(string); ok && filename != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	}

	w.WriteHeader(status)
	w.Write(raw)
	return true
}

// writeJSON writes a JSON response (used by internal endpoints like health/errors).
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
func (c *Connector) writeError(w http.ResponseWriter, err error) int {
	// Check for FlowError with custom response
	var flowErr *flow.FlowError
	if errors.As(err, &flowErr) {
		// Set custom headers
		for k, v := range flowErr.Headers {
			w.Header().Set(k, v)
		}
		c.writeJSON(w, flowErr.Status, flowErr.Body)
		return flowErr.Status
	}

	status := http.StatusInternalServerError

	// Check for specific error types
	errStr := err.Error()
	if strings.Contains(errStr, "validation") ||
		strings.Contains(errStr, "required") ||
		strings.Contains(errStr, "invalid") {
		status = http.StatusBadRequest
	}

	// In production, return minimal error info (no internal details)
	if c.environment == "production" || c.environment == "prod" {
		msg := http.StatusText(status)
		if status == http.StatusBadRequest {
			msg = errStr // validation errors are safe to expose
		}
		c.writeJSON(w, status, map[string]string{
			"error": msg,
		})
		return status
	}

	c.writeJSON(w, status, map[string]string{
		"error": errStr,
	})
	return status
}

// corsMiddleware adds CORS headers to responses.
func (c *Connector) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.cors != nil {
			// Explicit CORS config — use it
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
		} else if c.environment != "production" && c.environment != "prod" {
			// No CORS config + non-production: permissive CORS (allow all origins)
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
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
