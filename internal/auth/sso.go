package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SSO errors
var (
	ErrProviderNotFound       = errors.New("provider not found")
	ErrInvalidState           = errors.New("invalid state")
	ErrStateExpired           = errors.New("state expired")
	ErrSSONotConfigured       = errors.New("SSO is not configured")
	ErrProviderNotInitialized = errors.New("provider not initialized")
)

// SSOService orchestrates SSO and social login
type SSOService struct {
	config  *Config
	logger  *slog.Logger
	linking *AccountLinkingService

	// Providers
	socialProviders map[string]SocialProvider
	oidcProviders   map[string]*OIDCProvider

	// State management
	states map[string]*ssoState
	mu     sync.RWMutex
}

type ssoState struct {
	Provider  string
	State     string
	Nonce     string
	CreatedAt time.Time
	ExpiresAt time.Time
	Metadata  map[string]interface{}
}

// NewSSOService creates a new SSO service
func NewSSOService(config *Config, linkingStore LinkedAccountStore, userStore UserStore, logger *slog.Logger) *SSOService {
	if logger == nil {
		logger = slog.Default()
	}

	svc := &SSOService{
		config:          config,
		logger:          logger,
		socialProviders: make(map[string]SocialProvider),
		oidcProviders:   make(map[string]*OIDCProvider),
		states:          make(map[string]*ssoState),
	}

	// Initialize account linking
	var linkingConfig *AccountLinkingConfig
	if config != nil && config.Social != nil {
		// Use social config for now
	}
	svc.linking = NewAccountLinkingService(linkingConfig, linkingStore, userStore)

	// Initialize providers
	svc.initializeProviders()

	return svc
}

func (s *SSOService) initializeProviders() {
	if s.config == nil {
		return
	}

	// Get redirect base URL from config
	redirectBase := s.getRedirectBaseURL()

	// Initialize social providers
	if s.config.Social != nil {
		if s.config.Social.Google != nil {
			s.socialProviders[ProviderGoogle] = NewGoogleProvider(
				s.config.Social.Google,
				redirectBase+"/auth/callback/google",
			)
		}
		if s.config.Social.GitHub != nil {
			s.socialProviders[ProviderGitHub] = NewGitHubProvider(
				s.config.Social.GitHub,
				redirectBase+"/auth/callback/github",
			)
		}
		if s.config.Social.Apple != nil {
			s.socialProviders[ProviderApple] = NewAppleProvider(
				s.config.Social.Apple,
				redirectBase+"/auth/callback/apple",
			)
		}
	}

	// Initialize OIDC providers
	if s.config.SSO != nil && s.config.SSO.OIDC != nil {
		for _, oidcConfig := range s.config.SSO.OIDC {
			provider := NewOIDCProvider(
				oidcConfig,
				redirectBase+"/auth/callback/oidc/"+oidcConfig.Name,
			)
			s.oidcProviders[oidcConfig.Name] = provider
		}
	}
}

func (s *SSOService) getRedirectBaseURL() string {
	// Try to get from endpoints config
	if s.config.Endpoints != nil && s.config.Endpoints.Prefix != "" {
		return s.config.Endpoints.Prefix
	}
	// Default to relative paths
	return ""
}

// InitializeOIDCProviders initializes OIDC providers by fetching discovery documents
func (s *SSOService) InitializeOIDCProviders(ctx context.Context) error {
	for name, provider := range s.oidcProviders {
		if err := provider.Initialize(ctx); err != nil {
			s.logger.Warn("failed to initialize OIDC provider", "name", name, "error", err)
		}
	}
	return nil
}

// GetAvailableProviders returns list of configured providers
func (s *SSOService) GetAvailableProviders() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var providers []string
	for name := range s.socialProviders {
		providers = append(providers, name)
	}
	for name := range s.oidcProviders {
		providers = append(providers, "oidc:"+name)
	}
	return providers
}

// BeginAuth starts the SSO authentication flow
func (s *SSOService) BeginAuth(ctx context.Context, providerName string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate state
	oauth := NewOAuth2Service()
	state, err := oauth.GenerateState()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	nonce, _ := oauth.GenerateState() // Generate nonce for OIDC

	// Store state
	now := time.Now()
	s.states[state] = &ssoState{
		Provider:  providerName,
		State:     state,
		Nonce:     nonce,
		CreatedAt: now,
		ExpiresAt: now.Add(10 * time.Minute),
	}

	// Get auth URL
	var authURL string

	// Check social providers
	if provider, ok := s.socialProviders[providerName]; ok {
		authURL = provider.GetAuthURL(state)
	} else if provider, ok := s.oidcProviders[providerName]; ok {
		// OIDC provider
		authURL = provider.GetAuthURL(state)
	} else {
		delete(s.states, state)
		return "", "", ErrProviderNotFound
	}

	s.logger.Info("SSO auth started", "provider", providerName, "state", state[:8]+"...")

	return authURL, state, nil
}

// HandleCallback handles the OAuth2 callback
func (s *SSOService) HandleCallback(ctx context.Context, state, code string) (*SSOResult, error) {
	s.mu.Lock()
	storedState, ok := s.states[state]
	if ok {
		delete(s.states, state)
	}
	s.mu.Unlock()

	if !ok {
		return nil, ErrInvalidState
	}

	if time.Now().After(storedState.ExpiresAt) {
		return nil, ErrStateExpired
	}

	providerName := storedState.Provider

	// Exchange code for token and get user info
	var token *OAuth2Token
	var userInfo *OAuth2UserInfo
	var err error

	if provider, ok := s.socialProviders[providerName]; ok {
		token, err = provider.ExchangeCode(ctx, code)
		if err != nil {
			return nil, fmt.Errorf("failed to exchange code: %w", err)
		}

		userInfo, err = provider.GetUserInfo(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("failed to get user info: %w", err)
		}
	} else if provider, ok := s.oidcProviders[providerName]; ok {
		token, err = provider.ExchangeCode(ctx, code)
		if err != nil {
			return nil, fmt.Errorf("failed to exchange code: %w", err)
		}

		userInfo, err = provider.GetUserInfo(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("failed to get user info: %w", err)
		}
	} else {
		return nil, ErrProviderNotFound
	}

	// Link or create user
	linkResult, err := s.linking.LinkOrCreate(ctx, userInfo, token)
	if err != nil {
		return nil, fmt.Errorf("failed to link account: %w", err)
	}

	s.logger.Info("SSO auth completed",
		"provider", providerName,
		"action", linkResult.Action,
		"user_id", linkResult.User.ID,
		"email", userInfo.Email,
	)

	return &SSOResult{
		User:              linkResult.User,
		LinkedAccount:     linkResult.LinkedAccount,
		Action:            linkResult.Action,
		NeedsConfirmation: linkResult.NeedsConfirmation,
		Provider:          providerName,
		UserInfo:          userInfo,
		Token:             token,
	}, nil
}

// SSOResult represents the result of SSO authentication
type SSOResult struct {
	User              *User
	LinkedAccount     *LinkedAccount
	Action            string // created, linked, existing, prompt
	NeedsConfirmation bool
	Provider          string
	UserInfo          *OAuth2UserInfo
	Token             *OAuth2Token
}

// ConfirmLink confirms account linking when user approval is needed
func (s *SSOService) ConfirmLink(ctx context.Context, userID, provider string, userInfo *OAuth2UserInfo, token *OAuth2Token) (*LinkedAccount, error) {
	return s.linking.ConfirmLink(ctx, userID, userInfo, token)
}

// UnlinkAccount unlinks a social account
func (s *SSOService) UnlinkAccount(ctx context.Context, userID, provider string) error {
	if err := s.linking.Unlink(ctx, userID, provider); err != nil {
		return err
	}

	s.logger.Info("account unlinked", "user_id", userID, "provider", provider)
	return nil
}

// GetLinkedAccounts returns all linked accounts for a user
func (s *SSOService) GetLinkedAccounts(ctx context.Context, userID string) ([]*LinkedAccount, error) {
	return s.linking.GetLinkedAccounts(ctx, userID)
}

// RefreshProviderToken refreshes tokens for a linked account
func (s *SSOService) RefreshProviderToken(ctx context.Context, account *LinkedAccount) (*OAuth2Token, error) {
	if account.RefreshToken == "" {
		return nil, errors.New("no refresh token available")
	}

	// Get OAuth2 service
	oauth := NewOAuth2Service()

	// Build config for the provider
	var config *OAuth2Config

	if provider, ok := s.socialProviders[account.Provider]; ok {
		switch p := provider.(type) {
		case *GoogleProvider:
			config = p.getOAuth2Config()
		case *GitHubProvider:
			return nil, errors.New("GitHub does not support token refresh")
		default:
			return nil, errors.New("provider does not support token refresh")
		}
	} else if provider, ok := s.oidcProviders[account.Provider]; ok {
		config = provider.getOAuth2Config()
	} else {
		return nil, ErrProviderNotFound
	}

	return oauth.RefreshToken(ctx, config, account.RefreshToken)
}

// CleanExpiredStates removes expired states
func (s *SSOService) CleanExpiredStates() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for state, data := range s.states {
		if now.After(data.ExpiresAt) {
			delete(s.states, state)
		}
	}
}

// StartStateCleanup starts a background goroutine to clean expired states
func (s *SSOService) StartStateCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.CleanExpiredStates()
			case <-ctx.Done():
				return
			}
		}
	}()
}
