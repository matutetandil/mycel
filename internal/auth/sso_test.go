package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuth2Service(t *testing.T) {
	t.Run("generate state", func(t *testing.T) {
		svc := NewOAuth2Service()

		state1, err := svc.GenerateState()
		if err != nil {
			t.Fatalf("GenerateState error: %v", err)
		}
		if state1 == "" {
			t.Error("state should not be empty")
		}

		state2, _ := svc.GenerateState()
		if state1 == state2 {
			t.Error("states should be unique")
		}
	})

	t.Run("get auth URL", func(t *testing.T) {
		svc := NewOAuth2Service()
		config := &OAuth2Config{
			ClientID:    "test-client",
			AuthURL:     "https://example.com/auth",
			RedirectURL: "https://myapp.com/callback",
			Scopes:      []string{"openid", "email"},
		}

		url := svc.GetAuthURL(config, "test-state")

		if url == "" {
			t.Error("auth URL should not be empty")
		}
		if !contains(url, "client_id=test-client") {
			t.Error("URL should contain client_id")
		}
		if !contains(url, "state=test-state") {
			t.Error("URL should contain state")
		}
		if !contains(url, "redirect_uri=") {
			t.Error("URL should contain redirect_uri")
		}
		if !contains(url, "scope=openid+email") {
			t.Error("URL should contain scopes")
		}
	})

	t.Run("exchange code with mock server", func(t *testing.T) {
		// Mock token endpoint
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"access_token": "test-access-token",
				"token_type": "Bearer",
				"refresh_token": "test-refresh-token",
				"expires_in": 3600
			}`))
		}))
		defer server.Close()

		svc := NewOAuth2Service()
		config := &OAuth2Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     server.URL,
			RedirectURL:  "https://myapp.com/callback",
		}

		token, err := svc.ExchangeCode(context.Background(), config, "test-code")
		if err != nil {
			t.Fatalf("ExchangeCode error: %v", err)
		}

		if token.AccessToken != "test-access-token" {
			t.Errorf("expected test-access-token, got %s", token.AccessToken)
		}
		if token.RefreshToken != "test-refresh-token" {
			t.Errorf("expected test-refresh-token, got %s", token.RefreshToken)
		}
		if token.ExpiresIn != 3600 {
			t.Errorf("expected 3600, got %d", token.ExpiresIn)
		}
	})

	t.Run("get user info with mock server", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer test-token" {
				t.Errorf("expected Bearer test-token, got %s", auth)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"sub": "12345",
				"email": "test@example.com",
				"email_verified": true,
				"name": "Test User"
			}`))
		}))
		defer server.Close()

		svc := NewOAuth2Service()
		userInfo, err := svc.GetUserInfo(context.Background(), server.URL, "test-token")
		if err != nil {
			t.Fatalf("GetUserInfo error: %v", err)
		}

		if userInfo["sub"] != "12345" {
			t.Errorf("expected sub=12345, got %v", userInfo["sub"])
		}
		if userInfo["email"] != "test@example.com" {
			t.Errorf("expected email=test@example.com, got %v", userInfo["email"])
		}
	})
}

func TestOIDCService(t *testing.T) {
	t.Run("discover OIDC with mock server", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/.well-known/openid-configuration" {
				t.Errorf("expected /.well-known/openid-configuration, got %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"issuer": "https://example.com",
				"authorization_endpoint": "https://example.com/auth",
				"token_endpoint": "https://example.com/token",
				"userinfo_endpoint": "https://example.com/userinfo",
				"jwks_uri": "https://example.com/.well-known/jwks.json"
			}`))
		}))
		defer server.Close()

		svc := NewOIDCService()
		discovery, err := svc.DiscoverOIDC(context.Background(), server.URL)
		if err != nil {
			t.Fatalf("DiscoverOIDC error: %v", err)
		}

		if discovery.Issuer != "https://example.com" {
			t.Errorf("expected issuer https://example.com, got %s", discovery.Issuer)
		}
		if discovery.AuthorizationEndpoint != "https://example.com/auth" {
			t.Error("authorization endpoint mismatch")
		}
		if discovery.TokenEndpoint != "https://example.com/token" {
			t.Error("token endpoint mismatch")
		}
	})

	t.Run("parse ID token", func(t *testing.T) {
		svc := NewOIDCService()

		// Create a fake ID token (header.payload.signature)
		// This is just for parsing, not verification
		// payload: {"sub":"user123","email":"test@example.com","email_verified":true}
		idToken := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzIiwiZW1haWwiOiJ0ZXN0QGV4YW1wbGUuY29tIiwiZW1haWxfdmVyaWZpZWQiOnRydWV9.signature"

		claims, err := svc.ParseIDToken(idToken)
		if err != nil {
			t.Fatalf("ParseIDToken error: %v", err)
		}

		if claims.Subject != "user123" {
			t.Errorf("expected sub=user123, got %s", claims.Subject)
		}
		if claims.Email != "test@example.com" {
			t.Errorf("expected email=test@example.com, got %s", claims.Email)
		}
		if !claims.EmailVerified {
			t.Error("expected email_verified=true")
		}
	})
}

func TestAccountLinkingService(t *testing.T) {
	t.Run("link new user", func(t *testing.T) {
		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewAccountLinkingService(&AccountLinkingConfig{
			Enabled: true,
			MatchBy: "email",
		}, linkStore, userStore)

		userInfo := &OAuth2UserInfo{
			ID:            "google123",
			Email:         "test@example.com",
			EmailVerified: true,
			Name:          "Test User",
			Provider:      ProviderGoogle,
		}
		token := &OAuth2Token{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
		}

		result, err := svc.LinkOrCreate(context.Background(), userInfo, token)
		if err != nil {
			t.Fatalf("LinkOrCreate error: %v", err)
		}

		if result.Action != "created" {
			t.Errorf("expected action=created, got %s", result.Action)
		}
		if result.User == nil {
			t.Fatal("user should not be nil")
		}
		if result.User.Email != "test@example.com" {
			t.Errorf("expected email=test@example.com, got %s", result.User.Email)
		}
		if result.LinkedAccount == nil {
			t.Fatal("linked account should not be nil")
		}
	})

	t.Run("link to existing user", func(t *testing.T) {
		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewAccountLinkingService(&AccountLinkingConfig{
			Enabled:             true,
			MatchBy:             "email",
			RequireVerification: false,
			OnMatch:             "link",
		}, linkStore, userStore)

		// Create existing user
		existingUser := &User{
			ID:           "user123",
			Email:        "test@example.com",
			PasswordHash: "hash",
			CreatedAt:    time.Now(),
		}
		userStore.Create(context.Background(), existingUser)

		userInfo := &OAuth2UserInfo{
			ID:            "github456",
			Email:         "test@example.com",
			EmailVerified: true,
			Name:          "Test User",
			Provider:      ProviderGitHub,
		}
		token := &OAuth2Token{AccessToken: "token"}

		result, err := svc.LinkOrCreate(context.Background(), userInfo, token)
		if err != nil {
			t.Fatalf("LinkOrCreate error: %v", err)
		}

		if result.Action != "linked" {
			t.Errorf("expected action=linked, got %s", result.Action)
		}
		if result.User.ID != existingUser.ID {
			t.Errorf("expected user ID %s, got %s", existingUser.ID, result.User.ID)
		}
	})

	t.Run("reuse existing linked account", func(t *testing.T) {
		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewAccountLinkingService(&AccountLinkingConfig{
			Enabled: true,
			MatchBy: "email",
		}, linkStore, userStore)

		userInfo := &OAuth2UserInfo{
			ID:       "google123",
			Email:    "test@example.com",
			Provider: ProviderGoogle,
		}
		token := &OAuth2Token{AccessToken: "token1"}

		// First login - creates user
		result1, _ := svc.LinkOrCreate(context.Background(), userInfo, token)

		// Second login - reuses existing
		token2 := &OAuth2Token{AccessToken: "token2"}
		result2, err := svc.LinkOrCreate(context.Background(), userInfo, token2)
		if err != nil {
			t.Fatalf("LinkOrCreate error: %v", err)
		}

		if result2.Action != "existing" {
			t.Errorf("expected action=existing, got %s", result2.Action)
		}
		if result2.User.ID != result1.User.ID {
			t.Error("should return same user")
		}
	})

	t.Run("unlink account", func(t *testing.T) {
		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewAccountLinkingService(&AccountLinkingConfig{
			Enabled: true,
		}, linkStore, userStore)

		// Create user with password (so they can unlink)
		user := &User{
			ID:           "user123",
			Email:        "test@example.com",
			PasswordHash: "hash", // Has password
			CreatedAt:    time.Now(),
		}
		userStore.Create(context.Background(), user)

		// Create linked account
		linkedAccount := &LinkedAccount{
			ID:         "link123",
			UserID:     user.ID,
			Provider:   ProviderGoogle,
			ProviderID: "google123",
		}
		linkStore.Create(context.Background(), linkedAccount)

		// Unlink
		err := svc.Unlink(context.Background(), user.ID, ProviderGoogle)
		if err != nil {
			t.Fatalf("Unlink error: %v", err)
		}

		// Verify unlinked
		accounts, _ := svc.GetLinkedAccounts(context.Background(), user.ID)
		if len(accounts) != 0 {
			t.Error("expected no linked accounts")
		}
	})

	t.Run("cannot unlink only auth method", func(t *testing.T) {
		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewAccountLinkingService(&AccountLinkingConfig{
			Enabled: true,
		}, linkStore, userStore)

		// Create user WITHOUT password
		user := &User{
			ID:        "user123",
			Email:     "test@example.com",
			CreatedAt: time.Now(),
		}
		userStore.Create(context.Background(), user)

		// Create single linked account
		linkedAccount := &LinkedAccount{
			ID:         "link123",
			UserID:     user.ID,
			Provider:   ProviderGoogle,
			ProviderID: "google123",
		}
		linkStore.Create(context.Background(), linkedAccount)

		// Try to unlink - should fail
		err := svc.Unlink(context.Background(), user.ID, ProviderGoogle)
		if err == nil {
			t.Error("expected error when unlinking only auth method")
		}
	})
}

func TestSSOService(t *testing.T) {
	t.Run("get available providers", func(t *testing.T) {
		config := &Config{
			Social: &SocialConfig{
				Google: &OAuthProviderConfig{
					ClientID:     "google-client",
					ClientSecret: "google-secret",
				},
				GitHub: &OAuthProviderConfig{
					ClientID:     "github-client",
					ClientSecret: "github-secret",
				},
			},
		}

		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewSSOService(config, linkStore, userStore, nil)

		providers := svc.GetAvailableProviders()
		if len(providers) != 2 {
			t.Errorf("expected 2 providers, got %d", len(providers))
		}

		hasGoogle := false
		hasGitHub := false
		for _, p := range providers {
			if p == ProviderGoogle {
				hasGoogle = true
			}
			if p == ProviderGitHub {
				hasGitHub = true
			}
		}
		if !hasGoogle {
			t.Error("expected google provider")
		}
		if !hasGitHub {
			t.Error("expected github provider")
		}
	})

	t.Run("begin auth", func(t *testing.T) {
		config := &Config{
			Social: &SocialConfig{
				Google: &OAuthProviderConfig{
					ClientID:     "google-client",
					ClientSecret: "google-secret",
				},
			},
		}

		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewSSOService(config, linkStore, userStore, nil)

		authURL, state, err := svc.BeginAuth(context.Background(), ProviderGoogle)
		if err != nil {
			t.Fatalf("BeginAuth error: %v", err)
		}

		if authURL == "" {
			t.Error("auth URL should not be empty")
		}
		if state == "" {
			t.Error("state should not be empty")
		}
		if !contains(authURL, "accounts.google.com") {
			t.Error("should use Google auth URL")
		}
		// State is URL-encoded, so check for "state=" parameter
		if !contains(authURL, "state=") {
			t.Error("auth URL should contain state parameter")
		}
	})

	t.Run("begin auth unknown provider", func(t *testing.T) {
		config := &Config{}
		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewSSOService(config, linkStore, userStore, nil)

		_, _, err := svc.BeginAuth(context.Background(), "unknown")
		if err != ErrProviderNotFound {
			t.Errorf("expected ErrProviderNotFound, got %v", err)
		}
	})

	t.Run("handle callback invalid state", func(t *testing.T) {
		config := &Config{}
		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewSSOService(config, linkStore, userStore, nil)

		_, err := svc.HandleCallback(context.Background(), "invalid-state", "code")
		if err != ErrInvalidState {
			t.Errorf("expected ErrInvalidState, got %v", err)
		}
	})

	t.Run("clean expired states", func(t *testing.T) {
		config := &Config{
			Social: &SocialConfig{
				Google: &OAuthProviderConfig{
					ClientID:     "google-client",
					ClientSecret: "google-secret",
				},
			},
		}

		userStore := NewMemoryUserStore()
		linkStore := NewMemoryLinkedAccountStore()
		svc := NewSSOService(config, linkStore, userStore, nil)

		// Create a state
		_, state, _ := svc.BeginAuth(context.Background(), ProviderGoogle)

		// Manually expire it
		svc.mu.Lock()
		svc.states[state].ExpiresAt = time.Now().Add(-1 * time.Hour)
		svc.mu.Unlock()

		// Clean expired
		svc.CleanExpiredStates()

		// Verify cleaned
		svc.mu.RLock()
		_, exists := svc.states[state]
		svc.mu.RUnlock()

		if exists {
			t.Error("expired state should have been cleaned")
		}
	})
}

func TestMemoryLinkedAccountStore(t *testing.T) {
	t.Run("CRUD operations", func(t *testing.T) {
		store := NewMemoryLinkedAccountStore()
		ctx := context.Background()

		// Create
		account := &LinkedAccount{
			ID:         "acc123",
			UserID:     "user123",
			Provider:   ProviderGoogle,
			ProviderID: "google123",
			Email:      "test@example.com",
		}
		err := store.Create(ctx, account)
		if err != nil {
			t.Fatalf("Create error: %v", err)
		}

		// Find by provider ID
		found, err := store.FindByProviderID(ctx, ProviderGoogle, "google123")
		if err != nil {
			t.Fatalf("FindByProviderID error: %v", err)
		}
		if found.ID != account.ID {
			t.Error("found account mismatch")
		}

		// Find by user ID
		accounts, err := store.FindByUserID(ctx, "user123")
		if err != nil {
			t.Fatalf("FindByUserID error: %v", err)
		}
		if len(accounts) != 1 {
			t.Errorf("expected 1 account, got %d", len(accounts))
		}

		// Find by email
		accounts, err = store.FindByEmail(ctx, "test@example.com")
		if err != nil {
			t.Fatalf("FindByEmail error: %v", err)
		}
		if len(accounts) != 1 {
			t.Errorf("expected 1 account, got %d", len(accounts))
		}

		// Update
		account.Email = "updated@example.com"
		err = store.Update(ctx, account)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}

		found, _ = store.FindByProviderID(ctx, ProviderGoogle, "google123")
		if found.Email != "updated@example.com" {
			t.Error("update not applied")
		}

		// Delete
		err = store.Delete(ctx, account.ID)
		if err != nil {
			t.Fatalf("Delete error: %v", err)
		}

		_, err = store.FindByProviderID(ctx, ProviderGoogle, "google123")
		if err == nil {
			t.Error("expected error after delete")
		}
	})

	t.Run("prevent duplicate provider accounts", func(t *testing.T) {
		store := NewMemoryLinkedAccountStore()
		ctx := context.Background()

		account1 := &LinkedAccount{
			ID:         "acc1",
			UserID:     "user1",
			Provider:   ProviderGoogle,
			ProviderID: "google123",
		}
		store.Create(ctx, account1)

		account2 := &LinkedAccount{
			ID:         "acc2",
			UserID:     "user2",
			Provider:   ProviderGoogle,
			ProviderID: "google123", // Same provider ID
		}
		err := store.Create(ctx, account2)
		if err != ErrAccountAlreadyLinked {
			t.Errorf("expected ErrAccountAlreadyLinked, got %v", err)
		}
	})
}

func TestGoogleProvider(t *testing.T) {
	t.Run("get auth URL", func(t *testing.T) {
		config := &OAuthProviderConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
		}
		provider := NewGoogleProvider(config, "https://myapp.com/callback")

		url := provider.GetAuthURL("test-state")

		if !contains(url, "accounts.google.com") {
			t.Error("should use Google auth URL")
		}
		if !contains(url, "client_id=test-client") {
			t.Error("should contain client_id")
		}
		if !contains(url, "access_type=offline") {
			t.Error("should request offline access")
		}
		if !contains(url, "scope=openid+email+profile") {
			t.Error("should include default scopes")
		}
	})
}

func TestGitHubProvider(t *testing.T) {
	t.Run("get auth URL", func(t *testing.T) {
		config := &OAuthProviderConfig{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
		}
		provider := NewGitHubProvider(config, "https://myapp.com/callback")

		url := provider.GetAuthURL("test-state")

		if !contains(url, "github.com") {
			t.Error("should use GitHub auth URL")
		}
		if !contains(url, "client_id=test-client") {
			t.Error("should contain client_id")
		}
	})
}

func TestAppleProvider(t *testing.T) {
	t.Run("get auth URL", func(t *testing.T) {
		config := &AppleConfig{
			ClientID: "com.myapp.auth",
			TeamID:   "TEAMID123",
			KeyID:    "KEYID123",
		}
		provider := NewAppleProvider(config, "https://myapp.com/callback")

		url := provider.GetAuthURL("test-state")

		if !contains(url, "appleid.apple.com") {
			t.Error("should use Apple auth URL")
		}
		if !contains(url, "client_id=com.myapp.auth") {
			t.Error("should contain client_id")
		}
		if !contains(url, "response_mode=form_post") {
			t.Error("should use form_post response mode")
		}
	})
}
