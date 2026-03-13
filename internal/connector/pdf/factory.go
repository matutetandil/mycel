package pdf

import (
	"context"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates PDF connectors.
type Factory struct{}

// NewFactory creates a new PDF connector factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "pdf"
}

// Create creates a new PDF connector based on configuration.
func (f *Factory) Create(_ context.Context, config *connector.Config) (connector.Connector, error) {
	cfg := &Config{
		OutputDir:   getString(config.Properties, "output_dir", ""),
		PageSize:    getString(config.Properties, "page_size", "A4"),
		Font:        getString(config.Properties, "font", "Helvetica"),
		MarginLeft:  getFloat(config.Properties, "margin_left", 15),
		MarginTop:   getFloat(config.Properties, "margin_top", 15),
		MarginRight: getFloat(config.Properties, "margin_right", 15),
	}

	return New(config.Name, cfg), nil
}

func getString(props map[string]interface{}, key, defaultVal string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

func getFloat(props map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := props[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return defaultVal
}
