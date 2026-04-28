package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
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

	// Auth
	if authCfg, ok := config.Properties["auth"].(map[string]interface{}); ok {
		serverConfig.Auth = parseAuthConfig(authCfg)
	}

	return NewServerConnector(config.Name, serverConfig, f.logger), nil
}

// parseAuthConfig parses authentication configuration.
func parseAuthConfig(props map[string]interface{}) *AuthConfig {
	cfg := &AuthConfig{
		Type: getString(props, "type", ""),
	}

	// Parse public methods
	if public, ok := props["public"].([]interface{}); ok {
		for _, p := range public {
			if s, ok := p.(string); ok {
				cfg.Public = append(cfg.Public, s)
			}
		}
	}

	// Parse JWT config
	if cfg.Type == "jwt" {
		cfg.JWT = parseJWTAuthConfig(props)
	}

	// Parse API Key config
	if cfg.Type == "api_key" {
		cfg.APIKey = parseAPIKeyConfig(props)
	}

	return cfg
}

// parseJWTAuthConfig parses JWT authentication configuration.
func parseJWTAuthConfig(props map[string]interface{}) *JWTAuthConfig {
	cfg := &JWTAuthConfig{
		Secret:   getString(props, "secret", ""),
		JWKSURL:  getString(props, "jwks_url", ""),
		Issuer:   getString(props, "issuer", ""),
	}

	// Audience can be string or array
	if aud, ok := props["audience"].(string); ok {
		cfg.Audience = []string{aud}
	} else if aud, ok := props["audience"].([]interface{}); ok {
		for _, a := range aud {
			if s, ok := a.(string); ok {
				cfg.Audience = append(cfg.Audience, s)
			}
		}
	}

	// Algorithms
	if algs, ok := props["algorithms"].([]interface{}); ok {
		for _, a := range algs {
			if s, ok := a.(string); ok {
				cfg.Algorithms = append(cfg.Algorithms, s)
			}
		}
	}

	return cfg
}

// parseAPIKeyConfig parses API key authentication configuration.
func parseAPIKeyConfig(props map[string]interface{}) *APIKeyConfig {
	cfg := &APIKeyConfig{
		Header:   getString(props, "header", "x-api-key"),
		Metadata: getString(props, "metadata", "api-key"),
	}

	// Keys can be single string or array
	if key, ok := props["keys"].(string); ok {
		cfg.Keys = []string{key}
	} else if keys, ok := props["keys"].([]interface{}); ok {
		for _, k := range keys {
			if s, ok := k.(string); ok {
				cfg.Keys = append(cfg.Keys, s)
			}
		}
	}

	return cfg
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

	// Auth
	if authCfg, ok := config.Properties["auth"].(map[string]interface{}); ok {
		clientConfig.Auth = parseClientAuthConfig(authCfg)
	}

	// Keep-alive
	if kaCfg, ok := config.Properties["keep_alive"].(map[string]interface{}); ok {
		clientConfig.KeepAlive = &KeepAliveConfig{
			Time:    getDuration(kaCfg, "time", 30*time.Second),
			Timeout: getDuration(kaCfg, "timeout", 10*time.Second),
		}
	}

	// Load balancing
	if lbCfg, ok := config.Properties["load_balancing"].(map[string]interface{}); ok {
		clientConfig.LoadBalancing = parseLoadBalancingConfig(lbCfg)
	} else if lbPolicy := getString(config.Properties, "load_balancing", ""); lbPolicy != "" {
		// Simple form: load_balancing = "round_robin"
		clientConfig.LoadBalancing = &LoadBalancingConfig{
			Policy: lbPolicy,
		}
	}

	return NewClientConnector(config.Name, clientConfig), nil
}

// parseLoadBalancingConfig parses load balancing configuration.
func parseLoadBalancingConfig(props map[string]interface{}) *LoadBalancingConfig {
	cfg := &LoadBalancingConfig{
		Policy:      getString(props, "policy", "pick_first"),
		HealthCheck: getBool(props, "health_check", false),
	}

	// Targets can be single string or array
	if target, ok := props["targets"].(string); ok {
		cfg.Targets = []string{target}
	} else if targets, ok := props["targets"].([]interface{}); ok {
		for _, t := range targets {
			if s, ok := t.(string); ok {
				cfg.Targets = append(cfg.Targets, s)
			}
		}
	}

	return cfg
}

// parseClientAuthConfig parses client authentication configuration.
func parseClientAuthConfig(props map[string]interface{}) *ClientAuthConfig {
	cfg := &ClientAuthConfig{
		Type:  getString(props, "type", ""),
		Token: getString(props, "token", ""),
	}

	// API Key config
	if cfg.Type == "api_key" {
		cfg.APIKey = &ClientAPIKeyConfig{
			Key:      getString(props, "api_key", ""),
			Metadata: getString(props, "metadata", "api-key"),
		}
	}

	// OAuth2 config
	if cfg.Type == "oauth2" || cfg.Type == "client_credentials" {
		cfg.OAuth2 = &OAuth2Config{
			TokenURL:     getString(props, "token_url", ""),
			ClientID:     getString(props, "client_id", ""),
			ClientSecret: getString(props, "client_secret", ""),
		}

		// Scopes
		if scopes, ok := props["scopes"].([]interface{}); ok {
			for _, s := range scopes {
				if str, ok := s.(string); ok {
					cfg.OAuth2.Scopes = append(cfg.OAuth2.Scopes, str)
				}
			}
		}
	}

	return cfg
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
