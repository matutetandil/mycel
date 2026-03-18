package websocket

// ValidateSourceParams validates parameters for flows using WebSocket as a source.
// Operation is optional — defaults to "message" for the standard message handler.
func (c *Connector) ValidateSourceParams(params map[string]interface{}) error {
	if _, ok := params["operation"]; !ok {
		params["operation"] = "message"
	}
	return nil
}
