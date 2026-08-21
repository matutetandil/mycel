package auth

import (
	"context"
	"testing"
)

// A refresh token that has been used.
//
// Rotation exists so that a refresh token is good once: the response carries a
// new one and the old one stops working, which is what limits the damage when
// one is stolen. The old token was added to the denylist, and the denylist was
// consulted only when replay protection was separately switched on — so with
// `jwt { rotation = true }` alone, a used refresh token kept working, and the
// list it was written to was read by nobody.

func rotatingManager(t *testing.T, replayProtection bool) *Manager {
	t.Helper()

	config := &Config{
		Preset: "development",
		JWT: &JWTConfig{
			Algorithm: "HS256",
			Secret:    "a-secret-long-enough-to-be-plausible",
			Rotation:  true,
		},
	}
	if replayProtection {
		config.Security = &SecurityConfig{
			ReplayProtection: &ReplayProtectionConfig{Enabled: true},
		}
	}

	m, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// signedIn registers an account and returns the tokens it was given.
func registeredWithTokens(t *testing.T, m *Manager) *TokenPair {
	t.Helper()
	ctx := context.Background()

	_, tokens, err := m.Register(ctx, &RegisterRequest{
		Email:    "ada@example.com",
		Password: "a-long-enough-password-1",
		Name:     "Ada",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if tokens == nil || tokens.RefreshToken == "" {
		t.Fatal("registering produced no refresh token")
	}
	return tokens
}

func TestARefreshTokenIsGoodOnce(t *testing.T) {
	ctx := context.Background()
	m := rotatingManager(t, true)

	first := registeredWithTokens(t, m)

	_, second, err := m.RefreshToken(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("refreshing: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("rotation is on and the same refresh token came back")
	}

	// The whole point: the one already spent must not work again.
	if _, _, err := m.RefreshToken(ctx, first.RefreshToken); err == nil {
		t.Error("a refresh token that had already been used was accepted a second time")
	}

	// And the new one still does.
	if _, _, err := m.RefreshToken(ctx, second.RefreshToken); err != nil {
		t.Errorf("the refresh token just issued was refused: %v", err)
	}
}

func TestWithoutRotationARefreshTokenKeepsWorking(t *testing.T) {
	// The other direction: a service that has not asked for rotation is not
	// suddenly invalidating tokens.
	ctx := context.Background()

	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	tokens := registeredWithTokens(t, m)
	if _, _, err := m.RefreshToken(ctx, tokens.RefreshToken); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if _, _, err := m.RefreshToken(ctx, tokens.RefreshToken); err != nil {
		t.Errorf("with rotation off the refresh token stopped working: %v", err)
	}
}

func TestRotationAloneInvalidatesTheOldToken(t *testing.T) {
	// `jwt { rotation = true }` says a refresh token is exchanged for a new
	// one. Whether the old one is then refused was decided by a separate
	// setting nobody has to write — so rotation alone rotated the token and
	// kept honouring the one it replaced.
	ctx := context.Background()
	m := rotatingManager(t, false) // rotation on, no replay_protection block

	first := registeredWithTokens(t, m)

	_, second, err := m.RefreshToken(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("refreshing: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("rotation is on and the same refresh token came back")
	}

	if _, _, err := m.RefreshToken(ctx, first.RefreshToken); err == nil {
		t.Error("a refresh token that had already been exchanged was accepted again; " +
			"rotation put it on the denylist and nothing read the list")
	}
}
