package ftp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates FTP/SFTP connectors.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new FTP connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "ftp"
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "ftp"
}

// Create creates a new FTP/SFTP connector based on configuration.
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	cfg := DefaultConfig()

	cfg.Host = getString(config.Properties, "host", "")
	if cfg.Host == "" {
		return nil, fmt.Errorf("ftp connector requires 'host' property")
	}

	cfg.Username = getString(config.Properties, "username", "")
	cfg.Password = getString(config.Properties, "password", "")
	cfg.Protocol = getString(config.Properties, "protocol", "ftp")
	cfg.BasePath = getString(config.Properties, "base_path", "")
	cfg.KeyFile = getString(config.Properties, "key_file", "")
	cfg.Passive = getBool(config.Properties, "passive", true)
	cfg.TLS = getBool(config.Properties, "tls", false)

	if port := getInt(config.Properties, "port", 0); port > 0 {
		cfg.Port = port
	} else {
		// Set default port based on protocol
		if cfg.Protocol == "sftp" {
			cfg.Port = 22
		} else {
			cfg.Port = 21
		}
	}

	if timeout := getString(config.Properties, "timeout", ""); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			cfg.Timeout = d
		}
	}

	conn := New(config.Name, cfg, f.logger)
	if err := conn.Connect(ctx); err != nil {
		return nil, err
	}

	return conn, nil
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
