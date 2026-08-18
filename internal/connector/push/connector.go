package push

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
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
	Priority     string               `json:"priority,omitempty"` // "high" or "normal"
	TTL          string               `json:"ttl,omitempty"`      // e.g., "86400s"
	CollapseKey  string               `json:"collapse_key,omitempty"`
	Notification *AndroidNotification `json:"notification,omitempty"`
	Data         map[string]string    `json:"data,omitempty"`
}

// AndroidNotification for Android notification display
type AndroidNotification struct {
	Title       string `json:"title,omitempty"`
	Body        string `json:"body,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	Sound       string `json:"sound,omitempty"`
	Tag         string `json:"tag,omitempty"`
	ClickAction string `json:"click_action,omitempty"`
	ChannelID   string `json:"channel_id,omitempty"`
}

// APNSConfig for iOS-specific options
type APNSConfig struct {
	Headers map[string]string `json:"headers,omitempty"`
	Payload *APNSPayload      `json:"payload,omitempty"`
}

// APNSPayload for iOS notification payload
type APNSPayload struct {
	Aps  *Aps                   `json:"aps,omitempty"`
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

	// What the v1 API needs: the service account, and the access tokens minted
	// from it. Both are nil until Connect has read the credentials.
	account *serviceAccount
	tokens  *googleTokenSource
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

// Connect reads the service account and proves it can be signed with.
//
// The legacy server key is refused rather than tried: Google retired that API
// in June 2024, so a service configured with one would start, look healthy, and
// fail every push against an endpoint that no longer exists. Saying so at
// startup is the difference between a configuration error and a mystery.
func (c *FCMConnector) Connect(ctx context.Context) error {
	if c.config.FCM.ServiceAccountJSON == "" {
		if c.config.FCM.ServerKey != "" {
			return fmt.Errorf("fcm is configured with server_key, which is the legacy API Google retired in June 2024; " +
				"use service_account_json with the service account file from the Firebase console, and project_id")
		}
		return fmt.Errorf("fcm requires service_account_json, the service account file from the Firebase console")
	}

	account, err := loadServiceAccount(c.config.FCM.ServiceAccountJSON)
	if err != nil {
		return err
	}
	c.account = account

	// Signing is checked here rather than at the first push: a key that cannot
	// be parsed should stop a deployment, not a notification.
	tokens, err := newGoogleTokenSource(account, c.httpClient)
	if err != nil {
		return err
	}
	c.tokens = tokens

	if c.projectID() == "" {
		return fmt.Errorf("fcm has no project_id, and the service account does not name one either")
	}
	return nil
}

func (c *FCMConnector) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	if c.tokens == nil {
		err := fmt.Errorf("fcm is not connected, so it has no credentials to send with")
		return &SendResult{Success: false, Provider: "fcm", Error: err.Error()}, err
	}
	return c.sendV1(ctx, msg)
}

// Write implements connector.Writer interface.
func (c *FCMConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	msg, err := pushFromData(data.Target, data.Payload)
	if err != nil {
		return nil, err
	}
	result, err := c.Send(ctx, msg)
	if err != nil {
		return nil, err
	}
	return &connector.Result{
		Rows:     []map[string]interface{}{{"result": result}},
		Affected: 1,
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

// Write implements connector.Writer interface.
func (c *APNsConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	msg, err := pushFromData(data.Target, data.Payload)
	if err != nil {
		return nil, err
	}
	result, err := c.Send(ctx, msg)
	if err != nil {
		return nil, err
	}
	return &connector.Result{
		Rows:     []map[string]interface{}{{"result": result}},
		Affected: 1,
	}, nil
}

func (c *APNsConnector) Health(ctx context.Context) error {
	return nil // APNs doesn't have a health check endpoint
}

func (c *APNsConnector) Close(ctx context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// pushFromData builds a Message from a connector.Data payload.
func pushFromData(target string, payload interface{}) (*Message, error) {
	msg := &Message{}

	switch p := payload.(type) {
	case *Message:
		return p, nil
	case Message:
		return &p, nil
	case map[string]interface{}:
		msg.Token = textOf(p["token"])
		msg.Topic = textOf(p["topic"])
		msg.Condition = textOf(p["condition"])
		msg.Title = textOf(p["title"])
		msg.Body = textOf(p["body"])
		msg.Priority = textOf(p["priority"])
		msg.CollapseKey = textOf(p["collapse_key"])
		msg.Data = dataOf(p["data"])
		msg.Tokens = tokensOf(p["tokens"])

		if ttl, ok := intOf(p["ttl"]); ok {
			msg.TTL = ttl
		}
	case string:
		msg.Body = p
		if target != "" {
			msg.Token = target
		}
	default:
		return nil, fmt.Errorf("a push notification is a record or a line of text, and %T is neither", payload)
	}

	if msg.Token == "" && target != "" {
		msg.Token = target
	}

	return msg, nil
}

// dataOf reads the data payload a notification carries for the app to act on.
//
// It used to be read with a `map[string]string` type assertion, and a flow's
// payload is a `map[string]interface{}` — which is what a transform produces
// and what JSON decodes into — so the assertion never held and the data was
// dropped every time. The notification still arrived and still said the right
// thing; tapping it landed nowhere, because the order id was not in it.
//
// The values are rendered as text because that is all the platform carries. A
// transform that computes a number would otherwise lose exactly the identifier
// the app needs.
func dataOf(value interface{}) map[string]string {
	switch data := value.(type) {
	case nil:
		return nil
	case map[string]string:
		return data
	case map[string]interface{}:
		if len(data) == 0 {
			return nil
		}
		out := make(map[string]string, len(data))
		for key, item := range data {
			out[key] = textOf(item)
		}
		return out
	}
	return nil
}

// tokensOf reads a list of devices, however the list arrived.
func tokensOf(value interface{}) []string {
	switch list := value.(type) {
	case []string:
		return list
	case []interface{}:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if token := textOf(item); token != "" {
				out = append(out, token)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

// textOf renders a value as the text a push payload carries.
func textOf(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		// Whole numbers are identifiers, not measurements: an order id must
		// not arrive as 1234.
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprintf("%v", value)
}

// intOf reads a whole number however it arrived.
func intOf(value interface{}) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if parsed, err := strconv.Atoi(n); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// Ensure connectors implement interface
var (
	_ connector.Connector = (*FCMConnector)(nil)
	_ connector.Connector = (*APNsConnector)(nil)
	_ connector.Writer    = (*FCMConnector)(nil)
	_ connector.Writer    = (*APNsConnector)(nil)
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
