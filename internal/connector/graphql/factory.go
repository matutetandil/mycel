package graphql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/envdefaults"
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
	// Environment-aware playground default: enabled in dev/staging, disabled in production
	defaults := envdefaults.ForEnvironment(cfg.Environment)

	config := &ServerConfig{
		Port:          getInt(cfg.Properties, "port", 4000),
		Host:          getString(cfg.Properties, "host", "0.0.0.0"),
		Endpoint:      getString(cfg.Properties, "endpoint", "/graphql"),
		Playground:    getBool(cfg.Properties, "playground", defaults.Playground),
		Introspection: getBool(cfg.Properties, "introspection", defaults.Introspection),
	}

	// Parse playground path
	if path := getString(cfg.Properties, "playground_path", ""); path != "" {
		config.PlaygroundPath = path
	}

	// Parse schema configuration.
	//
	// auto_generate was stored and read by nothing: where the schema comes
	// from was decided entirely by whether a path was given. So the two ways of
	// writing a server with no schema at all — no path and auto_generate = false
	// — started happily and answered every query with an empty schema, which
	// reads as a service that is up and knows nothing.
	if schemaCfg := getMap(cfg.Properties, "schema"); schemaCfg != nil {
		hasAutoGenerate := false
		if _, written := schemaCfg["auto_generate"]; written {
			hasAutoGenerate = true
		}
		config.Schema = SchemaConfig{
			Path:         getString(schemaCfg, "path", ""),
			AutoGenerate: getBool(schemaCfg, "auto_generate", true),
		}

		if config.Schema.Path != "" && hasAutoGenerate && config.Schema.AutoGenerate {
			return nil, fmt.Errorf(
				"graphql schema names a path and asks for auto_generate; the schema comes from one or the other, not both")
		}
		if config.Schema.Path == "" && !config.Schema.AutoGenerate {
			return nil, fmt.Errorf(
				"graphql schema has no path and auto_generate is false, so there is nothing to build a schema from")
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

	// Parse Subscriptions configuration
	if subsCfg := getMap(cfg.Properties, "subscriptions"); subsCfg != nil {
		config.Subscriptions = &SubscriptionsConfig{
			Enabled:           getBool(subsCfg, "enabled", true),
			Path:              getString(subsCfg, "path", "/subscriptions"),
			KeepAliveInterval: getDuration(subsCfg, "keep_alive_interval", 30*time.Second),
			ConnectionTimeout: getDuration(subsCfg, "connection_timeout", 60*time.Second),
		}
	}

	server := NewServer(cfg.Name, config, f.logger)
	server.environment = cfg.Environment
	return server, nil
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

	// Parse subscriptions configuration
	if subsCfg := getMap(cfg.Properties, "subscriptions"); subsCfg != nil {
		config.Subscriptions = &SubscriptionsConfig{
			Enabled:           getBool(subsCfg, "enabled", true),
			Path:              getString(subsCfg, "path", ""),
			KeepAliveInterval: getDuration(subsCfg, "keep_alive_interval", 30*time.Second),
			ConnectionTimeout: getDuration(subsCfg, "connection_timeout", 60*time.Second),
		}
	}

	return NewClient(cfg.Name, config, f.logger), nil
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

// getBool reads a switch that may have been written as a word: env() hands back
// strings, so a spelt-out "false" has to mean false rather than falling through
// to the default.
func getBool(props map[string]interface{}, key string, defaultVal bool) bool {
	return connector.BoolFromProps(props, key, defaultVal)
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
