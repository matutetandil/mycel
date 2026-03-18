package graphql

import "fmt"

// ValidateSourceParams validates parameters for flows using GraphQL as a source.
// Requires "operation" (e.g., "Query.users", "Mutation.createUser").
func (c *ServerConnector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		return fmt.Errorf("'operation' is required (e.g., \"Query.users\")")
	}
	return nil
}
