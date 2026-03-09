package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/matutetandil/mycel/internal/connector"
)

// Registry manages loaded plugins and their connectors.
type Registry struct {
	loader  *Loader
	plugins map[string]*LoadedPlugin
	mu      sync.RWMutex
}

// NewRegistry creates a new plugin registry.
func NewRegistry(baseDir string) *Registry {
	return NewRegistryWithLogger(baseDir, nil)
}

// NewRegistryWithLogger creates a new plugin registry with a logger.
func NewRegistryWithLogger(baseDir string, logger *slog.Logger) *Registry {
	return &Registry{
		loader:  NewLoaderWithLogger(baseDir, logger),
		plugins: make(map[string]*LoadedPlugin),
	}
}

// Load loads a plugin from its declaration.
func (r *Registry) Load(ctx context.Context, decl *PluginDeclaration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already loaded
	if _, exists := r.plugins[decl.Name]; exists {
		return nil // Already loaded
	}

	// Load the plugin
	loaded, err := r.loader.Load(ctx, decl)
	if err != nil {
		return fmt.Errorf("failed to load plugin %s: %w", decl.Name, err)
	}

	r.plugins[decl.Name] = loaded
	return nil
}

// LoadAll loads all plugins from their declarations.
// After loading, it saves the lock file with resolved versions.
func (r *Registry) LoadAll(ctx context.Context, decls []*PluginDeclaration) error {
	for _, decl := range decls {
		if err := r.Load(ctx, decl); err != nil {
			return err
		}
	}

	// Save lock file with resolved versions
	if err := r.loader.SaveLockFile(); err != nil {
		return fmt.Errorf("failed to save plugins.lock: %w", err)
	}

	return nil
}

// GetPlugin returns a loaded plugin by name.
func (r *Registry) GetPlugin(name string) (*LoadedPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// GetConnector returns a connector from a loaded plugin.
// The connectorType should match the connector name as defined in the plugin manifest.
func (r *Registry) GetConnector(pluginName, connectorType string) (*WASMConnector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, ok := r.plugins[pluginName]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found", pluginName)
	}

	conn, ok := plugin.Connectors[connectorType]
	if !ok {
		return nil, fmt.Errorf("connector %q not found in plugin %q", connectorType, pluginName)
	}

	return conn, nil
}

// CreateConnectorInstance creates a new connector instance from a plugin.
// This clones the connector with the provided configuration.
func (r *Registry) CreateConnectorInstance(pluginName, connectorType, instanceName string, config map[string]interface{}) (connector.Connector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, ok := r.plugins[pluginName]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found", pluginName)
	}

	// Find the connector template
	template, ok := plugin.Connectors[connectorType]
	if !ok {
		return nil, fmt.Errorf("connector %q not found in plugin %q", connectorType, pluginName)
	}

	// Create a new instance with the provided config
	return NewWASMConnector(instanceName, connectorType, template.wasmPath, config)
}

// GetConnectorTypes returns all connector types provided by loaded plugins.
func (r *Registry) GetConnectorTypes() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make(map[string]string) // connectorType -> pluginName
	for pluginName, plugin := range r.plugins {
		for connName := range plugin.Connectors {
			types[connName] = pluginName
		}
	}
	return types
}

// GetFunctionsConfigs returns all functions configurations from loaded plugins.
func (r *Registry) GetFunctionsConfigs() map[string]*LoadedPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*LoadedPlugin)
	for name, plugin := range r.plugins {
		if plugin.FunctionsExports != nil && len(plugin.FunctionsExports) > 0 {
			result[name] = plugin
		}
	}
	return result
}

// Close closes all loaded plugins.
func (r *Registry) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for _, plugin := range r.plugins {
		for _, conn := range plugin.Connectors {
			if err := conn.Close(ctx); err != nil {
				lastErr = err
			}
		}
	}

	r.plugins = make(map[string]*LoadedPlugin)
	return lastErr
}

// Plugins returns all loaded plugins.
func (r *Registry) Plugins() map[string]*LoadedPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*LoadedPlugin)
	for k, v := range r.plugins {
		result[k] = v
	}
	return result
}

// Loader returns the plugin loader.
func (r *Registry) Loader() *Loader {
	return r.loader
}
