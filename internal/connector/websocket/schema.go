package websocket

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for WebSocket.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "WebSocket server port", Type: schema.TypeNumber},
			{Name: "host", Doc: "WebSocket server hostname", Type: schema.TypeString},
			{Name: "path", Doc: "WebSocket endpoint path", Type: schema.TypeString},
			{Name: "ping_interval", Doc: "Ping interval for keep-alive", Type: schema.TypeDuration},
			{Name: "pong_timeout", Doc: "Pong response timeout", Type: schema.TypeDuration},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "WebSocket event to handle", Type: schema.TypeString},
		},
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block { return nil }
