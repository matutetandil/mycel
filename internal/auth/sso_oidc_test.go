package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Signing in through somebody else's identity provider.
//
// OIDC is the one that is configured rather than built in: a company's Okta,
// Keycloak or Entra, discovered from an issuer URL. Everything about it comes
// from the wire, so it is the provider most able to be wrong in a way nothing
// notices — a claim mapping that does not apply leaves every user with no
// email, and an account matched by email is an account matched by nothing.

// identityProvider stands up the three endpoints an OIDC provider serves.
type identityProvider struct {
	server *httptest.Server

	tokenAnswer    map[string]interface{}
	userInfoAnswer map[string]interface{}
	userInfoStatus int

	tokenForm url.Values
	bearer    string
}

func newIdentityProvider(t *testing.T) *identityProvider {
	t.Helper()

	idp := &identityProvider{
		tokenAnswer: map[string]interface{}{
			"access_token": "access-1",
			"token_type":   "Bearer",
			"expires_in":   3600,
		},
		userInfoAnswer: map[string]interface{}{
			"sub":            "user-1",
			"email":          "someone@example.test",
			"email_verified": true,
			"name":           "Someone",
			"given_name":     "Some",
			"family_name":    "One",
			"picture":        "https://example.test/photo.png",
			"locale":         "en-NZ",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                 idp.server.URL,
			"authorization_endpoint": idp.server.URL + "/authorize",
			"token_endpoint":         idp.server.URL + "/token",
			"userinfo_endpoint":      idp.server.URL + "/userinfo",
			"jwks_uri":               idp.server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.tokenForm = r.Form
		_ = json.NewEncoder(w).Encode(idp.tokenAnswer)
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		idp.bearer = r.Header.Get("Authorization")
		if idp.userInfoStatus != 0 {
			w.WriteHeader(idp.userInfoStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(idp.userInfoAnswer)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (idp *identityProvider) provider(t *testing.T, config *OIDCConfig) *OIDCProvider {
	t.Helper()
	config.Issuer = idp.server.URL
	if config.Name == "" {
		config.Name = "work"
	}
	if config.ClientID == "" {
		config.ClientID = "client-1"
		config.ClientSecret = "secret-1"
	}

	provider := NewOIDCProvider(config, "https://service.example.test/auth/callback")
	if err := provider.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return provider
}

func TestWhereSomebodyIsSentToSignIn(t *testing.T) {
	idp := newIdentityProvider(t)
	provider := idp.provider(t, &OIDCConfig{})

	authURL := provider.GetAuthURL("state-1")

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("the sign-in address is not an address: %v", err)
	}
	// The endpoint comes from discovery, not from anything configured: a
	// company that moves its login page moves it for us too.
	if !strings.HasPrefix(authURL, idp.server.URL+"/authorize") {
		t.Errorf("sent to %s", authURL)
	}

	query := parsed.Query()
	if query.Get("client_id") != "client-1" {
		t.Errorf("client id = %q", query.Get("client_id"))
	}
	// The state is what ties the answer back to this sign-in; without it the
	// callback cannot tell a real return from a forged one.
	if query.Get("state") != "state-1" {
		t.Errorf("state = %q", query.Get("state"))
	}
	if query.Get("redirect_uri") != "https://service.example.test/auth/callback" {
		t.Errorf("redirect = %q", query.Get("redirect_uri"))
	}
	// openid is what makes this OIDC rather than plain OAuth.
	if !strings.Contains(query.Get("scope"), "openid") {
		t.Errorf("scope = %q", query.Get("scope"))
	}
	if provider.Name() != "work" {
		t.Errorf("name = %q, want the one it was configured under", provider.Name())
	}
}

func TestTradingTheCodeForAToken(t *testing.T) {
	idp := newIdentityProvider(t)
	provider := idp.provider(t, &OIDCConfig{})

	token, err := provider.ExchangeCode(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token.AccessToken != "access-1" {
		t.Errorf("token = %+v", token)
	}

	// What the provider has to be sent, or it answers with an error that
	// names none of it.
	if idp.tokenForm.Get("code") != "code-1" {
		t.Errorf("code = %q", idp.tokenForm.Get("code"))
	}
	if idp.tokenForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant type = %q", idp.tokenForm.Get("grant_type"))
	}
	// The redirect has to match the one the sign-in was started with — every
	// provider checks it, and a mismatch is the commonest reason a callback
	// fails after everything else was set up right.
	if idp.tokenForm.Get("redirect_uri") != "https://service.example.test/auth/callback" {
		t.Errorf("redirect = %q", idp.tokenForm.Get("redirect_uri"))
	}
}

func TestWhoTheProviderSaysTheyAre(t *testing.T) {
	idp := newIdentityProvider(t)
	provider := idp.provider(t, &OIDCConfig{})

	info, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "access-1"})
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}

	if info.ID != "user-1" {
		t.Errorf("id = %q, want the subject — it is what the account is keyed on", info.ID)
	}
	if info.Email != "someone@example.test" || !info.EmailVerified {
		t.Errorf("email = %q, verified = %v", info.Email, info.EmailVerified)
	}
	if info.Name != "Someone" || info.GivenName != "Some" || info.FamilyName != "One" {
		t.Errorf("name = %+v", info)
	}
	if info.Picture == "" || info.Locale != "en-NZ" {
		t.Errorf("info = %+v", info)
	}
	// Which provider an account came from: two people with the same email at
	// two providers are two accounts.
	if info.Provider != "work" {
		t.Errorf("provider = %q", info.Provider)
	}
	// The token is sent as a bearer credential; sent any other way the
	// provider answers 401 and the sign-in fails at the last step.
	if idp.bearer != "Bearer access-1" {
		t.Errorf("authorization = %q", idp.bearer)
	}
}

func TestAProviderThatNamesItsClaimsDifferently(t *testing.T) {
	// The reason the mapping exists: a corporate IdP that puts the email
	// under `upn` and the name under `displayName`. Without it, every user
	// arrives with no email — and an account matched by email is matched by
	// nothing.
	idp := newIdentityProvider(t)
	idp.userInfoAnswer = map[string]interface{}{
		"sub":         "user-1",
		"upn":         "someone@example.test",
		"displayName": "Someone",
	}

	provider := idp.provider(t, &OIDCConfig{
		Claims: map[string]string{"email": "upn", "name": "displayName"},
	})

	info, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "access-1"})
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info.Email != "someone@example.test" {
		t.Errorf("email = %q, want the one under upn", info.Email)
	}
	if info.Name != "Someone" {
		t.Errorf("name = %q", info.Name)
	}
	// The original claims are still there: a mapping adds a name for a value,
	// it does not take the provider's own away.
	if info.Raw["upn"] != "someone@example.test" {
		t.Errorf("the provider's own claims were dropped: %v", info.Raw)
	}
	// A mapping naming a claim the provider did not send leaves the target
	// alone rather than blanking it.
	blank := idp.provider(t, &OIDCConfig{Claims: map[string]string{"email": "not_sent"}})
	if info, err := blank.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "access-1"}); err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	} else if info.Email != "" {
		t.Errorf("email = %q, want nothing when the claim was not sent", info.Email)
	}
}

func TestAProviderThatRefusesToSayWhoTheyAre(t *testing.T) {
	// An expired or withdrawn token. The sign-in has to fail rather than
	// continue with an account of nobody.
	idp := newIdentityProvider(t)
	idp.userInfoStatus = http.StatusUnauthorized
	provider := idp.provider(t, &OIDCConfig{})

	if _, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "expired"}); err == nil {
		t.Error("a provider that refused the token produced a user anyway")
	}
}

func TestAnIssuerThatCannotBeDiscovered(t *testing.T) {
	// The configuration names an issuer that is not there, or is not an OIDC
	// provider at all. Failing at start-up is the point: the alternative is
	// discovering it at the first sign-in.
	provider := NewOIDCProvider(&OIDCConfig{
		Name:     "work",
		Issuer:   "http://127.0.0.1:1",
		ClientID: "client-1",
	}, "https://service.example.test/auth/callback")

	if err := provider.Initialize(context.Background()); err == nil {
		t.Error("an issuer nobody is running was discovered")
	}

	// Without discovery there is nowhere to send anybody, and asking for the
	// user info has nowhere to ask.
	if _, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "access-1"}); err == nil {
		t.Error("user info was fetched from a provider that was never discovered")
	}
}

func TestScopesThatWereAskedFor(t *testing.T) {
	// A provider only sends the claims the scopes cover, so a scope list that
	// is silently replaced is a set of claims that never arrive.
	idp := newIdentityProvider(t)
	provider := idp.provider(t, &OIDCConfig{Scopes: []string{"openid", "groups"}})

	scope := parseQuery(t, provider.GetAuthURL("state-1")).Get("scope")
	if !strings.Contains(scope, "groups") {
		t.Errorf("scope = %q, want the configured one", scope)
	}
}

func parseQuery(t *testing.T, rawURL string) url.Values {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %s: %v", rawURL, err)
	}
	return parsed.Query()
}
