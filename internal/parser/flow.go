package parser

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/transform"
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
		Attributes: []hcl.AttributeSchema{
			{Name: "returns"}, // GraphQL return type for HCL-first mode
			{Name: "cache"},   // Reference to named cache (cache.name)
			{Name: "when"},    // Flow trigger schedule
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "from"},
			{Type: "to"},
			{Type: "step", LabelNames: []string{"name"}}, // Intermediate connector calls
			{Type: "lock"},
			{Type: "semaphore"},
			{Type: "coordinate"},
			{Type: "cache"},
			{Type: "validate"},
			{Type: "enrich", LabelNames: []string{"name"}},
			{Type: "transform"},
			{Type: "require"},
			{Type: "after"},
			{Type: "error_handling"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("flow content error: %s", diags.Error())
	}

	// Parse returns attribute (for GraphQL HCL-first mode)
	if attr, ok := content.Attributes["returns"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			config.Returns = val.AsString()
		}
	}

	// Parse cache attribute (reference to named cache)
	if attr, ok := content.Attributes["cache"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			config.Cache = &flow.CacheConfig{
				Use: parseCacheReference(val.AsString()),
			}
		}
	}

	// Parse when attribute (flow trigger schedule)
	if attr, ok := content.Attributes["when"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			config.When = val.AsString()
		}
	}

	// Collect all 'to' blocks first
	var toBlocks []*flow.ToConfig

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
			toBlocks = append(toBlocks, to)

		case "step":
			step, err := parseStepBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("step block error: %w", err)
			}
			config.Steps = append(config.Steps, step)

		case "lock":
			lock, err := parseLockBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("lock block error: %w", err)
			}
			config.Lock = lock

		case "semaphore":
			sem, err := parseSemaphoreBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("semaphore block error: %w", err)
			}
			config.Semaphore = sem

		case "coordinate":
			coord, err := parseCoordinateBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("coordinate block error: %w", err)
			}
			config.Coordinate = coord

		case "cache":
			cache, err := parseCacheBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("cache block error: %w", err)
			}
			config.Cache = cache

		case "validate":
			validate, err := parseValidateBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("validate block error: %w", err)
			}
			config.Validate = validate

		case "enrich":
			enrich, err := parseEnrichBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("enrich block error: %w", err)
			}
			config.Enrichments = append(config.Enrichments, enrich)

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

		case "after":
			after, err := parseAfterBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("after block error: %w", err)
			}
			config.After = after

		case "error_handling":
			eh, err := parseErrorHandlingBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("error_handling block error: %w", err)
			}
			config.ErrorHandling = eh
		}
	}

	// Assign to blocks: single -> To, multiple -> MultiTo
	if len(toBlocks) == 1 {
		config.To = toBlocks[0]
	} else if len(toBlocks) > 1 {
		config.MultiTo = toBlocks
	}

	return config, nil
}

// parseFromBlock parses a from block.
// Supports format:
//
//	from {
//	  connector = "api"
//	  operation = "GET /users"
//	  filter    = "input.metadata.origin != 'internal'"
//	}
func parseFromBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.FromConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "operation", Required: true},
			{Name: "filter"},
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

	if attr, ok := content.Attributes["filter"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("from filter error: %s", diags.Error())
		}
		from.Filter = val.AsString()
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
//	  operation = "INSERT_ONE"  // optional, for specifying write operation type
//	  filter    = "user_id = ${context.user_id}"  // optional
//	  query     = "SELECT * FROM users WHERE id = :id"  // optional, for SQL
//	  query_filter = { status = "active" }  // optional, for NoSQL (MongoDB)
//	  update    = { "$set" = { status = "active" } }  // optional, for NoSQL updates
//	  when      = "output.total > 1000"  // optional, conditional write
//	  parallel  = true  // optional, parallel execution (default true)
//	  transform { ... }  // optional, per-destination transform
//	}
func parseToBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.ToConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "target"},
			{Name: "operation"}, // Operation type: INSERT_ONE, UPDATE_ONE, DELETE_ONE, etc.
			{Name: "filter"},
			{Name: "query"},
			{Name: "query_filter"},
			{Name: "update"},
			{Name: "params"},   // Parameters for operations like S3 COPY
			{Name: "when"},     // Conditional write
			{Name: "parallel"}, // Parallel execution (default true)
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "transform"}, // Per-destination transform
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("to block content error: %s", diags.Error())
	}

	to := &flow.ToConfig{
		Parallel: true, // Default to parallel execution
	}

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

	// Parse operation for write operations (INSERT_ONE, UPDATE_ONE, DELETE_ONE, etc.)
	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to operation error: %s", diags.Error())
		}
		to.Operation = val.AsString()
	}

	// Parse params for operations like S3 COPY
	if attr, ok := content.Attributes["params"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to params error: %s", diags.Error())
		}
		to.Params = ctyValueToMap(val)
	}

	if attr, ok := content.Attributes["filter"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to filter error: %s", diags.Error())
		}
		to.Filter = val.AsString()
	}

	if attr, ok := content.Attributes["query"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to query error: %s", diags.Error())
		}
		to.Query = val.AsString()
	}

	// Parse query_filter for NoSQL (MongoDB)
	if attr, ok := content.Attributes["query_filter"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to query_filter error: %s", diags.Error())
		}
		to.QueryFilter = ctyValueToMap(val)
	}

	// Parse update for NoSQL (MongoDB)
	if attr, ok := content.Attributes["update"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to update error: %s", diags.Error())
		}
		to.Update = ctyValueToMap(val)
	}

	// Parse when (conditional write)
	if attr, ok := content.Attributes["when"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			// Try to extract raw expression
			to.When = extractExpressionText(attr.Expr)
		} else {
			to.When = val.AsString()
		}
	}

	// Parse parallel (default true)
	if attr, ok := content.Attributes["parallel"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("to parallel error: %s", diags.Error())
		}
		if val.Type() == cty.Bool {
			to.Parallel = val.True()
		}
	}

	// Parse nested transform block
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "transform" {
			transformMappings, err := parseTransformMappings(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("to transform error: %w", err)
			}
			to.Transform = transformMappings
		}
	}

	if to.Connector == "" {
		return nil, fmt.Errorf("to block must specify a connector")
	}

	return to, nil
}

// parseStepBlock parses a step block for intermediate connector calls.
// Example:
//
//	step "get_customer" {
//	  connector = "customers_db"
//	  operation = "query"
//	  query     = "SELECT * FROM customers WHERE id = ?"
//	  params    = [input.customer_id]
//	  when      = "input.customer_id != ''"
//	  timeout   = "5s"
//	  on_error  = "skip"
//	}
func parseStepBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.StepConfig, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("step block requires a name label")
	}

	step := &flow.StepConfig{
		Name:   block.Labels[0],
		Params: make(map[string]interface{}),
		Body:   make(map[string]interface{}),
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "operation"},
			{Name: "when"},
			{Name: "query"},
			{Name: "target"},
			{Name: "params"},
			{Name: "body"},
			{Name: "timeout"},
			{Name: "on_error"},
			{Name: "default"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("step block content error: %s", diags.Error())
	}

	// Parse connector (required)
	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step connector error: %s", diags.Error())
		}
		step.Connector = parseConnectorReference(val.AsString())
	}

	// Parse operation
	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step operation error: %s", diags.Error())
		}
		step.Operation = val.AsString()
	}

	// Parse when (conditional execution)
	if attr, ok := content.Attributes["when"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step when error: %s", diags.Error())
		}
		step.When = val.AsString()
	}

	// Parse query (for database connectors)
	if attr, ok := content.Attributes["query"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step query error: %s", diags.Error())
		}
		step.Query = val.AsString()
	}

	// Parse target
	if attr, ok := content.Attributes["target"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step target error: %s", diags.Error())
		}
		step.Target = val.AsString()
	}

	// Parse params (can be map or list)
	if attr, ok := content.Attributes["params"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step params error: %s", diags.Error())
		}
		step.Params = ctyValueToMap(val)
	}

	// Parse body (for HTTP connectors)
	if attr, ok := content.Attributes["body"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step body error: %s", diags.Error())
		}
		step.Body = ctyValueToMap(val)
	}

	// Parse timeout
	if attr, ok := content.Attributes["timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step timeout error: %s", diags.Error())
		}
		step.Timeout = val.AsString()
	}

	// Parse on_error
	if attr, ok := content.Attributes["on_error"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step on_error error: %s", diags.Error())
		}
		step.OnError = val.AsString()
	}

	// Parse default value
	if attr, ok := content.Attributes["default"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("step default error: %s", diags.Error())
		}
		step.Default = ctyValueToInterface(val)
	}

	return step, nil
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

// parseEnrichBlock parses an enrich block.
// Example:
//
//	enrich "pricing" {
//	  connector = "pricing_service"
//	  operation = "getPrice"
//	  params {
//	    product_id = "input.id"
//	  }
//	}
func parseEnrichBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.EnrichConfig, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("enrich block requires a name label")
	}

	enrich := &flow.EnrichConfig{
		Name:   block.Labels[0],
		Params: make(map[string]string),
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "operation", Required: true},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "params"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("enrich block content error: %s", diags.Error())
	}

	// Parse connector attribute
	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("enrich connector error: %s", diags.Error())
		}
		enrich.Connector = parseConnectorReference(val.AsString())
	}

	// Parse operation attribute
	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("enrich operation error: %s", diags.Error())
		}
		enrich.Operation = val.AsString()
	}

	// Parse params block
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "params" {
			params, err := parseParamsBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("enrich params error: %w", err)
			}
			enrich.Params = params
		}
	}

	return enrich, nil
}

// parseConnectorReference parses a connector reference (e.g., "connector.pricing" -> "pricing").
func parseConnectorReference(ref string) string {
	if strings.HasPrefix(ref, "connector.") {
		return strings.TrimPrefix(ref, "connector.")
	}
	return ref
}

// parseParamsBlock parses a params block inside enrich.
func parseParamsBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]string, error) {
	params := make(map[string]string)

	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("params block attributes error: %s", diags.Error())
	}

	for name, attr := range attrs {
		// Try to evaluate as a simple value (for quoted strings)
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			params[name] = val.AsString()
		} else {
			// Extract raw expression text for unquoted expressions
			exprStr := extractExpressionText(attr.Expr)
			if exprStr != "" {
				params[name] = exprStr
			}
		}
	}

	return params, nil
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
		// First try to evaluate as a simple value (for quoted strings)
		val, diags := attr.Expr.Value(ctx)

		if name == "use" {
			if !diags.HasErrors() {
				transform.Use = parseTransformReference(val.AsString())
			}
			continue
		}

		// For transform mappings:
		// - Quoted strings like email = "lower(input.email)" are evaluated by HCL
		//   and we get the string content (lower(input.email)) which we then
		//   evaluate at runtime with our transform engine
		// - Unquoted expressions are extracted as raw text
		if !diags.HasErrors() {
			// HCL evaluated it successfully - use the string value
			// This handles quoted strings: "lower(input.email)" -> lower(input.email)
			transform.Mappings[name] = val.AsString()
		} else {
			// Try to extract raw expression for unquoted expressions
			exprStr := extractExpressionText(attr.Expr)
			if exprStr != "" {
				transform.Mappings[name] = exprStr
			}
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

// extractExpressionText extracts the raw text from an HCL expression.
func extractExpressionText(expr hcl.Expression) string {
	// Get the expression range
	rng := expr.Range()

	// For simple expressions, we can get the raw bytes from the file
	// However, since we don't have direct access to file bytes here,
	// we'll use expression traversal to reconstruct simple cases

	// Try to get variables from the expression
	vars := expr.Variables()

	// If it's a simple variable reference, construct the path
	if len(vars) == 1 {
		var parts []string
		for _, t := range vars[0] {
			switch tt := t.(type) {
			case hcl.TraverseRoot:
				parts = append(parts, tt.Name)
			case hcl.TraverseAttr:
				parts = append(parts, tt.Name)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ".")
		}
	}

	// For function calls and complex expressions,
	// we need to extract from the source file
	// The filename contains the path
	filename := rng.Filename
	if filename != "" {
		content, err := readFileRange(filename, rng)
		if err == nil && content != "" {
			return content
		}
	}

	return ""
}

// readFileRange reads a specific range from a file.
func readFileRange(filename string, rng hcl.Range) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}

	// Convert byte offsets
	start := rng.Start.Byte
	end := rng.End.Byte

	if start >= 0 && end <= len(content) && start < end {
		return string(content[start:end]), nil
	}

	return "", fmt.Errorf("invalid range")
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
			{Type: "fallback"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("error_handling block content error: %s", diags.Error())
	}

	eh := &flow.ErrorHandlingConfig{}

	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "retry":
			retry, err := parseRetryConfigBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("retry block error: %w", err)
			}
			eh.Retry = retry
		case "fallback":
			fallback, err := parseFallbackConfigBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("fallback block error: %w", err)
			}
			eh.Fallback = fallback
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
			{Name: "max_delay"},
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

	if attr, ok := content.Attributes["max_delay"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("retry max_delay error: %s", diags.Error())
		}
		retry.MaxDelay = val.AsString()
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

// parseFallbackConfigBlock parses a fallback block within error_handling.
func parseFallbackConfigBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.FallbackConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "target", Required: true},
			{Name: "include_error"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "transform"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("fallback block content error: %s", diags.Error())
	}

	fallback := &flow.FallbackConfig{}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("fallback connector error: %s", diags.Error())
		}
		fallback.Connector = val.AsString()
	}

	if attr, ok := content.Attributes["target"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("fallback target error: %s", diags.Error())
		}
		fallback.Target = val.AsString()
	}

	if attr, ok := content.Attributes["include_error"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("fallback include_error error: %s", diags.Error())
		}
		fallback.IncludeError = val.True()
	}

	// Parse optional transform block
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "transform" {
			mappings, err := parseTransformMappings(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("fallback transform error: %w", err)
			}
			fallback.Transform = mappings
		}
	}

	return fallback, nil
}

// parseTransformMappings parses transform mappings as map[string]string.
func parseTransformMappings(block *hcl.Block, ctx *hcl.EvalContext) (map[string]string, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("transform attributes error: %s", diags.Error())
	}

	mappings := make(map[string]string)

	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			mappings[name] = val.AsString()
		} else {
			// Try to extract raw expression
			exprStr := extractExpressionText(attr.Expr)
			if exprStr != "" {
				mappings[name] = exprStr
			}
		}
	}

	return mappings, nil
}

// parseNamedTransformBlock parses a named transform block.
// Example:
//
//	transform "user_input" {
//	  enrich "pricing" {
//	    connector = "pricing_service"
//	    operation = "getPrice"
//	    params { product_id = "input.id" }
//	  }
//
//	  id        = "uuid()"
//	  email     = "lower(trim(input.email))"
//	  price     = "enriched.pricing.value"
//	}
func parseNamedTransformBlock(block *hcl.Block, ctx *hcl.EvalContext) (*transform.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("transform block requires a name label")
	}

	cfg := &transform.Config{
		Name:     block.Labels[0],
		Mappings: make(map[string]string),
	}

	// Use PartialContent to allow both attributes and enrich blocks
	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "enrich", LabelNames: []string{"name"}},
		},
	}

	content, remain, diags := block.Body.PartialContent(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("transform block content error: %s", diags.Error())
	}

	// Parse enrich blocks
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "enrich" {
			enrich, err := parseTransformEnrichBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("enrich block error: %w", err)
			}
			cfg.Enrichments = append(cfg.Enrichments, enrich)
		}
	}

	// Parse remaining attributes as transform mappings
	attrs, diags := remain.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("transform block attributes error: %s", diags.Error())
	}

	for name, attr := range attrs {
		// Try to evaluate as simple value (for quoted strings)
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			// HCL evaluated it - use the string value
			cfg.Mappings[name] = val.AsString()
		} else {
			// Extract raw expression text for unquoted expressions
			exprStr := extractExpressionText(attr.Expr)
			if exprStr != "" {
				cfg.Mappings[name] = exprStr
			}
		}
	}

	return cfg, nil
}

// parseTransformEnrichBlock parses an enrich block inside a named transform.
// Returns transform.EnrichConfig to avoid circular imports.
func parseTransformEnrichBlock(block *hcl.Block, ctx *hcl.EvalContext) (*transform.EnrichConfig, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("enrich block requires a name label")
	}

	enrich := &transform.EnrichConfig{
		Name:   block.Labels[0],
		Params: make(map[string]string),
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "operation", Required: true},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "params"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("enrich block content error: %s", diags.Error())
	}

	// Parse connector attribute
	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("enrich connector error: %s", diags.Error())
		}
		enrich.Connector = parseConnectorReference(val.AsString())
	}

	// Parse operation attribute
	if attr, ok := content.Attributes["operation"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("enrich operation error: %s", diags.Error())
		}
		enrich.Operation = val.AsString()
	}

	// Parse params block
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "params" {
			params, err := parseParamsBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("enrich params error: %w", err)
			}
			enrich.Params = params
		}
	}

	return enrich, nil
}

// ctyValueToMap converts a cty.Value to map[string]interface{}.
// Used for parsing NoSQL query filters and update documents from HCL.
func ctyValueToMap(val cty.Value) map[string]interface{} {
	if val.IsNull() {
		return nil
	}

	// Handle object/map types
	if val.Type().IsObjectType() || val.Type().IsMapType() {
		result := make(map[string]interface{})
		for it := val.ElementIterator(); it.Next(); {
			key, v := it.Element()
			result[key.AsString()] = ctyValueToInterface(v)
		}
		return result
	}

	return nil
}

// ctyValueToInterface converts a cty.Value to interface{}.
// Handles all cty types recursively.
func ctyValueToInterface(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}

	valType := val.Type()

	// Handle primitives
	if valType == cty.String {
		return val.AsString()
	}
	if valType == cty.Number {
		bf := val.AsBigFloat()
		// Try to return as int64 if it's a whole number
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		// Otherwise return as float64
		f, _ := bf.Float64()
		return f
	}
	if valType == cty.Bool {
		return val.True()
	}

	// Handle lists/tuples
	if valType.IsListType() || valType.IsTupleType() || valType.IsSetType() {
		var result []interface{}
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			result = append(result, ctyValueToInterface(v))
		}
		return result
	}

	// Handle objects/maps
	if valType.IsObjectType() || valType.IsMapType() {
		result := make(map[string]interface{})
		for it := val.ElementIterator(); it.Next(); {
			key, v := it.Element()
			result[key.AsString()] = ctyValueToInterface(v)
		}
		return result
	}

	// Fallback: try to get string representation
	return val.GoString()
}

// parseCacheReference parses a cache reference (e.g., "cache.products" -> "products").
func parseCacheReference(ref string) string {
	if strings.HasPrefix(ref, "cache.") {
		return strings.TrimPrefix(ref, "cache.")
	}
	return ref
}

// parseCacheBlock parses a cache block in a flow.
// Supports format:
//
//	cache {
//	  storage = "redis_cache"
//	  ttl     = "5m"
//	  key     = "products:${input.params.id}"
//	  invalidate_on = ["products:updated:${input.params.id}"]
//	}
func parseCacheBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.CacheConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "ttl"},
			{Name: "key"},
			{Name: "invalidate_on"},
			{Name: "use"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("cache block content error: %s", diags.Error())
	}

	cache := &flow.CacheConfig{}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache storage error: %s", diags.Error())
		}
		cache.Storage = parseConnectorReference(val.AsString())
	}

	if attr, ok := content.Attributes["ttl"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache ttl error: %s", diags.Error())
		}
		cache.TTL = val.AsString()
	}

	if attr, ok := content.Attributes["key"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache key error: %s", diags.Error())
		}
		cache.Key = val.AsString()
	}

	if attr, ok := content.Attributes["invalidate_on"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache invalidate_on error: %s", diags.Error())
		}
		if val.Type().IsListType() || val.Type().IsTupleType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				cache.InvalidateOn = append(cache.InvalidateOn, v.AsString())
			}
		}
	}

	if attr, ok := content.Attributes["use"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache use error: %s", diags.Error())
		}
		cache.Use = parseCacheReference(val.AsString())
	}

	return cache, nil
}

// parseAfterBlock parses an after block in a flow.
// Supports format:
//
//	after {
//	  invalidate {
//	    storage = "redis_cache"
//	    keys    = ["products:${input.params.id}"]
//	    patterns = ["products:list:*"]
//	  }
//	}
func parseAfterBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.AfterConfig, error) {
	schema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "invalidate"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("after block content error: %s", diags.Error())
	}

	after := &flow.AfterConfig{}

	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "invalidate" {
			inv, err := parseInvalidateBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("invalidate block error: %w", err)
			}
			after.Invalidate = inv
		}
	}

	return after, nil
}

// parseInvalidateBlock parses an invalidate block.
// Supports format:
//
//	invalidate {
//	  storage  = "redis_cache"
//	  keys     = ["products:${input.params.id}"]
//	  patterns = ["products:list:*"]
//	}
func parseInvalidateBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.InvalidateConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "keys"},
			{Name: "patterns"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("invalidate block content error: %s", diags.Error())
	}

	inv := &flow.InvalidateConfig{}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("invalidate storage error: %s", diags.Error())
		}
		inv.Storage = parseConnectorReference(val.AsString())
	}

	if attr, ok := content.Attributes["keys"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("invalidate keys error: %s", diags.Error())
		}
		if val.Type().IsListType() || val.Type().IsTupleType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				inv.Keys = append(inv.Keys, v.AsString())
			}
		}
	}

	if attr, ok := content.Attributes["patterns"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("invalidate patterns error: %s", diags.Error())
		}
		if val.Type().IsListType() || val.Type().IsTupleType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				inv.Patterns = append(inv.Patterns, v.AsString())
			}
		}
	}

	return inv, nil
}

// parseNamedCacheBlock parses a named cache block.
// Supports format:
//
//	cache "products_cache" {
//	  storage       = "redis_cache"
//	  ttl           = "10m"
//	  prefix        = "products"
//	  invalidate_on = ["product.created", "product.updated"]
//	}
func parseNamedCacheBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.NamedCacheConfig, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("cache block requires a name label")
	}

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "ttl"},
			{Name: "prefix"},
			{Name: "invalidate_on"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("cache block content error: %s", diags.Error())
	}

	cache := &flow.NamedCacheConfig{
		Name: block.Labels[0],
	}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache storage error: %s", diags.Error())
		}
		cache.Storage = parseConnectorReference(val.AsString())
	}

	if attr, ok := content.Attributes["ttl"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache ttl error: %s", diags.Error())
		}
		cache.TTL = val.AsString()
	}

	if attr, ok := content.Attributes["prefix"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache prefix error: %s", diags.Error())
		}
		cache.Prefix = val.AsString()
	}

	if attr, ok := content.Attributes["invalidate_on"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cache invalidate_on error: %s", diags.Error())
		}
		if val.Type().IsListType() || val.Type().IsTupleType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				cache.InvalidateOn = append(cache.InvalidateOn, v.AsString())
			}
		}
	}

	return cache, nil
}

// parseLockBlock parses a lock block in a flow.
// Supports format:
//
//	lock {
//	  storage = "redis"
//	  key     = "'user:' + input.body.user_id"
//	  timeout = "30s"
//	  wait    = true
//	  retry   = "100ms"
//	}
func parseLockBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.LockConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "key", Required: true},
			{Name: "timeout"},
			{Name: "wait"},
			{Name: "retry"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("lock block content error: %s", diags.Error())
	}

	lock := &flow.LockConfig{}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("lock storage error: %s", diags.Error())
		}
		lock.Storage = parseConnectorReference(val.AsString())
	}

	if attr, ok := content.Attributes["key"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("lock key error: %s", diags.Error())
		}
		lock.Key = val.AsString()
	}

	if attr, ok := content.Attributes["timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("lock timeout error: %s", diags.Error())
		}
		lock.Timeout = val.AsString()
	}

	if attr, ok := content.Attributes["wait"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("lock wait error: %s", diags.Error())
		}
		lock.Wait = val.True()
	}

	if attr, ok := content.Attributes["retry"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("lock retry error: %s", diags.Error())
		}
		lock.Retry = val.AsString()
	}

	return lock, nil
}

// parseSemaphoreBlock parses a semaphore block in a flow.
// Supports format:
//
//	semaphore {
//	  storage     = "redis"
//	  key         = "'external_api'"
//	  max_permits = 10
//	  timeout     = "30s"
//	  lease       = "60s"
//	}
func parseSemaphoreBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.SemaphoreConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "key", Required: true},
			{Name: "max_permits", Required: true},
			{Name: "timeout"},
			{Name: "lease"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("semaphore block content error: %s", diags.Error())
	}

	sem := &flow.SemaphoreConfig{}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("semaphore storage error: %s", diags.Error())
		}
		sem.Storage = parseConnectorReference(val.AsString())
	}

	if attr, ok := content.Attributes["key"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("semaphore key error: %s", diags.Error())
		}
		sem.Key = val.AsString()
	}

	if attr, ok := content.Attributes["max_permits"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("semaphore max_permits error: %s", diags.Error())
		}
		bf := val.AsBigFloat()
		i, _ := bf.Int64()
		sem.MaxPermits = int(i)
	}

	if attr, ok := content.Attributes["timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("semaphore timeout error: %s", diags.Error())
		}
		sem.Timeout = val.AsString()
	}

	if attr, ok := content.Attributes["lease"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("semaphore lease error: %s", diags.Error())
		}
		sem.Lease = val.AsString()
	}

	return sem, nil
}

// parseCoordinateBlock parses a coordinate block in a flow.
// Supports format:
//
//	coordinate {
//	  storage              = "redis"
//	  timeout              = "60s"
//	  on_timeout           = "fail"
//	  max_retries          = 3
//	  max_concurrent_waits = 10
//
//	  wait {
//	    when = "input.headers.type == 'child'"
//	    for  = "'parent:' + input.headers.parent_id + ':ready'"
//	  }
//
//	  signal {
//	    when = "input.headers.type == 'parent'"
//	    emit = "'parent:' + input.body.id + ':ready'"
//	    ttl  = "5m"
//	  }
//
//	  preflight {
//	    connector = "postgres"
//	    query     = "SELECT 1 FROM entities WHERE id = :parent_id"
//	    params    = { parent_id = "input.headers.parent_id" }
//	    if_exists = "pass"
//	  }
//	}
func parseCoordinateBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.CoordinateConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "storage", Required: true},
			{Name: "timeout"},
			{Name: "on_timeout"},
			{Name: "max_retries"},
			{Name: "max_concurrent_waits"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "wait"},
			{Type: "signal"},
			{Type: "preflight"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("coordinate block content error: %s", diags.Error())
	}

	coord := &flow.CoordinateConfig{}

	if attr, ok := content.Attributes["storage"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("coordinate storage error: %s", diags.Error())
		}
		coord.Storage = parseConnectorReference(val.AsString())
	}

	if attr, ok := content.Attributes["timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("coordinate timeout error: %s", diags.Error())
		}
		coord.Timeout = val.AsString()
	}

	if attr, ok := content.Attributes["on_timeout"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("coordinate on_timeout error: %s", diags.Error())
		}
		coord.OnTimeout = val.AsString()
	}

	if attr, ok := content.Attributes["max_retries"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("coordinate max_retries error: %s", diags.Error())
		}
		bf := val.AsBigFloat()
		i, _ := bf.Int64()
		coord.MaxRetries = int(i)
	}

	if attr, ok := content.Attributes["max_concurrent_waits"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("coordinate max_concurrent_waits error: %s", diags.Error())
		}
		bf := val.AsBigFloat()
		i, _ := bf.Int64()
		coord.MaxConcurrentWaits = int(i)
	}

	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "wait":
			wait, err := parseWaitBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("wait block error: %w", err)
			}
			coord.Wait = wait

		case "signal":
			signal, err := parseSignalBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("signal block error: %w", err)
			}
			coord.Signal = signal

		case "preflight":
			preflight, err := parsePreflightBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("preflight block error: %w", err)
			}
			coord.Preflight = preflight
		}
	}

	return coord, nil
}

// parseWaitBlock parses a wait block inside coordinate.
func parseWaitBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.WaitConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "when", Required: true},
			{Name: "for", Required: true},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("wait block content error: %s", diags.Error())
	}

	wait := &flow.WaitConfig{}

	if attr, ok := content.Attributes["when"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("wait when error: %s", diags.Error())
		}
		wait.When = val.AsString()
	}

	if attr, ok := content.Attributes["for"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("wait for error: %s", diags.Error())
		}
		wait.For = val.AsString()
	}

	return wait, nil
}

// parseSignalBlock parses a signal block inside coordinate.
func parseSignalBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.SignalConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "when", Required: true},
			{Name: "emit", Required: true},
			{Name: "ttl"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("signal block content error: %s", diags.Error())
	}

	signal := &flow.SignalConfig{}

	if attr, ok := content.Attributes["when"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("signal when error: %s", diags.Error())
		}
		signal.When = val.AsString()
	}

	if attr, ok := content.Attributes["emit"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("signal emit error: %s", diags.Error())
		}
		signal.Emit = val.AsString()
	}

	if attr, ok := content.Attributes["ttl"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("signal ttl error: %s", diags.Error())
		}
		signal.TTL = val.AsString()
	}

	return signal, nil
}

// parsePreflightBlock parses a preflight block inside coordinate.
func parsePreflightBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.PreflightConfig, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector", Required: true},
			{Name: "query", Required: true},
			{Name: "params"},
			{Name: "if_exists"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("preflight block content error: %s", diags.Error())
	}

	preflight := &flow.PreflightConfig{
		Params: make(map[string]string),
	}

	if attr, ok := content.Attributes["connector"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("preflight connector error: %s", diags.Error())
		}
		preflight.Connector = parseConnectorReference(val.AsString())
	}

	if attr, ok := content.Attributes["query"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("preflight query error: %s", diags.Error())
		}
		preflight.Query = val.AsString()
	}

	if attr, ok := content.Attributes["params"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("preflight params error: %s", diags.Error())
		}
		if val.Type().IsObjectType() || val.Type().IsMapType() {
			for it := val.ElementIterator(); it.Next(); {
				k, v := it.Element()
				preflight.Params[k.AsString()] = v.AsString()
			}
		}
	}

	if attr, ok := content.Attributes["if_exists"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("preflight if_exists error: %s", diags.Error())
		}
		preflight.IfExists = val.AsString()
	}

	return preflight, nil
}
