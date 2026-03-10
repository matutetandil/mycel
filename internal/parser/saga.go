package parser

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"

	"github.com/matutetandil/mycel/internal/saga"
)

// parseSagaBlock parses a saga block from HCL.
func parseSagaBlock(block *hcl.Block, ctx *hcl.EvalContext) (*saga.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("saga block requires a name label")
	}

	config := &saga.Config{
		Name: block.Labels[0],
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "timeout"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "from"},
			{Type: "step", LabelNames: []string{"name"}},
			{Type: "on_complete"},
			{Type: "on_failure"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("saga content error: %s", diags.Error())
	}

	// Parse timeout
	if attr, ok := content.Attributes["timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			config.Timeout = val.AsString()
		}
	}

	// Parse blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "from":
			from, err := parseSagaFromBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("saga from block error: %w", err)
			}
			config.From = from

		case "step":
			step, err := parseSagaStepBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("saga step block error: %w", err)
			}
			config.Steps = append(config.Steps, step)

		case "on_complete":
			action, err := parseSagaActionBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("saga on_complete block error: %w", err)
			}
			config.OnComplete = action

		case "on_failure":
			action, err := parseSagaActionBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("saga on_failure block error: %w", err)
			}
			config.OnFailure = action
		}
	}

	if len(config.Steps) == 0 {
		return nil, fmt.Errorf("saga %q must have at least one step", config.Name)
	}

	return config, nil
}

// parseSagaFromBlock parses the from block inside a saga.
func parseSagaFromBlock(block *hcl.Block, ctx *hcl.EvalContext) (*saga.FromConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "operation"},
			{Name: "filter"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("saga from block content error: %s", diags.Error())
	}

	from := &saga.FromConfig{}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("saga from connector error: %s", diags.Error())
		}
		from.Connector = parseSagaConnectorRef(val.AsString())
	}

	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("saga from operation error: %s", diags.Error())
		}
		from.Operation = val.AsString()
	}

	if attr, ok := content.Attributes["filter"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("saga from filter error: %s", diags.Error())
		}
		from.Filter = val.AsString()
	}

	return from, nil
}

// parseSagaStepBlock parses a step block inside a saga.
func parseSagaStepBlock(block *hcl.Block, ctx *hcl.EvalContext) (*saga.StepConfig, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("saga step block requires a name label")
	}

	step := &saga.StepConfig{
		Name: block.Labels[0],
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "timeout"},
			{Name: "on_error"},
			{Name: "delay"},
			{Name: "await"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "action"},
			{Type: "compensate"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("saga step block content error: %s", diags.Error())
	}

	// Parse timeout
	if attr, ok := content.Attributes["timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			step.Timeout = val.AsString()
		}
	}

	// Parse on_error
	if attr, ok := content.Attributes["on_error"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			step.OnError = val.AsString()
		}
	}

	// Parse delay
	if attr, ok := content.Attributes["delay"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			step.Delay = val.AsString()
		}
	}

	// Parse await
	if attr, ok := content.Attributes["await"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			step.Await = val.AsString()
		}
	}

	// Parse action and compensate blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "action":
			action, err := parseSagaActionBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("saga step action error: %w", err)
			}
			step.Action = action

		case "compensate":
			compensate, err := parseSagaActionBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("saga step compensate error: %w", err)
			}
			step.Compensate = compensate
		}
	}

	// Delay and await steps don't need an action block
	if step.Action == nil && step.Delay == "" && step.Await == "" {
		return nil, fmt.Errorf("saga step %q must have an action, delay, or await", step.Name)
	}

	return step, nil
}

// parseSagaActionBlock parses an action/compensate/on_complete/on_failure block.
func parseSagaActionBlock(block *hcl.Block, ctx *hcl.EvalContext) (*saga.ActionConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector"},
			{Name: "operation"},
			{Name: "target"},
			{Name: "query"},
			{Name: "data"},
			{Name: "body"},
			{Name: "set"},
			{Name: "where"},
			{Name: "params"},
			{Name: "template"},
			{Name: "to"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("saga action block content error: %s", diags.Error())
	}

	action := &saga.ActionConfig{}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("saga action connector error: %s", diags.Error())
		}
		action.Connector = parseSagaConnectorRef(val.AsString())
	}

	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("saga action operation error: %s", diags.Error())
		}
		action.Operation = val.AsString()
	}

	if attr, ok := content.Attributes["target"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Target = val.AsString()
		}
	}

	if attr, ok := content.Attributes["query"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Query = val.AsString()
		}
	}

	if attr, ok := content.Attributes["data"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Data = ctyValueToMap(val)
		}
	}

	if attr, ok := content.Attributes["body"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Body = ctyValueToMap(val)
		}
	}

	if attr, ok := content.Attributes["set"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Set = ctyValueToMap(val)
		}
	}

	if attr, ok := content.Attributes["where"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Where = ctyValueToMap(val)
		}
	}

	if attr, ok := content.Attributes["params"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Params = ctyValueToMap(val)
		}
	}

	if attr, ok := content.Attributes["template"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Template = val.AsString()
		}
	}

	if attr, ok := content.Attributes["to"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.To = val.AsString()
		}
	}

	return action, nil
}

// parseSagaConnectorRef strips the "connector." prefix from a connector reference.
func parseSagaConnectorRef(ref string) string {
	if strings.HasPrefix(ref, "connector.") {
		return strings.TrimPrefix(ref, "connector.")
	}
	return ref
}
