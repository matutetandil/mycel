package rest

import "fmt"

// ValidateSourceParams validates parameters for flows using this connector as a source.
// REST requires "operation" (e.g., "GET /users", "POST /users/:id").
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		return fmt.Errorf("'operation' is required (e.g., \"GET /users\")")
	}
	return nil
}
