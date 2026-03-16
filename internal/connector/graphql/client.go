package graphql

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matutetandil/mycel/internal/connector"
)

// ClientConnector calls external GraphQL APIs.
type ClientConnector struct {
	name       string
	config     *ClientConfig
	client     *http.Client
	logger     *slog.Logger
	mu         sync.RWMutex

	// OAuth2 token management
	accessToken string
	tokenExpiry time.Time

	// Subscription client support (implements Starter + RouteRegistrar)
	handlers map[string]HandlerFunc
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  bool
}

// NewClient creates a new GraphQL client connector.
func NewClient(name string, config *ClientConfig, logger *slog.Logger) *ClientConnector {
	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.RetryCount == 0 {
		config.RetryCount = 1
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &ClientConnector{
		name:     name,
		config:   config,
		logger:   logger,
		handlers: make(map[string]HandlerFunc),
		client: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// Name returns the connector name.
func (c *ClientConnector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *ClientConnector) Type() string {
	return "graphql"
}

// Connect validates the configuration.
func (c *ClientConnector) Connect(ctx context.Context) error {
	if c.config.Endpoint == "" {
		return fmt.Errorf("graphql client requires endpoint")
	}
	return nil
}

// Close cancels all subscription goroutines and waits for them to finish.
func (c *ClientConnector) Close(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.running = false
	c.mu.Unlock()

	c.wg.Wait()
	return nil
}

// Health checks connectivity to the GraphQL endpoint.
func (c *ClientConnector) Health(ctx context.Context) error {
	// Execute introspection query to check health
	query := `{ __typename }`
	_, err := c.execute(ctx, query, nil)
	return err
}

// Read executes a GraphQL query.
func (c *ClientConnector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	// Use Target as the GraphQL query, Filters as variables
	result, err := c.execute(ctx, query.Target, query.Filters)
	if err != nil {
		return nil, err
	}

	// Convert result to connector.Result format
	return c.toConnectorResult(result)
}

// Write executes a GraphQL mutation.
func (c *ClientConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	// Use Target as the GraphQL mutation, Payload as variables
	result, err := c.execute(ctx, data.Target, data.Payload)
	if err != nil {
		return nil, err
	}

	return c.toConnectorResult(result)
}

// Call executes a GraphQL operation (for enrichment).
func (c *ClientConnector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	return c.execute(ctx, operation, params)
}

// execute sends a GraphQL request to the endpoint.
func (c *ClientConnector) execute(ctx context.Context, query string, variables map[string]interface{}) (interface{}, error) {
	var lastErr error

	for attempt := 0; attempt < c.config.RetryCount; attempt++ {
		if attempt > 0 {
			time.Sleep(c.config.RetryDelay)
		}

		result, err := c.doRequest(ctx, query, variables)
		if err != nil {
			lastErr = err
			// Don't retry on client errors (4xx)
			if isClientError(err) {
				return nil, err
			}
			continue
		}

		return result, nil
	}

	return nil, lastErr
}

// doRequest sends a single GraphQL request.
func (c *ClientConnector) doRequest(ctx context.Context, query string, variables map[string]interface{}) (interface{}, error) {
	// Build request body
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.Endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Add authentication
	if err := c.addAuth(req); err != nil {
		return nil, fmt.Errorf("failed to add authentication: %w", err)
	}

	// Add custom headers
	for key, value := range c.config.Headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode >= 400 {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	// Parse GraphQL response
	var gqlResp GraphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for GraphQL errors
	if len(gqlResp.Errors) > 0 {
		return nil, &GraphQLErrors{Errors: gqlResp.Errors}
	}

	return gqlResp.Data, nil
}

// addAuth adds authentication headers to the request.
func (c *ClientConnector) addAuth(req *http.Request) error {
	if c.config.Auth == nil {
		return nil
	}

	switch strings.ToLower(c.config.Auth.Type) {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.config.Auth.Token)

	case "apikey":
		header := c.config.Auth.APIKeyHeader
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, c.config.Auth.APIKey)

	case "basic":
		credentials := base64.StdEncoding.EncodeToString(
			[]byte(c.config.Auth.Username + ":" + c.config.Auth.Password),
		)
		req.Header.Set("Authorization", "Basic "+credentials)

	case "oauth2":
		token, err := c.getOAuth2Token()
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return nil
}

// getOAuth2Token gets or refreshes the OAuth2 token.
func (c *ClientConnector) getOAuth2Token() (string, error) {
	c.mu.RLock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		token := c.accessToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	// Refresh token
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	// Request new token
	token, expiry, err := c.requestOAuth2Token()
	if err != nil {
		return "", err
	}

	c.accessToken = token
	c.tokenExpiry = expiry
	return token, nil
}

// requestOAuth2Token requests a new OAuth2 token.
func (c *ClientConnector) requestOAuth2Token() (string, time.Time, error) {
	data := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s",
		c.config.Auth.ClientID, c.config.Auth.ClientSecret)

	if len(c.config.Auth.Scopes) > 0 {
		data += "&scope=" + strings.Join(c.config.Auth.Scopes, " ")
	}

	req, err := http.NewRequest(http.MethodPost, c.config.Auth.TokenURL,
		strings.NewReader(data))
	if err != nil {
		return "", time.Time{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("OAuth2 token request failed: %s", string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", time.Time{}, err
	}

	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return tokenResp.AccessToken, expiry, nil
}

// toConnectorResult converts GraphQL response to connector.Result.
func (c *ClientConnector) toConnectorResult(data interface{}) (*connector.Result, error) {
	result := &connector.Result{
		Rows: make([]map[string]interface{}, 0),
	}

	switch v := data.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				result.Rows = append(result.Rows, m)
			}
		}
	case map[string]interface{}:
		// Check if it's a data wrapper with a single field
		for _, value := range v {
			switch inner := value.(type) {
			case []interface{}:
				for _, item := range inner {
					if m, ok := item.(map[string]interface{}); ok {
						result.Rows = append(result.Rows, m)
					}
				}
				return result, nil
			case map[string]interface{}:
				result.Rows = append(result.Rows, inner)
				return result, nil
			}
		}
		// Otherwise, treat the whole thing as a single row
		result.Rows = append(result.Rows, v)
	}

	return result, nil
}

// HTTPError represents an HTTP error.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// GraphQLErrors represents multiple GraphQL errors.
type GraphQLErrors struct {
	Errors []GraphQLError
}

func (e *GraphQLErrors) Error() string {
	if len(e.Errors) == 0 {
		return "unknown GraphQL error"
	}
	messages := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		messages[i] = err.Message
	}
	return strings.Join(messages, "; ")
}

// isClientError checks if the error is a client error (4xx).
func isClientError(err error) bool {
	if httpErr, ok := err.(*HTTPError); ok {
		return httpErr.StatusCode >= 400 && httpErr.StatusCode < 500
	}
	return false
}

// RegisterRoute registers a handler for a subscription operation.
// Operations should be in the form "Subscription.fieldName".
func (c *ClientConnector) RegisterRoute(operation string, handler HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.handlers[operation]; ok {
		c.handlers[operation] = HandlerFunc(connector.ChainEventDriven(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			c.logger,
		))
		c.logger.Info("fan-out: multiple flows registered", "operation", operation)
	} else {
		c.handlers[operation] = handler
	}
	c.logger.Debug("registered subscription handler", "connector", c.name, "operation", operation)
}

// Start begins consuming GraphQL subscriptions from the remote server.
func (c *ClientConnector) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("client connector %s already running", c.name)
	}
	c.running = true
	c.ctx, c.cancel = context.WithCancel(ctx)
	handlers := make(map[string]HandlerFunc, len(c.handlers))
	for k, v := range c.handlers {
		handlers[k] = v
	}
	c.mu.Unlock()

	for operation, handler := range handlers {
		fieldName := operation
		// Strip "Subscription." prefix if present
		if strings.HasPrefix(operation, "Subscription.") {
			fieldName = strings.TrimPrefix(operation, "Subscription.")
		}

		query := buildSubscriptionQuery(fieldName)

		c.wg.Add(1)
		go func(topic, q string, h HandlerFunc) {
			defer c.wg.Done()
			c.subscribeToTopic(c.ctx, topic, q, h)
		}(fieldName, query, handler)
	}

	if len(handlers) > 0 {
		c.logger.Info("graphql subscription client started",
			"connector", c.name,
			"subscriptions", len(handlers),
		)
	}

	return nil
}

// buildSubscriptionQuery generates a subscription query for a field name.
func buildSubscriptionQuery(fieldName string) string {
	return fmt.Sprintf("subscription { %s }", fieldName)
}

// subscribeToTopic connects to the remote GraphQL server via WebSocket,
// subscribes to a topic, and dispatches each message to the handler.
// Reconnects with exponential backoff on disconnection.
func (c *ClientConnector) subscribeToTopic(ctx context.Context, topic, query string, handler HandlerFunc) {
	const (
		initialBackoff = 1 * time.Second
		maxBackoff     = 60 * time.Second
	)
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.runSubscription(ctx, topic, query, handler)
		if err == nil || ctx.Err() != nil {
			return
		}

		attempt++
		backoff := time.Duration(float64(initialBackoff) * math.Pow(2, float64(attempt-1)))
		if backoff > maxBackoff {
			backoff = maxBackoff
		}

		c.logger.Warn("subscription disconnected, reconnecting",
			"connector", c.name,
			"topic", topic,
			"attempt", attempt,
			"backoff", backoff,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// runSubscription performs a single WebSocket subscription session.
// Returns nil on clean shutdown, error on unexpected disconnect.
func (c *ClientConnector) runSubscription(ctx context.Context, topic, query string, handler HandlerFunc) error {
	wsURL, err := c.buildWebSocketURL()
	if err != nil {
		return fmt.Errorf("failed to build WebSocket URL: %w", err)
	}

	// Add auth headers if configured
	reqHeader := http.Header{}
	for key, value := range c.config.Headers {
		reqHeader.Set(key, value)
	}
	if c.config.Auth != nil {
		dummyReq, _ := http.NewRequest("GET", wsURL, nil)
		if err := c.addAuth(dummyReq); err == nil {
			if auth := dummyReq.Header.Get("Authorization"); auth != "" {
				reqHeader.Set("Authorization", auth)
			}
		}
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: c.config.Timeout,
		Subprotocols:     []string{"graphql-transport-ws"},
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, reqHeader)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}
	defer conn.Close()

	// Send connection_init
	initMsg := wsMessage{Type: msgConnectionInit}
	if err := conn.WriteJSON(initMsg); err != nil {
		return fmt.Errorf("failed to send connection_init: %w", err)
	}

	// Wait for connection_ack
	if err := c.waitForAck(ctx, conn); err != nil {
		return fmt.Errorf("connection_ack failed: %w", err)
	}

	// Send subscribe
	subPayload, _ := json.Marshal(subscribePayload{Query: query})
	subMsg := wsMessage{
		ID:      "sub-" + topic,
		Type:    msgSubscribe,
		Payload: subPayload,
	}
	if err := conn.WriteJSON(subMsg); err != nil {
		return fmt.Errorf("failed to send subscribe: %w", err)
	}

	c.logger.Debug("subscription active", "connector", c.name, "topic", topic, "url", wsURL)

	// Read messages
	for {
		select {
		case <-ctx.Done():
			// Send complete to cleanly unsubscribe
			conn.WriteJSON(wsMessage{ID: "sub-" + topic, Type: msgComplete})
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		var msg wsMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read error: %w", err)
		}

		switch msg.Type {
		case msgNext:
			c.handleNextMessage(ctx, topic, msg.Payload, handler)

		case msgError:
			c.logger.Error("subscription error from server",
				"connector", c.name,
				"topic", topic,
				"payload", string(msg.Payload),
			)

		case msgComplete_:
			c.logger.Debug("subscription completed by server",
				"connector", c.name,
				"topic", topic,
			)
			return fmt.Errorf("server completed subscription")

		case msgPing:
			conn.WriteJSON(wsMessage{Type: msgPong})

		case msgConnectionAck:
			// Already acked, ignore duplicate
		}
	}
}

// waitForAck waits for a connection_ack message from the server.
func (c *ClientConnector) waitForAck(ctx context.Context, conn *websocket.Conn) error {
	timeout := 10 * time.Second
	if c.config.Subscriptions != nil && c.config.Subscriptions.ConnectionTimeout > 0 {
		timeout = c.config.Subscriptions.ConnectionTimeout
	}

	conn.SetReadDeadline(time.Now().Add(timeout))

	var msg wsMessage
	if err := conn.ReadJSON(&msg); err != nil {
		return fmt.Errorf("failed to read ack: %w", err)
	}

	if msg.Type != msgConnectionAck {
		return fmt.Errorf("expected connection_ack, got %s", msg.Type)
	}

	return nil
}

// handleNextMessage deserializes a subscription next payload and dispatches to the handler.
func (c *ClientConnector) handleNextMessage(ctx context.Context, topic string, payload json.RawMessage, handler HandlerFunc) {
	var result struct {
		Data   map[string]interface{} `json:"data"`
		Errors []GraphQLError         `json:"errors,omitempty"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		c.logger.Error("failed to unmarshal subscription payload",
			"connector", c.name,
			"topic", topic,
			"error", err,
		)
		return
	}

	if len(result.Errors) > 0 {
		c.logger.Error("subscription payload contains errors",
			"connector", c.name,
			"topic", topic,
			"errors", result.Errors[0].Message,
		)
		return
	}

	// Extract the subscription field data (e.g., data.orderUpdated)
	input := make(map[string]interface{})
	if result.Data != nil {
		if fieldData, ok := result.Data[topic]; ok {
			if m, ok := fieldData.(map[string]interface{}); ok {
				input = m
			} else {
				input["data"] = fieldData
			}
		} else {
			// Use the entire data object
			input = result.Data
		}
	}

	if _, err := handler(ctx, input); err != nil {
		c.logger.Error("subscription handler error",
			"connector", c.name,
			"topic", topic,
			"error", err,
		)
	}
}

// buildWebSocketURL converts the HTTP endpoint to a WebSocket URL.
func (c *ClientConnector) buildWebSocketURL() (string, error) {
	parsed, err := url.Parse(c.config.Endpoint)
	if err != nil {
		return "", err
	}

	// Convert scheme
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
		// Already a WebSocket URL
	default:
		parsed.Scheme = "ws"
	}

	// Use custom subscriptions path if configured
	if c.config.Subscriptions != nil && c.config.Subscriptions.Path != "" {
		parsed.Path = c.config.Subscriptions.Path
	}

	return parsed.String(), nil
}

// HasSubscriptions returns true if subscription handlers are registered.
func (c *ClientConnector) HasSubscriptions() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.handlers) > 0
}

// Ensure ClientConnector implements the required interfaces.
var (
	_ connector.Connector = (*ClientConnector)(nil)
	_ connector.Reader    = (*ClientConnector)(nil)
	_ connector.Writer    = (*ClientConnector)(nil)
)
