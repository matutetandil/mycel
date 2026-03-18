package tcp

import "fmt"

// ValidateSourceParams validates parameters for flows using TCP server as a source.
// Requires "operation" (the message type to handle).
func (c *ServerConnector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		return fmt.Errorf("'operation' is required (message type)")
	}
	return nil
}
