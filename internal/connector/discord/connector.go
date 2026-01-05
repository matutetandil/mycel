package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Config represents Discord connector configuration
type Config struct {
	// Name is the connector name
	Name string

	// WebhookURL is the Discord webhook URL
	WebhookURL string

	// BotToken is the bot token for API calls
	BotToken string

	// DefaultChannelID for bot messages
	DefaultChannelID string

	// Username to display (webhook only)
	Username string

	// AvatarURL to display (webhook only)
	AvatarURL string

	// Timeout for requests
	Timeout time.Duration
}

// Message represents a Discord message
type Message struct {
	// Content is the message text
	Content string `json:"content,omitempty"`

	// Username to display (overrides default)
	Username string `json:"username,omitempty"`

	// AvatarURL to display
	AvatarURL string `json:"avatar_url,omitempty"`

	// TTS enables text-to-speech
	TTS bool `json:"tts,omitempty"`

	// Embeds for rich content
	Embeds []Embed `json:"embeds,omitempty"`

	// AllowedMentions controls mentions
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`

	// Components for interactive elements
	Components []Component `json:"components,omitempty"`

	// ThreadName to create a new thread (forum channels)
	ThreadName string `json:"thread_name,omitempty"`
}

// Embed represents a Discord embed
type Embed struct {
	Title       string          `json:"title,omitempty"`
	Type        string          `json:"type,omitempty"` // "rich" by default
	Description string          `json:"description,omitempty"`
	URL         string          `json:"url,omitempty"`
	Timestamp   string          `json:"timestamp,omitempty"` // ISO8601
	Color       int             `json:"color,omitempty"`     // Decimal color
	Footer      *EmbedFooter    `json:"footer,omitempty"`
	Image       *EmbedMedia     `json:"image,omitempty"`
	Thumbnail   *EmbedMedia     `json:"thumbnail,omitempty"`
	Video       *EmbedMedia     `json:"video,omitempty"`
	Provider    *EmbedProvider  `json:"provider,omitempty"`
	Author      *EmbedAuthor    `json:"author,omitempty"`
	Fields      []EmbedField    `json:"fields,omitempty"`
}

// EmbedFooter represents embed footer
type EmbedFooter struct {
	Text    string `json:"text"`
	IconURL string `json:"icon_url,omitempty"`
}

// EmbedMedia represents embed media (image/thumbnail/video)
type EmbedMedia struct {
	URL    string `json:"url"`
	Height int    `json:"height,omitempty"`
	Width  int    `json:"width,omitempty"`
}

// EmbedProvider represents embed provider
type EmbedProvider struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// EmbedAuthor represents embed author
type EmbedAuthor struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

// EmbedField represents an embed field
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// AllowedMentions controls who can be mentioned
type AllowedMentions struct {
	Parse       []string `json:"parse,omitempty"`        // "roles", "users", "everyone"
	Roles       []string `json:"roles,omitempty"`        // Role IDs
	Users       []string `json:"users,omitempty"`        // User IDs
	RepliedUser bool     `json:"replied_user,omitempty"` // Mention the replied user
}

// Component represents an interactive component
type Component struct {
	Type       int          `json:"type"`
	Style      int          `json:"style,omitempty"`
	Label      string       `json:"label,omitempty"`
	Emoji      *Emoji       `json:"emoji,omitempty"`
	CustomID   string       `json:"custom_id,omitempty"`
	URL        string       `json:"url,omitempty"`
	Disabled   bool         `json:"disabled,omitempty"`
	Components []Component  `json:"components,omitempty"`
}

// Emoji represents a Discord emoji
type Emoji struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Animated bool   `json:"animated,omitempty"`
}

// SendResult represents the result of sending a message
type SendResult struct {
	Success   bool   `json:"success"`
	MessageID string `json:"message_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Connector sends messages to Discord
type Connector struct {
	name       string
	config     *Config
	httpClient *http.Client
}

// NewConnector creates a new Discord connector
func NewConnector(name string, config *Config) *Connector {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	return &Connector{
		name:   name,
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// Name returns the connector name
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type
func (c *Connector) Type() string {
	return "discord"
}

// Connect validates the configuration
func (c *Connector) Connect(ctx context.Context) error {
	if c.config.WebhookURL == "" && c.config.BotToken == "" {
		return fmt.Errorf("either webhook_url or bot_token is required")
	}
	return nil
}

// Send sends a message to Discord
func (c *Connector) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	// Apply defaults
	if msg.Username == "" {
		msg.Username = c.config.Username
	}
	if msg.AvatarURL == "" {
		msg.AvatarURL = c.config.AvatarURL
	}

	// Use webhook or API based on config
	if c.config.WebhookURL != "" {
		return c.sendViaWebhook(ctx, msg)
	}
	return c.sendViaAPI(ctx, msg, c.config.DefaultChannelID)
}

func (c *Connector) sendViaWebhook(ctx context.Context, msg *Message) (*SendResult, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}

	// Add ?wait=true to get message details in response
	url := c.config.WebhookURL + "?wait=true"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &SendResult{
			Success: false,
			Error:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, fmt.Errorf("discord webhook error: %s", string(respBody))
	}

	// Parse response for message ID
	var result struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil {
		return &SendResult{
			Success:   true,
			MessageID: result.ID,
			ChannelID: result.ChannelID,
		}, nil
	}

	return &SendResult{Success: true}, nil
}

func (c *Connector) sendViaAPI(ctx context.Context, msg *Message, channelID string) (*SendResult, error) {
	if channelID == "" {
		return &SendResult{
			Success: false,
			Error:   "channel_id is required for bot API",
		}, fmt.Errorf("channel_id is required")
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+c.config.BotToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &SendResult{
			Success: false,
			Error:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, fmt.Errorf("discord API error: %s", string(respBody))
	}

	var result struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil {
		return &SendResult{
			Success:   true,
			MessageID: result.ID,
			ChannelID: result.ChannelID,
		}, nil
	}

	return &SendResult{Success: true}, nil
}

// Write implements a simple interface for sending messages
func (c *Connector) Write(ctx context.Context, target string, data interface{}) (interface{}, error) {
	// If data is already a Message, use it
	if msg, ok := data.(*Message); ok {
		if c.config.BotToken != "" && target != "" {
			return c.sendViaAPI(ctx, msg, target)
		}
		return c.Send(ctx, msg)
	}
	if msg, ok := data.(Message); ok {
		if c.config.BotToken != "" && target != "" {
			return c.sendViaAPI(ctx, &msg, target)
		}
		return c.Send(ctx, &msg)
	}

	// If data is a map, try to build a message
	if m, ok := data.(map[string]interface{}); ok {
		msg := &Message{}
		if content, ok := m["content"].(string); ok {
			msg.Content = content
		}
		if c.config.BotToken != "" && target != "" {
			return c.sendViaAPI(ctx, msg, target)
		}
		return c.Send(ctx, msg)
	}

	// If data is a string, use it as content
	if content, ok := data.(string); ok {
		msg := &Message{Content: content}
		if c.config.BotToken != "" && target != "" {
			return c.sendViaAPI(ctx, msg, target)
		}
		return c.Send(ctx, msg)
	}

	return nil, fmt.Errorf("unsupported data type for Discord message")
}

// Health checks Discord connectivity
func (c *Connector) Health(ctx context.Context) error {
	if c.config.BotToken != "" {
		req, err := http.NewRequestWithContext(ctx, "GET",
			"https://discord.com/api/v10/users/@me", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bot "+c.config.BotToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("discord auth check failed: HTTP %d", resp.StatusCode)
		}
	}
	return nil
}

// Close closes the connector
func (c *Connector) Close(ctx context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Ensure Connector implements connector interface
var _ connector.Connector = (*Connector)(nil)

// Factory creates Discord connectors
type Factory struct{}

// NewFactory creates a new Discord factory
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type
func (f *Factory) Type() string {
	return "discord"
}

// Create creates a new Discord connector
func (f *Factory) Create(name string, config map[string]interface{}) (connector.Connector, error) {
	cfg := &Config{
		Name:             name,
		WebhookURL:       getString(config, "webhook_url", ""),
		BotToken:         getString(config, "bot_token", ""),
		DefaultChannelID: getString(config, "channel_id", ""),
		Username:         getString(config, "username", ""),
		AvatarURL:        getString(config, "avatar_url", ""),
		Timeout:          getDuration(config, "timeout", 30*time.Second),
	}

	return NewConnector(name, cfg), nil
}

func getString(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

func getDuration(m map[string]interface{}, key string, defaultVal time.Duration) time.Duration {
	if v, ok := m[key].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}
