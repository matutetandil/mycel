package elasticsearch

import (
	"context"
	"log/slog"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Config holds Elasticsearch connector configuration.
type Config struct {
	Nodes    []string
	Username string
	Password string
	Index    string
	Timeout  time.Duration
}

// Factory creates Elasticsearch connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new Elasticsearch connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "elasticsearch"
}

// Create creates a new Elasticsearch connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	config := &Config{
		Username: cfg.GetString("username"),
		Password: cfg.GetString("password"),
		Index:    cfg.GetString("index"),
		Timeout:  parseDuration(cfg.Properties, "timeout", 30*time.Second),
	}

	// Parse nodes
	config.Nodes = parseNodes(cfg.Properties)
	if len(config.Nodes) == 0 {
		config.Nodes = []string{"http://localhost:9200"}
	}

	return New(cfg.Name, config, f.logger), nil
}

// parseNodes extracts the list of ES cluster nodes from properties.
func parseNodes(props map[string]interface{}) []string {
	switch v := props["nodes"].(type) {
	case []interface{}:
		nodes := make([]string, 0, len(v))
		for _, n := range v {
			if s, ok := n.(string); ok {
				nodes = append(nodes, s)
			}
		}
		return nodes
	case string:
		return []string{v}
	}

	// Fall back to single "url" or "host" property
	if url, ok := props["url"].(string); ok {
		return []string{url}
	}

	return nil
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
