package graphql

import (
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// CreateResolver wraps a flow handler as a GraphQL resolver function.
func CreateResolver(handler HandlerFunc) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Build input from GraphQL arguments
		input := MapArgsToInput(p)

		// Call the flow handler
		result, err := handler(p.Context, input)
		if err != nil {
			return nil, err
		}

		// Convert result to GraphQL-compatible format
		return MapResultToGraphQL(result), nil
	}
}

// MapArgsToInput converts GraphQL resolver params to a flow input map.
func MapArgsToInput(p graphql.ResolveParams) map[string]interface{} {
	input := make(map[string]interface{})

	// Copy all GraphQL arguments to input
	for key, value := range p.Args {
		// Handle the special "input" argument that may contain nested data
		if key == "input" {
			if inputMap, ok := value.(map[string]interface{}); ok {
				// Flatten input argument into the main input map
				for k, v := range inputMap {
					input[k] = v
				}
				continue
			}
		}
		input[key] = value
	}

	// Add parent value if available (for nested resolvers)
	if p.Source != nil {
		if sourceMap, ok := p.Source.(map[string]interface{}); ok {
			// Don't overwrite existing args with parent data
			for key, value := range sourceMap {
				if _, exists := input[key]; !exists {
					input["parent_"+key] = value
				}
			}
		}
	}

	// Add context variables if available
	if p.Info.VariableValues != nil {
		for key, value := range p.Info.VariableValues {
			if _, exists := input[key]; !exists {
				input[key] = value
			}
		}
	}

	return input
}

// MapResultToGraphQL converts a flow result to GraphQL-compatible format.
func MapResultToGraphQL(result interface{}) interface{} {
	if result == nil {
		return nil
	}

	switch v := result.(type) {
	case []map[string]interface{}:
		// Array of objects - common for list queries
		return v
	case map[string]interface{}:
		// Single object
		return v
	case []interface{}:
		// Generic array
		return v
	default:
		// Wrap primitive values
		return result
	}
}

// ExtractSelectionFields extracts the requested field names from a GraphQL query.
// This can be used to optimize database queries to only fetch requested fields.
func ExtractSelectionFields(p graphql.ResolveParams) []string {
	fields := make([]string, 0)

	// Get the selection set from the resolve info
	for _, selection := range p.Info.FieldASTs {
		if selection.SelectionSet != nil {
			for _, sel := range selection.SelectionSet.Selections {
				if field, ok := sel.(*ast.Field); ok {
					fields = append(fields, field.Name.Value)
				}
			}
		}
	}

	return fields
}

// BuildErrorResponse creates a GraphQL error response.
func BuildErrorResponse(err error) *GraphQLResponse {
	return &GraphQLResponse{
		Errors: []GraphQLError{
			{
				Message: err.Error(),
			},
		},
	}
}

// BuildDataResponse creates a GraphQL data response.
func BuildDataResponse(data interface{}) *GraphQLResponse {
	return &GraphQLResponse{
		Data: data,
	}
}
