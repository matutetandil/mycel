package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/matutetandil/mycel/internal/functions"
)

// parseFunctionsBlock parses a functions block.
func parseFunctionsBlock(block *hcl.Block, ctx *hcl.EvalContext) (*functions.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("functions block requires a name label")
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "wasm", Required: true},
			{Name: "exports", Required: true},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("functions block error: %s", diags.Error())
	}

	cfg := &functions.Config{
		Name: block.Labels[0],
	}

	// Parse wasm path
	if attr, ok := content.Attributes["wasm"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("wasm attribute error: %s", diags.Error())
		}
		cfg.WASM = val.AsString()
	}

	// Parse exports array
	if attr, ok := content.Attributes["exports"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("exports attribute error: %s", diags.Error())
		}

		exports := []string{}
		for _, v := range val.AsValueSlice() {
			exports = append(exports, v.AsString())
		}
		cfg.Exports = exports
	}

	return cfg, nil
}
