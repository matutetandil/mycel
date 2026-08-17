package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

// A service signs its own tokens, and which algorithm it signs with is a
// deployment decision: a shared secret is fine for one service, and a key pair
// is what lets somebody else verify a token without being able to mint one.
// Only the shared-secret path had tests, so the asymmetric ones — the reason
// the setting exists — had never been built or used.

func rsaPEMs(t *testing.T) (private, public string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	private = string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshalling the public key: %v", err)
	}
	public = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	return private, public
}

func ecPEMs(t *testing.T) (private, public string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	privateDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling the private key: %v", err)
	}
	private = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privateDER}))
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshalling the public key: %v", err)
	}
	public = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	return private, public
}

func TestATokenIsSignedAndReadBackForEveryAlgorithm(t *testing.T) {
	rsaPrivate, rsaPublic := rsaPEMs(t)
	ecPrivate, ecPublic := ecPEMs(t)

	for name, config := range map[string]*JWTConfig{
		"a shared secret": {
			Algorithm: "HS256", Secret: "a-secret-long-enough-to-sign-with",
			AccessLifetime: "15m", RefreshLifetime: "7d",
		},
		"a stronger shared secret": {
			Algorithm: "HS512", Secret: "a-secret-long-enough-to-sign-with",
			AccessLifetime: "15m", RefreshLifetime: "7d",
		},
		"an RSA key pair": {
			Algorithm: "RS256", PrivateKey: rsaPrivate, PublicKey: rsaPublic,
			AccessLifetime: "15m", RefreshLifetime: "7d",
		},
		"an elliptic curve key pair": {
			Algorithm: "ES256", PrivateKey: ecPrivate, PublicKey: ecPublic,
			AccessLifetime: "15m", RefreshLifetime: "7d",
		},
	} {
		t.Run(name, func(t *testing.T) {
			tm, err := NewTokenManager(config)
			if err != nil {
				t.Fatalf("NewTokenManager: %v", err)
			}

			pair, err := tm.GenerateTokenPair(&User{ID: "user-1", Email: "ada@example.com"}, "s-1", nil)
			if err != nil {
				t.Fatalf("GenerateTokenPair: %v", err)
			}

			claims, err := tm.ValidateToken(pair.AccessToken)
			if err != nil {
				t.Fatalf("a token this service signed was refused by it: %v", err)
			}
			if claims.UserID != "user-1" || claims.SessionID != "s-1" {
				t.Errorf("claims = %+v", claims)
			}
		})
	}
}

func TestAKeyPairThatIsNotOneIsRefusedAtStartup(t *testing.T) {
	// A service that cannot sign has to say so when it is configured, not when
	// the first person tries to sign in.
	rsaPrivate, rsaPublic := rsaPEMs(t)

	for name, config := range map[string]*JWTConfig{
		"nothing at all":                    nil,
		"a secret algorithm with no secret": {Algorithm: "HS256"},
		"an RSA algorithm with no keys":     {Algorithm: "RS256"},
		"an RSA algorithm with half a pair": {Algorithm: "RS256", PrivateKey: rsaPrivate},
		"a private key that is not a key":   {Algorithm: "RS256", PrivateKey: "not a key", PublicKey: rsaPublic},
		"a public key that is not a key":    {Algorithm: "RS256", PrivateKey: rsaPrivate, PublicKey: "not a key"},
		"an algorithm nobody implements":    {Algorithm: "PS999", Secret: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewTokenManager(config); err == nil {
				t.Error("a token manager was built that cannot sign a token")
			}
		})
	}
}

func TestATokenSignedByAnotherServiceIsRefused(t *testing.T) {
	// Two services, two secrets. This is the check that stops a token minted
	// somewhere else from opening this one.
	ours, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "our-secret-long-enough-to-sign",
		AccessLifetime: "15m", RefreshLifetime: "7d",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	theirs, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "their-secret-long-enough-to-sign",
		AccessLifetime: "15m", RefreshLifetime: "7d",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	pair, err := theirs.GenerateTokenPair(&User{ID: "user-1"}, "s-1", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	if _, err := ours.ValidateToken(pair.AccessToken); err == nil {
		t.Error("a token signed by another service was accepted")
	}
}

func TestSomethingThatIsNotATokenIsRefused(t *testing.T) {
	tm, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "a-secret-long-enough-to-sign-with",
		AccessLifetime: "15m", RefreshLifetime: "7d",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	for name, token := range map[string]string{
		"nothing":            "",
		"not a token at all": "hello",
		"only two parts":     "header.payload",
		"a broken signature": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.wrong",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tm.ValidateToken(token); err == nil {
				t.Error("it was accepted as a token")
			}
		})
	}
}

func TestTheIssuerAndAudienceTravelWithTheToken(t *testing.T) {
	// A gateway in front of several services checks these, so a token that
	// does not carry them is refused by everything downstream.
	tm, err := NewTokenManager(&JWTConfig{
		Algorithm: "HS256", Secret: "a-secret-long-enough-to-sign-with",
		Issuer: "https://accounts.example.com", Audience: []string{"api.example.com"},
		AccessLifetime: "15m", RefreshLifetime: "7d",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	pair, err := tm.GenerateTokenPair(&User{ID: "user-1"}, "s-1", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	claims, err := tm.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Issuer != "https://accounts.example.com" {
		t.Errorf("issuer = %q", claims.Issuer)
	}
	if len(claims.Audience) == 0 || !strings.Contains(strings.Join(claims.Audience, ","), "api.example.com") {
		t.Errorf("audience = %v", claims.Audience)
	}
}

func TestNamingNoAlgorithmSignsWithTheSharedSecret(t *testing.T) {
	// The ordinary case, and the one the refusal above must not break: a
	// configuration that names a secret and no algorithm.
	tm, err := NewTokenManager(&JWTConfig{
		Secret:         "a-secret-long-enough-to-sign-with",
		AccessLifetime: "15m", RefreshLifetime: "7d",
	})
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	pair, err := tm.GenerateTokenPair(&User{ID: "user-1"}, "s-1", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	if _, err := tm.ValidateToken(pair.AccessToken); err != nil {
		t.Errorf("a token signed with the default algorithm was refused: %v", err)
	}
}
