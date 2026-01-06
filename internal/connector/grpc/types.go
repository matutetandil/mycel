// Package grpc provides a gRPC connector for exposing and consuming gRPC services.
package grpc

import (
	"time"
)

// ServerConfig holds configuration for a gRPC server.
type ServerConfig struct {
	Host       string
	Port       int
	ProtoPath  string   // Path to .proto files directory
	ProtoFiles []string // Specific .proto files to load
	Reflection bool     // Enable gRPC reflection for tools like grpcurl
	TLS        *TLSConfig
	Auth       *AuthConfig // Server-side authentication
	MaxRecv    int         // Max receive message size in MB (default: 4)
	MaxSend    int         // Max send message size in MB (default: 4)
}

// AuthConfig holds authentication configuration for gRPC servers.
type AuthConfig struct {
	Type   string         // jwt, api_key, mtls, none
	JWT    *JWTAuthConfig // JWT validation config
	APIKey *APIKeyConfig  // API key validation config
	Public []string       // Public methods (no auth required)
}

// JWTAuthConfig holds JWT validation configuration.
type JWTAuthConfig struct {
	Secret     string   // Secret for HS* algorithms
	JWKSURL    string   // URL for JWKS (RS*, ES*)
	Issuer     string   // Expected issuer
	Audience   []string // Expected audience
	Algorithms []string // Allowed algorithms
}

// APIKeyConfig holds API key validation configuration.
type APIKeyConfig struct {
	Keys     []string // Valid API keys
	Header   string   // Header name (default: x-api-key)
	Metadata string   // Metadata key (default: api-key)
}

// ClientConfig holds configuration for a gRPC client.
type ClientConfig struct {
	Target     string // host:port or DNS name
	ProtoPath  string // Path to .proto files directory
	ProtoFiles []string
	TLS        *TLSConfig
	Auth       *ClientAuthConfig // Client-side authentication
	Timeout    time.Duration
	MaxRecv    int // Max receive message size in MB
	MaxSend    int // Max send message size in MB

	// Connection settings
	Insecure        bool // Use insecure connection (no TLS)
	WaitForReady    bool // Wait for server to be ready
	ConnectTimeout  time.Duration
	KeepAlive       *KeepAliveConfig
	RetryCount      int
	RetryBackoff    time.Duration
}

// ClientAuthConfig holds authentication configuration for gRPC clients.
type ClientAuthConfig struct {
	Type   string // bearer, api_key, oauth2, mtls
	Token  string // Bearer token (static)
	APIKey *ClientAPIKeyConfig
	OAuth2 *OAuth2Config
}

// ClientAPIKeyConfig holds API key config for gRPC clients.
type ClientAPIKeyConfig struct {
	Key      string // The API key value
	Metadata string // Metadata key name (default: api-key)
}

// OAuth2Config holds OAuth2 client credentials config.
type OAuth2Config struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// TLSConfig holds TLS configuration.
type TLSConfig struct {
	Enabled    bool
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
	SkipVerify bool
}

// KeepAliveConfig holds keep-alive configuration.
type KeepAliveConfig struct {
	Time    time.Duration
	Timeout time.Duration
}

// MethodHandler is a function that handles a gRPC method call.
type MethodHandler func(input map[string]interface{}) (interface{}, error)
