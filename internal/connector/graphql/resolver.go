package graphql

import (
	"context"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/matutetandil/mycel/internal/graphql/analyzer"
	"github.com/matutetandil/mycel/internal/graphql/pruner"
)

// contextKey is a type for context keys in this package.
type contextKey string

// RequestedFieldsKey is the context key for requested fields.
const RequestedFieldsKey contextKey = "requested_fields"

// GetRequestedFields retrieves the RequestedFields from context.
func GetRequestedFields(ctx context.Context) *analyzer.RequestedFields {
	if rf, ok := ctx.Value(RequestedFieldsKey).(*analyzer.RequestedFields); ok {
		return rf
	}
	return nil
}

// WithRequestedFields adds RequestedFields to the context.
func WithRequestedFields(ctx context.Context, rf *analyzer.RequestedFields) context.Context {
	return context.WithValue(ctx, RequestedFieldsKey, rf)
}

// ResolverOptions configures how the resolver transforms results.
type ResolverOptions struct {
	// UnwrapSingleResult unwraps single-element arrays to a single object.
	// Use when the schema expects a single object but the handler returns an array.
	UnwrapSingleResult bool

	// ReturnCreatedObject indicates this is a create mutation that should
	// return the created object instead of {id, affected}.
	ReturnCreatedObject bool
}

// CreateResolver wraps a flow handler as a GraphQL resolver function.
func CreateResolver(handler HandlerFunc) graphql.FieldResolveFn {
	return CreateResolverWithOptions(handler, ResolverOptions{})
}

// CreateOptimizedResolver creates a resolver with automatic field extraction and result pruning.
// This enables query optimization by making requested fields available to the handler
// and ensuring only requested fields are returned.
func CreateOptimizedResolver(handler HandlerFunc) graphql.FieldResolveFn {
	return CreateOptimizedResolverWithOptions(handler, ResolverOptions{})
}

// CreateOptimizedResolverWithOptions creates an optimized resolver with custom options.
func CreateOptimizedResolverWithOptions(handler HandlerFunc, opts ResolverOptions) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Extract requested fields from GraphQL AST
		fields := analyzer.ExtractFields(p)

		// Add requested fields to context for use in flow execution
		ctx := p.Context
		if ctx == nil {
			ctx = context.Background()
		}
		ctx = WithRequestedFields(ctx, fields)

		// Build input from GraphQL arguments
		input := MapArgsToInput(p)

		// Add requested fields info to input for use in transforms/steps
		input["__requested_fields"] = fields.ListFlat()
		input["__requested_top_fields"] = fields.List()

		// Call the flow handler with enriched context
		result, err := handler(ctx, input)
		if err != nil {
			return nil, err
		}

		// Convert result to GraphQL-compatible format
		converted := MapResultToGraphQL(result)

		// Apply options
		if opts.UnwrapSingleResult {
			converted = unwrapSingleResult(converted)
		} else if !isListType(p.Info.ReturnType) {
			// Smart unwrap if not explicitly disabled
			converted = unwrapSingleResult(converted)
		}

		// Prune result to only include requested fields
		converted = pruner.Prune(converted, fields)

		return converted, nil
	}
}

// CreateResolverWithOptions wraps a flow handler with custom options.
func CreateResolverWithOptions(handler HandlerFunc, opts ResolverOptions) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Build input from GraphQL arguments
		input := MapArgsToInput(p)

		// Call the flow handler
		result, err := handler(p.Context, input)
		if err != nil {
			return nil, err
		}

		// Convert result to GraphQL-compatible format
		converted := MapResultToGraphQL(result)

		// Apply options
		if opts.UnwrapSingleResult {
			converted = unwrapSingleResult(converted)
		}

		return converted, nil
	}
}

// CreateSmartResolver creates a resolver that automatically detects whether
// to unwrap results based on the GraphQL return type.
// It also includes field extraction and result pruning for query optimization.
func CreateSmartResolver(handler HandlerFunc) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Extract requested fields from GraphQL AST for optimization
		fields := analyzer.ExtractFields(p)

		// Add requested fields to context for use in flow execution
		ctx := p.Context
		if ctx == nil {
			ctx = context.Background()
		}
		ctx = WithRequestedFields(ctx, fields)

		// Build input from GraphQL arguments
		input := MapArgsToInput(p)

		// Add requested fields info to input for use in transforms/steps
		input["__requested_fields"] = fields.ListFlat()
		input["__requested_top_fields"] = fields.List()

		// Call the flow handler with enriched context
		result, err := handler(ctx, input)
		if err != nil {
			return nil, err
		}

		// Convert result to GraphQL-compatible format
		converted := MapResultToGraphQL(result)

		// Check if return type expects a single object (not a list)
		if !isListType(p.Info.ReturnType) {
			converted = unwrapSingleResult(converted)
		}

		// Prune result to only include requested fields (safety net)
		converted = pruner.Prune(converted, fields)

		return converted, nil
	}
}

// unwrapSingleResult extracts the first element from a single-element array.
func unwrapSingleResult(result interface{}) interface{} {
	switch v := result.(type) {
	case []map[string]interface{}:
		if len(v) == 1 {
			return v[0]
		}
		if len(v) == 0 {
			return nil
		}
		return v
	case []interface{}:
		if len(v) == 1 {
			return v[0]
		}
		if len(v) == 0 {
			return nil
		}
		return v
	default:
		return result
	}
}

// isListType checks if a GraphQL type is a list type.
func isListType(t graphql.Type) bool {
	// Unwrap NonNull wrapper
	if nonNull, ok := t.(*graphql.NonNull); ok {
		t = nonNull.OfType
	}

	// Check if it's a List
	_, isList := t.(*graphql.List)
	return isList
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
// It also converts snake_case keys to camelCase for GraphQL compatibility.
func MapResultToGraphQL(result interface{}) interface{} {
	if result == nil {
		return nil
	}

	switch v := result.(type) {
	case []map[string]interface{}:
		// Array of objects - convert each map's keys to camelCase
		converted := make([]map[string]interface{}, len(v))
		for i, row := range v {
			converted[i] = convertKeysToCamelCase(row)
		}
		return converted
	case map[string]interface{}:
		// Single object - convert keys to camelCase
		return convertKeysToCamelCase(v)
	case []interface{}:
		// Generic array - check if elements are maps
		converted := make([]interface{}, len(v))
		for i, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				converted[i] = convertKeysToCamelCase(m)
			} else {
				converted[i] = item
			}
		}
		return converted
	default:
		// Wrap primitive values
		return result
	}
}

// convertKeysToCamelCase converts all snake_case keys in a map to camelCase.
func convertKeysToCamelCase(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for key, value := range m {
		camelKey := snakeToCamel(key)
		// Recursively convert nested maps
		if nestedMap, ok := value.(map[string]interface{}); ok {
			result[camelKey] = convertKeysToCamelCase(nestedMap)
		} else if nestedSlice, ok := value.([]map[string]interface{}); ok {
			converted := make([]map[string]interface{}, len(nestedSlice))
			for i, item := range nestedSlice {
				converted[i] = convertKeysToCamelCase(item)
			}
			result[camelKey] = converted
		} else {
			result[camelKey] = value
		}
	}
	return result
}

// snakeToCamel converts a snake_case string to camelCase.
// Examples: "external_id" -> "externalId", "created_at" -> "createdAt"
func snakeToCamel(s string) string {
	// If no underscore, return as-is
	if !containsUnderscore(s) {
		return s
	}

	result := make([]byte, 0, len(s))
	capitalizeNext := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			capitalizeNext = true
			continue
		}
		if capitalizeNext && c >= 'a' && c <= 'z' {
			result = append(result, c-32) // to uppercase
			capitalizeNext = false
		} else {
			result = append(result, c)
			capitalizeNext = false
		}
	}

	return string(result)
}

// containsUnderscore checks if a string contains an underscore.
func containsUnderscore(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			return true
		}
	}
	return false
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
