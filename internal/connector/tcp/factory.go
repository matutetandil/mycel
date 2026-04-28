package tcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates TCP connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new TCP connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the specified connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "tcp"
}

// Create creates a new TCP connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = "server"
	}

	switch driver {
	case "server":
		return f.createServer(cfg)
	case "client":
		return f.createClient(cfg)
	default:
		return nil, fmt.Errorf("unknown TCP driver: %s (expected 'server' or 'client')", driver)
	}
}

// createServer creates a TCP server connector.
func (f *Factory) createServer(cfg *connector.Config) (*ServerConnector, error) {
	// Required fields
	port := getInt(cfg.Properties, "port", 9000)

	// Optional fields
	host := getString(cfg.Properties, "host", "0.0.0.0")
	protocol := getString(cfg.Properties, "protocol", "json")
	maxConns := getInt(cfg.Properties, "max_connections", 100)
	readTimeout := getDuration(cfg.Properties, "read_timeout", 30*time.Second)
	writeTimeout := getDuration(cfg.Properties, "write_timeout", 30*time.Second)

	opts := []ServerOption{
		WithServerLogger(f.logger),
		WithMaxConnections(maxConns),
		WithServerTimeouts(readTimeout, writeTimeout),
	}

	// TLS configuration
	if tlsCfg := getTLSMap(cfg.Properties, "tls"); tlsCfg != nil {
		if getBool(tlsCfg, "enabled", false) {
			tlsConfig, err := f.buildServerTLS(tlsCfg)
			if err != nil {
				return nil, fmt.Errorf("failed to configure TLS: %w", err)
			}
			opts = append(opts, WithServerTLS(tlsConfig))
		}
	}

	return NewServer(cfg.Name, host, port, protocol, opts...)
}

// createClient creates a TCP client connector.
func (f *Factory) createClient(cfg *connector.Config) (*ClientConnector, error) {
	// Required fields
	host := getString(cfg.Properties, "host", "")
	if host == "" {
		return nil, fmt.Errorf("TCP client requires 'host' property")
	}

	port := getInt(cfg.Properties, "port", 0)
	if port == 0 {
		return nil, fmt.Errorf("TCP client requires 'port' property")
	}

	// Optional fields
	protocol := getString(cfg.Properties, "protocol", "json")
	poolSize := getInt(cfg.Properties, "pool_size", 10)
	connectTimeout := getDuration(cfg.Properties, "connect_timeout", 10*time.Second)
	readTimeout := getDuration(cfg.Properties, "read_timeout", 30*time.Second)
	writeTimeout := getDuration(cfg.Properties, "write_timeout", 30*time.Second)
	idleTimeout := getDuration(cfg.Properties, "idle_timeout", 5*time.Minute)
	retryCount := getInt(cfg.Properties, "retry_count", 3)
	retryDelay := getDuration(cfg.Properties, "retry_delay", time.Second)

	opts := []ClientOption{
		WithClientLogger(f.logger),
		WithPoolSize(poolSize),
		WithClientTimeouts(connectTimeout, readTimeout, writeTimeout, idleTimeout),
		WithRetry(retryCount, retryDelay),
	}

	// TLS configuration
	if tlsCfg := getTLSMap(cfg.Properties, "tls"); tlsCfg != nil {
		if getBool(tlsCfg, "enabled", false) {
			tlsConfig, err := f.buildClientTLS(tlsCfg)
			if err != nil {
				return nil, fmt.Errorf("failed to configure TLS: %w", err)
			}
			opts = append(opts, WithClientTLS(tlsConfig))
		}
	}

	return NewClient(cfg.Name, host, port, protocol, opts...)
}

// buildServerTLS builds TLS configuration for the server.
func (f *Factory) buildServerTLS(cfg map[string]interface{}) (*tls.Config, error) {
	certFile := getString(cfg, "cert", "")
	keyFile := getString(cfg, "key", "")

	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("TLS requires 'cert' and 'key' properties")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// buildClientTLS builds TLS configuration for the client.
func (f *Factory) buildClientTLS(cfg map[string]interface{}) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Insecure skip verify (not recommended for production)
	if getBool(cfg, "insecure_skip_verify", false) {
		tlsConfig.InsecureSkipVerify = true
	}

	// Custom CA certificate
	if caFile := getString(cfg, "ca_cert", ""); caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.RootCAs = caCertPool
	}

	// Client certificate (mutual TLS)
	certFile := getString(cfg, "cert", "")
	keyFile := getString(cfg, "key", "")
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// Helper functions for extracting configuration values

func getString(props map[string]interface{}, key, defaultVal string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getInt(props map[string]interface{}, key string, defaultVal int) int {
	return connector.IntFromProps(props, key, defaultVal)
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
		case int64:
			return time.Duration(d) * time.Millisecond
		case float64:
			return time.Duration(d) * time.Millisecond
		}
	}
	return defaultVal
}

func getTLSMap(props map[string]interface{}, key string) map[string]interface{} {
	if v, ok := props[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}
