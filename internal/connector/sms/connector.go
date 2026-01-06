package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/matutetandil/mycel/internal/connector"
)

// Config represents SMS connector configuration
type Config struct {
	Name   string
	Driver string // "twilio" or "sns"

	// Twilio config
	Twilio *TwilioConfig

	// SNS config
	SNS *SNSConfig

	// Default sender
	From string
}

// TwilioConfig for Twilio SMS
type TwilioConfig struct {
	AccountSID string
	AuthToken  string
	From       string // Phone number or messaging service SID
	Timeout    time.Duration
}

// SNSConfig for AWS SNS SMS
type SNSConfig struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SenderID        string
	SMSType         string // "Promotional" or "Transactional"
	Timeout         time.Duration
}

// Message represents an SMS message
type Message struct {
	To   string `json:"to"`
	Body string `json:"body"`
	From string `json:"from,omitempty"` // Override default
}

// SendResult represents the result of sending an SMS
type SendResult struct {
	Success   bool   `json:"success"`
	MessageID string `json:"message_id,omitempty"`
	Provider  string `json:"provider"`
	Error     string `json:"error,omitempty"`
}

// TwilioConnector sends SMS via Twilio
type TwilioConnector struct {
	name       string
	config     *Config
	httpClient *http.Client
}

// NewTwilioConnector creates a new Twilio connector
func NewTwilioConnector(name string, cfg *Config) *TwilioConnector {
	if cfg.Twilio.Timeout == 0 {
		cfg.Twilio.Timeout = 30 * time.Second
	}

	return &TwilioConnector{
		name:   name,
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Twilio.Timeout,
		},
	}
}

func (c *TwilioConnector) Name() string { return c.name }
func (c *TwilioConnector) Type() string { return "sms" }

func (c *TwilioConnector) Connect(ctx context.Context) error {
	if c.config.Twilio.AccountSID == "" || c.config.Twilio.AuthToken == "" {
		return fmt.Errorf("twilio account_sid and auth_token are required")
	}
	return nil
}

func (c *TwilioConnector) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	from := msg.From
	if from == "" {
		from = c.config.Twilio.From
	}
	if from == "" {
		from = c.config.From
	}

	data := url.Values{}
	data.Set("To", msg.To)
	data.Set("From", from)
	data.Set("Body", msg.Body)

	url := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json",
		c.config.Twilio.AccountSID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(data.Encode()))
	if err != nil {
		return &SendResult{Success: false, Provider: "twilio", Error: err.Error()}, err
	}

	req.SetBasicAuth(c.config.Twilio.AccountSID, c.config.Twilio.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &SendResult{Success: false, Provider: "twilio", Error: err.Error()}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return &SendResult{
			Success:  false,
			Provider: "twilio",
			Error:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)),
		}, fmt.Errorf("twilio error: %s", string(body))
	}

	var result struct {
		SID string `json:"sid"`
	}
	json.Unmarshal(body, &result)

	return &SendResult{
		Success:   true,
		Provider:  "twilio",
		MessageID: result.SID,
	}, nil
}

func (c *TwilioConnector) Health(ctx context.Context) error {
	url := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s.json",
		c.config.Twilio.AccountSID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.config.Twilio.AccountSID, c.config.Twilio.AuthToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("twilio health check failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *TwilioConnector) Close(ctx context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// SNSConnector sends SMS via AWS SNS
type SNSConnector struct {
	name   string
	config *Config
	client *sns.Client
}

// NewSNSConnector creates a new SNS connector
func NewSNSConnector(name string, cfg *Config) *SNSConnector {
	return &SNSConnector{
		name:   name,
		config: cfg,
	}
}

func (c *SNSConnector) Name() string { return c.name }
func (c *SNSConnector) Type() string { return "sms" }

func (c *SNSConnector) Connect(ctx context.Context) error {
	var opts []func(*config.LoadOptions) error

	if c.config.SNS.Region != "" {
		opts = append(opts, config.WithRegion(c.config.SNS.Region))
	}

	if c.config.SNS.AccessKeyID != "" && c.config.SNS.SecretAccessKey != "" {
		creds := credentials.NewStaticCredentialsProvider(
			c.config.SNS.AccessKeyID,
			c.config.SNS.SecretAccessKey,
			"",
		)
		opts = append(opts, config.WithCredentialsProvider(creds))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	c.client = sns.NewFromConfig(cfg)
	return nil
}

func (c *SNSConnector) Send(ctx context.Context, msg *Message) (*SendResult, error) {
	if c.client == nil {
		return &SendResult{Success: false, Provider: "sns", Error: "not connected"}, fmt.Errorf("not connected")
	}

	input := &sns.PublishInput{
		PhoneNumber: aws.String(msg.To),
		Message:     aws.String(msg.Body),
	}

	// Note: SMS attributes (SenderID, SMSType) can be set via SNS console
	// or by using MessageAttributes with types.MessageAttributeValue

	result, err := c.client.Publish(ctx, input)
	if err != nil {
		return &SendResult{Success: false, Provider: "sns", Error: err.Error()}, err
	}

	return &SendResult{
		Success:   true,
		Provider:  "sns",
		MessageID: aws.ToString(result.MessageId),
	}, nil
}

func (c *SNSConnector) Health(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}
	// Check by getting SMS attributes
	_, err := c.client.GetSMSAttributes(ctx, &sns.GetSMSAttributesInput{})
	return err
}

func (c *SNSConnector) Close(ctx context.Context) error {
	return nil
}

// Ensure connectors implement interface
var (
	_ connector.Connector = (*TwilioConnector)(nil)
	_ connector.Connector = (*SNSConnector)(nil)
)

// Factory creates SMS connectors
type Factory struct{}

func NewFactory() *Factory { return &Factory{} }

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connectorType, driver string) bool {
	return connectorType == "sms"
}

func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	props := cfg.Properties
	driver := getString(props, "driver", "twilio")

	smsCfg := &Config{
		Name:   cfg.Name,
		Driver: driver,
		From:   getString(props, "from", ""),
	}

	switch driver {
	case "twilio":
		smsCfg.Twilio = &TwilioConfig{
			AccountSID: getString(props, "account_sid", ""),
			AuthToken:  getString(props, "auth_token", ""),
			From:       getString(props, "from", ""),
			Timeout:    getDuration(props, "timeout", 30*time.Second),
		}
		return NewTwilioConnector(cfg.Name, smsCfg), nil

	case "sns":
		smsCfg.SNS = &SNSConfig{
			Region:          getString(props, "region", "us-east-1"),
			AccessKeyID:     getString(props, "access_key_id", ""),
			SecretAccessKey: getString(props, "secret_access_key", ""),
			SenderID:        getString(props, "sender_id", ""),
			SMSType:         getString(props, "sms_type", "Transactional"),
			Timeout:         getDuration(props, "timeout", 30*time.Second),
		}
		return NewSNSConnector(cfg.Name, smsCfg), nil

	default:
		return nil, fmt.Errorf("unknown SMS driver: %s", driver)
	}
}

func getString(m map[string]interface{}, key, def string) string {
	if v, ok := m[key].(string); ok {
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
