package sse

import "fmt"

// ValidateSourceParams validates parameters for flows using SSE as a source.
// Requires "operation" (the SSE event path, e.g., "GET /events").
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		return fmt.Errorf("'operation' is required (e.g., \"GET /events\")")
	}
	return nil
}
