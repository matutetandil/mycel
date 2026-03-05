package push

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	"golang.org/x/net/http2"
)

// Config represents push notification connector configuration
type Config struct {
	Name   string
	Driver string // "fcm" or "apns"

	// FCM config
	FCM *FCMConfig

	// APNs config
	APNs *APNsConfig
}

// FCMConfig for Firebase Cloud Messaging
type FCMConfig struct {
	// ServerKey is the legacy FCM server key
	ServerKey string

	// ProjectID for FCM v1 API
	ProjectID string

	// ServiceAccountJSON is the path to service account credentials
	ServiceAccountJSON string

	// APIURL is the base URL for the FCM API.
	// Default: "https://fcm.googleapis.com"
	APIURL string

	// Timeout for requests
	Timeout time.Duration
}

// APNsConfig for Apple Push Notification service
type APNsConfig struct {
	// TeamID is the Apple Developer Team ID
	TeamID string

	// KeyID is the APNs auth key ID
	KeyID string

	// PrivateKey is the APNs auth key (P8 format)
	PrivateKey string

	// BundleID is the app bundle identifier
	BundleID string

	// Production indicates whether to use production or sandbox
	Production bool

	// APIURL overrides the APNs endpoint.
	// Default: "https://api.push.apple.com" (production) or "https://api.sandbox.push.apple.com" (sandbox)
	APIURL string

	// Timeout for requests
	Timeout time.Duration
}

// Message represents a push notification
type Message struct {
	// Token is the device token
	Token string `json:"token,omitempty"`

	// Tokens for sending to multiple devices
	Tokens []string `json:"tokens,omitempty"`

	// Topic for topic-based messaging (FCM)
	Topic string `json:"topic,omitempty"`

	// Condition for condition-based messaging (FCM)
	Condition string `json:"condition,omitempty"`

	// Title of the notification
	Title string `json:"title,omitempty"`

	// Body of the notification
	Body string `json:"body,omitempty"`

	// Data payload
	Data map[string]string `json:"data,omitempty"`

	// Platform-specific options
	Android *AndroidConfig `json:"android,omitempty"`
	APNS    *APNSConfig    `json:"apns,omitempty"`
	Web     *WebConfig     `json:"web,omitempty"`

	// Priority: "high" or "normal"
	Priority string `json:"priority,omitempty"`

	// TTL in seconds
	TTL int `json:"ttl,omitempty"`

	// CollapseKey for collapsible notifications
	CollapseKey string `json:"collapse_key,omitempty"`
}

// AndroidConfig for Android-specific options
type AndroidConfig struct {
	Priority     string            `json:"priority,omitempty"` // "high" or "normal"
	TTL          string            `json:"ttl,omitempty"`      // e.g., "86400s"
	CollapseKey  string            `json:"collapse_key,omitempty"`
	Notification *AndroidNotification `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
}

// AndroidNotification for Android notification display
type AndroidNotification struct {
	Title        string `json:"title,omitempty"`
	Body         string `json:"body,omitempty"`
	Icon         string `json:"icon,omitempty"`
	Color        string `json:"color,omitempty"`
	Sound        string `json:"sound,omitempty"`
	Tag          string `json:"tag,omitempty"`
	ClickAction  string `json:"click_action,omitempty"`
	ChannelID    string `json:"channel_id,omitempty"`
}

// APNSConfig for iOS-specific options
type APNSConfig struct {
	Headers map[string]string `json:"headers,omitempty"`
	Payload *APNSPayload      `json:"payload,omitempty"`
}

// APNSPayload for iOS notification payload
type APNSPayload struct {
	Aps  *Aps              `json:"aps,omitempty"`
	Data map[string]interface{} `json:"data,omitempty"`
}

// Aps is the Apple Push notification payload
type Aps struct {
	Alert            interface{} `json:"alert,omitempty"`
	Badge            *int        `json:"badge,omitempty"`
	Sound            interface{} `json:"sound,omitempty"`
	ContentAvailable int         `json:"content-available,omitempty"`
	MutableContent   int         `json:"mutable-content,omitempty"`
	Category         string      `json:"category,omitempty"`
	ThreadID         string      `json:"thread-id,omitempty"`
}

// WebConfig for web push options
type WebConfig struct {
	Headers      map[string]string `json:"headers,omitempty"`
	Notification *WebNotification  `json:"notification,omitempty"`
}

// WebNotification for web notification display
type WebNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// SendResult represents the result of sending a push notification
type SendResult struct {
	Success      bool     `json:"success"`
	MessageID    string   `json:"message_id,omitempty"`
	Provider     string   `json:"provider"`
	Error        string   `json:"error,omitempty"`
	FailedTokens []string `json:"failed_tokens,omitempty"`
}

// FCMConnector sends push notifications via Firebase Cloud Messaging
type FCMConnector struct {
	name       string
	config     *Config
	httpClient *http.Client
}

// NewFCMConnector creates a new FCM connector
func NewFCMConnector(name string, cfg *Config) *FCMConnector {
	if cfg.FCM.Timeout == 0 {
		cfg.FCM.Timeout = 30 * time.Second
	}
	if cfg.FCM.APIURL == "" {
		cfg.FCM.APIURL = "https://fcm.googleapis.com"
	}
	if len(cfg.FCM.APIURL) > 0 && cfg.FCM.APIURL[len(cfg.FCM.APIURL)-1] == '/' {
		cfg.FCM.APIURL = cfg.FCM.APIURL[:len(cfg.FCM.APIURL)-1]
	}

	return &FCMConnector{
		name:   name,
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.FCM.Timeout,
		},
	}
}

func (c *FCMConnector) Name() string { return c.name }
func (c *FCMConnector) Type() string { return "push" }

func (c *FCMConnector) Connect(ctx context.Context) error {
	if c.config.FCM.ServerKey == "" {
		return fmt.Errorf("fcm server_key is required")
	}
	return nil
}

func (c *FCMConnector) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	// Build FCM message
	fcmMsg := map[string]interface{}{}

	// Single token or multiple
	if msg.Token != "" {
		fcmMsg["to"] = msg.Token
	} else if len(msg.Tokens) > 0 {
		fcmMsg["registration_ids"] = msg.Tokens
	} else if msg.Topic != "" {
		fcmMsg["to"] = "/topics/" + msg.Topic
	} else if msg.Condition != "" {
		fcmMsg["condition"] = msg.Condition
	}

	// Notification
	if msg.Title != "" || msg.Body != "" {
		fcmMsg["notification"] = map[string]string{
			"title": msg.Title,
			"body":  msg.Body,
		}
	}

	// Data
	if len(msg.Data) > 0 {
		fcmMsg["data"] = msg.Data
	}

	// Priority
	if msg.Priority != "" {
		fcmMsg["priority"] = msg.Priority
	}

	// TTL
	if msg.TTL > 0 {
		fcmMsg["time_to_live"] = msg.TTL
	}

	// Collapse key
	if msg.CollapseKey != "" {
		fcmMsg["collapse_key"] = msg.CollapseKey
	}

	body, err := json.Marshal(fcmMsg)
	if err != nil {
		return &SendResult{Success: false, Provider: "fcm", Error: err.Error()}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.config.FCM.APIURL+"/fcm/send", bytes.NewReader(body))
	if err != nil {
		return &SendResult{Success: false, Provider: "fcm", Error: err.Error()}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "key="+c.config.FCM.ServerKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &SendResult{Success: false, Provider: "fcm", Error: err.Error()}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return &SendResult{
			Success:  false,
			Provider: "fcm",
			Error:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, fmt.Errorf("FCM error: %s", string(respBody))
	}

	var result struct {
		MessageID    int64 `json:"message_id"`
		Success      int   `json:"success"`
		Failure      int   `json:"failure"`
		Results      []struct {
			MessageID string `json:"message_id"`
			Error     string `json:"error"`
		} `json:"results"`
	}
	json.Unmarshal(respBody, &result)

	// Collect failed tokens
	var failedTokens []string
	if len(msg.Tokens) > 0 && len(result.Results) > 0 {
		for i, r := range result.Results {
			if r.Error != "" && i < len(msg.Tokens) {
				failedTokens = append(failedTokens, msg.Tokens[i])
			}
		}
	}

	return &SendResult{
		Success:      result.Success > 0 || result.Failure == 0,
		Provider:     "fcm",
		MessageID:    fmt.Sprintf("%d", result.MessageID),
		FailedTokens: failedTokens,
	}, nil
}

func (c *FCMConnector) Health(ctx context.Context) error {
	return nil // FCM doesn't have a health check endpoint
}

func (c *FCMConnector) Close(ctx context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// APNsConnector sends push notifications via Apple Push Notification service
type APNsConnector struct {
	name       string
	config     *Config
	httpClient *http.Client
}

// NewAPNsConnector creates a new APNs connector
func NewAPNsConnector(name string, cfg *Config) *APNsConnector {
	if cfg.APNs.Timeout == 0 {
		cfg.APNs.Timeout = 30 * time.Second
	}

	// Create HTTP/2 client for APNs
	transport := &http2.Transport{
		TLSClientConfig: &tls.Config{},
	}

	return &APNsConnector{
		name:   name,
		config: cfg,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.APNs.Timeout,
		},
	}
}

func (c *APNsConnector) Name() string { return c.name }
func (c *APNsConnector) Type() string { return "push" }

func (c *APNsConnector) Connect(ctx context.Context) error {
	if c.config.APNs.TeamID == "" || c.config.APNs.KeyID == "" || c.config.APNs.PrivateKey == "" {
		return fmt.Errorf("apns team_id, key_id, and private_key are required")
	}
	return nil
}

func (c *APNsConnector) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	if msg.Token == "" && len(msg.Tokens) == 0 {
		return &SendResult{Success: false, Provider: "apns", Error: "token is required"}, fmt.Errorf("token required")
	}

	// Send to single token
	token := msg.Token
	if token == "" && len(msg.Tokens) > 0 {
		token = msg.Tokens[0]
	}

	// Build APNs payload
	payload := map[string]interface{}{
		"aps": map[string]interface{}{
			"alert": map[string]string{
				"title": msg.Title,
				"body":  msg.Body,
			},
		},
	}

	// Add custom data
	for k, v := range msg.Data {
		payload[k] = v
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return &SendResult{Success: false, Provider: "apns", Error: err.Error()}, err
	}

	// Determine endpoint
	var baseURL string
	if c.config.APNs.APIURL != "" {
		baseURL = c.config.APNs.APIURL
	} else if c.config.APNs.Production {
		baseURL = "https://api.push.apple.com"
	} else {
		baseURL = "https://api.sandbox.push.apple.com"
	}
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	url := fmt.Sprintf("%s/3/device/%s", baseURL, token)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return &SendResult{Success: false, Provider: "apns", Error: err.Error()}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apns-topic", c.config.APNs.BundleID)

	// Note: In production, you would add JWT auth here
	// req.Header.Set("Authorization", "bearer "+jwtToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &SendResult{Success: false, Provider: "apns", Error: err.Error()}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return &SendResult{
			Success:  false,
			Provider: "apns",
			Error:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}, fmt.Errorf("APNs error: HTTP %d", resp.StatusCode)
	}

	return &SendResult{
		Success:   true,
		Provider:  "apns",
		MessageID: resp.Header.Get("apns-id"),
	}, nil
}

func (c *APNsConnector) Health(ctx context.Context) error {
	return nil // APNs doesn't have a health check endpoint
}

func (c *APNsConnector) Close(ctx context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Ensure connectors implement interface
var (
	_ connector.Connector = (*FCMConnector)(nil)
	_ connector.Connector = (*APNsConnector)(nil)
)

// Factory creates push notification connectors
type Factory struct{}

func NewFactory() *Factory { return &Factory{} }

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connectorType, driver string) bool {
	return connectorType == "push"
}

func (f *Factory) Create(ctx context.Context, connCfg *connector.Config) (connector.Connector, error) {
	props := connCfg.Properties
	driver := getString(props, "driver", "fcm")

	cfg := &Config{
		Name:   connCfg.Name,
		Driver: driver,
	}

	switch driver {
	case "fcm":
		cfg.FCM = &FCMConfig{
			ServerKey:          getString(props, "server_key", ""),
			ProjectID:          getString(props, "project_id", ""),
			ServiceAccountJSON: getString(props, "service_account_json", ""),
			APIURL:             getString(props, "api_url", ""),
			Timeout:            getDuration(props, "timeout", 30*time.Second),
		}
		return NewFCMConnector(connCfg.Name, cfg), nil

	case "apns":
		cfg.APNs = &APNsConfig{
			TeamID:     getString(props, "team_id", ""),
			KeyID:      getString(props, "key_id", ""),
			PrivateKey: getString(props, "private_key", ""),
			BundleID:   getString(props, "bundle_id", ""),
			Production: getBool(props, "production", false),
			APIURL:     getString(props, "api_url", ""),
			Timeout:    getDuration(props, "timeout", 30*time.Second),
		}
		return NewAPNsConnector(connCfg.Name, cfg), nil

	default:
		return nil, fmt.Errorf("unknown push driver: %s", driver)
	}
}

func getString(m map[string]interface{}, key, def string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return def
}

func getBool(m map[string]interface{}, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return def
}

func getDuration(m map[string]interface{}, key string, def time.Duration) time.Duration {
	if v, ok := m[key].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
