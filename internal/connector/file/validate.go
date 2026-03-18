package file

// ValidateSourceParams validates parameters for flows using File watch as a source.
// Operation is optional — defaults to "*" (catch-all) when watch patterns are in connector config.
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		params["operation"] = "*"
	}
	return nil
}
