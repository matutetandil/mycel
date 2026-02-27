package oauth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/matutetandil/mycel/internal/auth"
	"github.com/matutetandil/mycel/internal/connector"
)

// Factory creates OAuth connectors from configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new OAuth connector factory.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	return &Factory{logger: logger}
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "oauth"
}

// Create creates a new OAuth connector from configuration.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	driver := cfg.Driver
	if driver == "" {
		driver = cfg.GetString("driver")
	}
	if driver == "" {
		return nil, fmt.Errorf("oauth connector requires a driver (google, github, apple, oidc, custom)")
	}

	clientID := cfg.GetString("client_id")
	clientSecret := cfg.GetString("client_secret")
	redirectURI := cfg.GetString("redirect_uri")
	scopes := parseScopes(cfg.Properties)

	oauth2Svc := auth.NewOAuth2Service()

	var provider auth.SocialProvider
	var oauth2Config *auth.OAuth2Config

	switch driver {
	case "google":
		providerCfg := &auth.OAuthProviderConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       scopes,
		}
		provider = auth.NewGoogleProvider(providerCfg, redirectURI)
		oauth2Config = &auth.OAuth2Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			UserInfoURL:  "https://www.googleapis.com/oauth2/v3/userinfo",
			Scopes:       scopes,
			RedirectURL:  redirectURI,
		}

	case "github":
		providerCfg := &auth.OAuthProviderConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       scopes,
		}
		provider = auth.NewGitHubProvider(providerCfg, redirectURI)
		oauth2Config = &auth.OAuth2Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthURL:      "https://github.com/login/oauth/authorize",
			TokenURL:     "https://github.com/login/oauth/access_token",
			UserInfoURL:  "https://api.github.com/user",
			Scopes:       scopes,
			RedirectURL:  redirectURI,
		}

	case "apple":
		appleCfg := &auth.AppleConfig{
			ClientID:   clientID,
			TeamID:     cfg.GetString("team_id"),
			KeyID:      cfg.GetString("key_id"),
			PrivateKey: cfg.GetString("private_key"),
		}
		provider = auth.NewAppleProvider(appleCfg, redirectURI)
		oauth2Config = &auth.OAuth2Config{
			ClientID:    clientID,
			AuthURL:     "https://appleid.apple.com/auth/authorize",
			TokenURL:    "https://appleid.apple.com/auth/token",
			Scopes:      scopes,
			RedirectURL: redirectURI,
		}

	case "oidc":
		oidcCfg := &auth.OIDCConfig{
			Name:         cfg.GetString("name"),
			Issuer:       cfg.GetString("issuer_url"),
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       scopes,
		}
		oidcProvider := auth.NewOIDCProvider(oidcCfg, redirectURI)
		// Initialize discovery
		if err := oidcProvider.Initialize(ctx); err != nil {
			f.logger.Warn("oidc discovery failed, will retry on first use", "error", err)
		}
		provider = oidcProvider
		oauth2Config = &auth.OAuth2Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       scopes,
			RedirectURL:  redirectURI,
		}

	case "custom":
		authURL := cfg.GetString("auth_url")
		tokenURL := cfg.GetString("token_url")
		userInfoURL := cfg.GetString("userinfo_url")

		if authURL == "" || tokenURL == "" {
			return nil, fmt.Errorf("custom oauth provider requires auth_url and token_url")
		}

		oauth2Config = &auth.OAuth2Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthURL:      authURL,
			TokenURL:     tokenURL,
			UserInfoURL:  userInfoURL,
			Scopes:       scopes,
			RedirectURL:  redirectURI,
		}

		provider = &customProvider{
			oauth2Svc: oauth2Svc,
			config:    oauth2Config,
			name:      "custom",
		}

	default:
		return nil, fmt.Errorf("unsupported oauth driver: %s (supported: google, github, apple, oidc, custom)", driver)
	}

	return New(cfg.Name, driver, provider, oauth2Svc, oauth2Config, f.logger), nil
}

// parseScopes extracts scopes from properties.
func parseScopes(props map[string]interface{}) []string {
	switch v := props["scopes"].(type) {
	case []interface{}:
		scopes := make([]string, 0, len(v))
		for _, s := range v {
			if str, ok := s.(string); ok {
				scopes = append(scopes, str)
			}
		}
		return scopes
	case []string:
		return v
	}
	return nil
}

// customProvider wraps auth.OAuth2Service for custom OAuth2 providers.
type customProvider struct {
	oauth2Svc *auth.OAuth2Service
	config    *auth.OAuth2Config
	name      string
}

func (p *customProvider) Name() string { return p.name }

func (p *customProvider) GetAuthURL(state string) string {
	return p.oauth2Svc.GetAuthURL(p.config, state)
}

func (p *customProvider) ExchangeCode(ctx context.Context, code string) (*auth.OAuth2Token, error) {
	return p.oauth2Svc.ExchangeCode(ctx, p.config, code)
}

func (p *customProvider) GetUserInfo(ctx context.Context, token *auth.OAuth2Token) (*auth.OAuth2UserInfo, error) {
	if p.config.UserInfoURL == "" {
		return &auth.OAuth2UserInfo{Provider: p.name}, nil
	}

	raw, err := p.oauth2Svc.GetUserInfo(ctx, p.config.UserInfoURL, token.AccessToken)
	if err != nil {
		return nil, err
	}

	return &auth.OAuth2UserInfo{
		ID:       getString(raw, "sub", getString(raw, "id", "")),
		Email:    getString(raw, "email", ""),
		Name:     getString(raw, "name", ""),
		Picture:  getString(raw, "picture", getString(raw, "avatar_url", "")),
		Provider: p.name,
		Raw:      raw,
	}, nil
}

func getString(m map[string]interface{}, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}
