package sse

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for SSE.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
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

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "SSE event type to handle", Type: schema.TypeString, Required: true},
		},
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block { return nil }
