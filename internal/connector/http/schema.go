package http

import "github.com/matutetandil/mycel/pkg/schema"

// Schema implements ConnectorSchemaProvider for HTTP client.
type Schema struct{}

func (Schema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "base_url", Doc: "Base URL for HTTP requests", Type: schema.TypeString, Required: true},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "retry_count", Doc: "Number of retries on failure", Type: schema.TypeNumber},
			{Name: "format", Doc: "Request/response format", Type: schema.TypeString, Values: []string{"json", "xml"}},
		},
		Children: []schema.Block{
			{Type: "headers", Doc: "Default request headers", Open: true},
			{Type: "auth", Doc: "Authentication", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type", Type: schema.TypeString, Values: []string{"bearer", "api_key", "basic", "oauth2"}},
				{Name: "token", Doc: "Bearer token", Type: schema.TypeString},
				{Name: "username", Doc: "Basic auth username", Type: schema.TypeString},
				{Name: "password", Doc: "Basic auth password", Type: schema.TypeString},
			}},
			{Type: "tls", Doc: "TLS settings", Attrs: []schema.Attr{
				{Name: "ca_cert", Doc: "CA certificate file", Type: schema.TypeString},
				{Name: "client_cert", Doc: "Client certificate file", Type: schema.TypeString},
				{Name: "client_key", Doc: "Client key file", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification", Type: schema.TypeBool},
			}},
		},
	}
}

func (Schema) SourceSchema() *schema.Block { return nil }
func (Schema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "HTTP method + path (e.g., GET /endpoint)", Type: schema.TypeString},
		},
	}
}
