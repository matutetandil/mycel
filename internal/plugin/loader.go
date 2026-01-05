package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/internal/functions"
	myhcl "github.com/matutetandil/mycel/pkg/hcl"
)

// Loader handles loading plugins from various sources.
type Loader struct {
	// BaseDir is the base directory for resolving relative paths.
	BaseDir string

	// CacheDir is where downloaded plugins are stored.
	CacheDir string
}

// NewLoader creates a new plugin loader.
func NewLoader(baseDir string) *Loader {
	cacheDir := os.Getenv("MYCEL_PLUGIN_CACHE")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "mycel", "plugins")
	}

	return &Loader{
		BaseDir:  baseDir,
		CacheDir: cacheDir,
	}
}

// Load loads a plugin from its declaration.
func (l *Loader) Load(ctx context.Context, decl *PluginDeclaration) (*LoadedPlugin, error) {
	// Determine plugin path
	pluginPath, err := l.resolvePluginPath(decl)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve plugin path: %w", err)
	}

	// Parse plugin manifest
	manifest, err := l.parseManifest(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plugin manifest: %w", err)
	}

	// Create loaded plugin
	loaded := &LoadedPlugin{
		Declaration: decl,
		Manifest:    manifest,
		Path:        pluginPath,
		Connectors:  make(map[string]*WASMConnector),
	}

	// Load connectors
	if manifest.Provides != nil {
		for _, connProvide := range manifest.Provides.Connectors {
			wasmPath := filepath.Join(pluginPath, connProvide.WASM)
			if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
				return nil, fmt.Errorf("connector WASM not found: %s", wasmPath)
			}

			// Create connector (will be initialized when used)
			conn, err := NewWASMConnector(
				connProvide.Name,
				connProvide.Name,
				wasmPath,
				nil, // Config will be set when connector is instantiated
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create connector %s: %w", connProvide.Name, err)
			}

			loaded.Connectors[connProvide.Name] = conn
		}

		// Record functions exports (will be loaded by runtime)
		if manifest.Provides.Functions != nil {
			loaded.FunctionsExports = manifest.Provides.Functions.Exports
		}
	}

	return loaded, nil
}

// resolvePluginPath resolves the actual path to a plugin.
func (l *Loader) resolvePluginPath(decl *PluginDeclaration) (string, error) {
	source := decl.Source

	// Local path
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "../") {
		absPath := source
		if !filepath.IsAbs(source) {
			absPath = filepath.Join(l.BaseDir, source)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return "", fmt.Errorf("plugin path not found: %s", absPath)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("plugin path is not a directory: %s", absPath)
		}

		return absPath, nil
	}

	// Git source (github.com/...)
	if strings.HasPrefix(source, "github.com/") || strings.HasPrefix(source, "gitlab.com/") {
		return "", fmt.Errorf("git plugin sources not yet implemented: %s", source)
	}

	// Registry source
	if strings.HasPrefix(source, "registry.mycel.dev/") {
		return "", fmt.Errorf("registry plugin sources not yet implemented: %s", source)
	}

	return "", fmt.Errorf("unknown plugin source format: %s", source)
}

// parseManifest parses the plugin.hcl manifest file.
func (l *Loader) parseManifest(pluginPath string) (*PluginManifest, error) {
	manifestPath := filepath.Join(pluginPath, "plugin.hcl")

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(manifestPath)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse plugin.hcl: %s", diags.Error())
	}

	ctx := &hcl.EvalContext{
		Functions: myhcl.Functions(),
	}

	// Define schema
	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "plugin"},
			{Type: "provides"},
		},
	}

	content, diags := file.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("manifest content error: %s", diags.Error())
	}

	manifest := &PluginManifest{}

	for _, block := range content.Blocks {
		switch block.Type {
		case "plugin":
			if err := l.parsePluginBlock(block, ctx, manifest); err != nil {
				return nil, err
			}
		case "provides":
			provides, err := l.parseProvidesBlock(block, ctx)
			if err != nil {
				return nil, err
			}
			manifest.Provides = provides
		}
	}

	return manifest, nil
}

// parsePluginBlock parses the plugin {} block.
func (l *Loader) parsePluginBlock(block *hcl.Block, ctx *hcl.EvalContext, manifest *PluginManifest) error {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "name", Required: true},
			{Name: "version", Required: true},
			{Name: "description"},
			{Name: "author"},
			{Name: "license"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return fmt.Errorf("plugin block error: %s", diags.Error())
	}

	if attr, ok := content.Attributes["name"]; ok {
		val, _ := attr.Expr.Value(ctx)
		manifest.Name = val.AsString()
	}

	if attr, ok := content.Attributes["version"]; ok {
		val, _ := attr.Expr.Value(ctx)
		manifest.Version = val.AsString()
	}

	if attr, ok := content.Attributes["description"]; ok {
		val, _ := attr.Expr.Value(ctx)
		manifest.Description = val.AsString()
	}

	if attr, ok := content.Attributes["author"]; ok {
		val, _ := attr.Expr.Value(ctx)
		manifest.Author = val.AsString()
	}

	if attr, ok := content.Attributes["license"]; ok {
		val, _ := attr.Expr.Value(ctx)
		manifest.License = val.AsString()
	}

	return nil
}

// parseProvidesBlock parses the provides {} block.
func (l *Loader) parseProvidesBlock(block *hcl.Block, ctx *hcl.EvalContext) (*ProvidesConfig, error) {
	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "connector", LabelNames: []string{"name"}},
			{Type: "functions"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("provides block error: %s", diags.Error())
	}

	provides := &ProvidesConfig{
		Connectors: make([]*ConnectorProvide, 0),
	}

	for _, b := range content.Blocks {
		switch b.Type {
		case "connector":
			conn, err := l.parseConnectorProvide(b, ctx)
			if err != nil {
				return nil, err
			}
			provides.Connectors = append(provides.Connectors, conn)

		case "functions":
			fn, err := l.parseFunctionsProvide(b, ctx)
			if err != nil {
				return nil, err
			}
			provides.Functions = fn
		}
	}

	return provides, nil
}

// parseConnectorProvide parses a connector {} block in provides.
func (l *Loader) parseConnectorProvide(block *hcl.Block, ctx *hcl.EvalContext) (*ConnectorProvide, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("connector block requires a name label")
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "wasm", Required: true},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "config"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("connector provide block error: %s", diags.Error())
	}

	conn := &ConnectorProvide{
		Name:         block.Labels[0],
		ConfigSchema: make(map[string]*ConfigField),
	}

	if attr, ok := content.Attributes["wasm"]; ok {
		val, _ := attr.Expr.Value(ctx)
		conn.WASM = val.AsString()
	}

	// Parse config schema
	for _, b := range content.Blocks {
		if b.Type == "config" {
			schema, err := l.parseConfigSchema(b, ctx)
			if err != nil {
				return nil, err
			}
			conn.ConfigSchema = schema
		}
	}

	return conn, nil
}

// parseConfigSchema parses a config {} block defining the connector's config schema.
func (l *Loader) parseConfigSchema(block *hcl.Block, ctx *hcl.EvalContext) (map[string]*ConfigField, error) {
	// Get all attributes dynamically
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("config schema error: %s", diags.Error())
	}

	schema := make(map[string]*ConfigField)

	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			continue
		}

		field := &ConfigField{
			Type: "string", // default
		}

		// Check if it's a simple type or a complex definition
		if val.Type() == cty.String {
			field.Type = val.AsString()
		} else if val.Type().IsObjectType() {
			// Complex definition like: string { required = true }
			valMap := val.AsValueMap()
			if t, ok := valMap["type"]; ok {
				field.Type = t.AsString()
			}
			if r, ok := valMap["required"]; ok {
				field.Required = r.True()
			}
			if d, ok := valMap["default"]; ok && !d.IsNull() {
				field.Default = ctyToGo(d)
			}
			if s, ok := valMap["sensitive"]; ok {
				field.Sensitive = s.True()
			}
			if desc, ok := valMap["description"]; ok {
				field.Description = desc.AsString()
			}
		}

		schema[name] = field
	}

	return schema, nil
}

// parseFunctionsProvide parses a functions {} block in provides.
func (l *Loader) parseFunctionsProvide(block *hcl.Block, ctx *hcl.EvalContext) (*FunctionsProvide, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "wasm", Required: true},
			{Name: "exports", Required: true},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("functions provide block error: %s", diags.Error())
	}

	fn := &FunctionsProvide{}

	if attr, ok := content.Attributes["wasm"]; ok {
		val, _ := attr.Expr.Value(ctx)
		fn.WASM = val.AsString()
	}

	if attr, ok := content.Attributes["exports"]; ok {
		val, _ := attr.Expr.Value(ctx)
		exports := []string{}
		for _, v := range val.AsValueSlice() {
			exports = append(exports, v.AsString())
		}
		fn.Exports = exports
	}

	return fn, nil
}

// ctyToGo converts a cty.Value to a Go value.
func ctyToGo(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}
	switch val.Type() {
	case cty.String:
		return val.AsString()
	case cty.Number:
		f, _ := val.AsBigFloat().Float64()
		return f
	case cty.Bool:
		return val.True()
	default:
		return nil
	}
}

// GetFunctionsConfig returns a functions.Config for the plugin's functions (if any).
func (l *Loader) GetFunctionsConfig(loaded *LoadedPlugin) *functions.Config {
	if loaded.Manifest.Provides == nil || loaded.Manifest.Provides.Functions == nil {
		return nil
	}

	fn := loaded.Manifest.Provides.Functions
	return &functions.Config{
		Name:    loaded.Manifest.Name + "_functions",
		WASM:    filepath.Join(loaded.Path, fn.WASM),
		Exports: fn.Exports,
	}
}
