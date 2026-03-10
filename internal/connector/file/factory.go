package file

import (
	"context"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
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

	// Parse CSV options from connector config
	if d := getString(config.Properties, "csv_delimiter", ""); d != "" {
		switch d {
		case "\\t", "\t", "tab":
			cfg.CSV.Delimiter = '\t'
		case ";", "semicolon":
			cfg.CSV.Delimiter = ';'
		case "|", "pipe":
			cfg.CSV.Delimiter = '|'
		default:
			if len(d) > 0 {
				cfg.CSV.Delimiter = rune(d[0])
			}
		}
	}
	if c := getString(config.Properties, "csv_comment", ""); c != "" && len(c) > 0 {
		cfg.CSV.Comment = rune(c[0])
	}
	cfg.CSV.NoHeader = getBool(config.Properties, "csv_no_header", false)
	cfg.CSV.TrimSpace = getBool(config.Properties, "csv_trim_space", false)
	cfg.CSV.SkipRows = getInt(config.Properties, "csv_skip_rows", 0)

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
