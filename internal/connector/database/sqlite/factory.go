package sqlite

import (
	"context"
	"log/slog"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates SQLite connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new SQLite connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "database" && driver == "sqlite"
}

// Create creates a new SQLite connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	path := cfg.GetString("database")
	if path == "" {
		path = "./data/mycel.db" // default path
	}

	return New(cfg.Name, path, f.logger), nil
}
