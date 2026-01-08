package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/validate"
)

// TypeSchema is an alias for validate.TypeSchema for use in HCL-first mode.
type TypeSchema = validate.TypeSchema

// ServerConnector exposes a GraphQL API endpoint.
type ServerConnector struct {
	name                string
	config              *ServerConfig
	schemaBuilder       *SchemaBuilder
	schema              *graphql.Schema
	server              *http.Server
	mux                 *http.ServeMux
	logger              *slog.Logger
	mu                  sync.RWMutex
	started             bool
	schemaBuilt         bool
	subscriptionManager *SubscriptionManager
}

// NewServer creates a new GraphQL server connector.
func NewServer(name string, config *ServerConfig, logger *slog.Logger) *ServerConnector {
	if logger == nil {
		logger = slog.Default()
	}

	// Set defaults
	if config.Host == "" {
		config.Host = "0.0.0.0"
	}
	if config.Port == 0 {
		config.Port = 4000
	}
	if config.Endpoint == "" {
		config.Endpoint = "/graphql"
	}
	if config.PlaygroundPath == "" {
		config.PlaygroundPath = "/playground"
	}

	return &ServerConnector{
		name:          name,
		config:        config,
		schemaBuilder: NewSchemaBuilder(),
		mux:           http.NewServeMux(),
		logger:        logger,
	}
}

// Name returns the connector name.
func (c *ServerConnector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *ServerConnector) Type() string {
	return "graphql"
}

// Connect initializes the GraphQL server.
func (c *ServerConnector) Connect(ctx context.Context) error {
	// Enable Federation if configured
	if c.config.Federation != nil && c.config.Federation.Enabled {
		version := c.config.Federation.Version
		if version == 0 {
			version = 2
		}
		c.schemaBuilder.EnableFederation(version)
		c.logger.Info("enabled GraphQL Federation",
			"version", version,
			"connector", c.name,
		)
	}

	// Load SDL schema if path is provided
	if c.config.Schema.Path != "" {
		if err := c.schemaBuilder.LoadSDL(c.config.Schema.Path); err != nil {
			return fmt.Errorf("failed to load GraphQL schema: %w", err)
		}
		c.logger.Info("loaded GraphQL schema from file",
			"path", c.config.Schema.Path,
			"connector", c.name,
		)
	}

	return nil
}

// LoadHCLTypes loads type schemas from HCL for HCL-first mode.
// This converts HCL type definitions to GraphQL types.
func (c *ServerConnector) LoadHCLTypes(types map[string]*TypeSchema) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(types) == 0 {
		return nil
	}

	if err := c.schemaBuilder.LoadHCLTypes(types); err != nil {
		return fmt.Errorf("failed to load HCL types: %w", err)
	}

	c.logger.Info("loaded HCL types for GraphQL",
		"count", len(types),
		"connector", c.name,
	)

	return nil
}

// RegisterRouteWithReturnType registers a handler with a specific return type.
// Use this for HCL-first mode where the return type is specified in the flow config.
func (c *ServerConnector) RegisterRouteWithReturnType(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error), returnType string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.schemaBuilder.RegisterHandlerWithReturnType(operation, handler, returnType); err != nil {
		c.logger.Error("failed to register GraphQL handler with return type",
			"operation", operation,
			"returnType", returnType,
			"error", err,
		)
		return
	}

	c.logger.Debug("registered GraphQL handler with return type",
		"operation", operation,
		"returnType", returnType,
		"connector", c.name,
	)
}

// RegisterEntity registers a federated entity for resolution by this subgraph.
func (c *ServerConnector) RegisterEntity(typeName string, keys []EntityKey, resolver EntityResolver) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.schemaBuilder.RegisterEntity(typeName, keys, resolver)
	c.logger.Debug("registered federated entity",
		"type", typeName,
		"connector", c.name,
	)
}

// IsFederationEnabled returns true if Federation is enabled.
func (c *ServerConnector) IsFederationEnabled() bool {
	return c.schemaBuilder.IsFederationEnabled()
}

// Close shuts down the GraphQL server.
func (c *ServerConnector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close subscription manager first
	if c.subscriptionManager != nil {
		c.subscriptionManager.Close()
	}

	if c.server != nil && c.started {
		c.logger.Info("shutting down GraphQL server", "connector", c.name)
		return c.server.Shutdown(ctx)
	}

	return nil
}

// Health checks if the connector is healthy.
func (c *ServerConnector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return nil // Not started yet is OK
	}

	return nil
}

// RegisterRoute registers a flow handler for a GraphQL operation.
// Operation format: "Query.fieldName" or "Mutation.fieldName"
// This method implements the runtime.RouteRegistrar interface.
func (c *ServerConnector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.schemaBuilder.RegisterHandler(operation, handler); err != nil {
		c.logger.Error("failed to register GraphQL handler",
			"operation", operation,
			"error", err,
		)
		return
	}

	c.logger.Debug("registered GraphQL handler",
		"operation", operation,
		"connector", c.name,
	)
}

// Start launches the HTTP server.
func (c *ServerConnector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	// Build schema from registered handlers
	schema, err := c.schemaBuilder.Build()
	if err != nil {
		return fmt.Errorf("failed to build GraphQL schema: %w", err)
	}
	c.schema = schema
	c.schemaBuilt = true

	// Initialize subscription manager if subscriptions are configured
	if c.config.Subscriptions != nil && c.config.Subscriptions.Enabled {
		c.subscriptionManager = NewSubscriptionManager(c.schema, c.logger)
		c.logger.Info("initialized GraphQL subscription manager",
			"connector", c.name,
		)
	}

	// Set up HTTP handlers
	c.setupHandlers()

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	c.server = &http.Server{
		Addr:         addr,
		Handler:      c.corsMiddleware(c.mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		c.logger.Info("starting GraphQL server",
			"address", addr,
			"endpoint", c.config.Endpoint,
			"playground", c.config.Playground,
			"connector", c.name,
		)

		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.logger.Error("GraphQL server error",
				"error", err,
				"connector", c.name,
			)
		}
	}()

	c.started = true
	return nil
}

// setupHandlers configures the HTTP handlers.
func (c *ServerConnector) setupHandlers() {
	// GraphQL endpoint
	c.mux.HandleFunc(c.config.Endpoint, c.handleGraphQL)

	// Playground endpoint
	if c.config.Playground {
		c.mux.HandleFunc(c.config.PlaygroundPath, c.handlePlayground)
	}

	// Subscriptions WebSocket endpoint
	if c.subscriptionManager != nil {
		subscriptionPath := "/subscriptions"
		if c.config.Subscriptions != nil && c.config.Subscriptions.Path != "" {
			subscriptionPath = c.config.Subscriptions.Path
		}
		c.mux.HandleFunc(subscriptionPath, c.subscriptionManager.Handler())
		c.logger.Info("registered GraphQL subscription endpoint",
			"path", subscriptionPath,
			"connector", c.name,
		)
	}

	// Health check
	c.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

// handleGraphQL handles GraphQL requests.
func (c *ServerConnector) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Only allow POST and GET
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request GraphQLRequest

	// Parse request based on method
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeGraphQLError(w, "Invalid JSON request body", http.StatusBadRequest)
			return
		}
	} else {
		// GET request - parse from query params
		request.Query = r.URL.Query().Get("query")
		request.OperationName = r.URL.Query().Get("operationName")
		if vars := r.URL.Query().Get("variables"); vars != "" {
			if err := json.Unmarshal([]byte(vars), &request.Variables); err != nil {
				writeGraphQLError(w, "Invalid variables JSON", http.StatusBadRequest)
				return
			}
		}
	}

	if request.Query == "" {
		writeGraphQLError(w, "Query is required", http.StatusBadRequest)
		return
	}

	// Execute GraphQL query
	result := graphql.Do(graphql.Params{
		Schema:         *c.schema,
		RequestString:  request.Query,
		VariableValues: request.Variables,
		OperationName:  request.OperationName,
		Context:        r.Context(),
	})

	// Write response
	if err := json.NewEncoder(w).Encode(result); err != nil {
		c.logger.Error("failed to encode GraphQL response",
			"error", err,
			"connector", c.name,
		)
	}
}

// handlePlayground serves the GraphiQL IDE.
func (c *ServerConnector) handlePlayground(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(GraphiQLHTML(c.config.Endpoint)))
}

// corsMiddleware adds CORS headers to responses.
func (c *ServerConnector) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Default CORS headers
		origins := "*"
		methods := "GET, POST, OPTIONS"
		headers := "Content-Type, Authorization"

		if c.config.CORS != nil {
			if len(c.config.CORS.Origins) > 0 {
				origins = strings.Join(c.config.CORS.Origins, ", ")
			}
			if len(c.config.CORS.Methods) > 0 {
				methods = strings.Join(c.config.CORS.Methods, ", ")
			}
			if len(c.config.CORS.Headers) > 0 {
				headers = strings.Join(c.config.CORS.Headers, ", ")
			}
		}

		w.Header().Set("Access-Control-Allow-Origin", origins)
		w.Header().Set("Access-Control-Allow-Methods", methods)
		w.Header().Set("Access-Control-Allow-Headers", headers)

		if c.config.CORS != nil && c.config.CORS.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Port returns the configured port.
func (c *ServerConnector) Port() int {
	return c.config.Port
}

// Publish sends data to all subscribers of a topic.
// Use this from flows to trigger subscription updates.
func (c *ServerConnector) Publish(topic string, data interface{}) {
	if c.subscriptionManager != nil {
		c.subscriptionManager.Broadcast(topic, data)
	}
}

// GetSubscriptionManager returns the subscription manager for advanced usage.
func (c *ServerConnector) GetSubscriptionManager() *SubscriptionManager {
	return c.subscriptionManager
}

// writeGraphQLError writes a GraphQL error response.
func writeGraphQLError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(GraphQLResponse{
		Errors: []GraphQLError{
			{Message: message},
		},
	})
}

// Ensure ServerConnector implements the required interfaces.
var (
	_ connector.Connector = (*ServerConnector)(nil)
)
