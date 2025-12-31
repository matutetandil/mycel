package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

// Factory creates gRPC connectors.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new gRPC connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{logger: logger}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "grpc"
}

// Supports returns true if this factory supports the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "grpc"
}

// Create creates a new gRPC connector based on configuration.
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	driver := config.Driver
	if driver == "" {
		driver = "server"
	}

	switch driver {
	case "server":
		return f.createServer(config)
	case "client":
		return f.createClient(config)
	default:
		return nil, fmt.Errorf("unknown gRPC driver: %s", driver)
	}
}

// createServer creates a gRPC server connector.
func (f *Factory) createServer(config *connector.Config) (connector.Connector, error) {
	serverConfig := &ServerConfig{
		Host:       getString(config.Properties, "host", "0.0.0.0"),
		Port:       getInt(config.Properties, "port", 50051),
		ProtoPath:  getString(config.Properties, "proto_path", ""),
		Reflection: getBool(config.Properties, "reflection", true),
		MaxRecv:    getInt(config.Properties, "max_recv_mb", 4),
		MaxSend:    getInt(config.Properties, "max_send_mb", 4),
	}

	// Proto files
	if files, ok := config.Properties["proto_files"].([]interface{}); ok {
		for _, f := range files {
			if s, ok := f.(string); ok {
				serverConfig.ProtoFiles = append(serverConfig.ProtoFiles, s)
			}
		}
	}

	// TLS
	if tlsCfg, ok := config.Properties["tls"].(map[string]interface{}); ok {
		serverConfig.TLS = parseTLSConfig(tlsCfg)
	}

	return NewServerConnector(config.Name, serverConfig, f.logger), nil
}

// createClient creates a gRPC client connector.
func (f *Factory) createClient(config *connector.Config) (connector.Connector, error) {
	clientConfig := &ClientConfig{
		Target:         getString(config.Properties, "target", "localhost:50051"),
		ProtoPath:      getString(config.Properties, "proto_path", ""),
		Timeout:        getDuration(config.Properties, "timeout", 30*time.Second),
		Insecure:       getBool(config.Properties, "insecure", true),
		WaitForReady:   getBool(config.Properties, "wait_for_ready", false),
		ConnectTimeout: getDuration(config.Properties, "connect_timeout", 10*time.Second),
		MaxRecv:        getInt(config.Properties, "max_recv_mb", 4),
		MaxSend:        getInt(config.Properties, "max_send_mb", 4),
		RetryCount:     getInt(config.Properties, "retry_count", 3),
		RetryBackoff:   getDuration(config.Properties, "retry_backoff", 100*time.Millisecond),
	}

	// Proto files
	if files, ok := config.Properties["proto_files"].([]interface{}); ok {
		for _, f := range files {
			if s, ok := f.(string); ok {
				clientConfig.ProtoFiles = append(clientConfig.ProtoFiles, s)
			}
		}
	}

	// TLS
	if tlsCfg, ok := config.Properties["tls"].(map[string]interface{}); ok {
		clientConfig.TLS = parseTLSConfig(tlsCfg)
		clientConfig.Insecure = false
	}

	// Keep-alive
	if kaCfg, ok := config.Properties["keep_alive"].(map[string]interface{}); ok {
		clientConfig.KeepAlive = &KeepAliveConfig{
			Time:    getDuration(kaCfg, "time", 30*time.Second),
			Timeout: getDuration(kaCfg, "timeout", 10*time.Second),
		}
	}

	return NewClientConnector(config.Name, clientConfig), nil
}

// parseTLSConfig parses TLS configuration from properties.
func parseTLSConfig(props map[string]interface{}) *TLSConfig {
	return &TLSConfig{
		Enabled:    getBool(props, "enabled", true),
		CertFile:   getString(props, "cert_file", ""),
		KeyFile:    getString(props, "key_file", ""),
		CAFile:     getString(props, "ca_file", ""),
		ServerName: getString(props, "server_name", ""),
		SkipVerify: getBool(props, "skip_verify", false),
	}
}

// Helper functions

func getString(props map[string]interface{}, key, defaultVal string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getInt(props map[string]interface{}, key string, defaultVal int) int {
	if v, ok := props[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return defaultVal
}

func getBool(props map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := props[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func getDuration(props map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
	if v, ok := props[key]; ok {
		switch d := v.(type) {
		case string:
			if parsed, err := time.ParseDuration(d); err == nil {
				return parsed
			}
		case time.Duration:
			return d
		case int:
			return time.Duration(d) * time.Second
		case int64:
			return time.Duration(d) * time.Second
		case float64:
			return time.Duration(d) * time.Second
		}
	}
	return defaultVal
}
