package graphql

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// ClientConnector calls external GraphQL APIs.
type ClientConnector struct {
	name       string
	config     *ClientConfig
	client     *http.Client
	mu         sync.RWMutex

	// OAuth2 token management
	accessToken string
	tokenExpiry time.Time
}

// NewClient creates a new GraphQL client connector.
func NewClient(name string, config *ClientConfig) *ClientConnector {
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

	return &ClientConnector{
		name:   name,
		config: config,
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

// Close is a no-op for the client.
func (c *ClientConnector) Close(ctx context.Context) error {
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

// Ensure ClientConnector implements the required interfaces.
var (
	_ connector.Connector = (*ClientConnector)(nil)
	_ connector.Reader    = (*ClientConnector)(nil)
	_ connector.Writer    = (*ClientConnector)(nil)
)
