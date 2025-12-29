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

	"github.com/mycel-labs/mycel/internal/connector"
	"github.com/mycel-labs/mycel/internal/flow"
	"github.com/mycel-labs/mycel/internal/validate"
	myhcl "github.com/mycel-labs/mycel/pkg/hcl"
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

	// ServiceConfig is the global service configuration.
	ServiceConfig *ServiceConfig
}

// ServiceConfig holds global service configuration.
type ServiceConfig struct {
	Name    string
	Version string
}

// NewConfiguration creates an empty configuration.
func NewConfiguration() *Configuration {
	return &Configuration{
		Connectors: make([]*connector.Config, 0),
		Flows:      make([]*flow.Config, 0),
		Types:      make([]*validate.TypeSchema, 0),
	}
}

// Merge merges another configuration into this one.
func (c *Configuration) Merge(other *Configuration) {
	c.Connectors = append(c.Connectors, other.Connectors...)
	c.Flows = append(c.Flows, other.Flows...)
	c.Types = append(c.Types, other.Types...)
	if other.ServiceConfig != nil {
		c.ServiceConfig = other.ServiceConfig
	}
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

// Parse parses all HCL files in the given directory recursively.
func (p *HCLParser) Parse(ctx context.Context, configDir string) (*Configuration, error) {
	config := NewConfiguration()

	// Walk directory and find all .hcl files
	err := filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip non-HCL files
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".hcl") {
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

		case "flow":
			f, err := parseFlowBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("flow parse error: %w", err)
			}
			config.Flows = append(config.Flows, f)

		case "type":
			t, err := parseTypeBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("type parse error: %w", err)
			}
			config.Types = append(config.Types, t)

		case "service":
			svc, err := parseServiceBlock(block, p.evalCtx)
			if err != nil {
				return nil, fmt.Errorf("service parse error: %w", err)
			}
			config.ServiceConfig = svc
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
			{Type: "service"},
		},
	}
}

// parseServiceBlock parses a service block.
func parseServiceBlock(block *hcl.Block, ctx *hcl.EvalContext) (*ServiceConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "name"},
			{Name: "version"},
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

	return svc, nil
}
