package email

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
	"time"
)

// Config represents email connector configuration
type Config struct {
	// Name is the connector name
	Name string

	// Driver: "smtp", "sendgrid", "ses"
	Driver string

	// SMTP configuration
	SMTP *SMTPConfig

	// SendGrid configuration
	SendGrid *SendGridConfig

	// SES configuration
	SES *SESConfig

	// Default sender
	From        string
	FromName    string
	ReplyTo     string
	ReplyToName string

	// Rate limiting
	RateLimit *RateLimitConfig
}

// SMTPConfig configures SMTP email sending
type SMTPConfig struct {
	// Host is the SMTP server hostname
	Host string

	// Port is the SMTP server port (25, 465, 587)
	Port int

	// Username for authentication
	Username string

	// Password for authentication
	Password string

	// TLS mode: "none", "starttls", "tls"
	TLS string

	// Timeout for SMTP operations
	Timeout time.Duration

	// PoolSize for connection pooling
	PoolSize int
}

// SendGridConfig configures SendGrid email sending
type SendGridConfig struct {
	// APIKey is the SendGrid API key
	APIKey string

	// Endpoint is the API endpoint (default: https://api.sendgrid.com)
	Endpoint string

	// Timeout for API calls
	Timeout time.Duration
}

// SESConfig configures AWS SES email sending
type SESConfig struct {
	// Region is the AWS region
	Region string

	// AccessKeyID for AWS authentication (optional, uses default chain if empty)
	AccessKeyID string

	// SecretAccessKey for AWS authentication
	SecretAccessKey string

	// ConfigurationSet is the SES configuration set name
	ConfigurationSet string

	// Timeout for API calls
	Timeout time.Duration
}

// RateLimitConfig configures email rate limiting
type RateLimitConfig struct {
	// PerSecond is the max emails per second
	PerSecond float64

	// PerMinute is the max emails per minute
	PerMinute int

	// PerHour is the max emails per hour
	PerHour int

	// PerDay is the max emails per day
	PerDay int
}

// Email represents an email to send
type Email struct {
	// From address (overrides default)
	From     string `json:"from,omitempty"`
	FromName string `json:"from_name,omitempty"`

	// Recipients
	To  []Recipient `json:"to"`
	CC  []Recipient `json:"cc,omitempty"`
	BCC []Recipient `json:"bcc,omitempty"`

	// Reply-To
	ReplyTo     string `json:"reply_to,omitempty"`
	ReplyToName string `json:"reply_to_name,omitempty"`

	// Content
	Subject  string `json:"subject"`
	TextBody string `json:"text_body,omitempty"`
	HTMLBody string `json:"html_body,omitempty"`

	// Template (for SendGrid/SES templates)
	TemplateID   string                 `json:"template_id,omitempty"`
	TemplateData map[string]interface{} `json:"template_data,omitempty"`

	// TemplateFile is a path to a local HTML template file.
	// If set, the file is rendered using Go text/template with TemplateData
	// (or the full payload) and the result is set as HTMLBody.
	TemplateFile string `json:"template_file,omitempty"`

	// Attachments
	Attachments []Attachment `json:"attachments,omitempty"`

	// Headers
	Headers map[string]string `json:"headers,omitempty"`

	// Tracking
	TrackOpens  bool   `json:"track_opens,omitempty"`
	TrackClicks bool   `json:"track_clicks,omitempty"`
	Tags        []string `json:"tags,omitempty"`

	// Scheduling
	SendAt *time.Time `json:"send_at,omitempty"`
}

// Recipient represents an email recipient
type Recipient struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string `json:"filename"`
	Content     []byte `json:"content,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	ContentID   string `json:"content_id,omitempty"` // For inline attachments
	URL         string `json:"url,omitempty"`        // URL to fetch content from
}

// SendResult represents the result of sending an email
type SendResult struct {
	// Success indicates if the email was sent
	Success bool `json:"success"`

	// MessageID is the provider-specific message ID
	MessageID string `json:"message_id,omitempty"`

	// Provider is the provider used
	Provider string `json:"provider"`

	// Error message if failed
	Error string `json:"error,omitempty"`

	// Recipients contains per-recipient results
	Recipients []RecipientResult `json:"recipients,omitempty"`
}

// RecipientResult contains the result for a specific recipient
type RecipientResult struct {
	Email   string `json:"email"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// RenderTemplate renders the TemplateFile (if set) into HTMLBody.
// Uses Go text/template syntax ({{.field}}, {{range}}, etc.).
// Data comes from TemplateData if set, otherwise falls back to the full payload fields.
func (e *Email) RenderTemplate(payload map[string]interface{}) error {
	if e.TemplateFile == "" {
		return nil
	}

	content, err := os.ReadFile(e.TemplateFile)
	if err != nil {
		return fmt.Errorf("failed to read email template %s: %w", e.TemplateFile, err)
	}

	tmpl, err := template.New("email").Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	data := e.TemplateData
	if data == nil {
		data = payload
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	e.HTMLBody = buf.String()
	return nil
}

// DefaultSMTPConfig returns sensible SMTP defaults
func DefaultSMTPConfig() *SMTPConfig {
	return &SMTPConfig{
		Port:     587,
		TLS:      "starttls",
		Timeout:  30 * time.Second,
		PoolSize: 5,
	}
}

// DefaultSendGridConfig returns sensible SendGrid defaults
func DefaultSendGridConfig() *SendGridConfig {
	return &SendGridConfig{
		Endpoint: "https://api.sendgrid.com",
		Timeout:  30 * time.Second,
	}
}

// DefaultSESConfig returns sensible SES defaults
func DefaultSESConfig() *SESConfig {
	return &SESConfig{
		Region:  "us-east-1",
		Timeout: 30 * time.Second,
	}
}
