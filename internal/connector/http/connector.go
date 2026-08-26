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

	"github.com/matutetandil/mycel/v3/internal/codec"
	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/tracing"
)

// Connector is an HTTP client for calling external REST APIs.
type Connector struct {
	name       string
	baseURL    string
	timeout    time.Duration
	client     *http.Client
	auth       *AuthConfig
	tlsConfig  *TLSConfig
	tlsErr     error
	headers    map[string]string
	retryCount int
	retry      RetryPolicy
	format     string      // default format ("json", "xml")
	codec      codec.Codec // codec for encoding/decoding

	// Token management for OAuth2
	mu          sync.RWMutex
	accessToken string
	tokenExpiry time.Time
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

// RetryPolicy describes how a failed request is retried.
//
// The waiting was hardcoded at attempt*100ms, which meant a caller could ask
// for three attempts but never for a gap longer than a fifth of a second — a
// dependency that has just fallen over gets hammered rather than given time.
// The names match error_handling.retry so the language has one retry
// vocabulary rather than two that look alike.
type RetryPolicy struct {
	// Attempts is the total number of tries, including the first.
	Attempts int
	// Delay is how long to wait before the second try.
	Delay time.Duration
	// MaxDelay caps the wait however far the backoff grows.
	MaxDelay time.Duration
	// Backoff is "constant", "linear" or "exponential".
	Backoff string
}

// DefaultRetryPolicy returns the policy used when only a count is given. The
// delay keeps the previous first wait, so a connector that said nothing about
// timing behaves as it did; the strategy is the exponential the old comment
// claimed and the linear implementation did not deliver.
func DefaultRetryPolicy(attempts int) RetryPolicy {
	if attempts < 1 {
		attempts = 1
	}
	return RetryPolicy{
		Attempts: attempts,
		Delay:    100 * time.Millisecond,
		MaxDelay: 30 * time.Second,
		Backoff:  "exponential",
	}
}

// nextDelay grows a wait according to the strategy, never past the cap.
func (p RetryPolicy) nextDelay(current time.Duration) time.Duration {
	var next time.Duration
	switch p.Backoff {
	case "exponential":
		next = current * 2
	case "linear":
		next = current + p.Delay
	default: // "constant"
		next = current
	}
	if p.MaxDelay > 0 && next > p.MaxDelay {
		return p.MaxDelay
	}
	return next
}

// WithRetryPolicy replaces the connector's retry policy and returns it, so a
// factory can build and configure in one expression.
func (c *Connector) WithRetryPolicy(policy RetryPolicy) *Connector {
	if policy.Attempts < 1 {
		policy.Attempts = 1
	}
	if policy.Delay <= 0 {
		policy.Delay = 100 * time.Millisecond
	}
	if policy.Backoff == "" {
		policy.Backoff = "exponential"
	}
	c.retry = policy
	c.retryCount = policy.Attempts
	return c
}

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

	// Build HTTP client with optional TLS.
	//
	// A failure here used to be discarded, and the connector carried on with
	// the default transport — so a mistyped ca_cert path meant verifying
	// against the system roots instead of the CA that was named, and a client
	// certificate that would not load meant connecting without one. Both look
	// like working TLS from the outside. The error is kept and returned from
	// Connect, which runs at startup.
	transport := &http.Transport{}
	var tlsErr error
	if tlsCfg != nil {
		tlsConf, err := buildTLSConfig(tlsCfg)
		switch {
		case err != nil:
			tlsErr = err
		case tlsConf != nil:
			transport.TLSClientConfig = tlsConf
		}
	}

	return &Connector{
		name:       name,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		timeout:    timeout,
		auth:       auth,
		tlsConfig:  tlsCfg,
		tlsErr:     tlsErr,
		headers:    headers,
		retryCount: retryCount,
		retry:      DefaultRetryPolicy(retryCount),
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

	// TLS that could not be built is TLS that is not in force.
	if c.tlsErr != nil {
		return fmt.Errorf("tls configuration for connector %q: %w", c.name, c.tlsErr)
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
	method, path := resolveMethodPath(data.Target, data.Operation)

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

	// Encode the request body ONCE. The retry loop below produces a fresh
	// bytes.Reader from this slice on every attempt — without this the
	// first attempt consumed the reader, and subsequent retries went out
	// with an empty body (server saw 500 once + then 400 "field required"
	// from the retry, mistaking a transient failure for permanent).
	var encoded []byte
	if data.Payload != nil && (method == "POST" || method == "PUT" || method == "PATCH" || method == "QUERY") {
		var err error
		encoded, err = c.codec.Encode(data.Payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode payload: %w", err)
		}

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
	delay := c.retry.Delay
	for attempt := 0; attempt < c.retryCount; attempt++ {
		// Fresh reader per attempt — bytes.Reader is one-shot.
		var body io.Reader
		if encoded != nil {
			body = bytes.NewReader(encoded)
		}

		result, err := c.doRequest(ctx, method, fullURL, body)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// Don't retry on client errors (4xx)
		if isClientError(err) {
			return nil, err
		}

		// Wait before retrying, growing the gap per the configured strategy.
		if attempt < c.retryCount-1 {
			time.Sleep(delay)
			delay = c.retry.nextDelay(delay)
		}
	}

	return nil, lastErr
}

// Read fetches data from an external API (implements connector.Reader).
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	method, path := resolveMethodPath(query.Target, query.Operation)

	// Build full URL
	fullURL := c.baseURL + path

	// QUERY (RFC 10008) carries its criteria in the request body — that is
	// the method's whole point — so filters travel as an encoded body
	// instead of query string parameters.
	if method == "QUERY" && len(query.Filters) > 0 {
		encoded, err := c.codec.Encode(query.Filters)
		if err != nil {
			return nil, fmt.Errorf("failed to encode query filters: %w", err)
		}
		return c.doRequest(ctx, method, fullURL, bytes.NewReader(encoded))
	}

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

// Call invokes an HTTP operation described as "METHOD /path" (or a bare
// path, defaulting to GET). It implements the Caller interface that saga
// actions, state machine actions, and flow steps dispatch through.
//
// Params placement follows the verb's semantics: body-carrying methods
// (POST/PUT/PATCH/QUERY) get them encoded as the request body; the rest
// (GET/DELETE/HEAD/OPTIONS) get them as query string parameters.
//
// The decoded response is returned directly — a single object for one-row
// responses, a list otherwise — so step/saga results are usable in CEL as
// step.<name>.<field>.
func (c *Connector) Call(ctx context.Context, operation string, params map[string]interface{}) (interface{}, error) {
	method, path := parseTarget(operation)

	// Path parameters, taken out of the params before the rest are sent.
	//
	// `GET /customers/:id` is how a REST API is addressed, and there was no
	// way to say it: the path was concatenated as written and every parameter
	// went to the query string or the body, so a step could not fetch
	// /customers/42 at all. The documentation had invented
	// `"GET /customers/${step.order.customer_id}"` for it, in eight places —
	// HCL interpolation of a CEL variable, which does not exist when the
	// configuration is read, so the attribute could not be evaluated and the
	// step ended up with no operation whatsoever.
	path, params = fillPathParams(path, params)
	fullURL := c.baseURL + path

	var body io.Reader
	if len(params) > 0 {
		switch method {
		case "POST", "PUT", "PATCH", "QUERY":
			encoded, err := c.codec.Encode(params)
			if err != nil {
				return nil, fmt.Errorf("failed to encode call params: %w", err)
			}
			body = bytes.NewReader(encoded)
		default:
			q := url.Values{}
			for k, v := range params {
				q.Add(k, fmt.Sprintf("%v", v))
			}
			if strings.Contains(fullURL, "?") {
				fullURL += "&" + q.Encode()
			} else {
				fullURL += "?" + q.Encode()
			}
		}
	}

	result, err := c.doRequest(ctx, method, fullURL, body)
	if err != nil {
		return nil, err
	}
	if len(result.Rows) == 1 {
		return result.Rows[0], nil
	}
	return result.Rows, nil
}

// fillPathParams substitutes `:name` and `{name}` segments from params and
// returns what is left for the query string or the body.
//
// A parameter that names a path segment is spent there: sending it again as a
// query parameter would be a second, contradictory statement of the same
// thing.
func fillPathParams(path string, params map[string]interface{}) (string, map[string]interface{}) {
	if len(params) == 0 || !strings.ContainsAny(path, ":{") {
		return path, params
	}

	remaining := make(map[string]interface{}, len(params))
	for name, value := range params {
		remaining[name] = value
	}

	segments := strings.Split(path, "/")
	for i, segment := range segments {
		name := ""
		switch {
		case strings.HasPrefix(segment, ":"):
			name = segment[1:]
		case strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}"):
			name = segment[1 : len(segment)-1]
		default:
			continue
		}

		value, declared := remaining[name]
		if !declared {
			continue
		}
		segments[i] = url.PathEscape(fmt.Sprintf("%v", value))
		delete(remaining, name)
	}

	return strings.Join(segments, "/"), remaining
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

	// Propagate the active distributed trace to the downstream service (no-op
	// when tracing is disabled). The ctx carries the connector span started by
	// the runtime, so the remote span links into this trace.
	tracing.InjectHTTP(ctx, req.Header)

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
	} else {
		// A provider that states no lifetime is not saying the token lasts for
		// ever. Leaving the expiry unset means it is never refreshed, so the
		// service works until the token quietly expires and then fails every
		// request until it is restarted. An hour, as the client credentials
		// grant already assumes.
		c.tokenExpiry = time.Now().Add(time.Hour)
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

// isHTTPMethod reports whether s is a verb this connector sends as-is.
func isHTTPMethod(s string) bool {
	switch s {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "QUERY", "HEAD", "OPTIONS":
		return true
	}
	return false
}

// resolveMethodPath normalizes the two ways callers describe an HTTP request.
//
// The runtime and flow config express requests either split (operation holds
// the verb, target holds the path — e.g. operation="POST", target="/orders")
// or combined (a single "METHOD /path" string, in either field — the form the
// connector schema documents). On top of that, the runtime's generic dispatch
// passes database-flavored operations (SELECT/INSERT/UPDATE) that are
// meaningless on the wire; historically they clobbered the verb parsed from
// target ("INSERT /orders" went out as method INSERT with no body).
//
// Resolution rules:
//   - operation containing a space is "METHOD /path" and wins entirely
//   - operation naming a bare HTTP verb overrides the method, path from target
//   - a DB operation maps to its HTTP equivalent (SELECT→GET, INSERT→POST,
//     UPDATE→PUT) only when target didn't carry an explicit verb
//   - anything else is ignored: target's parse stands
func resolveMethodPath(target, operation string) (method, path string) {
	method, path = parseTarget(target)
	targetHasMethod := strings.Contains(strings.TrimSpace(target), " ")

	trimmed := strings.TrimSpace(operation)
	op := strings.ToUpper(trimmed)
	if op == "" {
		return method, path
	}

	if strings.Contains(trimmed, " ") {
		// parseTarget uppercases the verb; the path keeps its casing
		return parseTarget(trimmed)
	}

	if isHTTPMethod(op) {
		return op, path
	}

	if !targetHasMethod {
		switch op {
		case "SELECT":
			return "GET", path
		case "INSERT", "CREATE":
			return "POST", path
		case "UPDATE":
			return "PUT", path
		}
	}

	return method, path
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

// IsPermanent satisfies connector.PermanentError. A 4xx response means
// the destination already evaluated the request and rejected it as-is —
// retrying the same bytes will produce the same status. 5xx is allowed
// to remain retryable (transient backend issues, rolling deploys, etc.).
func (e *HTTPError) IsPermanent() bool {
	return e != nil && e.StatusCode >= 400 && e.StatusCode < 500
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
