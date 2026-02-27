package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/auth"
	"github.com/matutetandil/mycel/internal/connector"
)

// mockProvider implements auth.SocialProvider for testing.
type mockProvider struct {
	name        string
	authURL     string
	token       *auth.OAuth2Token
	userInfo    *auth.OAuth2UserInfo
	exchangeErr error
	userInfoErr error
}

func (p *mockProvider) Name() string { return p.name }
func (p *mockProvider) GetAuthURL(state string) string {
	return p.authURL + "?state=" + state
}
func (p *mockProvider) ExchangeCode(ctx context.Context, code string) (*auth.OAuth2Token, error) {
	if p.exchangeErr != nil {
		return nil, p.exchangeErr
	}
	return p.token, nil
}
func (p *mockProvider) GetUserInfo(ctx context.Context, token *auth.OAuth2Token) (*auth.OAuth2UserInfo, error) {
	if p.userInfoErr != nil {
		return nil, p.userInfoErr
	}
	return p.userInfo, nil
}

func newTestConnector(t *testing.T) *Connector {
	t.Helper()
	provider := &mockProvider{
		name:    "test",
		authURL: "https://auth.example.com/authorize",
		token: &auth.OAuth2Token{
			AccessToken:  "access_token_123",
			RefreshToken: "refresh_token_456",
			ExpiresIn:    3600,
		},
		userInfo: &auth.OAuth2UserInfo{
			ID:            "user_123",
			Email:         "test@example.com",
			EmailVerified: true,
			Name:          "Test User",
			GivenName:     "Test",
			FamilyName:    "User",
			Picture:       "https://example.com/photo.jpg",
			Provider:      "test",
		},
	}

	config := &auth.OAuth2Config{
		ClientID:     "test_client",
		ClientSecret: "test_secret",
		AuthURL:      "https://auth.example.com/authorize",
		TokenURL:     "https://auth.example.com/token",
		UserInfoURL:  "https://auth.example.com/userinfo",
		Scopes:       []string{"openid", "email"},
		RedirectURL:  "http://localhost:3000/callback",
	}

	return New("test_oauth", "test", provider, auth.NewOAuth2Service(), config, nil)
}

func TestFactory(t *testing.T) {
	factory := NewFactory(nil)

	if !factory.Supports("oauth", "") {
		t.Error("factory should support 'oauth' type")
	}
	if factory.Supports("rest", "") {
		t.Error("factory should not support 'rest' type")
	}
}

func TestFactoryMissingDriver(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name:       "test",
		Type:       "oauth",
		Properties: map[string]interface{}{},
	}

	_, err := factory.Create(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for missing driver")
	}
}

func TestFactoryUnsupportedDriver(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name:   "test",
		Type:   "oauth",
		Driver: "facebook",
		Properties: map[string]interface{}{
			"client_id":     "id",
			"client_secret": "secret",
		},
	}

	_, err := factory.Create(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for unsupported driver")
	}
}

func TestFactoryCustomDriver(t *testing.T) {
	// Mock OAuth server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok"})
	}))
	defer server.Close()

	factory := NewFactory(nil)
	cfg := &connector.Config{
		Name:   "custom_oauth",
		Type:   "oauth",
		Driver: "custom",
		Properties: map[string]interface{}{
			"client_id":     "id",
			"client_secret": "secret",
			"redirect_uri":  "http://localhost/callback",
			"auth_url":      server.URL + "/authorize",
			"token_url":     server.URL + "/token",
			"userinfo_url":  server.URL + "/userinfo",
			"scopes":        []interface{}{"openid", "email"},
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory.Create failed: %v", err)
	}

	if conn.Name() != "custom_oauth" {
		t.Errorf("expected name=custom_oauth, got %s", conn.Name())
	}
	if conn.Type() != "oauth" {
		t.Errorf("expected type=oauth, got %s", conn.Type())
	}
}

func TestFactoryCustomMissingURLs(t *testing.T) {
	factory := NewFactory(nil)
	cfg := &connector.Config{
		Name:   "test",
		Type:   "oauth",
		Driver: "custom",
		Properties: map[string]interface{}{
			"client_id":     "id",
			"client_secret": "secret",
		},
	}

	_, err := factory.Create(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for missing auth_url/token_url in custom provider")
	}
}

func TestAuthorize(t *testing.T) {
	conn := newTestConnector(t)

	result, err := conn.Read(context.Background(), connector.Query{
		Operation: "authorize",
	})
	if err != nil {
		t.Fatalf("authorize failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}

	authURL, ok := result.Rows[0]["auth_url"].(string)
	if !ok || authURL == "" {
		t.Error("expected auth_url in result")
	}

	if !strings.Contains(authURL, "auth.example.com") {
		t.Errorf("auth_url should contain provider URL, got %s", authURL)
	}

	state, ok := result.Rows[0]["state"].(string)
	if !ok || state == "" {
		t.Error("expected state in result")
	}

	// Verify state was stored
	conn.mu.RLock()
	_, exists := conn.states[state]
	conn.mu.RUnlock()
	if !exists {
		t.Error("state should be stored for CSRF validation")
	}

	// Verify redirect metadata
	if result.Metadata == nil || result.Metadata["redirect"] == nil {
		t.Error("expected redirect in metadata")
	}
}

func TestCallback(t *testing.T) {
	conn := newTestConnector(t)

	// First authorize to generate a state
	authResult, _ := conn.Read(context.Background(), connector.Query{
		Operation: "authorize",
	})
	state := authResult.Rows[0]["state"].(string)

	// Callback with the state
	result, err := conn.Read(context.Background(), connector.Query{
		Operation: "callback",
		Params: map[string]interface{}{
			"code":  "auth_code_123",
			"state": state,
		},
	})
	if err != nil {
		t.Fatalf("callback failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}

	row := result.Rows[0]
	if row["email"] != "test@example.com" {
		t.Errorf("expected email=test@example.com, got %v", row["email"])
	}
	if row["name"] != "Test User" {
		t.Errorf("expected name=Test User, got %v", row["name"])
	}
	if row["provider_id"] != "user_123" {
		t.Errorf("expected provider_id=user_123, got %v", row["provider_id"])
	}
	if row["access_token"] != "access_token_123" {
		t.Errorf("expected access_token, got %v", row["access_token"])
	}
	if row["given_name"] != "Test" {
		t.Errorf("expected given_name=Test, got %v", row["given_name"])
	}
	if row["email_verified"] != true {
		t.Errorf("expected email_verified=true, got %v", row["email_verified"])
	}

	// State should be consumed
	conn.mu.RLock()
	_, exists := conn.states[state]
	conn.mu.RUnlock()
	if exists {
		t.Error("state should be consumed after callback")
	}
}

func TestCallbackMissingCode(t *testing.T) {
	conn := newTestConnector(t)

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "callback",
	})
	if err == nil {
		t.Error("expected error for missing code")
	}
}

func TestCallbackInvalidState(t *testing.T) {
	conn := newTestConnector(t)

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "callback",
		Params: map[string]interface{}{
			"code":  "auth_code_123",
			"state": "invalid_state",
		},
	})
	if err == nil {
		t.Error("expected error for invalid state")
	}
}

func TestCallbackExpiredState(t *testing.T) {
	conn := newTestConnector(t)

	// Manually add an expired state
	state := "expired_state"
	conn.mu.Lock()
	conn.states[state] = &stateEntry{createdAt: time.Now().Add(-15 * time.Minute)}
	conn.mu.Unlock()

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "callback",
		Params: map[string]interface{}{
			"code":  "auth_code_123",
			"state": state,
		},
	})
	if err == nil {
		t.Error("expected error for expired state")
	}
}

func TestCallbackWithoutState(t *testing.T) {
	conn := newTestConnector(t)

	// Callback without state should still work (no CSRF check)
	result, err := conn.Read(context.Background(), connector.Query{
		Operation: "callback",
		Params: map[string]interface{}{
			"code": "auth_code_123",
		},
	})
	if err != nil {
		t.Fatalf("callback without state should work: %v", err)
	}
	if result.Rows[0]["email"] != "test@example.com" {
		t.Errorf("expected user info, got %v", result.Rows[0])
	}
}

func TestCallbackExchangeError(t *testing.T) {
	conn := newTestConnector(t)
	conn.provider = &mockProvider{
		name:        "test",
		exchangeErr: fmt.Errorf("invalid code"),
	}

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "callback",
		Params: map[string]interface{}{
			"code": "bad_code",
		},
	})
	if err == nil {
		t.Error("expected error for exchange failure")
	}
}

func TestUserinfo(t *testing.T) {
	// Mock userinfo server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sub":   "user_123",
			"email": "test@example.com",
			"name":  "Test User",
		})
	}))
	defer server.Close()

	conn := newTestConnector(t)
	conn.config.UserInfoURL = server.URL

	result, err := conn.Read(context.Background(), connector.Query{
		Operation: "userinfo",
		Params:    map[string]interface{}{"access_token": "valid_token"},
	})
	if err != nil {
		t.Fatalf("userinfo failed: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["email"] != "test@example.com" {
		t.Errorf("expected email=test@example.com, got %v", result.Rows[0]["email"])
	}
}

func TestUserinfoMissingToken(t *testing.T) {
	conn := newTestConnector(t)

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "userinfo",
	})
	if err == nil {
		t.Error("expected error for missing access_token")
	}
}

func TestRefresh(t *testing.T) {
	// Mock token refresh server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new_access_token",
			"refresh_token": "new_refresh_token",
			"expires_in":    7200,
		})
	}))
	defer server.Close()

	conn := newTestConnector(t)
	conn.config.TokenURL = server.URL

	result, err := conn.Read(context.Background(), connector.Query{
		Operation: "refresh",
		Params:    map[string]interface{}{"refresh_token": "old_refresh_token"},
	})
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	if result.Rows[0]["access_token"] != "new_access_token" {
		t.Errorf("expected new access_token, got %v", result.Rows[0]["access_token"])
	}
}

func TestRefreshMissingToken(t *testing.T) {
	conn := newTestConnector(t)

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "refresh",
	})
	if err == nil {
		t.Error("expected error for missing refresh_token")
	}
}

func TestUnsupportedOperation(t *testing.T) {
	conn := newTestConnector(t)

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "invalid",
	})
	if err == nil {
		t.Error("expected error for unsupported operation")
	}
}

func TestHealth(t *testing.T) {
	conn := newTestConnector(t)
	if err := conn.Health(context.Background()); err != nil {
		t.Errorf("health should pass: %v", err)
	}

	// Without provider
	conn.provider = nil
	if err := conn.Health(context.Background()); err == nil {
		t.Error("health should fail without provider")
	}
}

func TestConnectClose(t *testing.T) {
	conn := newTestConnector(t)

	if err := conn.Connect(context.Background()); err != nil {
		t.Errorf("connect should be no-op: %v", err)
	}

	// Add some states
	conn.mu.Lock()
	conn.states["test1"] = &stateEntry{createdAt: time.Now()}
	conn.states["test2"] = &stateEntry{createdAt: time.Now()}
	conn.mu.Unlock()

	if err := conn.Close(context.Background()); err != nil {
		t.Errorf("close failed: %v", err)
	}

	conn.mu.RLock()
	if len(conn.states) != 0 {
		t.Error("states should be cleaned up on close")
	}
	conn.mu.RUnlock()
}

func TestOperationFromTarget(t *testing.T) {
	conn := newTestConnector(t)

	// When operation is empty, target should be used as operation
	result, err := conn.Read(context.Background(), connector.Query{
		Target: "authorize",
	})
	if err != nil {
		t.Fatalf("should use target as operation: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Error("expected authorize result")
	}
}

func TestParamFromFilters(t *testing.T) {
	conn := newTestConnector(t)

	// Callback should accept params from Filters too
	result, err := conn.Read(context.Background(), connector.Query{
		Operation: "callback",
		Filters: map[string]interface{}{
			"code": "auth_code_from_filters",
		},
	})
	if err != nil {
		t.Fatalf("callback with filters failed: %v", err)
	}
	if result.Rows[0]["email"] != "test@example.com" {
		t.Errorf("expected user info, got %v", result.Rows[0])
	}
}
