package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"

	"github.com/matutetandil/mycel/internal/validator"
)

// validatorBlockSchema defines the schema for a validator block.
var validatorBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "type", Required: true},
		{Name: "pattern"},    // For regex validators
		{Name: "expr"},       // For CEL validators
		{Name: "wasm"},       // For WASM validators
		{Name: "entrypoint"}, // For WASM validators
		{Name: "message"},    // Custom error message
	},
}

// hclValidatorBlock represents a validator block in HCL.
type hclValidatorBlock struct {
	Type       string `hcl:"type"`
	Pattern    string `hcl:"pattern,optional"`
	Expr       string `hcl:"expr,optional"`
	WASM       string `hcl:"wasm,optional"`
	Entrypoint string `hcl:"entrypoint,optional"`
	Message    string `hcl:"message,optional"`
}

// parseValidatorBlock parses a validator block from HCL.
func parseValidatorBlock(block *hcl.Block, ctx *hcl.EvalContext) (*validator.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("validator block requires a name label")
	}

	name := block.Labels[0]

	// Decode the block body
	var hclValidator hclValidatorBlock
	diags := gohcl.DecodeBody(block.Body, ctx, &hclValidator)
	if diags.HasErrors() {
		return nil, fmt.Errorf("validator parse error: %s", diags.Error())
	}

	// Convert to validator.Config
	cfg := &validator.Config{
		Name:    name,
		Message: hclValidator.Message,
	}

	// Set type and validate required fields
	switch hclValidator.Type {
	case "regex":
		cfg.Type = validator.ValidatorTypeRegex
		if hclValidator.Pattern == "" {
			return nil, fmt.Errorf("validator %s: regex type requires 'pattern' attribute", name)
		}
		cfg.Pattern = hclValidator.Pattern

	case "cel":
		cfg.Type = validator.ValidatorTypeCEL
		if hclValidator.Expr == "" {
			return nil, fmt.Errorf("validator %s: cel type requires 'expr' attribute", name)
		}
		cfg.Expr = hclValidator.Expr

	case "wasm":
		cfg.Type = validator.ValidatorTypeWASM
		if hclValidator.WASM == "" {
			return nil, fmt.Errorf("validator %s: wasm type requires 'wasm' attribute", name)
		}
		cfg.WASM = hclValidator.WASM
		cfg.Entrypoint = hclValidator.Entrypoint
		if cfg.Entrypoint == "" {
			cfg.Entrypoint = "validate" // Default entrypoint
		}

	default:
		return nil, fmt.Errorf("validator %s: unknown type '%s' (expected regex, cel, or wasm)", name, hclValidator.Type)
	}

	return cfg, nil
}
