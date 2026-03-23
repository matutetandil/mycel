package webhook

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for Webhook.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "mode", Doc: "Webhook mode", Type: schema.TypeString, Values: []string{"inbound", "outbound"}},
			{Name: "url", Doc: "Outbound webhook URL", Type: schema.TypeString},
			{Name: "path", Doc: "Inbound webhook path", Type: schema.TypeString},
			{Name: "method", Doc: "HTTP method for outbound", Type: schema.TypeString},
			{Name: "secret", Doc: "Webhook signing secret", Type: schema.TypeString},
			{Name: "signature_header", Doc: "Header name for signature", Type: schema.TypeString},
			{Name: "signature_algorithm", Doc: "Signature algorithm (hmac-sha256, etc.)", Type: schema.TypeString},
			{Name: "include_timestamp", Doc: "Include timestamp in signature", Type: schema.TypeBool},
			{Name: "timestamp_header", Doc: "Header name for timestamp", Type: schema.TypeString},
			{Name: "timestamp_tolerance", Doc: "Acceptable timestamp drift", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "require_https", Doc: "Require HTTPS for webhooks", Type: schema.TypeBool},
			{Name: "allowed_ips", Doc: "Allowed source IP addresses", Type: schema.TypeList},
		},
		Children: []schema.Block{
			{Type: "headers", Doc: "Custom HTTP headers", Open: true},
			{Type: "retry", Doc: "Retry policy for outbound webhooks", Attrs: []schema.Attr{
				{Name: "max_attempts", Doc: "Maximum retry attempts", Type: schema.TypeNumber},
				{Name: "initial_delay", Doc: "Initial delay between retries", Type: schema.TypeDuration},
				{Name: "max_delay", Doc: "Maximum delay between retries", Type: schema.TypeDuration},
				{Name: "multiplier", Doc: "Backoff multiplier", Type: schema.TypeNumber},
			}},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}
