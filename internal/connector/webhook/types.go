package webhook

import (
	"time"
)

// Config represents webhook connector configuration
type Config struct {
	// Name is the connector name
	Name string

	// Mode: "inbound" (receive webhooks) or "outbound" (send webhooks)
	Mode string

	// Inbound configuration
	Inbound *InboundConfig

	// Outbound configuration
	Outbound *OutboundConfig
}

// InboundConfig configures webhook receiving
type InboundConfig struct {
	// Path to listen on (e.g., "/webhooks/stripe")
	Path string

	// Secret for signature verification
	Secret string

	// SignatureHeader is the header containing the signature
	SignatureHeader string

	// SignatureAlgorithm: "hmac-sha256", "hmac-sha1", "none"
	SignatureAlgorithm string

	// TimestampHeader for replay protection
	TimestampHeader string

	// TimestampTolerance is max age of webhook (default: 5m)
	TimestampTolerance time.Duration

	// AllowedIPs restricts which IPs can send webhooks
	AllowedIPs []string

	// RequireHTTPS enforces HTTPS
	RequireHTTPS bool
}

// OutboundConfig configures webhook sending
type OutboundConfig struct {
	// URL to send webhooks to
	URL string

	// Method: POST (default), PUT
	Method string

	// Headers to include in requests
	Headers map[string]string

	// Secret for signing outbound webhooks
	Secret string

	// SignatureHeader where to put the signature
	SignatureHeader string

	// SignatureAlgorithm: "hmac-sha256", "hmac-sha1", "none"
	SignatureAlgorithm string

	// IncludeTimestamp adds timestamp to signature
	IncludeTimestamp bool

	// Timeout for requests (default: 30s)
	Timeout time.Duration

	// Retry configuration
	Retry *RetryConfig
}

// RetryConfig configures retry behavior
type RetryConfig struct {
	// MaxAttempts (default: 3)
	MaxAttempts int

	// InitialDelay before first retry (default: 1s)
	InitialDelay time.Duration

	// MaxDelay between retries (default: 30s)
	MaxDelay time.Duration

	// Multiplier for exponential backoff (default: 2.0)
	Multiplier float64

	// RetryableStatuses are HTTP status codes to retry on
	RetryableStatuses []int
}

// WebhookEvent represents an inbound webhook event
type WebhookEvent struct {
	// ID is a unique identifier for this event
	ID string `json:"id"`

	// Type is the event type (from header or body)
	Type string `json:"type"`

	// Source is the origin (IP, service name)
	Source string `json:"source"`

	// Timestamp when the webhook was received
	Timestamp time.Time `json:"timestamp"`

	// Headers from the request
	Headers map[string]string `json:"headers"`

	// Body is the raw request body
	Body []byte `json:"body"`

	// Payload is the parsed JSON body (if applicable)
	Payload map[string]interface{} `json:"payload"`

	// Signature verification result
	SignatureValid bool `json:"signature_valid"`
}

// WebhookRequest represents an outbound webhook request
type WebhookRequest struct {
	// URL to send to (overrides config URL)
	URL string `json:"url,omitempty"`

	// Method: POST, PUT (overrides config)
	Method string `json:"method,omitempty"`

	// Headers to include
	Headers map[string]string `json:"headers,omitempty"`

	// Payload to send
	Payload interface{} `json:"payload"`

	// EventType for the X-Webhook-Event header
	EventType string `json:"event_type,omitempty"`

	// IdempotencyKey for deduplication
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// WebhookResponse represents the result of sending a webhook
type WebhookResponse struct {
	// Success indicates if the webhook was delivered
	Success bool `json:"success"`

	// StatusCode is the HTTP status code
	StatusCode int `json:"status_code"`

	// Body is the response body
	Body string `json:"body,omitempty"`

	// Attempts is the number of delivery attempts
	Attempts int `json:"attempts"`

	// Error message if failed
	Error string `json:"error,omitempty"`

	// Duration of the request
	Duration time.Duration `json:"duration"`
}

// DefaultRetryConfig returns sensible retry defaults
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		Multiplier:    2.0,
		RetryableStatuses: []int{
			408, // Request Timeout
			429, // Too Many Requests
			500, // Internal Server Error
			502, // Bad Gateway
			503, // Service Unavailable
			504, // Gateway Timeout
		},
	}
}

// DefaultInboundConfig returns sensible inbound defaults
func DefaultInboundConfig() *InboundConfig {
	return &InboundConfig{
		Path:               "/webhook",
		SignatureAlgorithm: "hmac-sha256",
		SignatureHeader:    "X-Webhook-Signature",
		TimestampHeader:    "X-Webhook-Timestamp",
		TimestampTolerance: 5 * time.Minute,
		RequireHTTPS:       false,
	}
}

// DefaultOutboundConfig returns sensible outbound defaults
func DefaultOutboundConfig() *OutboundConfig {
	return &OutboundConfig{
		Method:             "POST",
		SignatureAlgorithm: "hmac-sha256",
		SignatureHeader:    "X-Webhook-Signature",
		IncludeTimestamp:   true,
		Timeout:            30 * time.Second,
		Retry:              DefaultRetryConfig(),
	}
}
