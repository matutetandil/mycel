package http

import "github.com/matutetandil/mycel/v2/pkg/schema"

// Schema implements ConnectorSchemaProvider for HTTP client.
type Schema struct{}

func (Schema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "base_url", Doc: "Base URL for HTTP requests", Type: schema.TypeString, Required: true},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "retry_count", Doc: "Shorthand for retry { attempts = N }. Prefer the retry block for finer control.", Type: schema.TypeNumber},
			{Name: "format", Doc: "Request/response format", Type: schema.TypeString, Values: []string{"json", "xml"}},
		},
		Children: []schema.Block{
			{Type: "headers", Doc: "Default request headers", Open: true},
			{Type: "retry", Doc: "Retry policy for failed requests. 4xx responses are not retried.", Attrs: []schema.Attr{
				{Name: "attempts", Doc: "Total tries, including the first", Type: schema.TypeNumber},
				{Name: "delay", Doc: "Wait before the second try (default 100ms)", Type: schema.TypeDuration},
				{Name: "max_delay", Doc: "Cap on the wait however far it grows (default 30s)", Type: schema.TypeDuration},
				{Name: "backoff", Doc: "How the wait grows between tries (default exponential)", Type: schema.TypeString,
					Values: []string{"constant", "linear", "exponential"}},
			}},
			{Type: "auth", Doc: "Authentication", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type", Type: schema.TypeString, Values: []string{"bearer", "api_key", "basic", "oauth2"}},
				{Name: "token", Doc: "Bearer token", Type: schema.TypeString},
				{Name: "username", Doc: "Basic auth username", Type: schema.TypeString},
				{Name: "password", Doc: "Basic auth password", Type: schema.TypeString},
			}},
			{Type: "tls", Doc: "TLS settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS (default true when the block is present)", Type: schema.TypeBool},
				{Name: "cert", Doc: "Certificate file this connector presents: its own when it is a server, the client certificate for mutual TLS", Type: schema.TypeString},
				{Name: "key", Doc: "Private key for cert", Type: schema.TypeString},
				{Name: "ca_cert", Doc: "CA certificate file used to verify the other side", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification (development only)", Type: schema.TypeBool},
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
