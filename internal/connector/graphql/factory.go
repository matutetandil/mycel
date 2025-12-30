package graphql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

// Factory creates GraphQL connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new GraphQL connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the specified connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "graphql"
}

// Create creates a new GraphQL connector from configuration.
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
		return nil, fmt.Errorf("unknown GraphQL driver: %s (expected 'server' or 'client')", driver)
	}
}

// createServer creates a GraphQL server connector.
func (f *Factory) createServer(cfg *connector.Config) (*ServerConnector, error) {
	config := &ServerConfig{
		Port:       getInt(cfg.Properties, "port", 4000),
		Host:       getString(cfg.Properties, "host", "0.0.0.0"),
		Endpoint:   getString(cfg.Properties, "endpoint", "/graphql"),
		Playground: getBool(cfg.Properties, "playground", true),
	}

	// Parse playground path
	if path := getString(cfg.Properties, "playground_path", ""); path != "" {
		config.PlaygroundPath = path
	}

	// Parse schema configuration
	if schemaCfg := getMap(cfg.Properties, "schema"); schemaCfg != nil {
		config.Schema = SchemaConfig{
			Path:         getString(schemaCfg, "path", ""),
			AutoGenerate: getBool(schemaCfg, "auto_generate", false),
		}
	}

	// Parse CORS configuration
	if corsCfg := getMap(cfg.Properties, "cors"); corsCfg != nil {
		config.CORS = &CORSConfig{
			Origins:          getStringSlice(corsCfg, "origins", []string{"*"}),
			Methods:          getStringSlice(corsCfg, "methods", []string{"GET", "POST", "OPTIONS"}),
			Headers:          getStringSlice(corsCfg, "headers", []string{"Content-Type", "Authorization"}),
			AllowCredentials: getBool(corsCfg, "allow_credentials", false),
		}
	}

	// Parse Federation configuration
	if fedCfg := getMap(cfg.Properties, "federation"); fedCfg != nil {
		config.Federation = &FederationServerConfig{
			Enabled: getBool(fedCfg, "enabled", true),
			Version: getInt(fedCfg, "version", 2),
		}
	}

	return NewServer(cfg.Name, config, f.logger), nil
}

// createClient creates a GraphQL client connector.
func (f *Factory) createClient(cfg *connector.Config) (*ClientConnector, error) {
	endpoint := getString(cfg.Properties, "endpoint", "")
	if endpoint == "" {
		return nil, fmt.Errorf("GraphQL client requires 'endpoint' property")
	}

	config := &ClientConfig{
		Endpoint:   endpoint,
		Timeout:    getDuration(cfg.Properties, "timeout", 30*time.Second),
		RetryCount: getInt(cfg.Properties, "retry_count", 1),
		RetryDelay: getDuration(cfg.Properties, "retry_delay", time.Second),
		Headers:    make(map[string]string),
	}

	// Parse headers
	if headers := getMap(cfg.Properties, "headers"); headers != nil {
		for k, v := range headers {
			if s, ok := v.(string); ok {
				config.Headers[k] = s
			}
		}
	}

	// Parse auth configuration
	if authCfg := getMap(cfg.Properties, "auth"); authCfg != nil {
		config.Auth = &AuthConfig{
			Type:         getString(authCfg, "type", ""),
			Token:        getString(authCfg, "token", ""),
			APIKey:       getString(authCfg, "api_key", ""),
			APIKeyHeader: getString(authCfg, "api_key_header", "X-API-Key"),
			Username:     getString(authCfg, "username", ""),
			Password:     getString(authCfg, "password", ""),
			ClientID:     getString(authCfg, "client_id", ""),
			ClientSecret: getString(authCfg, "client_secret", ""),
			TokenURL:     getString(authCfg, "token_url", ""),
			Scopes:       getStringSlice(authCfg, "scopes", nil),
		}
	}

	return NewClient(cfg.Name, config), nil
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
		case int64:
			return time.Duration(d) * time.Millisecond
		case float64:
			return time.Duration(d) * time.Millisecond
		}
	}
	return defaultVal
}

func getMap(props map[string]interface{}, key string) map[string]interface{} {
	if v, ok := props[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

func getStringSlice(props map[string]interface{}, key string, defaultVal []string) []string {
	if v, ok := props[key]; ok {
		switch s := v.(type) {
		case []string:
			return s
		case []interface{}:
			result := make([]string, 0, len(s))
			for _, item := range s {
				if str, ok := item.(string); ok {
					result = append(result, str)
				}
			}
			return result
		}
	}
	return defaultVal
}
