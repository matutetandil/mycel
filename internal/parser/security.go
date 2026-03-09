package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"

	"github.com/matutetandil/mycel/internal/security"
)

// parseSecurityBlock parses a security block from HCL configuration.
//
// Example HCL:
//
//	security {
//	  max_input_length = 2097152
//	  max_field_length = 131072
//	  max_field_depth  = 30
//	  allowed_control_chars = ["tab", "newline", "cr"]
//
//	  sanitizer "strip_html" {
//	    source     = "wasm"
//	    wasm       = "plugins/strip_html.wasm"
//	    entrypoint = "sanitize"
//	    apply_to   = ["flows/api/*"]
//	    fields     = ["body", "description"]
//	  }
//
//	  flow "bulk_import" {
//	    max_input_length = 10485760
//	    sanitizers       = ["strip_html"]
//	  }
//	}
func parseSecurityBlock(block *hcl.Block, ctx *hcl.EvalContext) (*security.Config, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "max_input_length"},
			{Name: "max_field_length"},
			{Name: "max_field_depth"},
			{Name: "allowed_control_chars"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "sanitizer", LabelNames: []string{"name"}},
			{Type: "flow", LabelNames: []string{"name"}},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("security block error: %s", diags.Error())
	}

	cfg := &security.Config{
		FlowOverrides: make(map[string]*security.FlowSecurityConfig),
	}

	// Parse attributes
	if attr, ok := content.Attributes["max_input_length"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("max_input_length error: %s", diags.Error())
		}
		n, _ := val.AsBigFloat().Int64()
		cfg.MaxInputLength = int(n)
	}

	if attr, ok := content.Attributes["max_field_length"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("max_field_length error: %s", diags.Error())
		}
		n, _ := val.AsBigFloat().Int64()
		cfg.MaxFieldLength = int(n)
	}

	if attr, ok := content.Attributes["max_field_depth"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("max_field_depth error: %s", diags.Error())
		}
		n, _ := val.AsBigFloat().Int64()
		cfg.MaxFieldDepth = int(n)
	}

	if attr, ok := content.Attributes["allowed_control_chars"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("allowed_control_chars error: %s", diags.Error())
		}
		var chars []string
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			chars = append(chars, v.AsString())
		}
		cfg.AllowedControlChars = chars
	}

	// Parse sanitizer blocks
	for _, b := range content.Blocks {
		switch b.Type {
		case "sanitizer":
			sanitizer, err := parseSanitizerBlock(b, ctx)
			if err != nil {
				return nil, fmt.Errorf("sanitizer %q error: %w", b.Labels[0], err)
			}
			cfg.Sanitizers = append(cfg.Sanitizers, sanitizer)

		case "flow":
			flowCfg, err := parseFlowSecurityBlock(b, ctx)
			if err != nil {
				return nil, fmt.Errorf("security flow %q error: %w", b.Labels[0], err)
			}
			cfg.FlowOverrides[b.Labels[0]] = flowCfg
		}
	}

	return cfg, nil
}

// parseSanitizerBlock parses a sanitizer block within security.
func parseSanitizerBlock(block *hcl.Block, ctx *hcl.EvalContext) (*security.SanitizerConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "source"},
			{Name: "wasm"},
			{Name: "entrypoint"},
			{Name: "apply_to"},
			{Name: "fields"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("sanitizer block error: %s", diags.Error())
	}

	cfg := &security.SanitizerConfig{
		Name: block.Labels[0],
	}

	if attr, ok := content.Attributes["source"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("source error: %s", diags.Error())
		}
		cfg.Source = val.AsString()
	}

	if attr, ok := content.Attributes["wasm"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("wasm error: %s", diags.Error())
		}
		cfg.WASM = val.AsString()
	}

	if attr, ok := content.Attributes["entrypoint"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("entrypoint error: %s", diags.Error())
		}
		cfg.Entrypoint = val.AsString()
	}

	if attr, ok := content.Attributes["apply_to"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("apply_to error: %s", diags.Error())
		}
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			cfg.ApplyTo = append(cfg.ApplyTo, v.AsString())
		}
	}

	if attr, ok := content.Attributes["fields"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("fields error: %s", diags.Error())
		}
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			cfg.Fields = append(cfg.Fields, v.AsString())
		}
	}

	return cfg, nil
}

// parseFlowSecurityBlock parses a flow override block within security.
func parseFlowSecurityBlock(block *hcl.Block, ctx *hcl.EvalContext) (*security.FlowSecurityConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "max_input_length"},
			{Name: "max_field_length"},
			{Name: "sanitizers"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("flow security block error: %s", diags.Error())
	}

	cfg := &security.FlowSecurityConfig{}

	if attr, ok := content.Attributes["max_input_length"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("max_input_length error: %s", diags.Error())
		}
		n, _ := val.AsBigFloat().Int64()
		cfg.MaxInputLength = int(n)
	}

	if attr, ok := content.Attributes["max_field_length"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("max_field_length error: %s", diags.Error())
		}
		n, _ := val.AsBigFloat().Int64()
		cfg.MaxFieldLength = int(n)
	}

	if attr, ok := content.Attributes["sanitizers"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("sanitizers error: %s", diags.Error())
		}
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			cfg.Sanitizers = append(cfg.Sanitizers, v.AsString())
		}
	}

	return cfg, nil
}
