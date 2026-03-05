package soap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Client is a SOAP client that calls external SOAP services.
// It implements both connector.Reader and connector.Writer — both
// delegate to callOperation since SOAP is always a POST.
type Client struct {
	name        string
	endpoint    string
	soapVersion string // "1.1" or "1.2"
	namespace   string
	timeout     time.Duration
	auth        *AuthConfig
	headers     map[string]string
	client      *http.Client
}

// AuthConfig holds authentication settings for the SOAP client.
type AuthConfig struct {
	Type     string // "basic", "bearer"
	Username string
	Password string
	Token    string
}

// NewClient creates a new SOAP client connector.
func NewClient(name, endpoint, soapVersion, namespace string, timeout time.Duration, auth *AuthConfig, headers map[string]string) *Client {
	if soapVersion == "" {
		soapVersion = "1.1"
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if headers == nil {
		headers = make(map[string]string)
	}

	return &Client{
		name:        name,
		endpoint:    endpoint,
		soapVersion: soapVersion,
		namespace:   namespace,
		timeout:     timeout,
		auth:        auth,
		headers:     headers,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) Name() string { return c.name }
func (c *Client) Type() string { return "soap" }

func (c *Client) Connect(ctx context.Context) error {
	if c.endpoint == "" {
		return fmt.Errorf("soap client requires endpoint")
	}
	return nil
}

func (c *Client) Close(ctx context.Context) error {
	c.client.CloseIdleConnections()
	return nil
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "HEAD", c.endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Read calls a SOAP operation (implements connector.Reader).
// query.Target or query.Operation is the SOAP operation name.
func (c *Client) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	operation := query.Operation
	if operation == "" {
		operation = query.Target
	}
	return c.callOperation(ctx, operation, query.Filters)
}

// Write calls a SOAP operation (implements connector.Writer).
// data.Target or data.Operation is the SOAP operation name.
func (c *Client) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	operation := data.Operation
	if operation == "" {
		operation = data.Target
	}
	params := data.Payload
	if params == nil {
		params = data.Filters
	}
	return c.callOperation(ctx, operation, params)
}

// callOperation executes a SOAP RPC call.
func (c *Client) callOperation(ctx context.Context, operation string, params map[string]interface{}) (*connector.Result, error) {
	if operation == "" {
		return nil, fmt.Errorf("SOAP operation name is required")
	}

	// Build SOAP envelope
	body := params
	if body == nil {
		body = make(map[string]interface{})
	}
	envelope, err := Envelope(c.soapVersion, c.namespace, operation, body)
	if err != nil {
		return nil, fmt.Errorf("failed to build SOAP envelope: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(envelope))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set Content-Type and SOAPAction based on version
	contentType := ContentTypeForVersion(c.soapVersion)
	if c.soapVersion == "1.2" {
		// SOAP 1.2: action goes in Content-Type parameter
		action := c.namespace + "/" + operation
		contentType = fmt.Sprintf(`application/soap+xml; charset=utf-8; action="%s"`, action)
	} else {
		// SOAP 1.1: SOAPAction header
		action := c.namespace + "/" + operation
		req.Header.Set("SOAPAction", `"`+action+`"`)
	}
	req.Header.Set("Content-Type", contentType)

	// Apply custom headers
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Apply auth
	c.applyAuth(req)

	// Execute
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SOAP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read SOAP response: %w", err)
	}

	// Check HTTP-level errors
	if resp.StatusCode >= 400 {
		// Try to extract SOAP fault from error response
		if _, _, fault, fErr := Unwrap(respBody); fErr == nil && fault != nil {
			return nil, fault
		}
		return nil, fmt.Errorf("SOAP HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	// Unwrap SOAP envelope
	_, resultBody, fault, err := Unwrap(respBody)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap SOAP response: %w", err)
	}
	if fault != nil {
		return nil, fault
	}

	// Build connector result
	result := &connector.Result{
		Affected: 1,
		Rows:     []map[string]interface{}{resultBody},
	}

	return result, nil
}

// applyAuth applies authentication to the request.
func (c *Client) applyAuth(req *http.Request) {
	if c.auth == nil {
		return
	}
	switch strings.ToLower(c.auth.Type) {
	case "basic":
		req.SetBasicAuth(c.auth.Username, c.auth.Password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.auth.Token)
	}
}
