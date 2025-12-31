package s3

import (
	"context"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates S3 connectors.
type Factory struct{}

// NewFactory creates a new S3 connector factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "s3"
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "s3"
}

// Create creates a new S3 connector based on configuration.
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	cfg := &Config{
		Bucket:       getString(config.Properties, "bucket", ""),
		Region:       getString(config.Properties, "region", ""),
		Endpoint:     getString(config.Properties, "endpoint", ""),
		AccessKey:    getString(config.Properties, "access_key", ""),
		SecretKey:    getString(config.Properties, "secret_key", ""),
		SessionToken: getString(config.Properties, "session_token", ""),
		Prefix:       getString(config.Properties, "prefix", ""),
		Format:       getString(config.Properties, "format", ""),
		UsePathStyle: getBool(config.Properties, "use_path_style", false),
	}

	if timeout := getString(config.Properties, "timeout", ""); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			cfg.Timeout = d
		}
	}

	conn := New(config.Name, cfg)
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

func getBool(props map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := props[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}
