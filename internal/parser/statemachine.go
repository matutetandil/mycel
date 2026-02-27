package parser

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/internal/statemachine"
)

// parseStateMachineBlock parses a state_machine block from HCL.
func parseStateMachineBlock(block *hcl.Block, ctx *hcl.EvalContext) (*statemachine.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("state_machine block requires a name label")
	}

	config := &statemachine.Config{
		Name:   block.Labels[0],
		States: make(map[string]*statemachine.StateConfig),
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "initial", Required: true},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "state", LabelNames: []string{"name"}},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("state_machine content error: %s", diags.Error())
	}

	// Parse initial state
	if attr, ok := content.Attributes["initial"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("state_machine initial error: %s", diags.Error())
		}
		config.Initial = val.AsString()
	}

	// Parse state blocks
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "state" {
			state, err := parseStateBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("state block error: %w", err)
			}
			config.States[state.Name] = state
		}
	}

	if len(config.States) == 0 {
		return nil, fmt.Errorf("state_machine %q must have at least one state", config.Name)
	}

	return config, nil
}

// parseStateBlock parses a state block inside a state_machine.
func parseStateBlock(block *hcl.Block, ctx *hcl.EvalContext) (*statemachine.StateConfig, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("state block requires a name label")
	}

	state := &statemachine.StateConfig{
		Name:        block.Labels[0],
		Transitions: make(map[string]*statemachine.TransitionConfig),
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "final"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "on", LabelNames: []string{"event"}},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("state block content error: %s", diags.Error())
	}

	// Parse final attribute
	if attr, ok := content.Attributes["final"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() && val.Type() == cty.Bool {
			state.Final = val.True()
		}
	}

	// Parse on (transition) blocks
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "on" {
			transition, err := parseTransitionBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("transition block error: %w", err)
			}
			state.Transitions[transition.Event] = transition
		}
	}

	return state, nil
}

// parseTransitionBlock parses an on block inside a state.
func parseTransitionBlock(block *hcl.Block, ctx *hcl.EvalContext) (*statemachine.TransitionConfig, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("on block requires an event label")
	}

	transition := &statemachine.TransitionConfig{
		Event: block.Labels[0],
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "transition_to", Required: true},
			{Name: "guard"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "action"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("on block content error: %s", diags.Error())
	}

	// Parse transition_to
	if attr, ok := content.Attributes["transition_to"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("transition_to error: %s", diags.Error())
		}
		transition.TransitionTo = val.AsString()
	}

	// Parse guard
	if attr, ok := content.Attributes["guard"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			transition.Guard = val.AsString()
		}
	}

	// Parse action block
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "action" {
			action, err := parseStateMachineActionBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("transition action error: %w", err)
			}
			transition.Action = action
		}
	}

	return transition, nil
}

// parseStateMachineActionBlock parses an action block inside a transition.
func parseStateMachineActionBlock(block *hcl.Block, ctx *hcl.EvalContext) (*statemachine.ActionConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector"},
			{Name: "operation"},
			{Name: "target"},
			{Name: "data"},
			{Name: "body"},
			{Name: "params"},
			{Name: "template"},
			{Name: "to"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("action block content error: %s", diags.Error())
	}

	action := &statemachine.ActionConfig{}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("action connector error: %s", diags.Error())
		}
		ref := val.AsString()
		if strings.HasPrefix(ref, "connector.") {
			ref = strings.TrimPrefix(ref, "connector.")
		}
		action.Connector = ref
	}

	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Operation = val.AsString()
		}
	}

	if attr, ok := content.Attributes["target"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			action.Target = val.AsString()
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
