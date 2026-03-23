package plugin

import (
	"context"
	"fmt"
	"log/slog"
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

	// CacheDir is where downloaded plugins are stored (legacy, kept for compat).
	CacheDir string

	cache    *CacheManager
	git      *GitResolver
	lockFile *LockFile
	logger   *slog.Logger
}

// NewLoader creates a new plugin loader.
func NewLoader(baseDir string) *Loader {
	return NewLoaderWithLogger(baseDir, nil)
}

// NewLoaderWithLogger creates a new plugin loader with a logger.
func NewLoaderWithLogger(baseDir string, logger *slog.Logger) *Loader {
	if logger == nil {
		logger = slog.Default()
	}

	return &Loader{
		BaseDir:  baseDir,
		CacheDir: filepath.Join(baseDir, "mycel_plugins"),
		cache:    NewCacheManager(baseDir),
		git:      &GitResolver{Logger: logger},
		logger:   logger,
	}
}

// InitLockFile reads the existing lock file or creates a new one.
func (l *Loader) InitLockFile() error {
	lf, err := ReadLockFile(l.BaseDir)
	if err != nil {
		return err
	}
	if lf == nil {
		lf = NewLockFile()
	}
	l.lockFile = lf
	return nil
}

// SaveLockFile writes the current lock file to disk.
func (l *Loader) SaveLockFile() error {
	if l.lockFile == nil {
		return nil
	}
	// Only write if there are entries
	if len(l.lockFile.Plugins) == 0 {
		return nil
	}
	return WriteLockFile(l.BaseDir, l.lockFile)
}

// LockFile returns the current lock file.
func (l *Loader) LockFile() *LockFile {
	return l.lockFile
}

// Cache returns the cache manager.
func (l *Loader) Cache() *CacheManager {
	return l.cache
}

// Load loads a plugin from its declaration.
func (l *Loader) Load(ctx context.Context, decl *PluginDeclaration) (*LoadedPlugin, error) {
	// Ensure lock file is initialized
	if l.lockFile == nil {
		if err := l.InitLockFile(); err != nil {
			l.logger.Warn("failed to read lock file", "error", err)
			l.lockFile = NewLockFile()
		}
	}

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
		return l.resolveLocalPlugin(decl)
	}

	// Git source
	if IsGitSource(source) {
		return l.resolveGitPlugin(decl)
	}

	return "", fmt.Errorf("unknown plugin source format: %s", source)
}

// resolveLocalPlugin resolves a local plugin from its path.
// If copy=true, copies it to mycel_plugins/ instead of using in place.
func (l *Loader) resolveLocalPlugin(decl *PluginDeclaration) (string, error) {
	absPath := decl.Source
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(l.BaseDir, absPath)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("plugin path not found: %s", absPath)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("plugin path is not a directory: %s", absPath)
	}

	// Copy to mycel_plugins/ if requested
	if decl.Copy {
		destName := decl.Name
		copied, err := l.cache.CopyPlugin(absPath, destName)
		if err != nil {
			return "", err
		}
		l.logger.Info("copied local plugin", "name", decl.Name, "from", absPath, "to", copied)
		return copied, nil
	}

	return absPath, nil
}

// resolveGitPlugin resolves a plugin from a git repository.
// Flow: check lockfile → check cache → git ls-remote → git clone → update lockfile.
func (l *Loader) resolveGitPlugin(decl *PluginDeclaration) (string, error) {
	if err := GitAvailable(); err != nil {
		return "", err
	}

	// Parse version constraint
	cs, err := ParseConstraint(decl.Version)
	if err != nil {
		return "", fmt.Errorf("invalid version constraint for plugin %s: %w", decl.Name, err)
	}

	// Check lock file for exact version
	if l.lockFile != nil {
		if entry := l.lockFile.GetEntry(decl.Name); entry != nil {
			lockedVersion, err := ParseVersion(entry.Version)
			if err == nil && cs.Match(lockedVersion) {
				// Locked version satisfies constraint — use it
				if l.cache.IsCached(decl.Source, lockedVersion) {
					path := l.cache.PluginDir(decl.Source, lockedVersion)
					l.logger.Info("using locked plugin",
						"name", decl.Name, "version", lockedVersion.String())
					return path, nil
				}

				// Locked but not cached — re-clone
				return l.cloneAndCache(decl, lockedVersion)
			}
			// Lock doesn't satisfy constraint — resolve fresh
		}
	}

	// Resolve version from git tags
	ctx := context.Background()
	resolved, err := l.git.ResolveVersion(ctx, decl.Source, cs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve version for plugin %s: %w", decl.Name, err)
	}

	// Check cache for resolved version
	if l.cache.IsCached(decl.Source, resolved) {
		l.logger.Info("using cached plugin",
			"name", decl.Name, "version", resolved.String())
		l.updateLockEntry(decl, resolved)
		return l.cache.PluginDir(decl.Source, resolved), nil
	}

	// Clone and cache
	return l.cloneAndCache(decl, resolved)
}

// cloneAndCache clones a plugin at the given version into the cache.
func (l *Loader) cloneAndCache(decl *PluginDeclaration, version Version) (string, error) {
	if err := l.cache.EnsureDir(); err != nil {
		return "", fmt.Errorf("failed to create plugin cache: %w", err)
	}

	destDir := l.cache.PluginDir(decl.Source, version)

	// Remove stale cache entry
	os.RemoveAll(destDir)

	// Ensure parent directories exist
	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return "", err
	}

	// Clone — use the tag as-is (could be "v1.0.0" or "1.0.0")
	// Try with "v" prefix first, then without
	ctx := context.Background()
	tag := version.String() // "v1.0.0"
	err := l.git.Clone(ctx, decl.Source, tag, destDir)
	if err != nil {
		// Try without "v" prefix
		tag = strings.TrimPrefix(tag, "v")
		err = l.git.Clone(ctx, decl.Source, tag, destDir)
		if err != nil {
			return "", fmt.Errorf("failed to clone plugin %s@%s: %w", decl.Name, version.String(), err)
		}
	}

	// Remove .git directory from cloned repo
	os.RemoveAll(filepath.Join(destDir, ".git"))

	l.logger.Info("installed plugin",
		"name", decl.Name, "version", version.String(), "path", destDir)

	l.updateLockEntry(decl, version)

	return destDir, nil
}

// updateLockEntry updates the lock file with a resolved version.
func (l *Loader) updateLockEntry(decl *PluginDeclaration, version Version) {
	if l.lockFile == nil {
		return
	}
	l.lockFile.SetEntry(decl.Name, &LockEntry{
		Source:   decl.Source,
		Version:  version.String(),
		Resolved: NormalizeGitURL(decl.Source),
	})
}

// parseManifest parses the plugin.mycel manifest file.
func (l *Loader) parseManifest(pluginPath string) (*PluginManifest, error) {
	manifestPath := filepath.Join(pluginPath, "plugin.mycel")

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(manifestPath)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse plugin.mycel: %s", diags.Error())
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
			{Type: "validator", LabelNames: []string{"name"}},
			{Type: "sanitizer", LabelNames: []string{"name"}},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("provides block error: %s", diags.Error())
	}

	provides := &ProvidesConfig{
		Connectors: make([]*ConnectorProvide, 0),
		Validators: make([]*ValidatorProvide, 0),
		Sanitizers: make([]*SanitizerProvide, 0),
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

		case "validator":
			v, err := l.parseValidatorProvide(b, ctx)
			if err != nil {
				return nil, err
			}
			provides.Validators = append(provides.Validators, v)

		case "sanitizer":
			s, err := l.parseSanitizerProvide(b, ctx)
			if err != nil {
				return nil, err
			}
			provides.Sanitizers = append(provides.Sanitizers, s)
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

// parseValidatorProvide parses a validator {} block in provides.
func (l *Loader) parseValidatorProvide(block *hcl.Block, ctx *hcl.EvalContext) (*ValidatorProvide, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("validator block requires a name label")
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "wasm", Required: true},
			{Name: "entrypoint"},
			{Name: "message"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("validator provide block error: %s", diags.Error())
	}

	v := &ValidatorProvide{
		Name: block.Labels[0],
	}

	if attr, ok := content.Attributes["wasm"]; ok {
		val, _ := attr.Expr.Value(ctx)
		v.WASM = val.AsString()
	}

	if attr, ok := content.Attributes["entrypoint"]; ok {
		val, _ := attr.Expr.Value(ctx)
		v.Entrypoint = val.AsString()
	}

	if attr, ok := content.Attributes["message"]; ok {
		val, _ := attr.Expr.Value(ctx)
		v.Message = val.AsString()
	}

	// Default entrypoint: validate_<name>
	if v.Entrypoint == "" {
		v.Entrypoint = "validate_" + v.Name
	}

	return v, nil
}

// parseSanitizerProvide parses a sanitizer {} block in provides.
func (l *Loader) parseSanitizerProvide(block *hcl.Block, ctx *hcl.EvalContext) (*SanitizerProvide, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("sanitizer block requires a name label")
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "wasm", Required: true},
			{Name: "entrypoint"},
			{Name: "apply_to"},
			{Name: "fields"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("sanitizer provide block error: %s", diags.Error())
	}

	s := &SanitizerProvide{
		Name: block.Labels[0],
	}

	if attr, ok := content.Attributes["wasm"]; ok {
		val, _ := attr.Expr.Value(ctx)
		s.WASM = val.AsString()
	}

	if attr, ok := content.Attributes["entrypoint"]; ok {
		val, _ := attr.Expr.Value(ctx)
		s.Entrypoint = val.AsString()
	}

	if attr, ok := content.Attributes["apply_to"]; ok {
		val, _ := attr.Expr.Value(ctx)
		for _, v := range val.AsValueSlice() {
			s.ApplyTo = append(s.ApplyTo, v.AsString())
		}
	}

	if attr, ok := content.Attributes["fields"]; ok {
		val, _ := attr.Expr.Value(ctx)
		for _, v := range val.AsValueSlice() {
			s.Fields = append(s.Fields, v.AsString())
		}
	}

	// Default entrypoint
	if s.Entrypoint == "" {
		s.Entrypoint = "sanitize"
	}

	return s, nil
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
