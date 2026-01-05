package plugin

import (
	"context"
	"fmt"

	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates connectors from plugins.
type Factory struct {
	registry *Registry
}

// NewFactory creates a new plugin connector factory.
func NewFactory(registry *Registry) *Factory {
	return &Factory{
		registry: registry,
	}
}

// Supports returns true if this factory can create the given connector type.
// Returns true for type="plugin" or for any type that matches a loaded plugin connector.
func (f *Factory) Supports(connectorType, driver string) bool {
	// Support generic "plugin" type
	if connectorType == "plugin" {
		return true
	}

	// Check if this type matches any loaded plugin connector
	connectorTypes := f.registry.GetConnectorTypes()
	_, ok := connectorTypes[connectorType]
	return ok
}

// Create creates a new plugin connector instance.
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	var pluginName, connectorType string

	// Check if the connector type itself matches a plugin connector type
	connectorTypes := f.registry.GetConnectorTypes()
	if pn, ok := connectorTypes[config.Type]; ok {
		// Type directly matches a plugin connector
		pluginName = pn
		connectorType = config.Type
	} else if config.Type == "plugin" {
		// Generic plugin type - get plugin name from driver or property
		pluginName = config.Driver
		if pluginName == "" {
			if pn, ok := config.Properties["plugin"].(string); ok {
				pluginName = pn
			}
		}
		if pluginName == "" {
			return nil, fmt.Errorf("plugin name is required (use driver or plugin property)")
		}

		// Get the connector type from the plugin (defaults to plugin name)
		connectorType = pluginName
		if ct, ok := config.Properties["connector_type"].(string); ok {
			connectorType = ct
		}
	} else {
		return nil, fmt.Errorf("unknown plugin connector type: %s", config.Type)
	}

	// Get plugin-specific configuration from properties
	pluginConfig := make(map[string]interface{})
	for k, v := range config.Properties {
		pluginConfig[k] = v
	}

	// Create connector instance from plugin
	conn, err := f.registry.CreateConnectorInstance(pluginName, connectorType, config.Name, pluginConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin connector: %w", err)
	}

	return conn, nil
}

// Ensure Factory implements connector.Factory interface.
var _ connector.Factory = (*Factory)(nil)
