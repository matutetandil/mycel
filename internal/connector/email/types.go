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

	// Template is the default HTML template file path.
	// Can be overridden per-email via the "template" payload field.
	Template string

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

	// Template is a path to a local HTML template file.
	// If set, the file is rendered using Go text/template with TemplateData
	// (or the full payload) and the result is set as HTMLBody.
	Template string `json:"template,omitempty"`

	// Attachments
	Attachments []Attachment `json:"attachments,omitempty"`

	// Headers
	Headers map[string]string `json:"headers,omitempty"`

	// Tracking
	TrackOpens  bool     `json:"track_opens,omitempty"`
	TrackClicks bool     `json:"track_clicks,omitempty"`
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

// RenderTemplate renders the Template file (if set) into HTMLBody.
// Uses Go text/template syntax ({{.field}}, {{range}}, etc.).
// Data comes from TemplateData if set, otherwise falls back to the full payload fields.
func (e *Email) RenderTemplate(payload map[string]interface{}) error {
	if e.Template == "" {
		return nil
	}

	content, err := os.ReadFile(e.Template)
	if err != nil {
		return fmt.Errorf("failed to read email template %s: %w", e.Template, err)
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

// emailFromData builds an Email from a connector.Data payload.
func emailFromData(target string, payload interface{}) (*Email, error) {
	email := &Email{}

	switch p := payload.(type) {
	case *Email:
		return p, nil
	case Email:
		return &p, nil
	case map[string]interface{}:
		email.To = recipientsOf(p["to"])
		email.CC = recipientsOf(p["cc"])
		email.BCC = recipientsOf(p["bcc"])

		email.From = stringOf(p["from"])
		email.FromName = stringOf(p["from_name"])
		email.ReplyTo = stringOf(p["reply_to"])
		email.ReplyToName = stringOf(p["reply_to_name"])

		email.Subject = stringOf(p["subject"])

		// Both spellings of each body. Only html_body was read while text had
		// two, so somebody writing the obvious pair sent a message with no
		// HTML in it.
		email.TextBody = firstOf(p, "text_body", "text")
		email.HTMLBody = firstOf(p, "html_body", "html")

		email.Template = stringOf(p["template"])
		email.TemplateID = stringOf(p["template_id"])
		if tmplData, ok := p["template_data"].(map[string]interface{}); ok {
			email.TemplateData = tmplData
		}

		email.Attachments = attachmentsOf(p["attachments"])
		email.Headers = headersOf(p["headers"])
		email.Tags = stringsOf(p["tags"])

		if track, ok := p["track_opens"].(bool); ok {
			email.TrackOpens = track
		}
		if track, ok := p["track_clicks"].(bool); ok {
			email.TrackClicks = track
		}
	case string:
		email.TextBody = p
		if target != "" {
			email.To = []Recipient{{Email: target}}
		}
	default:
		return nil, fmt.Errorf("an email is a record or a line of text, and %T is neither", payload)
	}

	// Use target as recipient if not set
	if len(email.To) == 0 && target != "" {
		email.To = []Recipient{{Email: target}}
	}

	return email, nil
}

// recipientsOf reads an address list, however a flow wrote it: one address, a
// list of them, or records carrying a name beside each.
func recipientsOf(value interface{}) []Recipient {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []Recipient{{Email: v}}
	case Recipient:
		return []Recipient{v}
	case []Recipient:
		return v
	case map[string]interface{}:
		if address := stringOf(v["email"]); address != "" {
			return []Recipient{{Email: address, Name: stringOf(v["name"])}}
		}
	case []interface{}:
		var out []Recipient
		for _, item := range v {
			out = append(out, recipientsOf(item)...)
		}
		return out
	case []string:
		out := make([]Recipient, 0, len(v))
		for _, address := range v {
			if address != "" {
				out = append(out, Recipient{Email: address})
			}
		}
		return out
	}
	return nil
}

// attachmentsOf reads what a flow attached — bytes it carried, or an address
// for the provider to fetch.
//
// Nothing read these before, so a flow that generated an invoice and attached
// it sent the email without it: the one thing the message existed to deliver.
func attachmentsOf(value interface{}) []Attachment {
	switch v := value.(type) {
	case nil:
		return nil
	case []Attachment:
		return v
	case Attachment:
		return []Attachment{v}
	case map[string]interface{}:
		if a, ok := attachmentOf(v); ok {
			return []Attachment{a}
		}
	case []interface{}:
		var out []Attachment
		for _, item := range v {
			switch entry := item.(type) {
			case Attachment:
				out = append(out, entry)
			case map[string]interface{}:
				if a, ok := attachmentOf(entry); ok {
					out = append(out, a)
				}
			}
		}
		return out
	}
	return nil
}

func attachmentOf(m map[string]interface{}) (Attachment, bool) {
	a := Attachment{
		Filename:    stringOf(m["filename"]),
		ContentType: stringOf(m["content_type"]),
		ContentID:   stringOf(m["content_id"]),
		URL:         stringOf(m["url"]),
	}

	switch content := m["content"].(type) {
	case []byte:
		a.Content = content
	case string:
		a.Content = []byte(content)
	}

	// Something to send, and something to call it.
	if len(a.Content) == 0 && a.URL == "" {
		return Attachment{}, false
	}
	if a.Filename == "" {
		a.Filename = "attachment"
	}
	return a, true
}

// headersOf reads custom headers, which is how a service threads its own
// identifiers through a provider.
func headersOf(value interface{}) map[string]string {
	switch v := value.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		if len(v) == 0 {
			return nil
		}
		out := make(map[string]string, len(v))
		for name, item := range v {
			out[name] = stringOf(item)
		}
		return out
	}
	return nil
}

func stringsOf(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := stringOf(item); s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

// firstOf returns the first of several spellings that carries a value.
func firstOf(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if s := stringOf(m[key]); s != "" {
			return s
		}
	}
	return ""
}

func stringOf(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
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
