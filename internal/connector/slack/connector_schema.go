package slack

import "github.com/matutetandil/mycel/pkg/schema"

// ConnectorSchemaDef implements ConnectorSchemaProvider for Slack.
type ConnectorSchemaDef struct{}

func (ConnectorSchemaDef) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "webhook_url", Doc: "Slack incoming webhook URL", Type: schema.TypeString},
			{Name: "token", Doc: "Slack Bot/User token", Type: schema.TypeString},
			{Name: "api_url", Doc: "Slack API base URL", Type: schema.TypeString},
			{Name: "channel", Doc: "Default channel to post to", Type: schema.TypeString},
			{Name: "username", Doc: "Bot display name", Type: schema.TypeString},
			{Name: "icon_emoji", Doc: "Bot icon emoji (e.g., :robot_face:)", Type: schema.TypeString},
			{Name: "icon_url", Doc: "Bot icon image URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}
