package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/internal/validate"
)

// coerceInt converts a cty.Value to int, accepting either a number or a
// numeric string. Returning a typed error (rather than panicking) lets HCL
// values produced by env() — which are always strings — be used wherever an
// int is expected.
func coerceInt(val cty.Value) (int, error) {
	if val.IsNull() {
		return 0, nil
	}
	switch val.Type() {
	case cty.Number:
		bf := val.AsBigFloat()
		i, _ := bf.Int64()
		return int(i), nil
	case cty.String:
		s := val.AsString()
		if s == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("expected number, got non-numeric string %q", s)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("expected number or numeric string, got %s", val.Type().FriendlyName())
	}
}

// coerceFloat is the float64 counterpart of coerceInt. Same rationale: env()
// always returns strings, so any float-typed user setting must accept both.
func coerceFloat(val cty.Value) (float64, error) {
	if val.IsNull() {
		return 0, nil
	}
	switch val.Type() {
	case cty.Number:
		f, _ := val.AsBigFloat().Float64()
		return f, nil
	case cty.String:
		s := val.AsString()
		if s == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("expected number, got non-numeric string %q", s)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("expected number or numeric string, got %s", val.Type().FriendlyName())
	}
}

// parseTypeBlock parses a type block from HCL.
// Type definitions look like:
//
//	type "user" {
//	  id    = number
//	  email = string { format = "email" }
//	  age   = number { min = 0, max = 150 }
//	}
//
// Federation directives can be added with underscore-prefixed attributes:
//
//	type "User" {
//	  _key         = "id"              # @key directive
//	  _shareable   = true              # @shareable directive
//	  _inaccessible = false            # @inaccessible directive
//	  _description = "A user entity"  # Type description
//	  _implements  = ["Node", "Entity"] # Interface implementations
//
//	  id    = string
//	  email = string { external = true }  # Field-level federation
//	}
func parseTypeBlock(block *hcl.Block, ctx *hcl.EvalContext) (*validate.TypeSchema, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("type block requires a name label")
	}

	schema := &validate.TypeSchema{
		Name:   block.Labels[0],
		Fields: make([]validate.FieldSchema, 0),
		Keys:   make([]string, 0),
	}

	// Get the raw attributes - type definitions are special
	// because the values are type identifiers, not regular values
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("type block attributes error: %s", diags.Error())
	}

	for name, attr := range attrs {
		// Handle federation directive attributes (underscore-prefixed)
		if strings.HasPrefix(name, "_") {
			if err := parseTypeDirective(schema, name, attr, ctx); err != nil {
				return nil, fmt.Errorf("directive %s error: %w", name, err)
			}
			continue
		}

		field, err := parseFieldDefinition(name, attr, ctx)
		if err != nil {
			return nil, fmt.Errorf("field %s error: %w", name, err)
		}
		schema.Fields = append(schema.Fields, *field)
	}

	return schema, nil
}

// parseTypeDirective parses a federation directive attribute on a type.
func parseTypeDirective(schema *validate.TypeSchema, name string, attr *hcl.Attribute, ctx *hcl.EvalContext) error {
	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return fmt.Errorf("directive value error: %s", diags.Error())
	}

	switch name {
	case "_key":
		// Can be a single string or list of strings
		if val.Type().IsTupleType() || val.Type().IsListType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				schema.Keys = append(schema.Keys, v.AsString())
			}
		} else {
			schema.Keys = append(schema.Keys, val.AsString())
		}

	case "_shareable":
		schema.Shareable = val.True()

	case "_inaccessible":
		schema.Inaccessible = val.True()

	case "_description":
		schema.Description = val.AsString()

	case "_implements":
		// List of interface names
		if val.Type().IsTupleType() || val.Type().IsListType() {
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				schema.InterfaceNames = append(schema.InterfaceNames, v.AsString())
			}
		} else {
			schema.InterfaceNames = append(schema.InterfaceNames, val.AsString())
		}
	}

	return nil
}

// parseFieldDefinition parses a field definition.
// Formats:
// - fieldName = string
// - fieldName = number { min = 0, max = 150 }
// - fieldName = string { format = "email" }
// - fieldName = string { external = true, requires = "otherField" }  # Federation
func parseFieldDefinition(name string, attr *hcl.Attribute, ctx *hcl.EvalContext) (*validate.FieldSchema, error) {
	field := &validate.FieldSchema{
		Name:        name,
		Required:    true, // Fields are required by default
		Constraints: make([]validate.Constraint, 0),
	}

	// Try to determine the field type from the expression
	switch expr := attr.Expr.(type) {
	case *hclsyntax.ScopeTraversalExpr:
		// Simple type reference: id = number
		field.Type = traversalToString(expr.Traversal)

	case *hclsyntax.FunctionCallExpr:
		// Function call with constraints: email = string { format = "email" }
		// This is actually parsed as a function call in HCL
		field.Type = expr.Name
		// Parse arguments as constraints and federation directives
		if err := parseConstraintsAndDirectives(field, expr.Args, ctx); err != nil {
			return nil, fmt.Errorf("constraint parse error: %w", err)
		}

	case *hclsyntax.ObjectConsExpr:
		// Object type with nested fields
		field.Type = "object"
		// Could recursively parse nested fields here

	default:
		// Try to evaluate as a regular value
		val, diags := attr.Expr.Value(ctx)
		if !diags.HasErrors() {
			field.Type = val.AsString()
		} else {
			// Fall back to extracting the expression as text
			field.Type = extractTypeFromExpression(attr.Expr)
		}
	}

	return field, nil
}

// traversalToString converts an HCL traversal to a string.
func traversalToString(traversal hcl.Traversal) string {
	parts := make([]string, 0, len(traversal))
	for _, traverser := range traversal {
		switch t := traverser.(type) {
		case hcl.TraverseRoot:
			parts = append(parts, t.Name)
		case hcl.TraverseAttr:
			parts = append(parts, t.Name)
		}
	}
	return strings.Join(parts, ".")
}

// parseConstraintsAndDirectives parses constraints and federation directives from function arguments.
func parseConstraintsAndDirectives(field *validate.FieldSchema, args []hclsyntax.Expression, ctx *hcl.EvalContext) error {
	for _, arg := range args {
		// Each argument should be an object with constraint/directive definitions
		if objExpr, ok := arg.(*hclsyntax.ObjectConsExpr); ok {
			for _, item := range objExpr.Items {
				keyVal, diags := item.KeyExpr.Value(ctx)
				if diags.HasErrors() {
					continue
				}
				key := keyVal.AsString()

				val, diags := item.ValueExpr.Value(ctx)
				if diags.HasErrors() {
					continue
				}

				value := ctyValueToGo(val)

				// Check for federation directives first
				if handled := parseFieldDirective(field, key, value); handled {
					continue
				}

				// Otherwise treat as validation constraint
				constraint := createConstraint(key, value)
				if constraint != nil {
					field.Constraints = append(field.Constraints, constraint)
				}
			}
		}
	}

	return nil
}

// parseFieldDirective parses a federation directive for a field.
// Returns true if the key was a directive (handled), false otherwise.
func parseFieldDirective(field *validate.FieldSchema, key string, value interface{}) bool {
	switch key {
	case "external":
		if b, ok := value.(bool); ok {
			field.External = b
		}
		return true

	case "provides":
		if s, ok := value.(string); ok {
			field.Provides = s
		}
		return true

	case "requires":
		if s, ok := value.(string); ok {
			field.Requires = s
		}
		return true

	case "shareable":
		if b, ok := value.(bool); ok {
			field.Shareable = b
		}
		return true

	case "inaccessible":
		if b, ok := value.(bool); ok {
			field.Inaccessible = b
		}
		return true

	case "override":
		if s, ok := value.(string); ok {
			field.Override = s
		}
		return true

	case "description":
		if s, ok := value.(string); ok {
			field.Description = s
		}
		return true

	case "required":
		if b, ok := value.(bool); ok {
			field.Required = b
		}
		return true

	case "validator":
		if s, ok := value.(string); ok {
			field.ValidatorRef = s
		}
		return true
	}

	return false
}

// parseConstraintsFromArgs parses constraints from function arguments (legacy).
func parseConstraintsFromArgs(args []hclsyntax.Expression, ctx *hcl.EvalContext) ([]validate.Constraint, error) {
	constraints := make([]validate.Constraint, 0)

	for _, arg := range args {
		// Each argument should be an object with constraint definitions
		if objExpr, ok := arg.(*hclsyntax.ObjectConsExpr); ok {
			for _, item := range objExpr.Items {
				keyVal, diags := item.KeyExpr.Value(ctx)
				if diags.HasErrors() {
					continue
				}
				key := keyVal.AsString()

				val, diags := item.ValueExpr.Value(ctx)
				if diags.HasErrors() {
					continue
				}

				constraint := createConstraint(key, ctyValueToGo(val))
				if constraint != nil {
					constraints = append(constraints, constraint)
				}
			}
		}
	}

	return constraints, nil
}

// extractTypeFromExpression attempts to extract type information from an expression.
func extractTypeFromExpression(expr hcl.Expression) string {
	// Get the source range and try to extract the type
	rng := expr.Range()
	// This is a simplified version - would need access to source to fully implement
	return fmt.Sprintf("unknown(%s)", rng.String())
}

// createConstraint creates a constraint from a key-value pair.
func createConstraint(key string, value interface{}) validate.Constraint {
	switch key {
	case "format":
		if s, ok := value.(string); ok {
			return &validate.FormatConstraint{Format: s}
		}
	case "min":
		return &validate.MinConstraint{Min: toFloat64(value)}
	case "max":
		return &validate.MaxConstraint{Max: toFloat64(value)}
	case "min_length":
		return &validate.MinLengthConstraint{MinLength: toInt(value)}
	case "max_length":
		return &validate.MaxLengthConstraint{MaxLength: toInt(value)}
	case "pattern":
		if s, ok := value.(string); ok {
			return &validate.PatternConstraint{Pattern: s}
		}
	case "enum":
		if arr, ok := value.([]interface{}); ok {
			values := make([]string, 0, len(arr))
			for _, v := range arr {
				if s, ok := v.(string); ok {
					values = append(values, s)
				}
			}
			return &validate.EnumConstraint{Values: values}
		}
	}
	return nil
}

// toFloat64 converts a value to float64.
func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float64:
		return val
	default:
		return 0
	}
}

// toInt converts a value to int.
func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}
