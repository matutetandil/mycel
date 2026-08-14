package grpc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/metadata"
)

// A gRPC service that trusts Auth0, Cognito or Keycloak does not hold a secret:
// it fetches the provider's published keys and checks the signature against the
// one the token names. Nothing exercised that here, and the shared jwks package
// exists because the same defect had already been written twice.

// jwksServer publishes a key set and counts how often it is asked for.
func jwksServer(t *testing.T, keys ...map[string]interface{}) (*httptest.Server, *int32) {
	t.Helper()
	var fetches int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetches, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": keys})
	}))
	t.Cleanup(server.Close)
	return server, &fetches
}

func rsaJWK(t *testing.T, kid string) (map[string]interface{}, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return map[string]interface{}{
		"kty": "RSA",
		"kid": kid,
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}, key
}

func ecJWK(t *testing.T, kid string) (map[string]interface{}, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return map[string]interface{}{
		"kty": "EC",
		"kid": kid,
		"alg": "ES256",
		"crv": "P-256",
		"x":   base64.RawURLEncoding.EncodeToString(key.X.Bytes()),
		"y":   base64.RawURLEncoding.EncodeToString(key.Y.Bytes()),
	}, key
}

// callWith builds the context a call carrying this token arrives in.
func callWith(token string) metadata.MD {
	return metadata.Pairs("authorization", "Bearer "+token)
}

func TestATokenSignedByTheProviderIsAccepted(t *testing.T) {
	jwk, key := rsaJWK(t, "key-2026")
	server, fetches := jwksServer(t, jwk)

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{JWKSURL: server.URL, Algorithms: []string{"RS256"}},
	})

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "ada"})
	token.Header["kid"] = "key-2026"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	ctx := metadata.NewIncomingContext(t.Context(), callWith(signed))
	authCtx, err := interceptor.authenticate(ctx)
	if err != nil {
		t.Fatalf("a token signed by the provider was refused: %v", err)
	}
	if authCtx.UserID != "ada" {
		t.Errorf("UserID = %q, want the subject of the token", authCtx.UserID)
	}

	// The second call is answered from the cache: a provider's key set is
	// fetched once per five minutes, not once per request. Otherwise every
	// call to the service becomes a call to the provider.
	if _, err := interceptor.authenticate(ctx); err != nil {
		t.Fatalf("the second call was refused: %v", err)
	}
	if got := atomic.LoadInt32(fetches); got != 1 {
		t.Errorf("%d fetches of the key set, want one: it is being fetched per request", got)
	}
}

func TestAnECTokenIsAccepted(t *testing.T) {
	// Cognito and Keycloak both publish EC keys, and the branch that builds
	// them is a different one.
	jwk, key := ecJWK(t, "ec-key")
	server, _ := jwksServer(t, jwk)

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{JWKSURL: server.URL, Algorithms: []string{"ES256"}},
	})

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"sub": "grace"})
	token.Header["kid"] = "ec-key"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	ctx := metadata.NewIncomingContext(t.Context(), callWith(signed))
	if _, err := interceptor.authenticate(ctx); err != nil {
		t.Errorf("a token signed with an EC key was refused: %v", err)
	}
}

func TestATokenNamingAKeyTheProviderDoesNotPublishIsRefused(t *testing.T) {
	// Which is what a token from another tenant, or from a provider that has
	// rotated its keys and dropped the old one, looks like.
	jwk, _ := rsaJWK(t, "key-2026")
	server, _ := jwksServer(t, jwk)

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{JWKSURL: server.URL},
	})

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "mallory"})
	token.Header["kid"] = "a-key-nobody-published"
	signed, err := token.SignedString(other)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	ctx := metadata.NewIncomingContext(t.Context(), callWith(signed))
	if _, err := interceptor.authenticate(ctx); err == nil {
		t.Error("a token signed by a key the provider does not publish was accepted")
	}
}

func TestATokenWithNoKeyIdentifierIsRefused(t *testing.T) {
	// Without a kid there is nothing to look up, and picking any key from the
	// set would defeat rotation entirely.
	jwk, key := rsaJWK(t, "key-2026")
	server, _ := jwksServer(t, jwk)

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{JWKSURL: server.URL},
	})

	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "ada"}).SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	ctx := metadata.NewIncomingContext(t.Context(), callWith(signed))
	if _, err := interceptor.authenticate(ctx); err == nil {
		t.Error("a token naming no key was accepted")
	}
}

func TestAProviderThatCannotBeReachedRefusesTheCall(t *testing.T) {
	// Failing closed is the right direction, and the reason this can look like
	// a configuration problem for a long time.
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer unreachable.Close()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "ada"})
	token.Header["kid"] = "key-2026"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	for name, url := range map[string]string{
		"answering with an error": unreachable.URL,
		"not answering at all":    "http://127.0.0.1:1/keys",
	} {
		t.Run(name, func(t *testing.T) {
			interceptor := NewAuthInterceptor(&AuthConfig{
				Type: "jwt", JWT: &JWTAuthConfig{JWKSURL: url},
			})
			ctx := metadata.NewIncomingContext(t.Context(), callWith(signed))
			if _, err := interceptor.authenticate(ctx); err == nil {
				t.Error("the call was accepted although the provider could not be asked")
			}
		})
	}
}

func TestAnAsymmetricTokenWithNoProviderConfiguredIsRefused(t *testing.T) {
	// A service configured with a secret and handed an RS256 token has nothing
	// to check the signature against, and a secret is not it.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "ada"})
	token.Header["kid"] = "key-2026"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt", JWT: &JWTAuthConfig{Secret: "a-shared-secret"},
	})
	ctx := metadata.NewIncomingContext(t.Context(), callWith(signed))
	_, err = interceptor.authenticate(ctx)
	if err == nil {
		t.Fatal("the token was accepted with no key to check it against")
	}
	if !strings.Contains(err.Error(), "JWKS") {
		t.Errorf("error = %q, want it to say what is missing", err)
	}
}

func TestAKeySetThatIsNotOneIsReported(t *testing.T) {
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not a key set"))
	}))
	defer garbage.Close()

	cache := &jwksCache{}
	if _, err := cache.GetKey(garbage.URL, "any"); err == nil {
		t.Error("a response that is not a key set was accepted")
	}
}

func TestKeysTheConnectorCannotBuildAreSkippedRatherThanFatal(t *testing.T) {
	// A provider publishing a key type this connector does not build, or one
	// with unusable parameters, must not stop the keys next to it from being
	// used — rotation puts several keys in a set at once.
	good, key := rsaJWK(t, "usable")
	server, _ := jwksServer(t,
		map[string]interface{}{"kty": "OKP", "kid": "ed25519-key", "crv": "Ed25519", "x": "AAAA"},
		map[string]interface{}{"kty": "RSA", "kid": "broken", "n": "!!!not-base64!!!", "e": "AQAB"},
		good,
	)

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt", JWT: &JWTAuthConfig{JWKSURL: server.URL},
	})

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "ada"})
	token.Header["kid"] = "usable"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	ctx := metadata.NewIncomingContext(t.Context(), callWith(signed))
	if _, err := interceptor.authenticate(ctx); err != nil {
		t.Errorf("a usable key was lost because another one in the set was not: %v", err)
	}
}
