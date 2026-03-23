package sms

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for SMS.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "SMS delivery driver", Type: schema.TypeString, Values: []string{"twilio", "sns"}},
			{Name: "from", Doc: "Sender phone number", Type: schema.TypeString, Required: true},
			{Name: "account_sid", Doc: "Twilio account SID", Type: schema.TypeString},
			{Name: "auth_token", Doc: "Twilio auth token", Type: schema.TypeString},
			{Name: "api_url", Doc: "Twilio API base URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "region", Doc: "AWS region for SNS", Type: schema.TypeString},
			{Name: "access_key_id", Doc: "AWS access key ID for SNS", Type: schema.TypeString},
			{Name: "secret_access_key", Doc: "AWS secret access key for SNS", Type: schema.TypeString},
			{Name: "sender_id", Doc: "SNS sender ID", Type: schema.TypeString},
			{Name: "sms_type", Doc: "SNS SMS type (Transactional, Promotional)", Type: schema.TypeString},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}
