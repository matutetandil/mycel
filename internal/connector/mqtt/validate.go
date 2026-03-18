package mqtt

// ValidateSourceParams validates parameters for flows using MQTT as a source.
// Operation is optional — defaults to "*" (catch-all) when topics are defined in connector config.
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		params["operation"] = "*"
	}
	return nil
}
