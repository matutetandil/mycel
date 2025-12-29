package parser

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"

	"github.com/mycel-labs/mycel/internal/flow"
)

// parseFlowBlock parses a flow block from HCL.
func parseFlowBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("flow block requires a name label")
	}

	config := &flow.Config{
		Name: block.Labels[0],
	}

	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "from"},
			{Type: "to"},
			{Type: "validate"},
			{Type: "transform"},
			{Type: "require"},
			{Type: "error_handling"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("flow content error: %s", diags.Error())
	}

	// Parse nested blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "from":
			from, err := parseFromBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("from block error: %w", err)
			}
			config.From = from

		case "to":
			to, err := parseToBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("to block error: %w", err)
			}
			config.To = to

		case "validate":
			validate, err := parseValidateBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("validate block error: %w", err)
			}
			config.Validate = validate

		case "transform":
			transform, err := parseTransformBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("transform block error: %w", err)
			}
			config.Transform = transform

		case "require":
			require, err := parseRequireBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("require block error: %w", err)
			}
			config.Require = require

		case "error_handling":
			eh, err := parseErrorHandlingBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("error_handling block error: %w", err)
			}
			config.ErrorHandling = eh
		}
	}

	return config, nil
}

// parseFromBlock parses a from block.
// Supports format:
//
//	from {
//	  connector = "api"
//	  operation = "GET /users"
//	}
func parseFromBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.FromConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "operation", Required: true},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("from block content error: %s", diags.Error())
	}

	from := &flow.FromConfig{}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("from connector error: %s", diags.Error())
		}
		from.Connector = val.AsString()
	}

	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("from operation error: %s", diags.Error())
		}
		from.Operation = val.AsString()
	}

	if from.Connector == "" {
		return nil, fmt.Errorf("from block must specify a connector")
	}

	return from, nil
}

// parseToBlock parses a to block.
// Supports format:
//
//	to {
//	  connector = "postgres"
//	  target    = "users"
//	  filter    = "user_id = ${context.user_id}"  // optional
//	}
func parseToBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.ToConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "target", Required: true},
			{Name: "filter"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("to block content error: %s", diags.Error())
	}

	to := &flow.ToConfig{}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to connector error: %s", diags.Error())
		}
		to.Connector = val.AsString()
	}

	if attr, ok := content.Attributes["target"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to target error: %s", diags.Error())
		}
		to.Target = val.AsString()
	}

	if attr, ok := content.Attributes["filter"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to filter error: %s", diags.Error())
		}
		to.Filter = val.AsString()
	}

	if to.Connector == "" {
		return nil, fmt.Errorf("to block must specify a connector")
	}

	return to, nil
}

// parseValidateBlock parses a validate block.
func parseValidateBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.ValidateConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "input"},
			{Name: "output"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("validate block content error: %s", diags.Error())
	}

	validate := &flow.ValidateConfig{}

	if attr, ok := content.Attributes["input"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("validate input error: %s", diags.Error())
		}
		// Handle type.name format
		validate.Input = parseTypeReference(val.AsString())
	}

	if attr, ok := content.Attributes["output"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("validate output error: %s", diags.Error())
		}
		validate.Output = parseTypeReference(val.AsString())
	}

	return validate, nil
}

// parseTypeReference parses a type reference (e.g., "type.user" -> "user").
func parseTypeReference(ref string) string {
	if strings.HasPrefix(ref, "type.") {
		return strings.TrimPrefix(ref, "type.")
	}
	return ref
}

// parseTransformBlock parses a transform block.
func parseTransformBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.TransformConfig, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("transform block attributes error: %s", diags.Error())
	}

	transform := &flow.TransformConfig{
		Mappings: make(map[string]string),
	}

	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			// If we can't evaluate, store the expression as string
			// This handles cases like uuid() or lower(input.email)
			transform.Mappings[name] = extractExpressionString(attr.Expr)
			continue
		}

		if name == "use" {
			// Reference to a named transform
			transform.Use = parseTransformReference(val.AsString())
		} else if strings.HasPrefix(name, "output.") {
			// Inline transformation mapping
			field := strings.TrimPrefix(name, "output.")
			transform.Mappings[field] = val.AsString()
		} else {
			transform.Mappings[name] = val.AsString()
		}
	}

	return transform, nil
}

// parseTransformReference parses a transform reference.
func parseTransformReference(ref string) string {
	if strings.HasPrefix(ref, "transform.") {
		return strings.TrimPrefix(ref, "transform.")
	}
	return ref
}

// extractExpressionString attempts to extract the source text of an expression.
func extractExpressionString(expr hcl.Expression) string {
	// Get the expression range and return a placeholder
	// A full implementation would reconstruct the expression
	return fmt.Sprintf("${%s}", expr.Range().String())
}

// parseRequireBlock parses a require block.
func parseRequireBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.RequireConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "roles"},
			{Name: "permissions"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("require block content error: %s", diags.Error())
	}

	require := &flow.RequireConfig{}

	if attr, ok := content.Attributes["roles"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("require roles error: %s", diags.Error())
		}
		if val.Type().IsListType() || val.Type().IsTupleType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				require.Roles = append(require.Roles, v.AsString())
			}
		}
	}

	if attr, ok := content.Attributes["permissions"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("require permissions error: %s", diags.Error())
		}
		if val.Type().IsListType() || val.Type().IsTupleType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				require.Permissions = append(require.Permissions, v.AsString())
			}
		}
	}

	return require, nil
}

// parseErrorHandlingBlock parses an error_handling block.
func parseErrorHandlingBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.ErrorHandlingConfig, error) {
	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "retry"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("error_handling block content error: %s", diags.Error())
	}

	eh := &flow.ErrorHandlingConfig{}

	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "retry" {
			retry, err := parseRetryConfigBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("retry block error: %w", err)
			}
			eh.Retry = retry
		}
	}

	return eh, nil
}

// parseRetryConfigBlock parses a retry block within error_handling.
func parseRetryConfigBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.RetryConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "attempts"},
			{Name: "delay"},
			{Name: "backoff"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("retry block content error: %s", diags.Error())
	}

	retry := &flow.RetryConfig{}

	if attr, ok := content.Attributes["attempts"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("retry attempts error: %s", diags.Error())
		}
		bf := val.AsBigFloat()
		i, _ := bf.Int64()
		retry.Attempts = int(i)
	}

	if attr, ok := content.Attributes["delay"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("retry delay error: %s", diags.Error())
		}
		retry.Delay = val.AsString()
	}

	if attr, ok := content.Attributes["backoff"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("retry backoff error: %s", diags.Error())
		}
		retry.Backoff = val.AsString()
	}

	return retry, nil
}
