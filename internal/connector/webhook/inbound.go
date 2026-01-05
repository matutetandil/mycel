package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/matutetandil/mycel/internal/connector"
)

// WebhookHandler is called when a webhook is received
type WebhookHandler func(ctx context.Context, event *WebhookEvent) error

// InboundConnector receives webhooks from external services
type InboundConnector struct {
	name     string
	config   *InboundConfig
	handler  WebhookHandler
	verifier *SignatureVerifier

	// Event channel for async processing
	events chan *WebhookEvent

	mu sync.RWMutex
}

// NewInboundConnector creates a new inbound webhook connector
func NewInboundConnector(name string, config *InboundConfig) *InboundConnector {
	if config.TimestampTolerance == 0 {
		config.TimestampTolerance = 5 * time.Minute
	}
	if config.SignatureHeader == "" {
		config.SignatureHeader = "X-Webhook-Signature"
	}
	if config.Path == "" {
		config.Path = "/webhook"
	}

	var verifier *SignatureVerifier
	if config.Secret != "" {
		verifier = NewSignatureVerifier(config.Secret, config.SignatureAlgorithm)
	}

	return &InboundConnector{
		name:     name,
		config:   config,
		verifier: verifier,
		events:   make(chan *WebhookEvent, 100),
	}
}

// Name returns the connector name
func (c *InboundConnector) Name() string {
	return c.name
}

// Type returns the connector type
func (c *InboundConnector) Type() string {
	return "webhook"
}

// Connect establishes the connection (no-op for webhook)
func (c *InboundConnector) Connect(ctx context.Context) error {
	return nil
}

// SetHandler sets the webhook handler
func (c *InboundConnector) SetHandler(handler WebhookHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

// Path returns the webhook path
func (c *InboundConnector) Path() string {
	return c.config.Path
}

// Events returns the event channel for async processing
func (c *InboundConnector) Events() <-chan *WebhookEvent {
	return c.events
}

// HandleHTTP handles incoming webhook HTTP requests
func (c *InboundConnector) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check method
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check allowed IPs
	if len(c.config.AllowedIPs) > 0 {
		clientIP := c.getClientIP(r)
		if !c.isIPAllowed(clientIP) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Create event
	event := &WebhookEvent{
		ID:        uuid.New().String(),
		Source:    c.getClientIP(r),
		Timestamp: time.Now(),
		Headers:   c.extractHeaders(r),
		Body:      body,
	}

	// Extract event type from headers
	event.Type = c.extractEventType(r)

	// Verify signature
	if c.verifier != nil && c.config.Secret != "" {
		signature := r.Header.Get(c.config.SignatureHeader)
		timestamp := r.Header.Get(c.config.TimestampHeader)

		err := c.verifier.VerifyWithTimestamp(body, signature, timestamp, c.config.TimestampTolerance)
		if err != nil {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
		event.SignatureValid = true
	} else {
		event.SignatureValid = true // No verification configured
	}

	// Parse JSON payload
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err == nil {
			event.Payload = payload

			// Try to extract event type from payload
			if event.Type == "" {
				event.Type = c.extractEventTypeFromPayload(payload)
			}
		}
	}

	// Call handler if set
	c.mu.RLock()
	handler := c.handler
	c.mu.RUnlock()

	if handler != nil {
		if err := handler(ctx, event); err != nil {
			http.Error(w, "Handler error", http.StatusInternalServerError)
			return
		}
	}

	// Send to event channel (non-blocking)
	select {
	case c.events <- event:
	default:
		// Channel full, event dropped
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"received": true,
		"id":       event.ID,
	})
}

// Read waits for the next webhook event
func (c *InboundConnector) Read(ctx context.Context, source string) (interface{}, error) {
	select {
	case event := <-c.events:
		return event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *InboundConnector) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func (c *InboundConnector) isIPAllowed(ip string) bool {
	for _, allowed := range c.config.AllowedIPs {
		if allowed == ip {
			return true
		}
		// Check CIDR
		if strings.Contains(allowed, "/") {
			_, network, err := net.ParseCIDR(allowed)
			if err == nil && network.Contains(net.ParseIP(ip)) {
				return true
			}
		}
	}
	return false
}

func (c *InboundConnector) extractHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string)
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	return headers
}

func (c *InboundConnector) extractEventType(r *http.Request) string {
	// Common event type headers
	headerNames := []string{
		"X-Webhook-Event",
		"X-Event-Type",
		"X-GitHub-Event",
		"X-Gitlab-Event",
		"X-Stripe-Event",
		"X-Event-Key", // Bitbucket
	}

	for _, name := range headerNames {
		if value := r.Header.Get(name); value != "" {
			return value
		}
	}

	return ""
}

func (c *InboundConnector) extractEventTypeFromPayload(payload map[string]interface{}) string {
	// Common event type fields
	fieldNames := []string{"type", "event", "event_type", "action", "eventType"}

	for _, name := range fieldNames {
		if value, ok := payload[name].(string); ok {
			return value
		}
	}

	return ""
}

// Health always returns healthy for inbound connectors
func (c *InboundConnector) Health(ctx context.Context) error {
	return nil
}

// Close closes the connector
func (c *InboundConnector) Close(ctx context.Context) error {
	close(c.events)
	return nil
}

// Ensure InboundConnector implements connector interface
var _ connector.Connector = (*InboundConnector)(nil)

// WebhookServer manages multiple inbound webhook connectors
type WebhookServer struct {
	connectors map[string]*InboundConnector
	mux        *http.ServeMux
	server     *http.Server
	mu         sync.RWMutex
}

// NewWebhookServer creates a new webhook server
func NewWebhookServer() *WebhookServer {
	return &WebhookServer{
		connectors: make(map[string]*InboundConnector),
		mux:        http.NewServeMux(),
	}
}

// Register registers an inbound connector
func (s *WebhookServer) Register(connector *InboundConnector) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connectors[connector.name] = connector
	s.mux.HandleFunc(connector.Path(), connector.HandleHTTP)
}

// Handler returns the HTTP handler
func (s *WebhookServer) Handler() http.Handler {
	return s.mux
}

// Start starts the webhook server
func (s *WebhookServer) Start(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *WebhookServer) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// Get returns a connector by name
func (s *WebhookServer) Get(name string) (*InboundConnector, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.connectors[name]
	return c, ok
}
