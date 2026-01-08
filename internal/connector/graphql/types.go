// Package graphql provides a connector for exposing and consuming GraphQL APIs.
package graphql

import (
	"context"
	"time"
)

// HandlerFunc is the signature for flow handlers.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// ServerConfig holds configuration for the GraphQL server connector.
type ServerConfig struct {
	// Port is the HTTP port to listen on.
	Port int

	// Host is the address to bind to.
	Host string

	// Schema configuration for loading GraphQL schema.
	Schema SchemaConfig

	// Playground enables the GraphQL Playground UI.
	Playground bool

	// PlaygroundPath is the path for the playground (default: /playground).
	PlaygroundPath string

	// Endpoint is the GraphQL endpoint path (default: /graphql).
	Endpoint string

	// CORS configuration.
	CORS *CORSConfig

	// Federation enables Apollo Federation v2 support.
	Federation *FederationServerConfig

	// Subscriptions enables GraphQL subscriptions over WebSocket.
	Subscriptions *SubscriptionsConfig
}

// SchemaConfig holds schema loading configuration.
type SchemaConfig struct {
	// Path to the GraphQL SDL file (schema-first mode).
	Path string

	// AutoGenerate enables automatic schema generation from flows (flow-first mode).
	AutoGenerate bool
}

// CORSConfig holds CORS settings for the GraphQL server.
type CORSConfig struct {
	// Origins allowed for CORS.
	Origins []string

	// Methods allowed for CORS.
	Methods []string

	// Headers allowed for CORS.
	Headers []string

	// AllowCredentials indicates whether credentials are allowed.
	AllowCredentials bool
}

// ClientConfig holds configuration for the GraphQL client connector.
type ClientConfig struct {
	// Endpoint is the GraphQL API URL.
	Endpoint string

	// Auth configuration for authentication.
	Auth *AuthConfig

	// Headers are custom HTTP headers to send with requests.
	Headers map[string]string

	// Timeout for requests.
	Timeout time.Duration

	// RetryCount is the number of retries for failed requests.
	RetryCount int

	// RetryDelay is the delay between retries.
	RetryDelay time.Duration
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	// Type is the authentication type: bearer, apikey, oauth2, basic.
	Type string

	// Token is the bearer token (for type=bearer).
	Token string

	// APIKey is the API key (for type=apikey).
	APIKey string

	// APIKeyHeader is the header name for the API key (default: X-API-Key).
	APIKeyHeader string

	// Username for basic auth (for type=basic).
	Username string

	// Password for basic auth (for type=basic).
	Password string

	// OAuth2 configuration (for type=oauth2).
	ClientID     string
	ClientSecret string
	TokenURL     string
	Scopes       []string
}

// GraphQLRequest represents a GraphQL request.
type GraphQLRequest struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName,omitempty"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse represents a GraphQL response.
type GraphQLResponse struct {
	Data   interface{}      `json:"data,omitempty"`
	Errors []GraphQLError   `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error.
type GraphQLError struct {
	Message    string                 `json:"message"`
	Locations  []ErrorLocation        `json:"locations,omitempty"`
	Path       []interface{}          `json:"path,omitempty"`
	Extensions map[string]interface{} `json:"extensions,omitempty"`
}

// ErrorLocation represents the location of an error in the query.
type ErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// FieldDefinition represents a GraphQL field definition for schema building.
type FieldDefinition struct {
	Name        string
	Type        string
	Description string
	Args        map[string]ArgDefinition
	Resolver    HandlerFunc
}

// ArgDefinition represents a GraphQL argument definition.
type ArgDefinition struct {
	Name         string
	Type         string
	DefaultValue interface{}
	Description  string
}

// TypeDefinition represents a GraphQL type definition.
type TypeDefinition struct {
	Name        string
	Description string
	Fields      map[string]FieldDefinition
}

// FederationServerConfig holds Federation-specific server configuration.
type FederationServerConfig struct {
	// Enabled enables Federation support (default: true if this block exists).
	Enabled bool

	// Version is the Federation version (1 or 2). Defaults to 2.
	Version int
}

// SubscriptionsConfig holds configuration for GraphQL subscriptions.
type SubscriptionsConfig struct {
	// Enabled enables subscription support (default: true if this block exists).
	Enabled bool

	// Path is the WebSocket endpoint path (default: /subscriptions).
	Path string

	// KeepAliveInterval is the interval for sending keep-alive pings.
	KeepAliveInterval time.Duration

	// ConnectionTimeout is the timeout for establishing a connection.
	ConnectionTimeout time.Duration
}

// EntityConfig represents a federated entity configuration in HCL.
type EntityConfig struct {
	// TypeName is the GraphQL type name.
	TypeName string

	// Keys are the @key fields for this entity.
	Keys []string

	// Connector is the data source for resolving this entity.
	Connector string

	// Target is the table/collection for resolution.
	Target string
}
