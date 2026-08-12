package connectors

import "github.com/matutetandil/mycel/v2/pkg/schema"

// DiscordSchema implements ConnectorSchemaProvider for Discord.
type DiscordSchema struct{}

func (DiscordSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "webhook_url", Doc: "Discord webhook URL", Type: schema.TypeString},
			{Name: "bot_token", Doc: "Discord bot token", Type: schema.TypeString},
			{Name: "api_url", Doc: "Discord API base URL", Type: schema.TypeString},
			{Name: "channel_id", Doc: "Default channel ID", Type: schema.TypeString},
			{Name: "username", Doc: "Bot display name", Type: schema.TypeString},
			{Name: "avatar_url", Doc: "Bot avatar image URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
	}
}

func (DiscordSchema) SourceSchema() *schema.Block { return nil }

func (DiscordSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}
