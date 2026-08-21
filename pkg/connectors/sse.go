package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// SSESchema implements ConnectorSchemaProvider for SSE.
type SSESchema struct{}

func (SSESchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "SSE server port", Type: schema.TypeNumber},
			{Name: "host", Doc: "SSE server hostname", Type: schema.TypeString},
			{Name: "path", Doc: "SSE endpoint path", Type: schema.TypeString},
			{Name: "heartbeat_interval", Doc: "Heartbeat interval for keep-alive", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "cors", Doc: "CORS settings", Attrs: []schema.Attr{
				{Name: "origins", Doc: "Allowed origins", Type: schema.TypeList},
			}},
		},
	}
}

func (SSESchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "SSE event type to handle", Type: schema.TypeString, Required: true},
		},
	}
}

func (SSESchema) TargetSchema() *schema.Block { return nil }
