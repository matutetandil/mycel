package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// WebhookSchema implements ConnectorSchemaProvider for Webhook.
type WebhookSchema struct{}

func (WebhookSchema) ConnectorSchema() schema.Block {
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
			{Name: "trusted_proxies", Doc: "Peers whose X-Forwarded-For is believed. Without this the allow-list is decided on the peer address, since a forwarding header can be written by anyone", Type: schema.TypeList},
		},
		Children: []schema.Block{
			{Type: "headers", Doc: "Custom HTTP headers", Open: true},
			{Type: "retry", Doc: "Retry policy for outbound webhooks", Attrs: []schema.Attr{
				{Name: "attempts", Doc: "Total tries, including the first", Type: schema.TypeNumber},
				{Name: "delay", Doc: "Wait before the second try", Type: schema.TypeDuration},
				{Name: "max_delay", Doc: "Cap on how far the wait grows", Type: schema.TypeDuration},
				{Name: "multiplier", Doc: "Factor the wait grows by on each try", Type: schema.TypeNumber},
				{Name: "retryable_statuses", Doc: "HTTP status codes worth retrying", Type: schema.TypeList},
			}},
		},
	}
}

func (WebhookSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

func (WebhookSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}
