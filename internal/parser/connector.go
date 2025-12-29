package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/mycel-labs/mycel/internal/connector"
)

// parseConnectorBlock parses a connector block from HCL.
func parseConnectorBlock(block *hcl.Block, ctx *hcl.EvalContext) (*connector.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("connector block requires a name label")
	}

	config := &connector.Config{
		Name:       block.Labels[0],
		Properties: make(map[string]interface{}),
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type", Required: true},
			{Name: "driver"},
			{Name: "host"},
			{Name: "port"},
			{Name: "database"},
			{Name: "user"},
			{Name: "password"},
			{Name: "base_url"},
			{Name: "timeout"},
			{Name: "retry_count"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "pool"},
			{Type: "cors"},
			{Type: "auth"},
			{Type: "retry"},
			{Type: "mock"},
			{Type: "headers"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("connector content error: %s", diags.Error())
	}

	// Parse required type attribute
	if attr, ok := content.Attributes["type"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("type attribute error: %s", diags.Error())
		}
		config.Type = val.AsString()
	}

	// Parse optional attributes
	for name, attr := range content.Attributes {
		if name == "type" {
			continue // Already handled
		}

		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}

		// Set driver on config directly for factory lookup
		if name == "driver" {
			config.Driver = val.AsString()
		}

		config.Properties[name] = ctyValueToGo(val)
	}

	// Parse nested blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "pool":
			pool, err := parsePoolBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("pool block error: %w", err)
			}
			config.Properties["pool"] = pool

		case "cors":
			cors, err := parseCorsBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("cors block error: %w", err)
			}
			config.Properties["cors"] = cors

		case "auth":
			auth, err := parseAuthBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("auth block error: %w", err)
			}
			config.Properties["auth"] = auth

		case "retry":
			retry, err := parseRetryBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("retry block error: %w", err)
			}
			config.Properties["retry"] = retry

		case "mock":
			mock, err := parseMockBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("mock block error: %w", err)
			}
			config.Properties["mock"] = mock

		case "headers":
			headers, err := parseHeadersBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("headers block error: %w", err)
			}
			config.Properties["headers"] = headers
		}
	}

	return config, nil
}

// parsePoolBlock parses a pool configuration block.
func parsePoolBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "min"},
			{Name: "max"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("pool block content error: %s", diags.Error())
	}

	pool := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("pool %s error: %s", name, diags.Error())
		}
		pool[name] = ctyValueToGo(val)
	}

	return pool, nil
}

// parseCorsBlock parses a CORS configuration block.
func parseCorsBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "origins"},
			{Name: "methods"},
			{Name: "headers"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("cors block content error: %s", diags.Error())
	}

	cors := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cors %s error: %s", name, diags.Error())
		}
		cors[name] = ctyValueToGo(val)
	}

	return cors, nil
}

// parseAuthBlock parses an auth configuration block.
func parseAuthBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type"},
			// Bearer token
			{Name: "token"},
			{Name: "header"},
			// OAuth2
			{Name: "refresh_token"},
			{Name: "token_url"},
			{Name: "client_id"},
			{Name: "client_secret"},
			{Name: "scopes"},
			// API Key
			{Name: "api_key"},
			{Name: "api_key_header"},
			{Name: "api_key_query"},
			// Basic auth
			{Name: "username"},
			{Name: "password"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("auth block content error: %s", diags.Error())
	}

	auth := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("auth %s error: %s", name, diags.Error())
		}
		auth[name] = ctyValueToGo(val)
	}

	return auth, nil
}

// parseHeadersBlock parses a headers configuration block.
func parseHeadersBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	// Headers block uses dynamic attributes
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("headers block content error: %s", diags.Error())
	}

	headers := make(map[string]interface{})
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("header %s error: %s", name, diags.Error())
		}
		headers[name] = ctyValueToGo(val)
	}

	return headers, nil
}

// parseRetryBlock parses a retry configuration block.
func parseRetryBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "attempts"},
			{Name: "backoff"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("retry block content error: %s", diags.Error())
	}

	retry := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("retry %s error: %s", name, diags.Error())
		}
		retry[name] = ctyValueToGo(val)
	}

	return retry, nil
}

// parseMockBlock parses a mock configuration block.
func parseMockBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "source"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("mock block content error: %s", diags.Error())
	}

	mock := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("mock %s error: %s", name, diags.Error())
		}
		mock[name] = ctyValueToGo(val)
	}

	return mock, nil
}

// ctyValueToGo converts a cty.Value to a native Go value.
func ctyValueToGo(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}

	switch val.Type() {
	case cty.String:
		return val.AsString()

	case cty.Number:
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return int(i)
		}
		f, _ := bf.Float64()
		return f

	case cty.Bool:
		return val.True()

	default:
		// Handle lists
		if val.Type().IsListType() || val.Type().IsTupleType() {
			var result []interface{}
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				result = append(result, ctyValueToGo(v))
			}
			return result
		}

		// Handle maps
		if val.Type().IsMapType() || val.Type().IsObjectType() {
			result := make(map[string]interface{})
			for it := val.ElementIterator(); it.Next(); {
				k, v := it.Element()
				result[k.AsString()] = ctyValueToGo(v)
			}
			return result
		}

		return val.GoString()
	}
}
