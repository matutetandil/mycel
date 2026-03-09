package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/matutetandil/mycel/internal/plugin"
)

// parsePluginBlock parses a plugin declaration block.
// Syntax:
//
//	plugin "name" {
//	  source  = "./plugins/salesforce"  # or "github.com/acme/plugin"
//	  version = "~> 1.0"                # optional, for git/registry sources
//	}
func parsePluginBlock(block *hcl.Block, ctx *hcl.EvalContext) (*plugin.PluginDeclaration, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("plugin block requires a name label")
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "source", Required: true},
			{Name: "version"},
			{Name: "copy"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("plugin block error: %s", diags.Error())
	}

	decl := &plugin.PluginDeclaration{
		Name: block.Labels[0],
	}

	// Source is required
	if attr, ok := content.Attributes["source"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("plugin source error: %s", diags.Error())
		}
		decl.Source = val.AsString()
	}

	// Version is optional (only used for git sources)
	if attr, ok := content.Attributes["version"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("plugin version error: %s", diags.Error())
		}
		decl.Version = val.AsString()
	}

	// Copy is optional (only for local plugins)
	if attr, ok := content.Attributes["copy"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("plugin copy error: %s", diags.Error())
		}
		decl.Copy = val.True()
	}

	return decl, nil
}
