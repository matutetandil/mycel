// Package http provides an HTTP client connector for calling external APIs.
package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/codec"
	"github.com/matutetandil/mycel/internal/connector"
)

// Connector is an HTTP client for calling external REST APIs.
type Connector struct {
	name       string
	baseURL    string
	timeout    time.Duration
	client     *http.Client
	auth       *AuthConfig
	tlsConfig  *TLSConfig
	headers    map[string]string
	retryCount int
	format     string      // default format ("json", "xml")
	codec      codec.Codec // codec for encoding/decoding

	// Token management for OAuth2
	mu           sync.RWMutex
	accessToken  string
	tokenExpiry  time.Time
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Type AuthType

	// Bearer token auth
	Token string

	// OAuth2 with refresh token
	RefreshToken string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string

	// OAuth2 grant type: "refresh_token" (default) or "client_credentials"
	GrantType string

	// API Key auth
	APIKey       string
	APIKeyHeader string // Default: "X-API-Key"
	APIKeyQuery  string // If set, sends as query param instead of header

	// Basic auth
	Username string
	Password string
}

// TLSConfig holds TLS configuration for client certificates.
type TLSConfig struct {
	// CA certificate for verifying server
	CACert string

	// Client certificate and key for mTLS
	ClientCert string
	ClientKey  string

	// Skip server verification (insecure, only for development)
	InsecureSkipVerify bool
}

// AuthType represents the type of authentication.
type AuthType string

const (
	AuthTypeNone              AuthType = "none"
	AuthTypeBearer            AuthType = "bearer"
	AuthTypeOAuth2            AuthType = "oauth2"
	AuthTypeClientCredentials AuthType = "client_credentials"
	AuthTypeAPIKey            AuthType = "apikey"
	AuthTypeBasic             AuthType = "basic"
)

// New creates a new HTTP client connector.
func New(name, baseURL string, timeout time.Duration, auth *AuthConfig, headers map[string]string, retryCount int) *Connector {
	return NewWithTLS(name, baseURL, timeout, auth, nil, headers, retryCount)
}

// NewWithTLS creates a new HTTP client connector with TLS configuration.
func NewWithTLS(name, baseURL string, timeout time.Duration, auth *AuthConfig, tlsCfg *TLSConfig, headers map[string]string, retryCount int) *Connector {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if retryCount == 0 {
		retryCount = 1
	}
	if headers == nil {
		headers = make(map[string]string)
	}

	// Build HTTP client with optional TLS
	transport := &http.Transport{}
	if tlsCfg != nil {
		tlsConf, err := buildTLSConfig(tlsCfg)
		if err == nil && tlsConf != nil {
			transport.TLSClientConfig = tlsConf
		}
	}

	return &Connector{
		name:       name,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		timeout:    timeout,
		auth:       auth,
		tlsConfig:  tlsCfg,
		headers:    headers,
		retryCount: retryCount,
		format:     "json",
		codec:      codec.Get("json"),
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// buildTLSConfig builds a TLS configuration from TLSConfig.
func buildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if cfg == nil {
		return nil, nil
	}

	tlsConf := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	// Load CA certificate
	if cfg.CACert != "" {
		caCert, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}
		tlsConf.RootCAs = caCertPool
	}

	// Load client certificate
	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert: %w", err)
		}
		tlsConf.Certificates = []tls.Certificate{cert}
	}

	return tlsConf, nil
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "http"
}

// SetFormat sets the default format and codec for this connector.
func (c *Connector) SetFormat(format string) {
	c.format = format
	c.codec = codec.Get(format)
}

// Connect initializes the connector (validates config, gets initial token if OAuth2).
func (c *Connector) Connect(ctx context.Context) error {
	// Validate base URL
	if _, err := url.Parse(c.baseURL); err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}

	// Loud, single-shot warning when TLS verification is disabled. Connect()
	// runs once per connector at startup, so this fires exactly once and is
	// hard to miss in logs — making accidental production use obvious.
	if c.tlsConfig != nil && c.tlsConfig.InsecureSkipVerify {
		slog.Warn("TLS verification disabled for HTTP connector — never use in production",
			"connector", c.name,
			"base_url", c.baseURL)
	}

	// If OAuth2 with refresh token, get initial access token
	if c.auth != nil && c.auth.Type == AuthTypeOAuth2 {
		if err := c.refreshAccessToken(ctx); err != nil {
			return fmt.Errorf("failed to get initial access token: %w", err)
		}
	}

	// If Client Credentials, get initial access token
	if c.auth != nil && c.auth.Type == AuthTypeClientCredentials {
		if err := c.getClientCredentialsToken(ctx); err != nil {
			return fmt.Errorf("failed to get client credentials token: %w", err)
		}
	}

	// If bearer token provided directly, store it
	if c.auth != nil && c.auth.Type == AuthTypeBearer && c.auth.Token != "" {
		c.accessToken = c.auth.Token
	}

	return nil
}

// Close closes the connector.
func (c *Connector) Close(ctx context.Context) error {
	c.client.CloseIdleConnections()
	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	// Try a simple HEAD request to base URL
	req, err := http.NewRequestWithContext(ctx, "HEAD", c.baseURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Write sends data to an external API (implements connector.Writer).
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	// Parse target as "METHOD /path"
	method, path := parseTarget(data.Target)
	if data.Operation != "" {
		// Operation overrides if specified
		method = data.Operation
	}

	// Build full URL
	fullURL := c.baseURL + path

	// Add query params from filters
	if len(data.Filters) > 0 {
		params := url.Values{}
		for k, v := range data.Filters {
			params.Add(k, fmt.Sprintf("%v", v))
		}
		if strings.Contains(fullURL, "?") {
			fullURL += "&" + params.Encode()
		} else {
			fullURL += "?" + params.Encode()
		}
	}

	// Build request body
	var body io.Reader
	if data.Payload != nil && (method == "POST" || method == "PUT" || method == "PATCH") {
		encoded, err := c.codec.Encode(data.Payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode payload: %w", err)
		}
		body = bytes.NewReader(encoded)

		// DEBUG: surface the outbound body shape so users can verify wrap /
		// envelope behavior without intercepting traffic. Only top-level keys
		// and size are logged — values stay out of the log to avoid leaking
		// sensitive content. Costs nothing when the level is above DEBUG.
		if slog.Default().Enabled(ctx, slog.LevelDebug) {
			slog.DebugContext(ctx, "outbound HTTP body",
				"connector", c.name,
				"method", method,
				"path", path,
				"size_bytes", len(encoded),
				"top_level_keys", topLevelKeys(data.Payload))
		}
	}

	// Execute with retry
	var lastErr error
	for attempt := 0; attempt < c.retryCount; attempt++ {
		result, err := c.doRequest(ctx, method, fullURL, body)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// Don't retry on client errors (4xx)
		if isClientError(err) {
			return nil, err
		}

		// Wait before retry (simple exponential backoff)
		if attempt < c.retryCount-1 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}

	return nil, lastErr
}

// Read fetches data from an external API (implements connector.Reader).
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	// Parse target as "METHOD /path" or just "/path" (defaults to GET)
	method, path := parseTarget(query.Target)
	if query.Operation != "" {
		method = query.Operation
	}
	if method == "" {
		method = "GET"
	}

	// Build full URL
	fullURL := c.baseURL + path

	// Add query params from filters
	if len(query.Filters) > 0 {
		params := url.Values{}
		for k, v := range query.Filters {
			params.Add(k, fmt.Sprintf("%v", v))
		}
		if strings.Contains(fullURL, "?") {
			fullURL += "&" + params.Encode()
		} else {
			fullURL += "?" + params.Encode()
		}
	}

	// Execute request
	return c.doRequest(ctx, method, fullURL, nil)
}

// doRequest executes an HTTP request with authentication.
func (c *Connector) doRequest(ctx context.Context, method, fullURL string, body io.Reader) (*connector.Result, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers using connector's codec
	req.Header.Set("Content-Type", c.codec.ContentType())
	req.Header.Set("Accept", c.codec.ContentType())

	// Set custom headers
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Apply authentication
	if err := c.applyAuth(ctx, req); err != nil {
		return nil, fmt.Errorf("failed to apply auth: %w", err)
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

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(respBody),
		}
	}

	// Parse response — auto-detect format from Content-Type, fall back to connector codec
	var data interface{}
	if len(respBody) > 0 {
		respCodec := codec.DetectFromContentType(resp.Header.Get("Content-Type"))
		if decoded, err := respCodec.Decode(respBody); err == nil {
			data = decoded
		} else {
			// If decoding fails, return as string
			data = string(respBody)
		}
	}

	// Build result
	result := &connector.Result{
		Affected: 1,
		Rows:     make([]map[string]interface{}, 0),
	}

	// Handle different response types
	switch v := data.(type) {
	case []interface{}:
		// Convert array of interfaces to array of maps
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				result.Rows = append(result.Rows, m)
			} else {
				// Wrap non-map items
				result.Rows = append(result.Rows, map[string]interface{}{"data": item})
			}
		}
	case map[string]interface{}:
		result.Rows = []map[string]interface{}{v}
	default:
		// Wrap other types in a map
		result.Rows = []map[string]interface{}{{"data": data}}
	}

	return result, nil
}

// applyAuth applies authentication to the request.
func (c *Connector) applyAuth(ctx context.Context, req *http.Request) error {
	if c.auth == nil || c.auth.Type == AuthTypeNone {
		return nil
	}

	switch c.auth.Type {
	case AuthTypeBearer:
		token := c.getAccessToken()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

	case AuthTypeOAuth2:
		// Check if token needs refresh
		if c.isTokenExpired() {
			if err := c.refreshAccessToken(ctx); err != nil {
				return err
			}
		}
		token := c.getAccessToken()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

	case AuthTypeClientCredentials:
		// Check if token needs refresh
		if c.isTokenExpired() {
			if err := c.getClientCredentialsToken(ctx); err != nil {
				return err
			}
		}
		token := c.getAccessToken()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

	case AuthTypeAPIKey:
		if c.auth.APIKeyQuery != "" {
			// Add as query parameter
			q := req.URL.Query()
			q.Add(c.auth.APIKeyQuery, c.auth.APIKey)
			req.URL.RawQuery = q.Encode()
		} else {
			// Add as header
			header := c.auth.APIKeyHeader
			if header == "" {
				header = "X-API-Key"
			}
			req.Header.Set(header, c.auth.APIKey)
		}

	case AuthTypeBasic:
		req.SetBasicAuth(c.auth.Username, c.auth.Password)
	}

	return nil
}

// refreshAccessToken gets a new access token using the refresh token.
func (c *Connector) refreshAccessToken(ctx context.Context) error {
	if c.auth == nil || c.auth.TokenURL == "" {
		return fmt.Errorf("token URL not configured")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", c.auth.RefreshToken)
	if c.auth.ClientID != "" {
		data.Set("client_id", c.auth.ClientID)
	}
	if c.auth.ClientSecret != "" {
		data.Set("client_secret", c.auth.ClientSecret)
	}
	if len(c.auth.Scopes) > 0 {
		data.Set("scope", strings.Join(c.auth.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.auth.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		// Set expiry with a small buffer
		c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	}

	// Update refresh token if a new one was provided
	if tokenResp.RefreshToken != "" {
		c.auth.RefreshToken = tokenResp.RefreshToken
	}

	return nil
}

// getClientCredentialsToken gets an access token using client credentials grant.
func (c *Connector) getClientCredentialsToken(ctx context.Context) error {
	if c.auth == nil || c.auth.TokenURL == "" {
		return fmt.Errorf("token URL not configured")
	}
	if c.auth.ClientID == "" || c.auth.ClientSecret == "" {
		return fmt.Errorf("client_id and client_secret required for client_credentials grant")
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.auth.ClientID)
	data.Set("client_secret", c.auth.ClientSecret)
	if len(c.auth.Scopes) > 0 {
		data.Set("scope", strings.Join(c.auth.Scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.auth.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Some OAuth2 servers prefer Basic auth for client credentials
	req.SetBasicAuth(c.auth.ClientID, c.auth.ClientSecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("client credentials token request failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.accessToken = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 0 {
		// Set expiry with a small buffer (60 seconds before actual expiry)
		c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	} else {
		// Default to 1 hour if no expiry provided
		c.tokenExpiry = time.Now().Add(time.Hour)
	}

	return nil
}

// getAccessToken returns the current access token.
func (c *Connector) getAccessToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessToken
}

// isTokenExpired checks if the access token is expired.
func (c *Connector) isTokenExpired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.tokenExpiry.IsZero() {
		return false
	}
	return time.Now().After(c.tokenExpiry)
}

// parseTarget parses "METHOD /path" or just "/path".
func parseTarget(target string) (method, path string) {
	parts := strings.SplitN(target, " ", 2)
	if len(parts) == 2 {
		return strings.ToUpper(parts[0]), parts[1]
	}
	// Assume GET if no method specified
	if strings.HasPrefix(target, "/") {
		return "GET", target
	}
	return "GET", "/" + target
}

// HTTPError represents an HTTP error response.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s - %s", e.StatusCode, e.Status, e.Body)
}

// isClientError checks if the error is a client error (4xx).
func isClientError(err error) bool {
	if httpErr, ok := err.(*HTTPError); ok {
		return httpErr.StatusCode >= 400 && httpErr.StatusCode < 500
	}
	return false
}

// topLevelKeys returns the keys of payload when it is a map, else nil. Used
// in DEBUG logging to describe the outbound body shape without dumping
// values (which may be sensitive). Sorted for stable log output.
func topLevelKeys(payload map[string]interface{}) []string {
	if len(payload) == 0 {
		return nil
	}
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
