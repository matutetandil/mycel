package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates MQTT connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new MQTT connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the specified connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "mqtt"
}

// Create creates a new MQTT connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	config := DefaultConfig()

	// Connection settings
	config.Broker = getString(cfg.Properties, "broker", config.Broker)
	config.ClientID = getString(cfg.Properties, "client_id", cfg.Name)
	config.Username = getString(cfg.Properties, "username", "")
	config.Password = getString(cfg.Properties, "password", "")
	config.Topic = getString(cfg.Properties, "topic", "")

	// QoS
	qos := getInt(cfg.Properties, "qos", int(config.QoS))
	if qos < 0 || qos > 2 {
		return nil, fmt.Errorf("invalid QoS level: %d (must be 0, 1, or 2)", qos)
	}
	config.QoS = byte(qos)

	// Session and keep-alive
	config.CleanSession = getBool(cfg.Properties, "clean_session", config.CleanSession)
	config.KeepAlive = getDuration(cfg.Properties, "keep_alive", config.KeepAlive)
	config.ConnectTimeout = getDuration(cfg.Properties, "connect_timeout", config.ConnectTimeout)
	config.AutoReconnect = getBool(cfg.Properties, "auto_reconnect", config.AutoReconnect)
	config.MaxReconnectInterval = getDuration(cfg.Properties, "max_reconnect_interval", config.MaxReconnectInterval)

	// TLS configuration
	if tlsCfg := getMap(cfg.Properties, "tls"); tlsCfg != nil {
		if getBool(tlsCfg, "enabled", false) {
			config.TLS = &TLSConfig{
				Enabled:            true,
				CertFile:           getString(tlsCfg, "cert", ""),
				KeyFile:            getString(tlsCfg, "key", ""),
				CAFile:             getString(tlsCfg, "ca_cert", ""),
				InsecureSkipVerify: getBool(tlsCfg, "insecure_skip_verify", false),
			}
		}
	}

	return NewConnector(cfg.Name, config, f.logger)
}

// Helper functions for extracting configuration values.

func getString(props map[string]interface{}, key, defaultVal string) string {
	if props == nil {
		return defaultVal
	}
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getInt(props map[string]interface{}, key string, defaultVal int) int {
	if props == nil {
		return defaultVal
	}
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
	if props == nil {
		return defaultVal
	}
	if v, ok := props[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

func getDuration(props map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
	if props == nil {
		return defaultVal
	}
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
	if props == nil {
		return nil
	}
	if v, ok := props[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}
