package rabbitmq

// ValidateSourceParams validates parameters for flows using RabbitMQ as a source.
// Operation is optional — defaults to "*" (catch-all) when the queue is defined in connector config.
// When specified, operation is used as a routing key pattern for message matching.
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	// Default operation to "*" if not specified (catch-all handler)
	if _, ok := params["operation"]; !ok {
		params["operation"] = "*"
	}
	return nil
}
