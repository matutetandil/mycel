package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/internal/aspect"
)

// parseAspectBlock parses an aspect block.
func parseAspectBlock(block *hcl.Block, ctx *hcl.EvalContext) (*aspect.Config, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "on", Required: true},
			{Name: "when", Required: true},
			{Name: "if"},
			{Name: "priority"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "action"},
			{Type: "cache"},
			{Type: "invalidate"},
			{Type: "rate_limit"},
			{Type: "circuit_breaker"},
			{Type: "response"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect block error: %s", diags.Error())
	}

	config := &aspect.Config{
		Name: block.Labels[0],
	}

	// Parse "on" attribute (list of patterns)
	if attr, ok := content.Attributes["on"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("aspect 'on' error: %s", diags.Error())
		}

		if val.Type().IsTupleType() || val.Type().IsListType() {
			for _, v := range val.AsValueSlice() {
				config.On = append(config.On, v.AsString())
			}
		} else if val.Type() == cty.String {
			config.On = append(config.On, val.AsString())
		}
	}

	// Parse "when" attribute
	if attr, ok := content.Attributes["when"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("aspect 'when' error: %s", diags.Error())
		}
		config.When = aspect.When(val.AsString())
	}

	// Parse "if" attribute (optional condition)
	if attr, ok := content.Attributes["if"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("aspect 'if' error: %s", diags.Error())
		}
		config.If = val.AsString()
	}

	// Parse "priority" attribute (optional)
	if attr, ok := content.Attributes["priority"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("aspect 'priority' error: %s", diags.Error())
		}
		priority, _ := val.AsBigFloat().Int64()
		config.Priority = int(priority)
	}

	// Parse nested blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "action":
			action, err := parseAspectActionBlock(nestedBlock, ctx)
			if err != nil {
				return nil, err
			}
			config.Action = action

		case "cache":
			cache, err := parseAspectCacheBlock(nestedBlock, ctx)
			if err != nil {
				return nil, err
			}
			config.Cache = cache

		case "invalidate":
			invalidate, err := parseAspectInvalidateBlock(nestedBlock, ctx)
			if err != nil {
				return nil, err
			}
			config.Invalidate = invalidate

		case "rate_limit":
			rateLimit, err := parseAspectRateLimitBlock(nestedBlock, ctx)
			if err != nil {
				return nil, err
			}
			config.RateLimit = rateLimit

		case "circuit_breaker":
			circuitBreaker, err := parseAspectCircuitBreakerBlock(nestedBlock, ctx)
			if err != nil {
				return nil, err
			}
			config.CircuitBreaker = circuitBreaker

		case "response":
			response, err := parseAspectResponseBlock(nestedBlock, ctx)
			if err != nil {
				return nil, err
			}
			config.Response = response
		}
	}

	return config, nil
}

// parseAspectActionBlock parses an action block within an aspect.
func parseAspectActionBlock(block *hcl.Block, ctx *hcl.EvalContext) (*aspect.ActionConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector"},
			{Name: "flow"},
			{Name: "operation"},
			{Name: "target"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "transform"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect action block error: %s", diags.Error())
	}

	action := &aspect.ActionConfig{}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("action 'connector' error: %s", diags.Error())
		}
		action.Connector = val.AsString()
	}

	if attr, ok := content.Attributes["flow"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("action 'flow' error: %s", diags.Error())
		}
		action.Flow = val.AsString()
	}

	// Validate mutual exclusivity: connector XOR flow
	if action.Connector != "" && action.Flow != "" {
		return nil, fmt.Errorf("aspect action: 'connector' and 'flow' are mutually exclusive")
	}
	if action.Connector == "" && action.Flow == "" {
		return nil, fmt.Errorf("aspect action: either 'connector' or 'flow' must be specified")
	}

	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("action 'operation' error: %s", diags.Error())
		}
		action.Operation = val.AsString()
	}

	if attr, ok := content.Attributes["target"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("action 'target' error: %s", diags.Error())
		}
		action.Target = val.AsString()
	}

	// Parse transform block
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "transform" {
			transform, err := parseAspectTransformBlock(nestedBlock, ctx)
			if err != nil {
				return nil, err
			}
			action.Transform = transform
		}
	}

	return action, nil
}

// parseAspectTransformBlock parses a transform block within an aspect action.
func parseAspectTransformBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]string, error) {
	// Use remain to get all attributes dynamically
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect transform block error: %s", diags.Error())
	}

	transform := make(map[string]string)
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("transform '%s' error: %s", name, diags.Error())
		}
		transform[name] = val.AsString()
	}

	return transform, nil
}

// parseAspectResponseBlock parses a response block within an aspect.
// Supports CEL expression fields (body enrichment) and a headers map (HTTP headers).
func parseAspectResponseBlock(block *hcl.Block, ctx *hcl.EvalContext) (*aspect.ResponseConfig, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect response block error: %s", diags.Error())
	}

	config := &aspect.ResponseConfig{
		Fields:  make(map[string]string),
		Headers: make(map[string]string),
	}

	for name, attr := range attrs {
		if name == "headers" {
			// Parse headers as a map of string → string
			val, valDiags := attr.Expr.Value(ctx)
			if valDiags.HasErrors() {
				return nil, fmt.Errorf("response 'headers' error: %s", valDiags.Error())
			}
			if val.Type().IsObjectType() || val.Type().IsMapType() {
				for k, v := range val.AsValueMap() {
					config.Headers[k] = v.AsString()
				}
			}
			continue
		}

		// Regular field — CEL expression
		val, valDiags := attr.Expr.Value(ctx)
		if valDiags.HasErrors() {
			return nil, fmt.Errorf("response '%s' error: %s", name, valDiags.Error())
		}
		config.Fields[name] = val.AsString()
	}

	return config, nil
}

// parseAspectCacheBlock parses a cache block within an aspect.
func parseAspectCacheBlock(block *hcl.Block, ctx *hcl.EvalContext) (*aspect.CacheConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "ttl", Required: true},
			{Name: "key", Required: true},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect cache block error: %s", diags.Error())
	}

	cache := &aspect.CacheConfig{}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache 'storage' error: %s", diags.Error())
		}
		cache.Storage = val.AsString()
	}

	if attr, ok := content.Attributes["ttl"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache 'ttl' error: %s", diags.Error())
		}
		cache.TTL = val.AsString()
	}

	if attr, ok := content.Attributes["key"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			// Key may contain template expressions like ${input.id}
			// Extract raw text instead of evaluating
			cache.Key = extractExpressionText(attr.Expr)
		} else {
			cache.Key = val.AsString()
		}
	}

	return cache, nil
}

// parseAspectInvalidateBlock parses an invalidate block within an aspect.
func parseAspectInvalidateBlock(block *hcl.Block, ctx *hcl.EvalContext) (*aspect.InvalidateConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "keys"},
			{Name: "patterns"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect invalidate block error: %s", diags.Error())
	}

	invalidate := &aspect.InvalidateConfig{}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("invalidate 'storage' error: %s", diags.Error())
		}
		invalidate.Storage = val.AsString()
	}

	if attr, ok := content.Attributes["keys"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			// Keys may contain template expressions like ${input.id}
			// Extract raw elements from the tuple expression
			invalidate.Keys = extractTupleElements(attr.Expr)
		} else if val.Type().IsTupleType() || val.Type().IsListType() {
			for _, v := range val.AsValueSlice() {
				invalidate.Keys = append(invalidate.Keys, v.AsString())
			}
		}
	}

	if attr, ok := content.Attributes["patterns"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			// Patterns may contain template expressions
			invalidate.Patterns = extractTupleElements(attr.Expr)
		} else if val.Type().IsTupleType() || val.Type().IsListType() {
			for _, v := range val.AsValueSlice() {
				invalidate.Patterns = append(invalidate.Patterns, v.AsString())
			}
		}
	}

	return invalidate, nil
}

// parseAspectRateLimitBlock parses a rate_limit block within an aspect.
func parseAspectRateLimitBlock(block *hcl.Block, ctx *hcl.EvalContext) (*aspect.RateLimitConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "key", Required: true},
			{Name: "requests_per_second", Required: true},
			{Name: "burst"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect rate_limit block error: %s", diags.Error())
	}

	rateLimit := &aspect.RateLimitConfig{}

	if attr, ok := content.Attributes["key"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("rate_limit 'key' error: %s", diags.Error())
		}
		rateLimit.Key = val.AsString()
	}

	if attr, ok := content.Attributes["requests_per_second"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("rate_limit 'requests_per_second' error: %s", diags.Error())
		}
		rps, _ := val.AsBigFloat().Float64()
		rateLimit.RequestsPerSecond = rps
	}

	if attr, ok := content.Attributes["burst"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("rate_limit 'burst' error: %s", diags.Error())
		}
		burst, _ := val.AsBigFloat().Int64()
		rateLimit.Burst = int(burst)
	}

	return rateLimit, nil
}

// parseAspectCircuitBreakerBlock parses a circuit_breaker block within an aspect.
func parseAspectCircuitBreakerBlock(block *hcl.Block, ctx *hcl.EvalContext) (*aspect.CircuitBreakerConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "name"},
			{Name: "failure_threshold", Required: true},
			{Name: "success_threshold"},
			{Name: "timeout", Required: true},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("aspect circuit_breaker block error: %s", diags.Error())
	}

	cb := &aspect.CircuitBreakerConfig{}

	if attr, ok := content.Attributes["name"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("circuit_breaker 'name' error: %s", diags.Error())
		}
		cb.Name = val.AsString()
	}

	if attr, ok := content.Attributes["failure_threshold"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("circuit_breaker 'failure_threshold' error: %s", diags.Error())
		}
		threshold, _ := val.AsBigFloat().Int64()
		cb.FailureThreshold = int(threshold)
	}

	if attr, ok := content.Attributes["success_threshold"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("circuit_breaker 'success_threshold' error: %s", diags.Error())
		}
		threshold, _ := val.AsBigFloat().Int64()
		cb.SuccessThreshold = int(threshold)
	}

	if attr, ok := content.Attributes["timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("circuit_breaker 'timeout' error: %s", diags.Error())
		}
		cb.Timeout = val.AsString()
	}

	return cb, nil
}

// extractTupleElements extracts string elements from a tuple expression
// without evaluating variables. Used for templates like ["products:${input.id}"].
func extractTupleElements(expr hcl.Expression) []string {
	var result []string

	// Try to cast to hclsyntax.TupleConsExpr
	if tupleExpr, ok := expr.(*hclsyntax.TupleConsExpr); ok {
		for _, elem := range tupleExpr.Exprs {
			// Extract the raw text of each element
			elemText := extractExpressionText(elem)
			if elemText != "" {
				result = append(result, elemText)
			}
		}
	}

	return result
}
