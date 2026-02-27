package sse

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Config holds SSE connector configuration.
type Config struct {
	Port              int
	Host              string
	Path              string
	HeartbeatInterval time.Duration
	CORSOrigins       []string
}

// Factory creates SSE connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new SSE connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "sse"
}

// Create creates a new SSE connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	config := &Config{
		Port:              cfg.GetInt("port"),
		Host:              cfg.GetString("host"),
		Path:              cfg.GetString("path"),
		HeartbeatInterval: parseDuration(cfg.Properties, "heartbeat_interval", 30*time.Second),
		CORSOrigins:       parseCORSOrigins(cfg.Properties),
	}

	if config.Port == 0 {
		config.Port = 3002
	}
	if config.Host == "" {
		config.Host = "0.0.0.0"
	}
	if config.Path == "" {
		config.Path = "/events"
	}

	return New(cfg.Name, config, f.logger), nil
}

// parseDuration extracts a duration from properties.
func parseDuration(props map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
	if v, ok := props[key]; ok {
		switch d := v.(type) {
		case string:
			if parsed, err := time.ParseDuration(d); err == nil {
				return parsed
			}
		case time.Duration:
			return d
		}
	}
	return defaultVal
}

// parseCORSOrigins extracts CORS origins from a nested cors block or string.
func parseCORSOrigins(props map[string]interface{}) []string {
	corsBlock, ok := props["cors"].(map[string]interface{})
	if !ok {
		return nil
	}

	switch origins := corsBlock["origins"].(type) {
	case []interface{}:
		result := make([]string, 0, len(origins))
		for _, o := range origins {
			if s, ok := o.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		return strings.Split(origins, ",")
	}
	return nil
}
