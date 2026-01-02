package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/profile"
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
			{Name: "type"}, // Not required - profiled connectors don't have type at root
			{Name: "driver"},
			{Name: "host"},
			{Name: "port"},
			{Name: "database"},
			{Name: "user"},
			{Name: "password"},
			{Name: "base_url"},
			{Name: "timeout"},
			{Name: "retry_count"},
			// GraphQL specific
			{Name: "endpoint"},
			{Name: "playground"},
			{Name: "playground_path"},
			// TCP specific
			{Name: "protocol"},
			{Name: "max_connections"},
			{Name: "read_timeout"},
			{Name: "write_timeout"},
			// MQ specific
			{Name: "brokers"},
			// Exec specific
			{Name: "command"},
			{Name: "args"},
			{Name: "shell"},
			{Name: "env"},
			{Name: "working_dir"},
			{Name: "input_format"},
			{Name: "output_format"},
			{Name: "retry_delay"},
			// Profile-specific attributes
			{Name: "select"},   // CEL expression for profile selection
			{Name: "default"},  // Default profile name
			{Name: "fallback"}, // Fallback profile list
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "pool"},
			{Type: "cors"},
			{Type: "auth"},
			{Type: "retry"},
			{Type: "mock"},
			{Type: "headers"},
			{Type: "schema"},
			{Type: "ssh"},
			{Type: "queue"},
			{Type: "publisher"},
			{Type: "consumer"},
			{Type: "producer"},
			{Type: "federation"},
			{Type: "profile", LabelNames: []string{"name"}}, // Profile blocks
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

		case "schema":
			schema, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("schema block error: %w", err)
			}
			config.Properties["schema"] = schema

		case "ssh":
			ssh, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("ssh block error: %w", err)
			}
			config.Properties["ssh"] = ssh

		case "queue":
			queue, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("queue block error: %w", err)
			}
			config.Properties["queue"] = queue

		case "publisher":
			pub, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("publisher block error: %w", err)
			}
			config.Properties["publisher"] = pub

		case "consumer":
			consumer, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("consumer block error: %w", err)
			}
			config.Properties["consumer"] = consumer

		case "producer":
			producer, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("producer block error: %w", err)
			}
			config.Properties["producer"] = producer

		case "federation":
			federation, err := parseFederationBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("federation block error: %w", err)
			}
			config.Properties["federation"] = federation

		case "profile":
			if len(nestedBlock.Labels) < 1 {
				return nil, fmt.Errorf("profile block requires a name label")
			}
			profileDef, err := parseProfileBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("profile %s error: %w", nestedBlock.Labels[0], err)
			}

			// Initialize profiles map if needed
			profiles, ok := config.Properties["_profiles"].(*profile.Config)
			if !ok {
				profiles = &profile.Config{
					Profiles: make(map[string]*profile.ProfileDef),
				}
				config.Properties["_profiles"] = profiles
			}
			profiles.Profiles[profileDef.Name] = profileDef
		}
	}

	// Handle profile configuration
	if profileConfig, ok := config.Properties["_profiles"].(*profile.Config); ok {
		// Get select, default, fallback from properties
		if sel, ok := config.Properties["select"].(string); ok {
			profileConfig.Select = sel
		}
		if def, ok := config.Properties["default"].(string); ok {
			profileConfig.Default = def
		}
		if fb, ok := config.Properties["fallback"].([]interface{}); ok {
			for _, f := range fb {
				if s, ok := f.(string); ok {
					profileConfig.Fallback = append(profileConfig.Fallback, s)
				}
			}
		}

		// Validate: profiled connector needs either select or default
		if profileConfig.Select == "" && profileConfig.Default == "" {
			return nil, fmt.Errorf("profiled connector %s requires 'select' or 'default' attribute", config.Name)
		}

		// Mark as profiled connector
		config.Type = "profiled"
	} else if config.Type == "" {
		return nil, fmt.Errorf("connector %s requires 'type' attribute or 'profile' blocks", config.Name)
	}

	return config, nil
}

// parseProfileBlock parses a profile block inside a connector.
func parseProfileBlock(block *hcl.Block, ctx *hcl.EvalContext) (*profile.ProfileDef, error) {
	profileName := block.Labels[0]

	// Profile uses the same schema as a regular connector plus transform
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
			{Name: "endpoint"},
			{Name: "playground"},
			{Name: "brokers"},
			{Name: "uri"},
			{Name: "url"},
			{Name: "address"},
			{Name: "bucket"},
			{Name: "region"},
			{Name: "access_key"},
			{Name: "secret_key"},
			{Name: "charset"},
			{Name: "ssl_mode"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "pool"},
			{Type: "auth"},
			{Type: "headers"},
			{Type: "transform"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("profile content error: %s", diags.Error())
	}

	// Build connector config for this profile
	connConfig := &connector.Config{
		Name:       profileName,
		Properties: make(map[string]interface{}),
	}

	// Parse attributes
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}

		if name == "type" {
			connConfig.Type = val.AsString()
		} else if name == "driver" {
			connConfig.Driver = val.AsString()
		}
		connConfig.Properties[name] = ctyValueToGo(val)
	}

	profileDef := &profile.ProfileDef{
		Name:            profileName,
		ConnectorConfig: connConfig,
		Transform:       make(map[string]string),
	}

	// Parse nested blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "pool":
			pool, err := parsePoolBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("pool block error: %w", err)
			}
			connConfig.Properties["pool"] = pool

		case "auth":
			auth, err := parseAuthBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("auth block error: %w", err)
			}
			connConfig.Properties["auth"] = auth

		case "headers":
			headers, err := parseHeadersBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("headers block error: %w", err)
			}
			connConfig.Properties["headers"] = headers

		case "transform":
			transform, err := parseProfileTransformBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("transform block error: %w", err)
			}
			profileDef.Transform = transform
		}
	}

	return profileDef, nil
}

// parseProfileTransformBlock parses a transform block inside a profile.
func parseProfileTransformBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]string, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("transform block content error: %s", diags.Error())
	}

	transform := make(map[string]string)
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("transform %s error: %s", name, diags.Error())
		}
		// Store as string (CEL expression)
		transform[name] = val.AsString()
	}

	return transform, nil
}

// parseFederationBlock parses a GraphQL Federation configuration block.
func parseFederationBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "version"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("federation block content error: %s", diags.Error())
	}

	federation := make(map[string]interface{})

	// Default enabled to true if block exists
	federation["enabled"] = true

	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("federation %s error: %s", name, diags.Error())
		}
		federation[name] = ctyValueToGo(val)
	}

	return federation, nil
}

// parseGenericBlock parses a block with arbitrary attributes.
func parseGenericBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("block content error: %s", diags.Error())
	}

	result := make(map[string]interface{})
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}
		result[name] = ctyValueToGo(val)
	}

	return result, nil
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
