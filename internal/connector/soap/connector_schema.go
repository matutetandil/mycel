package soap

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for SOAP.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "endpoint", Doc: "SOAP service endpoint URL", Type: schema.TypeString},
			{Name: "port", Doc: "SOAP server port", Type: schema.TypeNumber},
			{Name: "soap_version", Doc: "SOAP protocol version", Type: schema.TypeString, Values: []string{"1.1", "1.2"}},
			{Name: "namespace", Doc: "XML namespace for the service", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "auth", Doc: "Authentication settings", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type (basic, wsse, token)", Type: schema.TypeString},
				{Name: "username", Doc: "Auth username", Type: schema.TypeString},
				{Name: "password", Doc: "Auth password", Type: schema.TypeString},
				{Name: "token", Doc: "Auth token", Type: schema.TypeString},
			}},
			{Type: "headers", Doc: "Custom SOAP headers", Open: true},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "SOAP operation to expose", Type: schema.TypeString, Required: true},
		},
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "SOAP operation to call", Type: schema.TypeString},
		},
	}
}
