package grpc

import "fmt"

// ValidateSourceParams validates parameters for flows using gRPC server as a source.
// Requires "operation" (e.g., "GetUser", "ListUsers").
func (c *ServerConnector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		return fmt.Errorf("'operation' is required (e.g., \"GetUser\")")
	}
	return nil
}
