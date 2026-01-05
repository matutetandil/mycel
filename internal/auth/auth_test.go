package auth

import (
	"context"
	"testing"
	"time"
)

func TestPasswordHasher(t *testing.T) {
	hasher := NewPasswordHasher(nil)

	t.Run("hash and verify", func(t *testing.T) {
		password := "test-password-123"

		hash, err := hasher.Hash(password)
		if err != nil {
			t.Fatalf("failed to hash password: %v", err)
		}

		if hash == "" {
			t.Fatal("hash is empty")
		}

		// Should not be the same as the password
		if hash == password {
			t.Fatal("hash should not equal password")
		}

		// Verify correct password
		valid, err := hasher.Verify(password, hash)
		if err != nil {
			t.Fatalf("failed to verify password: %v", err)
		}
		if !valid {
			t.Fatal("password should be valid")
		}

		// Verify incorrect password
		valid, err = hasher.Verify("wrong-password", hash)
		if err != nil {
			t.Fatalf("failed to verify password: %v", err)
		}
		if valid {
			t.Fatal("wrong password should be invalid")
		}
	})

	t.Run("different hashes for same password", func(t *testing.T) {
		password := "test-password"

		hash1, _ := hasher.Hash(password)
		hash2, _ := hasher.Hash(password)

		if hash1 == hash2 {
			t.Fatal("hashes should be different (different salts)")
		}
	})
}

func TestPasswordValidator(t *testing.T) {
	config := &PasswordConfig{
		MinLength:      8,
		MaxLength:      128,
		RequireUpper:   true,
		RequireLower:   true,
		RequireNumber:  true,
		RequireSpecial: true,
	}
	validator := NewPasswordValidator(config)

	tests := []struct {
		name     string
		password string
		valid    bool
	}{
		{"valid password", "Test123!@#", true},
		{"too short", "Te1!", false},
		{"no uppercase", "test123!@#", false},
		{"no lowercase", "TEST123!@#", false},
		{"no number", "TestTest!@#", false},
		{"no special", "TestTest123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.password, nil)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestPasswordStrength(t *testing.T) {
	validator := NewPasswordValidator(nil)

	tests := []struct {
		password string
		minScore int
	}{
		{"a", 0},
		{"abcdefgh", 20},      // length + lowercase
		{"Abcdefgh", 30},      // + uppercase
		{"Abcdefg1", 40},      // + number
		{"Abcdefg1!", 50},     // + special
		{"AbCdEfGh1!@#", 70},  // longer, more unique
	}

	for _, tt := range tests {
		t.Run(tt.password, func(t *testing.T) {
			score := validator.ValidateStrength(tt.password)
			if score < tt.minScore {
				t.Errorf("expected score >= %d, got %d", tt.minScore, score)
			}
		})
	}
}

func TestTokenManager(t *testing.T) {
	config := &JWTConfig{
		Algorithm:       "HS256",
		Secret:          "test-secret-key-that-is-long-enough",
		AccessLifetime:  "15m",
		RefreshLifetime: "7d",
		Issuer:          "test-issuer",
	}

	tm, err := NewTokenManager(config)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}

	user := &User{
		ID:    "user-123",
		Email: "test@example.com",
		Roles: []string{"admin", "user"},
	}

	t.Run("generate and validate token pair", func(t *testing.T) {
		tokens, err := tm.GenerateTokenPair(user, "session-123", nil)
		if err != nil {
			t.Fatalf("failed to generate token pair: %v", err)
		}

		if tokens.AccessToken == "" {
			t.Error("access token is empty")
		}
		if tokens.RefreshToken == "" {
			t.Error("refresh token is empty")
		}
		if tokens.TokenType != "Bearer" {
			t.Errorf("expected token type Bearer, got %s", tokens.TokenType)
		}
		if tokens.ExpiresIn <= 0 {
			t.Error("expires_in should be positive")
		}

		// Validate access token
		claims, err := tm.ValidateAccessToken(tokens.AccessToken)
		if err != nil {
			t.Fatalf("failed to validate access token: %v", err)
		}
		if claims.UserID != user.ID {
			t.Errorf("expected user ID %s, got %s", user.ID, claims.UserID)
		}
		if claims.Email != user.Email {
			t.Errorf("expected email %s, got %s", user.Email, claims.Email)
		}
		if claims.TokenType != "access" {
			t.Errorf("expected token type access, got %s", claims.TokenType)
		}

		// Validate refresh token
		refreshClaims, err := tm.ValidateRefreshToken(tokens.RefreshToken)
		if err != nil {
			t.Fatalf("failed to validate refresh token: %v", err)
		}
		if refreshClaims.TokenType != "refresh" {
			t.Errorf("expected token type refresh, got %s", refreshClaims.TokenType)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := tm.ValidateToken("invalid-token")
		if err == nil {
			t.Error("expected error for invalid token")
		}
	})

	t.Run("wrong token type", func(t *testing.T) {
		tokens, _ := tm.GenerateTokenPair(user, "session-123", nil)

		// Try to validate refresh token as access token
		_, err := tm.ValidateAccessToken(tokens.RefreshToken)
		if err == nil {
			t.Error("expected error when validating refresh token as access token")
		}

		// Try to validate access token as refresh token
		_, err = tm.ValidateRefreshToken(tokens.AccessToken)
		if err == nil {
			t.Error("expected error when validating access token as refresh token")
		}
	})
}

func TestAuthManager(t *testing.T) {
	config := &Config{
		Preset: "development",
		JWT: &JWTConfig{
			Secret:          "test-secret-key-that-is-long-enough",
			AccessLifetime:  "15m",
			RefreshLifetime: "7d",
		},
	}

	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ctx := context.Background()

	t.Run("register user", func(t *testing.T) {
		req := &RegisterRequest{
			Email:    "test@example.com",
			Password: "password123",
		}

		user, tokens, err := manager.Register(ctx, req)
		if err != nil {
			t.Fatalf("failed to register user: %v", err)
		}

		if user.ID == "" {
			t.Error("user ID is empty")
		}
		if user.Email != req.Email {
			t.Errorf("expected email %s, got %s", req.Email, user.Email)
		}
		if tokens.AccessToken == "" {
			t.Error("access token is empty")
		}
	})

	t.Run("register duplicate user", func(t *testing.T) {
		req := &RegisterRequest{
			Email:    "test@example.com",
			Password: "password123",
		}

		_, _, err := manager.Register(ctx, req)
		if err == nil {
			t.Error("expected error for duplicate user")
		}
		if err != ErrUserExists {
			t.Errorf("expected ErrUserExists, got %v", err)
		}
	})

	t.Run("login", func(t *testing.T) {
		req := &LoginRequest{
			Email:    "test@example.com",
			Password: "password123",
		}

		user, tokens, err := manager.Login(ctx, req, "127.0.0.1", "test-agent")
		if err != nil {
			t.Fatalf("failed to login: %v", err)
		}

		if user.Email != req.Email {
			t.Errorf("expected email %s, got %s", req.Email, user.Email)
		}
		if tokens.AccessToken == "" {
			t.Error("access token is empty")
		}
	})

	t.Run("login with wrong password", func(t *testing.T) {
		req := &LoginRequest{
			Email:    "test@example.com",
			Password: "wrong-password",
		}

		_, _, err := manager.Login(ctx, req, "127.0.0.1", "test-agent")
		if err == nil {
			t.Error("expected error for wrong password")
		}
	})

	t.Run("validate token", func(t *testing.T) {
		loginReq := &LoginRequest{
			Email:    "test@example.com",
			Password: "password123",
		}

		_, tokens, _ := manager.Login(ctx, loginReq, "127.0.0.1", "test-agent")

		user, claims, err := manager.ValidateToken(ctx, tokens.AccessToken)
		if err != nil {
			t.Fatalf("failed to validate token: %v", err)
		}

		if user.Email != loginReq.Email {
			t.Errorf("expected email %s, got %s", loginReq.Email, user.Email)
		}
		if claims.UserID != user.ID {
			t.Errorf("expected user ID %s, got %s", user.ID, claims.UserID)
		}
	})

	t.Run("refresh token", func(t *testing.T) {
		loginReq := &LoginRequest{
			Email:    "test@example.com",
			Password: "password123",
		}

		_, tokens, _ := manager.Login(ctx, loginReq, "127.0.0.1", "test-agent")

		// Refresh
		_, newTokens, err := manager.RefreshToken(ctx, tokens.RefreshToken)
		if err != nil {
			t.Fatalf("failed to refresh token: %v", err)
		}

		if newTokens.AccessToken == "" {
			t.Error("new access token is empty")
		}
	})

	t.Run("logout", func(t *testing.T) {
		loginReq := &LoginRequest{
			Email:    "test@example.com",
			Password: "password123",
		}

		_, tokens, _ := manager.Login(ctx, loginReq, "127.0.0.1", "test-agent")

		// Validate first
		_, claims, _ := manager.ValidateToken(ctx, tokens.AccessToken)

		// Logout
		err := manager.Logout(ctx, claims.SessionID)
		if err != nil {
			t.Fatalf("failed to logout: %v", err)
		}
	})
}

func TestMemoryStores(t *testing.T) {
	ctx := context.Background()

	t.Run("user store", func(t *testing.T) {
		store := NewMemoryUserStore()

		user := &User{
			ID:        "user-1",
			Email:     "test@example.com",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		// Create
		if err := store.Create(ctx, user); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}

		// Find by ID
		found, err := store.FindByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("failed to find user by ID: %v", err)
		}
		if found.Email != user.Email {
			t.Errorf("expected email %s, got %s", user.Email, found.Email)
		}

		// Find by email
		found, err = store.FindByEmail(ctx, user.Email)
		if err != nil {
			t.Fatalf("failed to find user by email: %v", err)
		}
		if found.ID != user.ID {
			t.Errorf("expected ID %s, got %s", user.ID, found.ID)
		}

		// Duplicate create
		err = store.Create(ctx, user)
		if err != ErrUserExists {
			t.Errorf("expected ErrUserExists, got %v", err)
		}

		// Delete
		if err := store.Delete(ctx, user.ID); err != nil {
			t.Fatalf("failed to delete user: %v", err)
		}

		// Not found
		_, err = store.FindByID(ctx, user.ID)
		if err != ErrUserNotFound {
			t.Errorf("expected ErrUserNotFound, got %v", err)
		}
	})

	t.Run("session store", func(t *testing.T) {
		store := NewMemorySessionStore()

		session := &Session{
			ID:        "session-1",
			UserID:    "user-1",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}

		// Create
		if err := store.Create(ctx, session); err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		// Find by ID
		found, err := store.FindByID(ctx, session.ID)
		if err != nil {
			t.Fatalf("failed to find session: %v", err)
		}
		if found.UserID != session.UserID {
			t.Errorf("expected user ID %s, got %s", session.UserID, found.UserID)
		}

		// Find by user ID
		sessions, err := store.FindByUserID(ctx, session.UserID)
		if err != nil {
			t.Fatalf("failed to find sessions by user: %v", err)
		}
		if len(sessions) != 1 {
			t.Errorf("expected 1 session, got %d", len(sessions))
		}

		// Count
		count, _ := store.Count(ctx, session.UserID)
		if count != 1 {
			t.Errorf("expected count 1, got %d", count)
		}

		// Delete
		if err := store.Delete(ctx, session.ID); err != nil {
			t.Fatalf("failed to delete session: %v", err)
		}

		count, _ = store.Count(ctx, session.UserID)
		if count != 0 {
			t.Errorf("expected count 0, got %d", count)
		}
	})

	t.Run("brute force store", func(t *testing.T) {
		store := NewMemoryBruteForceStore()
		key := "user@example.com"

		// Increment
		count, _ := store.Increment(ctx, key, 15*time.Minute)
		if count != 1 {
			t.Errorf("expected count 1, got %d", count)
		}

		count, _ = store.Increment(ctx, key, 15*time.Minute)
		if count != 2 {
			t.Errorf("expected count 2, got %d", count)
		}

		// Get
		count, _ = store.Get(ctx, key)
		if count != 2 {
			t.Errorf("expected count 2, got %d", count)
		}

		// Lock
		if err := store.Lock(ctx, key, 5*time.Minute); err != nil {
			t.Fatalf("failed to lock: %v", err)
		}

		locked, until, _ := store.IsLocked(ctx, key)
		if !locked {
			t.Error("expected locked")
		}
		if until.IsZero() {
			t.Error("expected non-zero unlock time")
		}

		// Reset
		store.Reset(ctx, key)
		count, _ = store.Get(ctx, key)
		if count != 0 {
			t.Errorf("expected count 0 after reset, got %d", count)
		}
	})
}

func TestPresets(t *testing.T) {
	presets := []string{PresetStrict, PresetStandard, PresetRelaxed, PresetDevelopment}

	for _, name := range presets {
		t.Run(name, func(t *testing.T) {
			preset := GetPreset(name)
			if preset == nil {
				t.Fatal("preset is nil")
			}
			if preset.Preset != name {
				t.Errorf("expected preset name %s, got %s", name, preset.Preset)
			}
			if preset.JWT == nil {
				t.Error("JWT config is nil")
			}
			if preset.Password == nil {
				t.Error("Password config is nil")
			}
			if preset.Sessions == nil {
				t.Error("Sessions config is nil")
			}
		})
	}
}

func TestMergeWithPreset(t *testing.T) {
	// User config with partial settings
	cfg := &Config{
		Preset: "standard",
		JWT: &JWTConfig{
			Secret: "my-secret",
			// Other fields should be filled from preset
		},
	}

	merged := MergeWithPreset(cfg)

	// Check that user value is preserved
	if merged.JWT.Secret != "my-secret" {
		t.Errorf("expected secret my-secret, got %s", merged.JWT.Secret)
	}

	// Check that preset values are applied
	if merged.JWT.Algorithm != "HS256" {
		t.Errorf("expected algorithm HS256, got %s", merged.JWT.Algorithm)
	}
	if merged.JWT.AccessLifetime != "1h" {
		t.Errorf("expected access lifetime 1h, got %s", merged.JWT.AccessLifetime)
	}
}

func TestGenerateRandomPassword(t *testing.T) {
	password, err := GenerateRandomPassword(16)
	if err != nil {
		t.Fatalf("failed to generate password: %v", err)
	}

	if len(password) != 16 {
		t.Errorf("expected length 16, got %d", len(password))
	}

	// Verify it meets complexity requirements
	validator := NewPasswordValidator(&PasswordConfig{
		RequireUpper:   true,
		RequireLower:   true,
		RequireNumber:  true,
		RequireSpecial: true,
	})

	err = validator.Validate(password, nil)
	if err != nil {
		t.Errorf("generated password doesn't meet requirements: %v", err)
	}
}
