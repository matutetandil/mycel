package soap

import "fmt"

// ValidateSourceParams validates parameters for flows using SOAP server as a source.
// Requires "operation" (the SOAP action name).
func (s *Server) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		return fmt.Errorf("'operation' is required (SOAP action name)")
	}
	return nil
}
