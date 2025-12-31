package parser

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/matutetandil/mycel/internal/validate"
)

// parseTypeBlock parses a type block from HCL.
// Type definitions look like:
//
//	type "user" {
//	  id    = number
//	  email = string { format = "email" }
//	  age   = number { min = 0, max = 150 }
//	}
func parseTypeBlock(block *hcl.Block, ctx *hcl.EvalContext) (*validate.TypeSchema, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("type block requires a name label")
	}

	schema := &validate.TypeSchema{
		Name:   block.Labels[0],
		Fields: make([]validate.FieldSchema, 0),
	}

	// Get the raw attributes - type definitions are special
	// because the values are type identifiers, not regular values
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("type block attributes error: %s", diags.Error())
	}

	for name, attr := range attrs {
		field, err := parseFieldDefinition(name, attr, ctx)
		if err != nil {
			return nil, fmt.Errorf("field %s error: %w", name, err)
		}
		schema.Fields = append(schema.Fields, *field)
	}

	return schema, nil
}

// parseFieldDefinition parses a field definition.
// Formats:
// - fieldName = string
// - fieldName = number { min = 0, max = 150 }
// - fieldName = string { format = "email" }
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
		// Parse arguments as constraints
		constraints, err := parseConstraintsFromArgs(expr.Args, ctx)
		if err != nil {
			return nil, fmt.Errorf("constraint parse error: %w", err)
		}
		field.Constraints = constraints

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

// parseConstraintsFromArgs parses constraints from function arguments.
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
