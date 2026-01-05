// Package plugin provides the plugin system for extending Mycel with custom connectors.
package plugin

// PluginDeclaration represents a plugin declaration in plugins.hcl.
// This is what users write to specify which plugins to load.
type PluginDeclaration struct {
	// Name is the plugin name (used as connector type).
	Name string

	// Source is the plugin location:
	// - Local path: "./plugins/salesforce"
	// - Git URL: "github.com/acme/mycel-sap"
	// - Registry: "registry.mycel.dev/stripe" (future)
	Source string

	// Version constraint for git/registry sources.
	// Examples: "1.0.0", "~> 2.0", ">= 1.0, < 2.0"
	Version string
}

// PluginManifest represents the plugin.hcl manifest file.
// This is what plugin authors create to describe their plugin.
type PluginManifest struct {
	// Plugin metadata
	Name        string
	Version     string
	Description string
	Author      string
	License     string

	// Provides section - what the plugin provides
	Provides *ProvidesConfig
}

// ProvidesConfig specifies what a plugin provides.
type ProvidesConfig struct {
	// Connectors provided by this plugin.
	Connectors []*ConnectorProvide

	// Functions provided by this plugin (optional).
	Functions *FunctionsProvide
}

// ConnectorProvide describes a connector provided by a plugin.
type ConnectorProvide struct {
	// Name is the connector type name (e.g., "salesforce").
	Name string

	// WASM is the path to the connector.wasm file (relative to plugin dir).
	WASM string

	// ConfigSchema describes the configuration fields for this connector.
	ConfigSchema map[string]*ConfigField
}

// ConfigField describes a configuration field for a plugin connector.
type ConfigField struct {
	// Type is the field type: "string", "number", "bool".
	Type string

	// Required indicates if the field must be provided.
	Required bool

	// Default is the default value if not provided.
	Default interface{}

	// Sensitive indicates the field contains sensitive data (masked in logs).
	Sensitive bool

	// Description provides documentation for the field.
	Description string
}

// FunctionsProvide describes functions provided by a plugin.
type FunctionsProvide struct {
	// WASM is the path to the functions.wasm file (relative to plugin dir).
	WASM string

	// Exports is the list of function names to export.
	Exports []string
}

// LoadedPlugin represents a fully loaded and initialized plugin.
type LoadedPlugin struct {
	// Declaration is the original plugin declaration.
	Declaration *PluginDeclaration

	// Manifest is the parsed plugin manifest.
	Manifest *PluginManifest

	// Path is the absolute path to the plugin directory.
	Path string

	// Connectors are the WASM connector instances.
	Connectors map[string]*WASMConnector

	// Functions registry for this plugin (if any).
	FunctionsExports []string
}

// PluginConfig holds plugin configuration from the main config.
type PluginConfig struct {
	// Plugins is the list of plugin declarations.
	Plugins []*PluginDeclaration
}
