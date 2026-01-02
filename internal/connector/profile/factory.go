package profile

import (
	"context"
	"fmt"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates ProfiledConnector instances.
type Factory struct {
	// registry is used to create underlying connectors for each profile
	registry *connector.Registry
}

// NewFactory creates a new profile factory.
// The registry is used to create underlying connectors for each profile.
func NewFactory(registry *connector.Registry) *Factory {
	return &Factory{
		registry: registry,
	}
}

// Supports returns true for "profiled" type connectors.
func (f *Factory) Supports(connectorType, driver string) bool {
	return connectorType == "profiled"
}

// Create creates a new ProfiledConnector.
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	// Get profile config from properties
	profileConfig, ok := config.Properties["_profiles"].(*Config)
	if !ok {
		return nil, fmt.Errorf("profiled connector %s missing profile configuration", config.Name)
	}

	// Create the profiled connector with a factory function that uses our registry
	conn, err := New(config.Name, profileConfig, func(cfg *connector.Config) (connector.Connector, error) {
		return f.registry.Create(ctx, cfg)
	})
	if err != nil {
		return nil, fmt.Errorf("creating profiled connector %s: %w", config.Name, err)
	}

	return conn, nil
}
