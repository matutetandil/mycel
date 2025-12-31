package file

import (
	"context"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

// Factory creates file connectors.
type Factory struct{}

// NewFactory creates a new file connector factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "file"
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "file"
}

// Create creates a new file connector based on configuration.
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	cfg := &Config{
		BasePath:    getString(config.Properties, "base_path", ""),
		Format:      getString(config.Properties, "format", "json"),
		Watch:       getBool(config.Properties, "watch", false),
		CreateDirs:  getBool(config.Properties, "create_dirs", true),
		Permissions: uint32(getInt(config.Properties, "permissions", 0644)),
	}

	if interval := getString(config.Properties, "watch_interval", ""); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cfg.WatchInterval = d
		}
	}

	return New(config.Name, cfg), nil
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
