package analyzer

import (
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// ExtractFields extracts the requested fields from GraphQL resolve params.
// This is the main entry point for field extraction.
func ExtractFields(p graphql.ResolveParams) *RequestedFields {
	tree := NewFieldTree()

	// Process all field AST nodes
	for _, fieldAST := range p.Info.FieldASTs {
		extractFromSelectionSet(fieldAST.SelectionSet, tree, p.Info.Fragments)
	}

	return NewRequestedFields(tree)
}

// ExtractFieldsFromInfo extracts fields using just the resolve info.
func ExtractFieldsFromInfo(info graphql.ResolveInfo) *RequestedFields {
	tree := NewFieldTree()

	for _, fieldAST := range info.FieldASTs {
		extractFromSelectionSet(fieldAST.SelectionSet, tree, info.Fragments)
	}

	return NewRequestedFields(tree)
}

// extractFromSelectionSet recursively extracts fields from a selection set.
func extractFromSelectionSet(selSet *ast.SelectionSet, tree *FieldTree, fragments map[string]ast.Definition) {
	if selSet == nil {
		return
	}

	for _, selection := range selSet.Selections {
		switch sel := selection.(type) {
		case *ast.Field:
			extractField(sel, tree, fragments)
		case *ast.FragmentSpread:
			// Resolve fragment and extract its fields
			if frag, ok := fragments[sel.Name.Value]; ok {
				if fragDef, ok := frag.(*ast.FragmentDefinition); ok {
					extractFromSelectionSet(fragDef.SelectionSet, tree, fragments)
				}
			}
		case *ast.InlineFragment:
			// Process inline fragment
			extractFromSelectionSet(sel.SelectionSet, tree, fragments)
		}
	}
}

// extractField extracts a single field node.
func extractField(field *ast.Field, tree *FieldTree, fragments map[string]ast.Definition) {
	// Skip __typename introspection field
	if field.Name.Value == "__typename" {
		return
	}

	node := NewFieldNode(field.Name.Value)

	// Handle alias
	if field.Alias != nil {
		node.Alias = field.Alias.Value
	}

	// Extract arguments
	if field.Arguments != nil {
		for _, arg := range field.Arguments {
			node.Arguments[arg.Name.Value] = extractValue(arg.Value)
		}
	}

	// Process nested selections (children)
	if field.SelectionSet != nil && len(field.SelectionSet.Selections) > 0 {
		node.IsLeaf = false
		node.Children = NewFieldTree()
		extractFromSelectionSet(field.SelectionSet, node.Children, fragments)
	}

	tree.AddField(node)
}

// extractValue extracts the value from an AST value node.
func extractValue(value ast.Value) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case *ast.IntValue:
		return v.Value
	case *ast.FloatValue:
		return v.Value
	case *ast.StringValue:
		return v.Value
	case *ast.BooleanValue:
		return v.Value
	case *ast.EnumValue:
		return v.Value
	case *ast.ListValue:
		result := make([]interface{}, len(v.Values))
		for i, val := range v.Values {
			result[i] = extractValue(val)
		}
		return result
	case *ast.ObjectValue:
		result := make(map[string]interface{})
		for _, field := range v.Fields {
			result[field.Name.Value] = extractValue(field.Value)
		}
		return result
	case *ast.Variable:
		// Return variable name prefixed with $
		return "$" + v.Name.Value
	default:
		return nil
	}
}

// ExtractFieldNames returns just the field names (for backward compatibility).
func ExtractFieldNames(p graphql.ResolveParams) []string {
	rf := ExtractFields(p)
	return rf.List()
}

// AnalyzeQuery analyzes a query and returns detailed field information.
type QueryAnalysis struct {
	RequestedFields *RequestedFields
	HasNestedFields bool
	MaxDepth        int
	FieldCount      int
}

// AnalyzeQuery performs a detailed analysis of the requested fields.
func AnalyzeQuery(p graphql.ResolveParams) *QueryAnalysis {
	fields := ExtractFields(p)

	analysis := &QueryAnalysis{
		RequestedFields: fields,
		HasNestedFields: false,
		MaxDepth:        0,
		FieldCount:      0,
	}

	if fields.tree != nil {
		analysis.MaxDepth = calculateMaxDepth(fields.tree, 0)
		analysis.FieldCount = len(fields.ListFlat())
		analysis.HasNestedFields = analysis.MaxDepth > 1
	}

	return analysis
}

// calculateMaxDepth calculates the maximum nesting depth of the field tree.
func calculateMaxDepth(tree *FieldTree, current int) int {
	if tree == nil || len(tree.Fields) == 0 {
		return current
	}

	maxDepth := current + 1
	for _, node := range tree.Fields {
		if node.Children != nil {
			childDepth := calculateMaxDepth(node.Children, current+1)
			if childDepth > maxDepth {
				maxDepth = childDepth
			}
		}
	}

	return maxDepth
}
