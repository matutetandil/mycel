// Package parser provides HCL configuration parsing for Mycel.
package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"

	"github.com/matutetandil/mycel/internal/aspect"
	"github.com/matutetandil/mycel/internal/auth"
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/functions"
	"github.com/matutetandil/mycel/internal/mock"
	"github.com/matutetandil/mycel/internal/plugin"
	"github.com/matutetandil/mycel/internal/saga"
	"github.com/matutetandil/mycel/internal/security"
	"github.com/matutetandil/mycel/internal/statemachine"
	"github.com/matutetandil/mycel/internal/transform"
	"github.com/matutetandil/mycel/internal/validate"
	"github.com/matutetandil/mycel/internal/validator"
	myhcl "github.com/matutetandil/mycel/pkg/hcl"
)

// Parser parses HCL configuration files.
type Parser interface {
	// Parse parses all HCL files in the given directory recursively.
	Parse(ctx context.Context, configDir string) (*Configuration, error)

	// ParseFile parses a single HCL file.
	ParseFile(ctx context.Context, path string) (*Configuration, error)
}

// Configuration holds all parsed configuration.
type Configuration struct {
	// Connectors are all connector configurations.
	Connectors []*connector.Config

	// Flows are all flow configurations.
	Flows []*flow.Config

	// Types are all type schemas.
	Types []*validate.TypeSchema

	// Transforms are reusable transform configurations.
	Transforms []*transform.Config

	// NamedCaches are reusable cache configurations.
	NamedCaches []*flow.NamedCacheConfig

	// Aspects are cross-cutting concern configurations.
	Aspects []*aspect.Config

	// MockConfig is the mock system configuration.
	MockConfig *mock.Config

	// ServiceConfig is the global service configuration.
	ServiceConfig *ServiceConfig

	// Validators are custom validator configurations.
	Validators []*validator.Config

	// Functions are WASM function module configurations.
	Functions []*functions.Config

	// Plugins are plugin declarations for custom connectors/functions.
	Plugins []*plugin.PluginDeclaration

	// Auth is the authentication system configuration.
	Auth *auth.Config

	// Sagas are saga (distributed transaction) configurations.
	Sagas []*saga.Config

	// StateMachines are state machine configurations.
	StateMachines []*statemachine.Config

	// Security is the security configuration (sanitization, thresholds, WASM sanitizers).
	Security *security.Config

	// SourceFiles tracks which files each named item was defined in.
	// Key format: "type:name" (e.g., "connector:postgres", "flow:create_user").
	// Value is a slice because duplicates may exist (which ValidateUniqueNames catches).
	SourceFiles map[string][]string
}

// ServiceConfig holds global service configuration.
type ServiceConfig struct {
	Name      string
	Version   string
	AdminPort int // Port for standalone health/metrics server (default: 9090)
	RateLimit *RateLimitConfig
	Workflow  *WorkflowConfig
}

// WorkflowConfig holds workflow persistence configuration.
type WorkflowConfig struct {
	// Storage is the connector name to use for workflow persistence (e.g., "postgres").
	Storage string
	// Table is the database table name (default: "mycel_workflows").
	Table string
	// AutoCreate creates the table automatically on startup (default: true).
	AutoCreate bool
}

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	Enabled           bool
	RequestsPerSecond float64
	Burst             int
	KeyExtractor      string   // "ip", "header:X-API-Key", "query:api_key"
	ExcludePaths      []string // paths to exclude from rate limiting
	EnableHeaders     bool     // add X-RateLimit-* headers
	Storage           string   // cache connector name for distributed rate limiting (e.g., "redis_cache")
}

// NewConfiguration creates an empty configuration.
func NewConfiguration() *Configuration {
	return &Configuration{
		Connectors:    make([]*connector.Config, 0),
		Flows:         make([]*flow.Config, 0),
		Types:         make([]*validate.TypeSchema, 0),
		Transforms:    make([]*transform.Config, 0),
		NamedCaches:   make([]*flow.NamedCacheConfig, 0),
		Aspects:       make([]*aspect.Config, 0),
		Validators:    make([]*validator.Config, 0),
		Functions:     make([]*functions.Config, 0),
		Plugins:       make([]*plugin.PluginDeclaration, 0),
		Sagas:         make([]*saga.Config, 0),
		StateMachines: make([]*statemachine.Config, 0),
		SourceFiles:   make(map[string][]string),
	}
}

// Merge merges another configuration into this one.
func (c *Configuration) Merge(other *Configuration) {
	c.Connectors = append(c.Connectors, other.Connectors...)
	c.Flows = append(c.Flows, other.Flows...)
	c.Types = append(c.Types, other.Types...)
	c.Transforms = append(c.Transforms, other.Transforms...)
	c.NamedCaches = append(c.NamedCaches, other.NamedCaches...)
	c.Aspects = append(c.Aspects, other.Aspects...)
	c.Validators = append(c.Validators, other.Validators...)
	c.Functions = append(c.Functions, other.Functions...)
	c.Plugins = append(c.Plugins, other.Plugins...)
	c.Sagas = append(c.Sagas, other.Sagas...)
	c.StateMachines = append(c.StateMachines, other.StateMachines...)
	if other.ServiceConfig != nil {
		c.ServiceConfig = other.ServiceConfig
	}
	if other.MockConfig != nil {
		c.MockConfig = other.MockConfig
	}
	if other.Security != nil {
		c.Security = other.Security
	}
	for k, v := range other.SourceFiles {
		c.SourceFiles[k] = append(c.SourceFiles[k], v...)
	}
}

// ValidateUniqueNames checks that no two items of the same type share a name.
// Returns an error if duplicates are found, with file paths for both definitions.
func (c *Configuration) ValidateUniqueNames() error {
	// Check each category using the SourceFiles map which tracks all files per key
	categories := []struct {
		itemType string
		key      func() []string // returns keys like "connector:name"
	}{
		{"connector", func() []string {
			keys := make([]string, len(c.Connectors))
			for i, conn := range c.Connectors {
				keys[i] = "connector:" + conn.Name
			}
			return keys
		}},
		{"flow", func() []string {
			keys := make([]string, len(c.Flows))
			for i, f := range c.Flows {
				keys[i] = "flow:" + f.Name
			}
			return keys
		}},
		{"type", func() []string {
			keys := make([]string, len(c.Types))
			for i, t := range c.Types {
				keys[i] = "type:" + t.Name
			}
			return keys
		}},
		{"transform", func() []string {
			keys := make([]string, len(c.Transforms))
			for i, tr := range c.Transforms {
				keys[i] = "transform:" + tr.Name
			}
			return keys
		}},
		{"aspect", func() []string {
			keys := make([]string, len(c.Aspects))
			for i, a := range c.Aspects {
				keys[i] = "aspect:" + a.Name
			}
			return keys
		}},
		{"validator", func() []string {
			keys := make([]string, len(c.Validators))
			for i, v := range c.Validators {
				keys[i] = "validator:" + v.Name
			}
			return keys
		}},
	}

	for _, cat := range categories {
		seen := make(map[string]bool)
		for _, key := range cat.key() {
			name := key[len(cat.itemType)+1:] // strip "type:" prefix
			if seen[name] {
				files := c.SourceFiles[key]
				first := "(unknown)"
				second := "(unknown)"
				if len(files) >= 2 {
					first = files[0]
					second = files[1]
				} else if len(files) == 1 {
					first = files[0]
					second = files[0]
				}
				return fmt.Errorf("duplicate %s name %q: defined in %s and %s", cat.itemType, name, first, second)
			}
			seen[name] = true
		}
	}

	return nil
}

// HCLParser implements Parser using hashicorp/hcl/v2.
type HCLParser struct {
	hclParser *hclparse.Parser
	evalCtx   *hcl.EvalContext
}

// NewHCLParser creates a new HCL parser.
func NewHCLParser() *HCLParser {
	return &HCLParser{
		hclParser: hclparse.NewParser(),
		evalCtx:   newEvalContext(),
	}
}

// newEvalContext creates the HCL evaluation context with custom functions.
func newEvalContext() *hcl.EvalContext {
	return &hcl.EvalContext{
		Functions: myhcl.Functions(),
	}
}

// isPluginManifest returns true if the file is a plugin manifest (parsed by the
// plugin loader, not the main parser). A manifest has a top-level `plugin` block
// WITHOUT a label and a `provides` block — unlike config declarations which use
// `plugin "name" { source = "..." }` with a label.
func isPluginManifest(path string) bool {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return false
	}

	// Use a permissive schema that accepts plugin blocks with 0 labels.
	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "plugin"},   // no labels required
			{Type: "provides"}, // only exists in manifests
		},
	}

	content, diags := file.Body.Content(schema)
	if diags.HasErrors() {
		return false
	}

	hasUnlabeledPlugin := false
	hasProvides := false
	for _, block := range content.Blocks {
		if block.Type == "plugin" && len(block.Labels) == 0 {
			hasUnlabeledPlugin = true
		}
		if block.Type == "provides" {
			hasProvides = true
		}
	}

	return hasUnlabeledPlugin && hasProvides
}

// Parse parses all HCL files in the given directory recursively.
func (p *HCLParser) Parse(ctx context.Context, configDir string) (*Configuration, error) {
	config := NewConfiguration()

	// Walk directory and find all .hcl files
	err := filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the plugin cache directory (managed by Mycel, not user config).
		if info.IsDir() && info.Name() == "mycel_plugins" {
			return filepath.SkipDir
		}

		// Skip non-HCL files
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".hcl") {
			return nil
		}

		// Skip plugin manifest files — these are parsed by the plugin loader.
		// A manifest is identified by having a top-level `plugin` block without
		// a label (e.g. `plugin { name = "..." }`), unlike config declarations
		// which use `plugin "name" { source = "..." }`.
		if isPluginManifest(path) {
			return nil
		}

		// Parse the file
		fileConfig, err := p.ParseFile(ctx, path)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		config.Merge(fileConfig)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Validate that no two items of the same type share a name
	if err := config.ValidateUniqueNames(); err != nil {
		return nil, err
	}

	return config, nil
}

// ParseFile parses a single HCL file.
func (p *HCLParser) ParseFile(ctx context.Context, path string) (*Configuration, error) {
	file, diags := p.hclParser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL parse error: %s", diags.Error())
	}

	config := NewConfiguration()

	// Decode the body into our schema
	content, diags := file.Body.Content(rootSchema())
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL content error: %s", diags.Error())
	}

	// Process each block type
	for _, block := range content.Blocks {
		switch block.Type {
		case "connector":
			conn, err := parseConnectorBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("connector parse error: %w", err)
			}
			config.Connectors = append(config.Connectors, conn)
			config.SourceFiles["connector:"+conn.Name] = append(config.SourceFiles["connector:"+conn.Name], path)

		case "flow":
			f, err := parseFlowBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("flow parse error: %w", err)
			}
			f.SourceFile = path
			config.Flows = append(config.Flows, f)
			config.SourceFiles["flow:"+f.Name] = append(config.SourceFiles["flow:"+f.Name], path)

		case "type":
			t, err := parseTypeBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("type parse error: %w", err)
			}
			config.Types = append(config.Types, t)
			config.SourceFiles["type:"+t.Name] = append(config.SourceFiles["type:"+t.Name], path)

		case "transform":
			tr, err := parseNamedTransformBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("transform parse error: %w", err)
			}
			config.Transforms = append(config.Transforms, tr)
			config.SourceFiles["transform:"+tr.Name] = append(config.SourceFiles["transform:"+tr.Name], path)

		case "cache":
			cache, err := parseNamedCacheBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("cache parse error: %w", err)
			}
			config.NamedCaches = append(config.NamedCaches, cache)

		case "aspect":
			asp, err := parseAspectBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("aspect parse error: %w", err)
			}
			config.Aspects = append(config.Aspects, asp)
			config.SourceFiles["aspect:"+asp.Name] = append(config.SourceFiles["aspect:"+asp.Name], path)

		case "service":
			svc, err := parseServiceBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("service parse error: %w", err)
			}
			config.ServiceConfig = svc

		case "mocks":
			mockCfg, err := parseMockConfig(block)
			if err != nil {
				return nil, fmt.Errorf("mocks parse error: %w", err)
			}
			config.MockConfig = mockCfg

		case "validator":
			v, err := parseValidatorBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("validator parse error: %w", err)
			}
			config.Validators = append(config.Validators, v)
			config.SourceFiles["validator:"+v.Name] = append(config.SourceFiles["validator:"+v.Name], path)

		case "functions":
			fn, err := parseFunctionsBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("functions parse error: %w", err)
			}
			config.Functions = append(config.Functions, fn)

		case "plugin":
			pl, err := parsePluginBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("plugin parse error: %w", err)
			}
			config.Plugins = append(config.Plugins, pl)

		case "auth":
			authCfg, err := p.parseAuthBlock(block)
			if err != nil {
				return nil, fmt.Errorf("auth parse error: %w", err)
			}
			config.Auth = authCfg

		case "saga":
			s, err := parseSagaBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("saga parse error: %w", err)
			}
			config.Sagas = append(config.Sagas, s)

		case "state_machine":
			sm, err := parseStateMachineBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("state_machine parse error: %w", err)
			}
			config.StateMachines = append(config.StateMachines, sm)

		case "security":
			sec, err := parseSecurityBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("security parse error: %w", err)
			}
			config.Security = sec
		}
	}

	return config, nil
}

// rootSchema returns the top-level HCL schema.
func rootSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "connector", LabelNames: []string{"name"}},
			{Type: "flow", LabelNames: []string{"name"}},
			{Type: "type", LabelNames: []string{"name"}},
			{Type: "transform", LabelNames: []string{"name"}},
			{Type: "cache", LabelNames: []string{"name"}},
			{Type: "aspect", LabelNames: []string{"name"}},
			{Type: "validator", LabelNames: []string{"name"}},
			{Type: "functions", LabelNames: []string{"name"}},
			{Type: "plugin", LabelNames: []string{"name"}},
			{Type: "saga", LabelNames: []string{"name"}},
			{Type: "state_machine", LabelNames: []string{"name"}},
			{Type: "service"},
			{Type: "mocks"},
			{Type: "auth"},
			{Type: "security"},
		},
	}
}

// parseServiceBlock parses a service block.
func parseServiceBlock(block *hcl.Block, ctx *hcl.EvalContext) (*ServiceConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "name"},
			{Name: "version"},
			{Name: "admin_port"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "rate_limit"},
			{Type: "workflow"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("service block error: %s", diags.Error())
	}

	svc := &ServiceConfig{}

	if attr, ok := content.Attributes["name"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("service name error: %s", diags.Error())
		}
		svc.Name = val.AsString()
	}

	if attr, ok := content.Attributes["version"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("service version error: %s", diags.Error())
		}
		svc.Version = val.AsString()
	}

	if attr, ok := content.Attributes["admin_port"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("service admin_port error: %s", diags.Error())
		}
		port, _ := val.AsBigFloat().Int64()
		svc.AdminPort = int(port)
	}

	// Parse nested blocks
	for _, b := range content.Blocks {
		switch b.Type {
		case "rate_limit":
			rl, err := parseRateLimitBlock(b, ctx)
			if err != nil {
				return nil, fmt.Errorf("rate_limit block error: %w", err)
			}
			svc.RateLimit = rl
		case "workflow":
			wf, err := parseWorkflowBlock(b, ctx)
			if err != nil {
				return nil, fmt.Errorf("workflow block error: %w", err)
			}
			svc.Workflow = wf
		}
	}

	return svc, nil
}

// parseRateLimitBlock parses a rate_limit block.
func parseRateLimitBlock(block *hcl.Block, ctx *hcl.EvalContext) (*RateLimitConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "requests_per_second"},
			{Name: "burst"},
			{Name: "key_extractor"},
			{Name: "exclude_paths"},
			{Name: "enable_headers"},
			{Name: "storage"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("rate_limit block error: %s", diags.Error())
	}

	// Defaults
	rl := &RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 100,
		Burst:             200,
		KeyExtractor:      "ip",
		EnableHeaders:     true,
		ExcludePaths:      []string{"/health", "/health/live", "/health/ready", "/metrics"},
	}

	if attr, ok := content.Attributes["enabled"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("enabled error: %s", diags.Error())
		}
		rl.Enabled = val.True()
	}

	if attr, ok := content.Attributes["requests_per_second"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("requests_per_second error: %s", diags.Error())
		}
		f, _ := val.AsBigFloat().Float64()
		rl.RequestsPerSecond = f
	}

	if attr, ok := content.Attributes["burst"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("burst error: %s", diags.Error())
		}
		i, _ := val.AsBigFloat().Int64()
		rl.Burst = int(i)
	}

	if attr, ok := content.Attributes["key_extractor"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("key_extractor error: %s", diags.Error())
		}
		rl.KeyExtractor = val.AsString()
	}

	if attr, ok := content.Attributes["exclude_paths"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("exclude_paths error: %s", diags.Error())
		}
		paths := []string{}
		for _, v := range val.AsValueSlice() {
			paths = append(paths, v.AsString())
		}
		rl.ExcludePaths = paths
	}

	if attr, ok := content.Attributes["enable_headers"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("enable_headers error: %s", diags.Error())
		}
		rl.EnableHeaders = val.True()
	}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("storage error: %s", diags.Error())
		}
		rl.Storage = val.AsString()
	}

	return rl, nil
}

// parseWorkflowBlock parses a workflow block inside the service block.
func parseWorkflowBlock(block *hcl.Block, ctx *hcl.EvalContext) (*WorkflowConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "table"},
			{Name: "auto_create"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("workflow block error: %s", diags.Error())
	}

	wf := &WorkflowConfig{
		Table:      "mycel_workflows",
		AutoCreate: true,
	}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("workflow storage error: %s", diags.Error())
		}
		ref := val.AsString()
		if strings.HasPrefix(ref, "connector.") {
			ref = strings.TrimPrefix(ref, "connector.")
		}
		wf.Storage = ref
	}

	if attr, ok := content.Attributes["table"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			wf.Table = val.AsString()
		}
	}

	if attr, ok := content.Attributes["auto_create"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			wf.AutoCreate = val.True()
		}
	}

	return wf, nil
}
