package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Signing in through an identity provider is a conversation with somebody
// else's server: a code is exchanged for a token, the token is spent on the
// person's details, and whoever comes back is signed in. A provider that can be
// stood up in this process is the only way to exercise that without an account
// somewhere.

func TestTheTokenIsSpentOnTheUsersDetails(t *testing.T) {
	var authorization string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"user-1","email":"ada@example.com","email_verified":true}`))
	}))
	defer provider.Close()

	info, err := NewOAuth2Service().GetUserInfo(context.Background(), provider.URL, "at-1")
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info["email"] != "ada@example.com" {
		t.Errorf("info = %v", info)
	}
	if authorization != "Bearer at-1" {
		t.Errorf("authorization = %q", authorization)
	}
}

func TestDetailsThatCannotBeFetchedAreNotGuessedAt(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer provider.Close()

	if _, err := NewOAuth2Service().GetUserInfo(context.Background(), provider.URL, "expired"); err == nil {
		t.Error("a rejected request produced user details")
	}
}

func TestARefreshTokenBuysANewAccessToken(t *testing.T) {
	var received string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = string(body)
		_, _ = w.Write([]byte(`{"access_token":"at-2","token_type":"Bearer"}`))
	}))
	defer provider.Close()

	token, err := NewOAuth2Service().RefreshToken(context.Background(),
		&OAuth2Config{ClientID: "client", ClientSecret: "secret", TokenURL: provider.URL}, "rt-1")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if token.AccessToken != "at-2" {
		t.Errorf("token = %+v", token)
	}
	if !strings.Contains(received, "grant_type=refresh_token") || !strings.Contains(received, "refresh_token=rt-1") {
		t.Errorf("the request carried %q", received)
	}
}

func TestAProviderDescribesItself(t *testing.T) {
	var path string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{
			"issuer": "https://issuer.example.com",
			"authorization_endpoint": "https://issuer.example.com/authorize",
			"token_endpoint": "https://issuer.example.com/token",
			"userinfo_endpoint": "https://issuer.example.com/userinfo",
			"jwks_uri": "https://issuer.example.com/jwks"
		}`))
	}))
	defer provider.Close()

	discovery, err := NewOIDCService().DiscoverOIDC(context.Background(), provider.URL)
	if err != nil {
		t.Fatalf("DiscoverOIDC: %v", err)
	}
	if path != "/.well-known/openid-configuration" {
		t.Errorf("discovery asked for %q", path)
	}
	if discovery.TokenEndpoint != "https://issuer.example.com/token" {
		t.Errorf("discovery = %+v", discovery)
	}
}

func TestAnIssuerThatDescribesNothingIsReported(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer provider.Close()

	if _, err := NewOIDCService().DiscoverOIDC(context.Background(), provider.URL); err == nil {
		t.Error("an issuer with no discovery document was accepted")
	}
}

func TestAnIdentityTokenIsReadForItsClaims(t *testing.T) {
	// This reads the token, it does not verify it — which is allowed because
	// the token comes straight back from the token endpoint over TLS, and it
	// is why nothing may hand this a token that arrived from a browser: the
	// claims would be whatever the sender chose.
	claims := base64.RawURLEncoding.EncodeToString([]byte(
		`{"sub":"user-1","email":"ada@example.com","email_verified":true,"name":"Ada"}`))
	idToken := "header." + claims + ".signature"

	parsed, err := NewOIDCService().ParseIDToken(idToken)
	if err != nil {
		t.Fatalf("ParseIDToken: %v", err)
	}
	if parsed.Subject != "user-1" || parsed.Email != "ada@example.com" || !parsed.EmailVerified {
		t.Errorf("claims = %+v", parsed)
	}
}

func TestSomethingThatIsNotAnIdentityTokenIsRefused(t *testing.T) {
	for name, token := range map[string]string{
		"not three parts":     "header.payload",
		"payload not base64":  "header.!!!not base64!!!.signature",
		"payload not an obje": "header." + base64.RawURLEncoding.EncodeToString([]byte(`"a string"`)) + ".sig",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewOIDCService().ParseIDToken(token); err == nil {
				t.Error("it was read as an identity token")
			}
		})
	}
}

// The whole sign-in, against a provider standing in this process.

func TestSigningInThroughAnIdentityProvider(t *testing.T) {
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 issuer,
				"authorization_endpoint": issuer + "/authorize",
				"token_endpoint":         issuer + "/token",
				"userinfo_endpoint":      issuer + "/userinfo",
			})
		case "/token":
			claims := base64.RawURLEncoding.EncodeToString([]byte(
				`{"sub":"user-1","email":"ada@example.com","email_verified":true,"name":"Ada"}`))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "at-1", "token_type": "Bearer",
				"id_token": "header." + claims + ".signature",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	issuer = provider.URL

	oidcProvider := NewOIDCProvider(&OIDCConfig{
		Name: "corp", Issuer: issuer,
		ClientID: "client", ClientSecret: "secret",
	}, "https://app.example.com/callback")

	if err := oidcProvider.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	token, err := oidcProvider.ExchangeCode(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	info, err := oidcProvider.GetUserInfo(context.Background(), token)
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info.ID != "user-1" || info.Email != "ada@example.com" {
		t.Errorf("who signed in = %+v", info)
	}
	if info.Provider != "corp" {
		t.Errorf("provider = %q, want the name the configuration gave it", info.Provider)
	}
}
