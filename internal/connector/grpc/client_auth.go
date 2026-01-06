package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// bearerCredentials implements PerRPCCredentials for bearer token auth.
type bearerCredentials struct {
	token string
}

// newBearerCredentials creates bearer token credentials.
func newBearerCredentials(token string) credentials.PerRPCCredentials {
	return &bearerCredentials{token: token}
}

// GetRequestMetadata returns the bearer token as metadata.
func (c *bearerCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + c.token,
	}, nil
}

// RequireTransportSecurity indicates whether transport security is required.
func (c *bearerCredentials) RequireTransportSecurity() bool {
	return false // Allow both secure and insecure for development
}

// apiKeyCredentials implements PerRPCCredentials for API key auth.
type apiKeyCredentials struct {
	key      string
	metadata string
}

// newAPIKeyCredentials creates API key credentials.
func newAPIKeyCredentials(key, metadata string) credentials.PerRPCCredentials {
	if metadata == "" {
		metadata = "api-key"
	}
	return &apiKeyCredentials{key: key, metadata: metadata}
}

// GetRequestMetadata returns the API key as metadata.
func (c *apiKeyCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		c.metadata: c.key,
	}, nil
}

// RequireTransportSecurity indicates whether transport security is required.
func (c *apiKeyCredentials) RequireTransportSecurity() bool {
	return false
}

// oauth2Credentials implements PerRPCCredentials for OAuth2 client credentials.
type oauth2Credentials struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scopes       []string

	mu          sync.RWMutex
	accessToken string
	expiry      time.Time
}

// newOAuth2Credentials creates OAuth2 client credentials.
func newOAuth2Credentials(config *OAuth2Config) credentials.PerRPCCredentials {
	return &oauth2Credentials{
		tokenURL:     config.TokenURL,
		clientID:     config.ClientID,
		clientSecret: config.ClientSecret,
		scopes:       config.Scopes,
	}
}

// GetRequestMetadata returns the OAuth2 token as metadata.
func (c *oauth2Credentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"authorization": "Bearer " + token,
	}, nil
}

// RequireTransportSecurity indicates whether transport security is required.
func (c *oauth2Credentials) RequireTransportSecurity() bool {
	return false
}

// getToken gets a valid token, refreshing if needed.
func (c *oauth2Credentials) getToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.accessToken != "" && time.Now().Before(c.expiry) {
		token := c.accessToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double check after acquiring write lock
	if c.accessToken != "" && time.Now().Before(c.expiry) {
		return c.accessToken, nil
	}

	return c.fetchToken(ctx)
}

// fetchToken fetches a new token from the OAuth2 server.
func (c *oauth2Credentials) fetchToken(ctx context.Context) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	if len(c.scopes) > 0 {
		data.Set("scope", strings.Join(c.scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request returned status %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	c.accessToken = tokenResp.AccessToken

	// Set expiry with some buffer
	if tokenResp.ExpiresIn > 0 {
		c.expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	} else {
		c.expiry = time.Now().Add(50 * time.Minute) // Default 50 minutes
	}

	return c.accessToken, nil
}

// BuildClientAuthOption builds gRPC dial option for client authentication.
func BuildClientAuthOption(config *ClientAuthConfig) grpc.DialOption {
	if config == nil || config.Type == "" {
		return nil
	}

	var creds credentials.PerRPCCredentials

	switch config.Type {
	case "bearer":
		if config.Token != "" {
			creds = newBearerCredentials(config.Token)
		}

	case "api_key":
		if config.APIKey != nil && config.APIKey.Key != "" {
			creds = newAPIKeyCredentials(config.APIKey.Key, config.APIKey.Metadata)
		}

	case "oauth2", "client_credentials":
		if config.OAuth2 != nil {
			creds = newOAuth2Credentials(config.OAuth2)
		}

	default:
		return nil
	}

	if creds != nil {
		return grpc.WithPerRPCCredentials(creds)
	}
	return nil
}
