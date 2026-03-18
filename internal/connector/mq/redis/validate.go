package redis

// ValidateSourceParams validates parameters for flows using Redis Pub/Sub as a source.
// Operation is optional — defaults to "*" (catch-all) when the channel is defined in connector config.
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		params["operation"] = "*"
	}
	return nil
}
