package rest

import (
	"context"
	"log/slog"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates REST connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new REST connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "rest"
}

// Create creates a new REST connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	port := cfg.GetInt("port")
	if port == 0 {
		port = 3000 // default port
	}

	var cors *CORSConfig
	if corsMap := cfg.GetMap("cors"); corsMap != nil {
		cors = &CORSConfig{}

		if origins, ok := corsMap["origins"].([]interface{}); ok {
			for _, o := range origins {
				if s, ok := o.(string); ok {
					cors.Origins = append(cors.Origins, s)
				}
			}
		}

		if methods, ok := corsMap["methods"].([]interface{}); ok {
			for _, m := range methods {
				if s, ok := m.(string); ok {
					cors.Methods = append(cors.Methods, s)
				}
			}
		}

		if headers, ok := corsMap["headers"].([]interface{}); ok {
			for _, h := range headers {
				if s, ok := h.(string); ok {
					cors.Headers = append(cors.Headers, s)
				}
			}
		}
	}

	return New(cfg.Name, port, cors, f.logger), nil
}
