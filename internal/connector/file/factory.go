package file

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
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
		Permissions: filePermissions(config.Properties, 0o644),
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
	return connector.IntFromProps(props, key, defaultVal)
}

// getBool reads a switch that may have been written as a word: env() hands back
// strings, so a spelt-out "false" has to mean false rather than falling through
// to the default.
func getBool(props map[string]interface{}, key string, defaultVal bool) bool {
	return connector.BoolFromProps(props, key, defaultVal)
}

// filePermissions reads the mode, which is octal.
//
// A file mode is written the way chmod takes it — 0644, or 644 — and it was
// read as a decimal number and handed to the operating system as one. So
// `permissions = "0644"`, which is what the documentation shows and what the
// files example wrote, created every file as mode 0o1204: --w----r--, which
// nothing can read back. The default in code was a Go octal literal and so was
// right, which is why this only bit configurations that set it.
func filePermissions(props map[string]interface{}, defaultMode uint32) uint32 {
	raw, ok := props["permissions"]
	if !ok || raw == nil {
		return defaultMode
	}

	var digits string
	switch v := raw.(type) {
	case string:
		digits = strings.TrimSpace(v)
	case int:
		digits = strconv.Itoa(v)
	case int64:
		digits = strconv.FormatInt(v, 10)
	case float64:
		digits = strconv.FormatInt(int64(v), 10)
	default:
		return defaultMode
	}
	if digits == "" {
		return defaultMode
	}

	mode, err := strconv.ParseUint(strings.TrimPrefix(digits, "0o"), 8, 32)
	if err != nil {
		return defaultMode
	}
	return uint32(mode)
}
