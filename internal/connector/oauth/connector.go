// Package oauth provides an OAuth2 connector for social login flows.
// It exposes OAuth2 authorize/callback/userinfo operations as a Mycel connector,
// reusing the existing auth.OAuth2Service and provider implementations.
package oauth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/auth"
	"github.com/matutetandil/mycel/internal/connector"
)

// stateEntry holds a pending OAuth state with expiry.
type stateEntry struct {
	createdAt time.Time
}

// Connector implements OAuth2 flows as a Mycel connector.
// It supports authorize (generate auth URL), callback (exchange code for user info),
// userinfo (fetch user profile), and refresh (refresh access token).
type Connector struct {
	name      string
	driver    string
	provider  auth.SocialProvider
	oauth2Svc *auth.OAuth2Service
	config    *auth.OAuth2Config
	logger    *slog.Logger

	mu     sync.RWMutex
	states map[string]*stateEntry
}

// New creates a new OAuth connector.
func New(name, driver string, provider auth.SocialProvider, oauth2Svc *auth.OAuth2Service, config *auth.OAuth2Config, logger *slog.Logger) *Connector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Connector{
		name:      name,
		driver:    driver,
		provider:  provider,
		oauth2Svc: oauth2Svc,
		config:    config,
		logger:    logger,
		states:    make(map[string]*stateEntry),
	}
}

// Name returns the connector name.
func (c *Connector) Name() string { return c.name }

// Type returns the connector type.
func (c *Connector) Type() string { return "oauth" }

// Connect is a no-op.
func (c *Connector) Connect(ctx context.Context) error { return nil }

// Close cleans up state entries.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	c.states = make(map[string]*stateEntry)
	c.mu.Unlock()
	return nil
}

// Health checks that the provider is configured.
func (c *Connector) Health(ctx context.Context) error {
	if c.provider == nil {
		return fmt.Errorf("oauth provider not configured")
	}
	return nil
}

// Read executes an OAuth operation.
// Supported operations: authorize, callback, userinfo, refresh.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	operation := query.Operation
	if operation == "" {
		operation = query.Target
	}

	switch operation {
	case "authorize":
		return c.authorize(ctx)
	case "callback":
		return c.callback(ctx, query)
	case "userinfo":
		return c.userinfo(ctx, query)
	case "refresh":
		return c.refresh(ctx, query)
	default:
		return nil, fmt.Errorf("unsupported oauth operation: %s", operation)
	}
}

// authorize generates an OAuth state and returns the authorization URL.
func (c *Connector) authorize(ctx context.Context) (*connector.Result, error) {
	state, err := c.oauth2Svc.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	c.mu.Lock()
	c.states[state] = &stateEntry{createdAt: time.Now()}
	c.mu.Unlock()

	// Clean expired states (older than 10 minutes)
	go c.cleanExpiredStates()

	authURL := c.provider.GetAuthURL(state)

	return &connector.Result{
		Rows: []map[string]interface{}{
			{
				"auth_url": authURL,
				"state":    state,
			},
		},
		Metadata: map[string]interface{}{
			"redirect": authURL,
		},
	}, nil
}

// callback exchanges the authorization code for tokens and user info.
func (c *Connector) callback(ctx context.Context, query connector.Query) (*connector.Result, error) {
	code := getParam(query, "code")
	state := getParam(query, "state")

	if code == "" {
		return nil, fmt.Errorf("callback requires 'code' parameter")
	}

	// Validate state (CSRF protection)
	if state != "" {
		c.mu.Lock()
		entry, exists := c.states[state]
		if exists {
			delete(c.states, state)
		}
		c.mu.Unlock()

		if !exists {
			return nil, fmt.Errorf("invalid or expired state")
		}

		if time.Since(entry.createdAt) > 10*time.Minute {
			return nil, fmt.Errorf("expired state")
		}
	}

	// Exchange code for token
	token, err := c.provider.ExchangeCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange failed: %w", err)
	}

	// Get user info
	userInfo, err := c.provider.GetUserInfo(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	result := map[string]interface{}{
		"provider_id":   userInfo.ID,
		"email":         userInfo.Email,
		"name":          userInfo.Name,
		"picture":       userInfo.Picture,
		"provider":      userInfo.Provider,
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"expires_in":    token.ExpiresIn,
	}

	if userInfo.GivenName != "" {
		result["given_name"] = userInfo.GivenName
	}
	if userInfo.FamilyName != "" {
		result["family_name"] = userInfo.FamilyName
	}
	if userInfo.Locale != "" {
		result["locale"] = userInfo.Locale
	}
	if userInfo.EmailVerified {
		result["email_verified"] = true
	}

	return &connector.Result{
		Rows: []map[string]interface{}{result},
	}, nil
}

// userinfo fetches user info using an existing access token.
func (c *Connector) userinfo(ctx context.Context, query connector.Query) (*connector.Result, error) {
	accessToken := getParam(query, "access_token")
	if accessToken == "" {
		return nil, fmt.Errorf("userinfo requires 'access_token' parameter")
	}

	if c.config.UserInfoURL == "" {
		return nil, fmt.Errorf("no userinfo URL configured for this provider")
	}

	raw, err := c.oauth2Svc.GetUserInfo(ctx, c.config.UserInfoURL, accessToken)
	if err != nil {
		return nil, fmt.Errorf("userinfo fetch failed: %w", err)
	}

	return &connector.Result{
		Rows: []map[string]interface{}{raw},
	}, nil
}

// refresh refreshes an expired access token.
func (c *Connector) refresh(ctx context.Context, query connector.Query) (*connector.Result, error) {
	refreshToken := getParam(query, "refresh_token")
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh requires 'refresh_token' parameter")
	}

	token, err := c.oauth2Svc.RefreshToken(ctx, c.config, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}

	return &connector.Result{
		Rows: []map[string]interface{}{
			{
				"access_token":  token.AccessToken,
				"refresh_token": token.RefreshToken,
				"expires_in":    token.ExpiresIn,
			},
		},
	}, nil
}

// cleanExpiredStates removes states older than 10 minutes.
func (c *Connector) cleanExpiredStates() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for k, v := range c.states {
		if v.createdAt.Before(cutoff) {
			delete(c.states, k)
		}
	}
}

// getParam retrieves a parameter from query Params or Filters.
func getParam(query connector.Query, key string) string {
	if query.Params != nil {
		if v, ok := query.Params[key].(string); ok {
			return v
		}
	}
	if query.Filters != nil {
		if v, ok := query.Filters[key].(string); ok {
			return v
		}
	}
	return ""
}
