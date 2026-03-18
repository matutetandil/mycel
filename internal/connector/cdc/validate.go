package cdc

// ValidateSourceParams validates parameters for flows using CDC as a source.
// Operation is optional — defaults to "*" (catch-all) when tables are defined in connector config.
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		params["operation"] = "*"
	}
	return nil
}
