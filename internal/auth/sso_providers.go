package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Provider names
const (
	ProviderGoogle = "google"
	ProviderGitHub = "github"
	ProviderApple  = "apple"
)

// SocialProvider interface for social login providers
type SocialProvider interface {
	Name() string
	GetAuthURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error)
	GetUserInfo(ctx context.Context, token *OAuth2Token) (*OAuth2UserInfo, error)
}

// GoogleProvider implements Google OAuth2
type GoogleProvider struct {
	oauth       *OAuth2Service
	config      *OAuthProviderConfig
	redirectURL string
}

// NewGoogleProvider creates a new Google provider
func NewGoogleProvider(config *OAuthProviderConfig, redirectURL string) *GoogleProvider {
	return &GoogleProvider{
		oauth:       NewOAuth2Service(),
		config:      config,
		redirectURL: redirectURL,
	}
}

func (p *GoogleProvider) Name() string {
	return ProviderGoogle
}

func (p *GoogleProvider) getOAuth2Config() *OAuth2Config {
	scopes := p.config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	return &OAuth2Config{
		ClientID:     p.config.ClientID,
		ClientSecret: p.config.ClientSecret,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://www.googleapis.com/oauth2/v3/userinfo",
		Scopes:       scopes,
		RedirectURL:  p.redirectURL,
	}
}

func (p *GoogleProvider) GetAuthURL(state string) string {
	config := p.getOAuth2Config()
	baseURL := p.oauth.GetAuthURL(config, state)
	// Add Google-specific parameters
	return baseURL + "&access_type=offline&prompt=consent"
}

func (p *GoogleProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	return p.oauth.ExchangeCode(ctx, p.getOAuth2Config(), code)
}

func (p *GoogleProvider) GetUserInfo(ctx context.Context, token *OAuth2Token) (*OAuth2UserInfo, error) {
	config := p.getOAuth2Config()
	raw, err := p.oauth.GetUserInfo(ctx, config.UserInfoURL, token.AccessToken)
	if err != nil {
		return nil, err
	}

	return &OAuth2UserInfo{
		ID:            getString(raw, "sub"),
		Email:         getString(raw, "email"),
		EmailVerified: getBool(raw, "email_verified"),
		Name:          getString(raw, "name"),
		GivenName:     getString(raw, "given_name"),
		FamilyName:    getString(raw, "family_name"),
		Picture:       getString(raw, "picture"),
		Locale:        getString(raw, "locale"),
		Provider:      ProviderGoogle,
		Raw:           raw,
	}, nil
}

// GitHubProvider implements GitHub OAuth2
type GitHubProvider struct {
	oauth       *OAuth2Service
	config      *OAuthProviderConfig
	redirectURL string
	httpClient  *http.Client
}

// NewGitHubProvider creates a new GitHub provider
func NewGitHubProvider(config *OAuthProviderConfig, redirectURL string) *GitHubProvider {
	return &GitHubProvider{
		oauth:       NewOAuth2Service(),
		config:      config,
		redirectURL: redirectURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *GitHubProvider) Name() string {
	return ProviderGitHub
}

func (p *GitHubProvider) getOAuth2Config() *OAuth2Config {
	scopes := p.config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"read:user", "user:email"}
	}
	return &OAuth2Config{
		ClientID:     p.config.ClientID,
		ClientSecret: p.config.ClientSecret,
		AuthURL:      "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		UserInfoURL:  "https://api.github.com/user",
		Scopes:       scopes,
		RedirectURL:  p.redirectURL,
	}
}

func (p *GitHubProvider) GetAuthURL(state string) string {
	return p.oauth.GetAuthURL(p.getOAuth2Config(), state)
}

func (p *GitHubProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	return p.oauth.ExchangeCode(ctx, p.getOAuth2Config(), code)
}

func (p *GitHubProvider) GetUserInfo(ctx context.Context, token *OAuth2Token) (*OAuth2UserInfo, error) {
	config := p.getOAuth2Config()
	raw, err := p.oauth.GetUserInfo(ctx, config.UserInfoURL, token.AccessToken)
	if err != nil {
		return nil, err
	}

	// GitHub doesn't always return email in user endpoint
	email := getString(raw, "email")
	if email == "" {
		// Fetch from emails endpoint
		email, _ = p.getPrimaryEmail(ctx, token.AccessToken)
	}

	return &OAuth2UserInfo{
		ID:            fmt.Sprintf("%v", raw["id"]),
		Email:         email,
		EmailVerified: email != "", // GitHub only returns verified emails
		Name:          getString(raw, "name"),
		Picture:       getString(raw, "avatar_url"),
		Provider:      ProviderGitHub,
		Raw:           raw,
	}, nil
}

func (p *GitHubProvider) getPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return "", err
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	return "", nil
}

// AppleProvider implements Sign in with Apple
type AppleProvider struct {
	config      *AppleConfig
	redirectURL string
	httpClient  *http.Client
}

// NewAppleProvider creates a new Apple provider
func NewAppleProvider(config *AppleConfig, redirectURL string) *AppleProvider {
	return &AppleProvider{
		config:      config,
		redirectURL: redirectURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *AppleProvider) Name() string {
	return ProviderApple
}

func (p *AppleProvider) GetAuthURL(state string) string {
	params := url.Values{}
	params.Set("client_id", p.config.ClientID)
	params.Set("redirect_uri", p.redirectURL)
	params.Set("response_type", "code id_token")
	params.Set("response_mode", "form_post")
	params.Set("state", state)
	params.Set("scope", "name email")

	return "https://appleid.apple.com/auth/authorize?" + params.Encode()
}

func (p *AppleProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	clientSecret, err := p.generateClientSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client secret: %w", err)
	}

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", p.redirectURL)
	data.Set("client_id", p.config.ClientID)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://appleid.apple.com/auth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var token OAuth2Token
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

func (p *AppleProvider) GetUserInfo(ctx context.Context, token *OAuth2Token) (*OAuth2UserInfo, error) {
	// Apple returns user info in the ID token
	if token.IDToken == "" {
		return nil, fmt.Errorf("no ID token in response")
	}

	// Parse ID token without verification (Apple signs with RS256)
	parts := strings.Split(token.IDToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid ID token format")
	}

	// Decode claims
	payload, err := jwt.NewParser().DecodeSegment(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode ID token: %w", err)
	}

	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified string `json:"email_verified"` // Apple returns string "true"
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	return &OAuth2UserInfo{
		ID:            claims.Sub,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified == "true",
		Provider:      ProviderApple,
		Raw: map[string]interface{}{
			"sub":            claims.Sub,
			"email":          claims.Email,
			"email_verified": claims.EmailVerified,
		},
	}, nil
}

func (p *AppleProvider) generateClientSecret() (string, error) {
	// Parse the private key
	block, _ := pem.Decode([]byte(p.config.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("failed to decode private key PEM")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not ECDSA")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": p.config.TeamID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"aud": "https://appleid.apple.com",
		"sub": p.config.ClientID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = p.config.KeyID

	return token.SignedString(ecdsaKey)
}

// OIDCProvider implements OIDC for enterprise SSO
type OIDCProvider struct {
	oidc        *OIDCService
	config      *OIDCConfig
	redirectURL string
	discovery   *OIDCDiscovery
}

// NewOIDCProvider creates a new OIDC provider
func NewOIDCProvider(config *OIDCConfig, redirectURL string) *OIDCProvider {
	return &OIDCProvider{
		oidc:        NewOIDCService(),
		config:      config,
		redirectURL: redirectURL,
	}
}

func (p *OIDCProvider) Name() string {
	return p.config.Name
}

// Initialize fetches OIDC discovery document
func (p *OIDCProvider) Initialize(ctx context.Context) error {
	discovery, err := p.oidc.DiscoverOIDC(ctx, p.config.Issuer)
	if err != nil {
		return fmt.Errorf("failed to discover OIDC: %w", err)
	}
	p.discovery = discovery
	return nil
}

func (p *OIDCProvider) getOAuth2Config() *OAuth2Config {
	scopes := p.config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}

	var authURL, tokenURL, userInfoURL string
	if p.discovery != nil {
		authURL = p.discovery.AuthorizationEndpoint
		tokenURL = p.discovery.TokenEndpoint
		userInfoURL = p.discovery.UserInfoEndpoint
	}

	return &OAuth2Config{
		ClientID:     p.config.ClientID,
		ClientSecret: p.config.ClientSecret,
		AuthURL:      authURL,
		TokenURL:     tokenURL,
		UserInfoURL:  userInfoURL,
		Scopes:       scopes,
		RedirectURL:  p.redirectURL,
	}
}

func (p *OIDCProvider) GetAuthURL(state string) string {
	config := p.getOAuth2Config()
	return p.oidc.GetAuthURL(config, state)
}

func (p *OIDCProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	return p.oidc.ExchangeCode(ctx, p.getOAuth2Config(), code)
}

func (p *OIDCProvider) GetUserInfo(ctx context.Context, token *OAuth2Token) (*OAuth2UserInfo, error) {
	// First try to get info from ID token
	if token.IDToken != "" {
		claims, err := p.oidc.ParseIDToken(token.IDToken)
		if err == nil {
			return &OAuth2UserInfo{
				ID:            claims.Subject,
				Email:         claims.Email,
				EmailVerified: claims.EmailVerified,
				Name:          claims.Name,
				GivenName:     claims.GivenName,
				FamilyName:    claims.FamilyName,
				Picture:       claims.Picture,
				Locale:        claims.Locale,
				Provider:      p.config.Name,
				Raw: map[string]interface{}{
					"sub":            claims.Subject,
					"email":          claims.Email,
					"email_verified": claims.EmailVerified,
					"name":           claims.Name,
				},
			}, nil
		}
	}

	// Fall back to userinfo endpoint
	config := p.getOAuth2Config()
	if config.UserInfoURL == "" {
		return nil, fmt.Errorf("no userinfo URL available")
	}

	raw, err := p.oidc.GetUserInfo(ctx, config.UserInfoURL, token.AccessToken)
	if err != nil {
		return nil, err
	}

	// Apply claim mappings if configured
	if p.config.Claims != nil {
		raw = p.applyClaimMappings(raw)
	}

	return &OAuth2UserInfo{
		ID:            getString(raw, "sub"),
		Email:         getString(raw, "email"),
		EmailVerified: getBool(raw, "email_verified"),
		Name:          getString(raw, "name"),
		GivenName:     getString(raw, "given_name"),
		FamilyName:    getString(raw, "family_name"),
		Picture:       getString(raw, "picture"),
		Locale:        getString(raw, "locale"),
		Provider:      p.config.Name,
		Raw:           raw,
	}, nil
}

func (p *OIDCProvider) applyClaimMappings(raw map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range raw {
		result[k] = v
	}

	for target, source := range p.config.Claims {
		if val, ok := raw[source]; ok {
			result[target] = val
		}
	}

	return result
}

// Helper functions
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			return b == "true"
		}
	}
	return false
}
