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
		Children: []schema.Block{
			{
				Type: "batch",
				Doc:  "Coalesce high-rate writes into a single summary message to avoid Slack's per-channel rate limit. Enabled by default since v2.5.0; set enabled = false to keep the old immediate-send behavior.",
				Attrs: []schema.Attr{
					{Name: "enabled", Doc: "Toggle batching (default: true).", Type: schema.TypeBool},
					{Name: "window", Doc: "Tumbling-window duration. First message in an empty bucket arms a timer; bucket flushes when the timer fires. Default: 3s.", Type: schema.TypeDuration},
					{Name: "max_size", Doc: "Force a flush when a bucket reaches this many queued messages. Default: 50.", Type: schema.TypeNumber},
					{Name: "group_by", Doc: "Bucket key: \"channel\" (default; one bucket per Slack channel) or \"global\".", Type: schema.TypeString, Values: []string{"channel", "global"}},
					{Name: "summary", Doc: "Optional CEL expression producing the collapsed text. Vars: messages, count, channel, window. Empty = built-in bullet list.", Type: schema.TypeString},
				},
			},
		},
	}
}

func (ConnectorSchemaDef) SourceSchema() *schema.Block { return nil }

func (ConnectorSchemaDef) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}
