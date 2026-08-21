package sqlite

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/matutetandil/mycel/v2/internal/connector"
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
	// No invented default.
	//
	// An empty `database` used to become ./data/mycel.db, so a connector whose
	// attribute was misspelled — `path` instead of `database`, which the
	// integration-patterns guide showed — opened a file that has none of your
	// tables, and every request answered "no such table" for as long as the
	// service ran, with nothing pointing at the cause. The schema has always
	// said the attribute is required and `mycel migrate` has always refused
	// without it; only startup invented something.
	path := cfg.GetString("database")
	if path == "" {
		return nil, fmt.Errorf("sqlite connector %q has no database: set `database` to the file to use, for example database = \"./data/app.db\"", cfg.Name)
	}

	return New(cfg.Name, path, f.logger), nil
}
