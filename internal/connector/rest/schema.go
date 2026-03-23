package rest

import "github.com/matutetandil/mycel/pkg/schema"

// Schema implements ConnectorSchemaProvider for REST server.
type Schema struct{}

func (Schema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "HTTP server port", Type: schema.TypeNumber, Required: true},
			{Name: "format", Doc: "Default response format", Type: schema.TypeString, Values: []string{"json", "xml"}},
		},
		Children: []schema.Block{
			{Type: "cors", Doc: "CORS settings", Attrs: []schema.Attr{
				{Name: "origins", Doc: "Allowed origins", Type: schema.TypeList},
				{Name: "methods", Doc: "Allowed HTTP methods", Type: schema.TypeList},
				{Name: "headers", Doc: "Allowed headers", Type: schema.TypeList},
			}},
			{Type: "auth", Doc: "Authentication", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type (jwt, api_key, basic)", Type: schema.TypeString, Values: []string{"jwt", "api_key", "basic"}},
				{Name: "public", Doc: "Public (unauthenticated) paths", Type: schema.TypeList},
			}},
		},
	}
}

func (Schema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "HTTP method + path (e.g., GET /users)", Type: schema.TypeString, Required: true},
		},
	}
}

func (Schema) TargetSchema() *schema.Block { return nil }
