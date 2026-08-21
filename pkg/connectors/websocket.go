package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// WebSocketSchema implements ConnectorSchemaProvider for WebSocket.
type WebSocketSchema struct{}

func (WebSocketSchema) ConnectorSchema() schema.Block {
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

func (WebSocketSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "WebSocket event to handle", Type: schema.TypeString, Default: "*"},
		},
	}
}

func (WebSocketSchema) TargetSchema() *schema.Block { return nil }
