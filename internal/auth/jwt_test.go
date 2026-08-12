package auth

import (
	"strings"
	"testing"
	"time"
)

// Token validation is where an authentication system is attacked, and its
// failures are silent by nature: a token accepted when it should not be looks
// exactly like a token accepted when it should. These cover the refusals as
// carefully as the successes.

func hmacManager(t *testing.T) *TokenManager {
	t.Helper()
	tm, err := NewTokenManager(&JWTConfig{
		Algorithm:       "HS256",
		Secret:          "a-secret-long-enough-to-be-plausible",
		AccessLifetime:  "15m",
		RefreshLifetime: "24h",
		Issuer:          "mycel-test",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return tm
}

func testUser() *User {
	return &User{ID: "user-1", Email: "person@example.com", Roles: []string{"admin"}}
}

func TestNewTokenManagerRejectsAnUnusableConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *JWTConfig
	}{
		{"no config at all", nil},
		{"HMAC without a secret", &JWTConfig{Algorithm: "HS256"}},
		{"RSA without keys", &JWTConfig{Algorithm: "RS256"}},
		{"RSA with a key that is not a key", &JWTConfig{
			Algorithm: "RS256", PrivateKey: "not-a-pem", PublicKey: "also-not-a-pem",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewTokenManager(tc.config); err == nil {
				t.Error("an unusable configuration was accepted, so signing would fail later or not at all")
			}
		})
	}
}

func TestARoundTripCarriesTheIdentity(t *testing.T) {
	tm := hmacManager(t)

	pair, err := tm.GenerateTokenPair(testUser(), "session-9", map[string]interface{}{"tenant": "acme"})
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("a pair came back with an empty token")
	}
	if pair.AccessToken == pair.RefreshToken {
		t.Error("the access and refresh tokens are the same string")
	}

	claims, err := tm.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != "user-1" || claims.Email != "person@example.com" {
		t.Errorf("claims = %+v", claims)
	}
	if claims.SessionID != "session-9" {
		t.Errorf("session = %q", claims.SessionID)
	}
}

func TestAnAccessTokenIsNotARefreshToken(t *testing.T) {
	// Otherwise a leaked access token, which travels on every request, could be
	// exchanged for a fresh pair and outlive its own expiry indefinitely.
	tm := hmacManager(t)
	pair, err := tm.GenerateTokenPair(testUser(), "s", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	if _, err := tm.ValidateRefreshToken(pair.AccessToken); err == nil {
		t.Error("an access token was accepted as a refresh token")
	}
	if _, err := tm.ValidateAccessToken(pair.RefreshToken); err == nil {
		t.Error("a refresh token was accepted as an access token")
	}
}

func TestATokenSignedWithAnotherSecretIsRefused(t *testing.T) {
	tm := hmacManager(t)
	other, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "a-completely-different-secret", AccessLifetime: "15m",
		Issuer: "mycel-test",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	pair, err := other.GenerateTokenPair(testUser(), "s", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	if _, err := tm.ValidateToken(pair.AccessToken); err == nil {
		t.Error("a token signed with another key was accepted")
	}
}

func TestTheAlgorithmIsPinned(t *testing.T) {
	// The alg:none family of attacks works by handing the server a token that
	// names its own verification method. The configured algorithm has to be the
	// one that decides.
	tm := hmacManager(t)

	// A well-formed token whose header says none, with an empty signature.
	const noneToken = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJzdWIiOiJ1c2VyLTEiLCJ1c2VyX2lkIjoidXNlci0xIn0."

	if _, err := tm.ValidateToken(noneToken); err == nil {
		t.Error("a token declaring alg:none was accepted")
	}
}

func TestGarbageIsRefusedRatherThanPanicking(t *testing.T) {
	tm := hmacManager(t)
	for _, s := range []string{"", "not-a-token", "a.b", "a.b.c", strings.Repeat("x", 5000)} {
		if _, err := tm.ValidateToken(s); err == nil {
			t.Errorf("a token was accepted from %d characters of nonsense", len(s))
		}
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	tm, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "secret-secret-secret",
		AccessLifetime: "1ns", RefreshLifetime: "1ns", Issuer: "mycel-test",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	pair, err := tm.GenerateTokenPair(testUser(), "s", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	if _, err := tm.ValidateToken(pair.AccessToken); err == nil {
		t.Error("an expired token was accepted")
	}
}

func TestTheIssuerIsChecked(t *testing.T) {
	// Two services sharing a signing secret is common; the issuer is what keeps
	// one from accepting the other's tokens.
	signer, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "shared-between-two-services",
		AccessLifetime: "15m", Issuer: "other-service",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	pair, err := signer.GenerateTokenPair(testUser(), "s", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	verifier, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "shared-between-two-services",
		AccessLifetime: "15m", Issuer: "this-service",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	if _, err := verifier.ValidateToken(pair.AccessToken); err == nil {
		t.Error("a token issued by another service was accepted")
	}
}

func TestRefreshProducesAUsablePair(t *testing.T) {
	tm := hmacManager(t)
	pair, err := tm.GenerateTokenPair(testUser(), "s", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	refreshed, err := tm.RefreshTokens(pair.RefreshToken, testUser())
	if err != nil {
		t.Fatalf("RefreshTokens: %v", err)
	}
	if _, err := tm.ValidateAccessToken(refreshed.AccessToken); err != nil {
		t.Errorf("the refreshed access token does not validate: %v", err)
	}

	// An access token must not be usable to refresh.
	if _, err := tm.RefreshTokens(pair.AccessToken, testUser()); err == nil {
		t.Error("an access token was accepted for refresh")
	}
}

func TestTokenIDsAreDistinct(t *testing.T) {
	// The identifier is what a revocation list keys on, so a repeat would let
	// one revocation take out an unrelated session.
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := generateTokenID()
		if err != nil {
			t.Fatalf("generateTokenID: %v", err)
		}
		if seen[id] {
			t.Fatalf("generateTokenID repeated %q", id)
		}
		seen[id] = true
	}
}
