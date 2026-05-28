package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Config represents Slack connector configuration
type Config struct {
	// Name is the connector name
	Name string

	// WebhookURL is the Slack incoming webhook URL
	WebhookURL string

	// Token is the Bot/User OAuth token (for API calls)
	Token string

	// APIURL is the base URL for the Slack API.
	// Default: "https://slack.com/api"
	APIURL string

	// DefaultChannel is the default channel to post to
	DefaultChannel string

	// Username to display (webhook only)
	Username string

	// IconEmoji to display (webhook only)
	IconEmoji string

	// IconURL to display (webhook only)
	IconURL string

	// Timeout for requests
	Timeout time.Duration

	// Batch configures the connector's message-batching behavior. When nil,
	// DefaultBatchConfig() is applied (batching enabled). See BatchConfig for
	// the knobs; opt out with `batch { enabled = false }` in HCL.
	Batch *BatchConfig
}

// Message represents a Slack message
type Message struct {
	// Channel to post to (overrides default)
	Channel string `json:"channel,omitempty"`

	// Text is the main message text
	Text string `json:"text,omitempty"`

	// Blocks for rich formatting
	Blocks []Block `json:"blocks,omitempty"`

	// Attachments for legacy formatting
	Attachments []Attachment `json:"attachments,omitempty"`

	// ThreadTS to reply to a thread
	ThreadTS string `json:"thread_ts,omitempty"`

	// Username to display (overrides default)
	Username string `json:"username,omitempty"`

	// IconEmoji to display
	IconEmoji string `json:"icon_emoji,omitempty"`

	// IconURL to display
	IconURL string `json:"icon_url,omitempty"`

	// Unfurl settings
	UnfurlLinks bool `json:"unfurl_links,omitempty"`
	UnfurlMedia bool `json:"unfurl_media,omitempty"`

	// Mrkdwn enables markdown parsing
	Mrkdwn bool `json:"mrkdwn,omitempty"`
}

// Block represents a Slack block element
type Block struct {
	Type     string      `json:"type"`
	Text     *TextObject `json:"text,omitempty"`
	BlockID  string      `json:"block_id,omitempty"`
	Elements []Element   `json:"elements,omitempty"`
	Fields   []TextObject `json:"fields,omitempty"`
	Accessory *Element   `json:"accessory,omitempty"`
}

// TextObject represents text in Slack
type TextObject struct {
	Type  string `json:"type"` // "plain_text" or "mrkdwn"
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

// Element represents an interactive element
type Element struct {
	Type     string      `json:"type"`
	Text     *TextObject `json:"text,omitempty"`
	ActionID string      `json:"action_id,omitempty"`
	URL      string      `json:"url,omitempty"`
	Value    string      `json:"value,omitempty"`
	Style    string      `json:"style,omitempty"`
}

// Attachment represents a legacy attachment
type Attachment struct {
	Color      string `json:"color,omitempty"`
	Pretext    string `json:"pretext,omitempty"`
	AuthorName string `json:"author_name,omitempty"`
	AuthorLink string `json:"author_link,omitempty"`
	AuthorIcon string `json:"author_icon,omitempty"`
	Title      string `json:"title,omitempty"`
	TitleLink  string `json:"title_link,omitempty"`
	Text       string `json:"text,omitempty"`
	Fields     []AttachmentField `json:"fields,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`
	ThumbURL   string `json:"thumb_url,omitempty"`
	Footer     string `json:"footer,omitempty"`
	FooterIcon string `json:"footer_icon,omitempty"`
	Timestamp  int64  `json:"ts,omitempty"`
}

// AttachmentField represents a field in an attachment
type AttachmentField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// SendResult represents the result of sending a message
type SendResult struct {
	Success   bool   `json:"success"`
	MessageTS string `json:"message_ts,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Connector sends messages to Slack
type Connector struct {
	name       string
	config     *Config
	httpClient *http.Client
	logger     *slog.Logger

	// batcher is non-nil when batching is enabled (the default). When set, the
	// Write/WriteData/Send path routes through it instead of calling the
	// webhook/API per message.
	batcher *batcher
}

// NewConnector creates a new Slack connector. When config.Batch is nil the
// default-on batcher (window=3s, max_size=50, per-channel) is installed; pass
// a BatchConfig with Enabled=false to restore the pre-v2.5.0 immediate-send
// behavior.
func NewConnector(name string, config *Config) *Connector {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.APIURL == "" {
		config.APIURL = "https://slack.com/api"
	}
	// Strip trailing slash
	if len(config.APIURL) > 0 && config.APIURL[len(config.APIURL)-1] == '/' {
		config.APIURL = config.APIURL[:len(config.APIURL)-1]
	}
	if config.Batch == nil {
		config.Batch = DefaultBatchConfig()
	}

	c := &Connector{
		name:   name,
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		logger: slog.Default(),
	}

	if config.Batch.Enabled {
		// rawSend is the unbuffered code path the batcher calls when it
		// flushes. Connector.Send (the public method) routes new traffic
		// through the batcher instead, so a direct call to rawSend is the only
		// way out of the batching layer.
		b, err := newBatcher(config.Batch, c.rawSend, c.logger)
		if err != nil {
			// A misconfigured batcher must not silently lose messages; fall
			// back to immediate sends and tell the operator.
			c.logger.Warn("slack: failed to init batcher, falling back to direct send",
				"connector", name,
				"error", err)
		} else {
			c.batcher = b
		}
	}
	return c
}

// Name returns the connector name
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type
func (c *Connector) Type() string {
	return "slack"
}

// Connect validates the configuration
func (c *Connector) Connect(ctx context.Context) error {
	if c.config.WebhookURL == "" && c.config.Token == "" {
		return fmt.Errorf("either webhook_url or token is required")
	}
	return nil
}

// Send sends a message to Slack. When batching is enabled (the default), the
// message is queued and a synthetic Success result is returned immediately;
// the batcher flushes a single collapsed message per window. When batching is
// disabled it falls through to rawSend, the pre-v2.5.0 behavior.
func (c *Connector) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	c.applyDefaults(msg)
	if c.batcher != nil {
		if err := c.batcher.Submit(ctx, msg); err != nil {
			return &SendResult{Success: false, Error: err.Error()}, err
		}
		return &SendResult{Success: true}, nil
	}
	return c.rawSend(ctx, msg)
}

// rawSend dispatches a message immediately, picking the webhook or Web API
// path. It is the unbatched code path; both the batcher's flush callback and
// the disabled-batch Send fall through here.
func (c *Connector) rawSend(ctx context.Context, msg *Message) (*SendResult, error) {
	c.applyDefaults(msg)
	if c.config.WebhookURL != "" {
		return c.sendViaWebhook(ctx, msg)
	}
	return c.sendViaAPI(ctx, msg)
}

// applyDefaults fills in connector-level defaults for fields the message left
// blank. Safe to call multiple times — Send and rawSend both invoke it so a
// message dispatched directly through rawSend (e.g. from the batcher) still
// gets the per-connector username/icon.
func (c *Connector) applyDefaults(msg *Message) {
	if msg.Channel == "" {
		msg.Channel = c.config.DefaultChannel
	}
	if msg.Username == "" {
		msg.Username = c.config.Username
	}
	if msg.IconEmoji == "" {
		msg.IconEmoji = c.config.IconEmoji
	}
	if msg.IconURL == "" {
		msg.IconURL = c.config.IconURL
	}
}

func (c *Connector) sendViaWebhook(ctx context.Context, msg *Message) (*SendResult, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}

	resp, err := c.doWithRateLimitRetry(ctx, "POST", c.config.WebhookURL,
		map[string]string{"Content-Type": "application/json"}, body)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return &SendResult{
			Success: false,
			Error:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, fmt.Errorf("slack webhook error: %s", string(respBody))
	}

	// Webhook returns "ok" on success
	if string(respBody) == "ok" {
		return &SendResult{Success: true}, nil
	}

	return &SendResult{
		Success: false,
		Error:   string(respBody),
	}, fmt.Errorf("slack error: %s", string(respBody))
}

func (c *Connector) sendViaAPI(ctx context.Context, msg *Message) (*SendResult, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}

	resp, err := c.doWithRateLimitRetry(ctx, "POST",
		c.config.APIURL+"/chat.postMessage",
		map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + c.config.Token,
		}, body)
	if err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	var result struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error,omitempty"`
		Channel   string `json:"channel,omitempty"`
		MessageTS string `json:"ts,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return &SendResult{Success: false, Error: err.Error()}, err
	}

	if !result.OK {
		return &SendResult{
			Success: false,
			Error:   result.Error,
		}, fmt.Errorf("slack API error: %s", result.Error)
	}

	return &SendResult{
		Success:   true,
		Channel:   result.Channel,
		MessageTS: result.MessageTS,
	}, nil
}

// WriteData implements connector.Writer for aspect/flow integration.
func (c *Connector) WriteData(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	result, err := c.writeMessage(ctx, data.Target, data.Payload)
	if err != nil {
		return nil, err
	}
	return &connector.Result{
		Rows:     []map[string]interface{}{{"result": result}},
		Affected: 1,
	}, nil
}

// Write implements connector.Writer interface.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	return c.WriteData(ctx, data)
}

// writeMessage sends a message via the legacy simple interface.
func (c *Connector) writeMessage(ctx context.Context, target string, data interface{}) (interface{}, error) {
	// If data is already a Message, use it
	if msg, ok := data.(*Message); ok {
		return c.Send(ctx, msg)
	}
	if msg, ok := data.(Message); ok {
		return c.Send(ctx, &msg)
	}

	// If data is a map, try to build a message
	if m, ok := data.(map[string]interface{}); ok {
		msg := &Message{
			Channel: target,
		}
		if text, ok := m["text"].(string); ok {
			msg.Text = text
		}
		if channel, ok := m["channel"].(string); ok {
			msg.Channel = channel
		}
		return c.Send(ctx, msg)
	}

	// If data is a string, use it as text
	if text, ok := data.(string); ok {
		return c.Send(ctx, &Message{
			Channel: target,
			Text:    text,
		})
	}

	return nil, fmt.Errorf("unsupported data type for Slack message")
}

// Health checks Slack connectivity
func (c *Connector) Health(ctx context.Context) error {
	if c.config.Token != "" {
		// Check API auth
		req, err := http.NewRequestWithContext(ctx, "POST",
			c.config.APIURL+"/auth.test", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.config.Token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("slack auth check failed: HTTP %d", resp.StatusCode)
		}
	}
	return nil
}

// Close drains the batcher (flushing any pending buckets so a graceful
// shutdown does not lose queued notifications) and releases HTTP resources.
func (c *Connector) Close(ctx context.Context) error {
	if c.batcher != nil {
		if err := c.batcher.Drain(ctx); err != nil {
			c.logger.Warn("slack: batcher drain failed during close",
				"connector", c.name,
				"error", err)
		}
	}
	c.httpClient.CloseIdleConnections()
	return nil
}

// Ensure Connector implements connector interface
var _ connector.Connector = (*Connector)(nil)

// Factory creates Slack connectors
type Factory struct{}

// NewFactory creates a new Slack factory
func NewFactory() *Factory {
	return &Factory{}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connectorType, driver string) bool {
	return connectorType == "slack"
}

// Create creates a new Slack connector
func (f *Factory) Create(ctx context.Context, config *connector.Config) (connector.Connector, error) {
	props := config.Properties
	cfg := &Config{
		Name:           config.Name,
		WebhookURL:     getString(props, "webhook_url", ""),
		Token:          getString(props, "token", ""),
		APIURL:         getString(props, "api_url", ""),
		DefaultChannel: getString(props, "channel", ""),
		Username:       getString(props, "username", ""),
		IconEmoji:      getString(props, "icon_emoji", ""),
		IconURL:        getString(props, "icon_url", ""),
		Timeout:        getDuration(props, "timeout", 30*time.Second),
	}

	// `batch { ... }` block — the parser stores it under Properties["batch"].
	// Absent → DefaultBatchConfig (enabled).
	batchCfg, err := parseBatchConfig(props["batch"])
	if err != nil {
		return nil, fmt.Errorf("slack connector %q: %w", config.Name, err)
	}
	cfg.Batch = batchCfg

	return NewConnector(config.Name, cfg), nil
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
