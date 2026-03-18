package kafka

// ValidateSourceParams validates parameters for flows using Kafka as a source.
// Operation is optional — defaults to "*" (catch-all) when the topic is defined in connector config.
// When specified, operation is used as a topic pattern for message matching.
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		params["operation"] = "*"
	}
	return nil
}
