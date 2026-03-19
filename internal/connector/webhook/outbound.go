package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/matutetandil/mycel/internal/connector"
)

// OutboundConnector sends webhooks to external URLs
type OutboundConnector struct {
	name       string
	config     *OutboundConfig
	httpClient *http.Client
	signer     *SignatureVerifier
}

// NewOutboundConnector creates a new outbound webhook connector
func NewOutboundConnector(name string, config *OutboundConfig) *OutboundConnector {
	if config.Retry == nil {
		config.Retry = DefaultRetryConfig()
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Method == "" {
		config.Method = "POST"
	}
	if config.SignatureHeader == "" {
		config.SignatureHeader = "X-Webhook-Signature"
	}

	var signer *SignatureVerifier
	if config.Secret != "" {
		signer = NewSignatureVerifier(config.Secret, config.SignatureAlgorithm)
	}

	return &OutboundConnector{
		name:   name,
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		signer: signer,
	}
}

// Name returns the connector name
func (c *OutboundConnector) Name() string {
	return c.name
}

// Type returns the connector type
func (c *OutboundConnector) Type() string {
	return "webhook"
}

// Connect establishes the connection (no-op for webhook)
func (c *OutboundConnector) Connect(ctx context.Context) error {
	return nil
}

// WriteData implements connector.Writer for aspect/flow integration.
func (c *OutboundConnector) WriteData(ctx context.Context, data *connector.Data) (*connector.Result, error) {
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
func (c *OutboundConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	return c.WriteData(ctx, data)
}

// writeMessage sends a webhook via the legacy simple interface.
func (c *OutboundConnector) writeMessage(ctx context.Context, target string, data interface{}) (interface{}, error) {
	req := c.buildRequest(target, data)
	return c.Send(ctx, req)
}

// Call sends a webhook (alias for writeMessage)
func (c *OutboundConnector) Call(ctx context.Context, method string, args interface{}) (interface{}, error) {
	return c.writeMessage(ctx, method, args)
}

// Send sends a webhook with full control
func (c *OutboundConnector) Send(ctx context.Context, req *WebhookRequest) (*WebhookResponse, error) {
	url := req.URL
	if url == "" {
		url = c.config.URL
	}
	if url == "" {
		return nil, fmt.Errorf("webhook URL is required")
	}

	method := req.Method
	if method == "" {
		method = c.config.Method
	}

	// Marshal payload
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Prepare headers
	headers := make(map[string]string)
	for k, v := range c.config.Headers {
		headers[k] = v
	}
	for k, v := range req.Headers {
		headers[k] = v
	}

	// Add standard headers
	headers["Content-Type"] = "application/json"
	headers["User-Agent"] = "Mycel-Webhook/1.0"

	// Add event ID
	eventID := req.IdempotencyKey
	if eventID == "" {
		eventID = uuid.New().String()
	}
	headers["X-Webhook-ID"] = eventID

	// Add event type
	if req.EventType != "" {
		headers["X-Webhook-Event"] = req.EventType
	}

	// Add timestamp and signature
	if c.signer != nil {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		if c.config.IncludeTimestamp {
			headers["X-Webhook-Timestamp"] = timestamp
			signature := c.signer.SignWithTimestamp(payload, timestamp)
			headers[c.config.SignatureHeader] = signature
		} else {
			signature := c.signer.Sign(payload)
			headers[c.config.SignatureHeader] = signature
		}
	}

	// Execute with retry
	return c.executeWithRetry(ctx, method, url, payload, headers)
}

func (c *OutboundConnector) executeWithRetry(ctx context.Context, method, url string, payload []byte, headers map[string]string) (*WebhookResponse, error) {
	var lastErr error
	var lastResponse *WebhookResponse

	delay := c.config.Retry.InitialDelay

	for attempt := 1; attempt <= c.config.Retry.MaxAttempts; attempt++ {
		start := time.Now()

		resp, err := c.doRequest(ctx, method, url, payload, headers)
		resp.Attempts = attempt
		resp.Duration = time.Since(start)

		if err == nil && resp.Success {
			return resp, nil
		}

		lastErr = err
		lastResponse = resp

		// Check if we should retry
		if !c.shouldRetry(resp, attempt) {
			break
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return lastResponse, ctx.Err()
		case <-time.After(delay):
		}

		// Exponential backoff
		delay = time.Duration(float64(delay) * c.config.Retry.Multiplier)
		if delay > c.config.Retry.MaxDelay {
			delay = c.config.Retry.MaxDelay
		}
	}

	if lastErr != nil {
		return lastResponse, lastErr
	}
	return lastResponse, nil
}

func (c *OutboundConnector) doRequest(ctx context.Context, method, url string, payload []byte, headers map[string]string) (*WebhookResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return &WebhookResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &WebhookResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	response := &WebhookResponse{
		StatusCode: resp.StatusCode,
		Body:       string(body),
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
	}

	if !response.Success {
		response.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return response, nil
}

func (c *OutboundConnector) shouldRetry(resp *WebhookResponse, attempt int) bool {
	if attempt >= c.config.Retry.MaxAttempts {
		return false
	}

	if resp == nil {
		return true // Network error, retry
	}

	for _, status := range c.config.Retry.RetryableStatuses {
		if resp.StatusCode == status {
			return true
		}
	}

	return false
}

func (c *OutboundConnector) buildRequest(target string, data interface{}) *WebhookRequest {
	req := &WebhookRequest{
		Payload: data,
	}

	// If target looks like a URL, use it
	if target != "" && (len(target) > 4 && target[:4] == "http") {
		req.URL = target
	} else if target != "" {
		req.EventType = target
	}

	// If data is already a WebhookRequest, use it
	if wr, ok := data.(*WebhookRequest); ok {
		return wr
	}
	if wr, ok := data.(WebhookRequest); ok {
		return &wr
	}

	// If data is a map, extract known fields
	if m, ok := data.(map[string]interface{}); ok {
		if url, ok := m["url"].(string); ok {
			req.URL = url
			delete(m, "url")
		}
		if eventType, ok := m["event_type"].(string); ok {
			req.EventType = eventType
			delete(m, "event_type")
		}
		if payload, ok := m["payload"]; ok {
			req.Payload = payload
		} else {
			req.Payload = m
		}
	}

	return req
}

// Health checks if the webhook endpoint is reachable
func (c *OutboundConnector) Health(ctx context.Context) error {
	if c.config.URL == "" {
		return nil // No URL configured, can't check
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", c.config.URL, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Any response (even 405 Method Not Allowed) means the server is reachable
	return nil
}

// Close closes the connector
func (c *OutboundConnector) Close(ctx context.Context) error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Ensure OutboundConnector implements connector interfaces
var (
	_ connector.Connector = (*OutboundConnector)(nil)
	_ connector.Writer    = (*OutboundConnector)(nil)
)
