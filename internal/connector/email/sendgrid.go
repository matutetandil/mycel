package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SendGridConnector sends emails via SendGrid API
type SendGridConnector struct {
	name       string
	config     *Config
	httpClient *http.Client
}

// NewSendGridConnector creates a new SendGrid connector
func NewSendGridConnector(name string, config *Config) *SendGridConnector {
	if config.SendGrid == nil {
		config.SendGrid = DefaultSendGridConfig()
	}
	if config.SendGrid.Endpoint == "" {
		config.SendGrid.Endpoint = "https://api.sendgrid.com"
	}
	if config.SendGrid.Timeout == 0 {
		config.SendGrid.Timeout = 30 * time.Second
	}

	return &SendGridConnector{
		name:   name,
		config: config,
		httpClient: &http.Client{
			Timeout: config.SendGrid.Timeout,
		},
	}
}

// Name returns the connector name
func (c *SendGridConnector) Name() string {
	return c.name
}

// Type returns the connector type
func (c *SendGridConnector) Type() string {
	return "email"
}

// Connect validates the API key
func (c *SendGridConnector) Connect(ctx context.Context) error {
	if c.config.SendGrid.APIKey == "" {
		return fmt.Errorf("SendGrid API key is required")
	}
	return nil
}

// Send sends an email via SendGrid
func (c *SendGridConnector) Send(ctx context.Context, email *Email) (*SendResult, error) {
	// Build SendGrid request
	payload := c.buildPayload(email)

	body, err := json.Marshal(payload)
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "sendgrid",
			Error:    err.Error(),
		}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.config.SendGrid.Endpoint+"/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "sendgrid",
			Error:    err.Error(),
		}, err
	}

	req.Header.Set("Authorization", "Bearer "+c.config.SendGrid.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &SendResult{
			Success:  false,
			Provider: "sendgrid",
			Error:    err.Error(),
		}, err
	}
	defer resp.Body.Close()

	// SendGrid returns 202 Accepted for successful sends
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
		messageID := resp.Header.Get("X-Message-Id")
		return &SendResult{
			Success:   true,
			Provider:  "sendgrid",
			MessageID: messageID,
		}, nil
	}

	// Read error response
	respBody, _ := io.ReadAll(resp.Body)
	return &SendResult{
		Success:  false,
		Provider: "sendgrid",
		Error:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)),
	}, fmt.Errorf("SendGrid error: HTTP %d", resp.StatusCode)
}

func (c *SendGridConnector) buildPayload(email *Email) map[string]interface{} {
	payload := make(map[string]interface{})

	// From
	from := email.From
	fromName := email.FromName
	if from == "" {
		from = c.config.From
		fromName = c.config.FromName
	}
	fromObj := map[string]string{"email": from}
	if fromName != "" {
		fromObj["name"] = fromName
	}
	payload["from"] = fromObj

	// Personalizations (recipients)
	personalization := make(map[string]interface{})

	// To
	var toList []map[string]string
	for _, r := range email.To {
		to := map[string]string{"email": r.Email}
		if r.Name != "" {
			to["name"] = r.Name
		}
		toList = append(toList, to)
	}
	personalization["to"] = toList

	// CC
	if len(email.CC) > 0 {
		var ccList []map[string]string
		for _, r := range email.CC {
			cc := map[string]string{"email": r.Email}
			if r.Name != "" {
				cc["name"] = r.Name
			}
			ccList = append(ccList, cc)
		}
		personalization["cc"] = ccList
	}

	// BCC
	if len(email.BCC) > 0 {
		var bccList []map[string]string
		for _, r := range email.BCC {
			bcc := map[string]string{"email": r.Email}
			if r.Name != "" {
				bcc["name"] = r.Name
			}
			bccList = append(bccList, bcc)
		}
		personalization["bcc"] = bccList
	}

	// Subject
	personalization["subject"] = email.Subject

	// Template data (dynamic template data)
	if len(email.TemplateData) > 0 {
		personalization["dynamic_template_data"] = email.TemplateData
	}

	payload["personalizations"] = []interface{}{personalization}

	// Reply-To
	replyTo := email.ReplyTo
	if replyTo == "" {
		replyTo = c.config.ReplyTo
	}
	if replyTo != "" {
		replyToObj := map[string]string{"email": replyTo}
		if email.ReplyToName != "" {
			replyToObj["name"] = email.ReplyToName
		}
		payload["reply_to"] = replyToObj
	}

	// Template ID
	if email.TemplateID != "" {
		payload["template_id"] = email.TemplateID
	} else {
		// Content
		var content []map[string]string
		if email.TextBody != "" {
			content = append(content, map[string]string{
				"type":  "text/plain",
				"value": email.TextBody,
			})
		}
		if email.HTMLBody != "" {
			content = append(content, map[string]string{
				"type":  "text/html",
				"value": email.HTMLBody,
			})
		}
		if len(content) > 0 {
			payload["content"] = content
		}
	}

	// Subject (top level for non-personalization)
	payload["subject"] = email.Subject

	// Tracking
	trackingSettings := make(map[string]interface{})
	if email.TrackOpens {
		trackingSettings["open_tracking"] = map[string]bool{"enable": true}
	}
	if email.TrackClicks {
		trackingSettings["click_tracking"] = map[string]bool{"enable": true}
	}
	if len(trackingSettings) > 0 {
		payload["tracking_settings"] = trackingSettings
	}

	// Categories (tags)
	if len(email.Tags) > 0 {
		payload["categories"] = email.Tags
	}

	// Send at (scheduling)
	if email.SendAt != nil {
		payload["send_at"] = email.SendAt.Unix()
	}

	// Custom headers
	if len(email.Headers) > 0 {
		payload["headers"] = email.Headers
	}

	return payload
}

// Health checks SendGrid API
func (c *SendGridConnector) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.config.SendGrid.Endpoint+"/v3/scopes", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.config.SendGrid.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SendGrid health check failed: HTTP %d", resp.StatusCode)
	}

	return nil
}

// Close closes the connector
func (c *SendGridConnector) Close(ctx context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}
