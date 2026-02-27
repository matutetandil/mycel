package websocket

import (
	"context"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Config holds WebSocket connector configuration.
type Config struct {
	Port         int
	Host         string
	Path         string
	PingInterval time.Duration
	PongTimeout  time.Duration
}

// Factory creates WebSocket connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new WebSocket connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "websocket"
}

// Create creates a new WebSocket connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	config := &Config{
		Port:         cfg.GetInt("port"),
		Host:         cfg.GetString("host"),
		Path:         cfg.GetString("path"),
		PingInterval: parseDuration(cfg.Properties, "ping_interval", 30*time.Second),
		PongTimeout:  parseDuration(cfg.Properties, "pong_timeout", 10*time.Second),
	}

	if config.Port == 0 {
		config.Port = 3001
	}
	if config.Host == "" {
		config.Host = "0.0.0.0"
	}
	if config.Path == "" {
		config.Path = "/ws"
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
