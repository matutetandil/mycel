package email

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for Email.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Email delivery driver", Type: schema.TypeString, Values: []string{"smtp", "sendgrid", "ses"}},
			{Name: "template", Doc: "HTML template file path", Type: schema.TypeString},
			{Name: "from", Doc: "Sender email address", Type: schema.TypeString, Required: true},
			{Name: "from_name", Doc: "Sender display name", Type: schema.TypeString},
			{Name: "reply_to", Doc: "Reply-to email address", Type: schema.TypeString},
			{Name: "host", Doc: "SMTP server hostname", Type: schema.TypeString},
			{Name: "port", Doc: "SMTP server port", Type: schema.TypeNumber},
			{Name: "username", Doc: "SMTP authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "SMTP authentication password", Type: schema.TypeString},
			{Name: "tls", Doc: "Enable TLS for SMTP", Type: schema.TypeString},
			{Name: "timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
			{Name: "pool_size", Doc: "SMTP connection pool size", Type: schema.TypeNumber},
			{Name: "api_key", Doc: "SendGrid API key", Type: schema.TypeString},
			{Name: "endpoint", Doc: "SES endpoint URL", Type: schema.TypeString},
			{Name: "region", Doc: "AWS region for SES", Type: schema.TypeString},
			{Name: "access_key_id", Doc: "AWS access key ID for SES", Type: schema.TypeString},
			{Name: "secret_access_key", Doc: "AWS secret access key for SES", Type: schema.TypeString},
			{Name: "configuration_set", Doc: "SES configuration set name", Type: schema.TypeString},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}
