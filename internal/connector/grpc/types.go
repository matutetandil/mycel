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
	MaxRecv    int // Max receive message size in MB (default: 4)
	MaxSend    int // Max send message size in MB (default: 4)
}

// ClientConfig holds configuration for a gRPC client.
type ClientConfig struct {
	Target     string // host:port or DNS name
	ProtoPath  string // Path to .proto files directory
	ProtoFiles []string
	TLS        *TLSConfig
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
