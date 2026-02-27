package cdc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates CDC connectors.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new CDC factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true for "cdc" connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "cdc"
}

// Create builds a CDC connector from HCL configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	config := &Config{
		Driver:      cfg.Driver,
		Host:        cfg.GetString("host"),
		Port:        cfg.GetInt("port"),
		Database:    cfg.GetString("database"),
		User:        cfg.GetString("user"),
		Password:    cfg.GetString("password"),
		SSLMode:     cfg.GetString("sslmode"),
		SlotName:    cfg.GetString("slot_name"),
		Publication: cfg.GetString("publication"),
	}

	// Defaults
	if config.Driver == "" {
		config.Driver = "postgres"
	}
	if config.Port == 0 {
		config.Port = 5432
	}
	if config.SSLMode == "" {
		config.SSLMode = "prefer"
	}
	if config.SlotName == "" {
		config.SlotName = "mycel_cdc"
	}
	if config.Publication == "" {
		config.Publication = "mycel_pub"
	}

	// Create driver-specific listener
	var listener Listener
	switch config.Driver {
	case "postgres", "postgresql":
		listener = NewPostgresListener(config, f.logger)
	default:
		return nil, fmt.Errorf("unsupported CDC driver: %s (supported: postgres)", config.Driver)
	}

	return New(cfg.Name, config, listener, f.logger), nil
}
