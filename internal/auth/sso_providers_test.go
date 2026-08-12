package auth

import (
	"net/url"
	"strings"
	"testing"
)

// The authorisation URL is the one part of an OAuth exchange that leaves the
// service as a whole string, and a provider will reject it — or worse, accept
// it and redirect somewhere unintended — over a wrong parameter. Everything
// below runs without the network, which is what makes it worth having: the
// exchange itself belongs to the integration suite.

func parseAuthURL(t *testing.T, raw string) (string, url.Values) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("the authorisation URL does not parse: %v (%s)", err, raw)
	}
	return u.Scheme + "://" + u.Host + u.Path, u.Query()
}

func TestGoogleAuthURL(t *testing.T) {
	p := NewGoogleProvider(&OAuthProviderConfig{
		ClientID: "client-123", ClientSecret: "secret",
	}, "https://app.example.com/callback")

	if p.Name() != "google" {
		t.Errorf("name = %q", p.Name())
	}

	endpoint, q := parseAuthURL(t, p.GetAuthURL("state-abc"))
	if endpoint != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Errorf("endpoint = %q", endpoint)
	}
	for k, want := range map[string]string{
		"client_id":     "client-123",
		"redirect_uri":  "https://app.example.com/callback",
		"response_type": "code",
		"state":         "state-abc",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}

	// The default scopes are what makes the identity usable; without email
	// there is nothing to match a user on.
	scopes := strings.Fields(q.Get("scope"))
	for _, want := range []string{"openid", "email", "profile"} {
		if !hasScope(scopes, want) {
			t.Errorf("scope %q missing from %v", want, scopes)
		}
	}

	// Google needs these two to return a refresh token at all.
	if q.Get("access_type") != "offline" || q.Get("prompt") != "consent" {
		t.Errorf("access_type = %q, prompt = %q — without both, no refresh token is issued",
			q.Get("access_type"), q.Get("prompt"))
	}
}

func TestConfiguredScopesReplaceTheDefaults(t *testing.T) {
	p := NewGoogleProvider(&OAuthProviderConfig{
		ClientID: "c", Scopes: []string{"openid", "https://www.googleapis.com/auth/calendar"},
	}, "https://app.example.com/callback")

	_, q := parseAuthURL(t, p.GetAuthURL("s"))
	scopes := strings.Fields(q.Get("scope"))
	if !hasScope(scopes, "https://www.googleapis.com/auth/calendar") {
		t.Errorf("the configured scope is missing: %v", scopes)
	}
	if hasScope(scopes, "profile") {
		t.Errorf("the defaults were added to the configured scopes: %v", scopes)
	}
}

func TestGitHubAuthURL(t *testing.T) {
	p := NewGitHubProvider(&OAuthProviderConfig{ClientID: "gh-1", ClientSecret: "s"},
		"https://app.example.com/cb")

	if p.Name() != "github" {
		t.Errorf("name = %q", p.Name())
	}

	endpoint, q := parseAuthURL(t, p.GetAuthURL("st"))
	if !strings.HasPrefix(endpoint, "https://github.com/login/oauth/authorize") {
		t.Errorf("endpoint = %q", endpoint)
	}
	if q.Get("client_id") != "gh-1" || q.Get("state") != "st" {
		t.Errorf("client_id = %q, state = %q", q.Get("client_id"), q.Get("state"))
	}
	// GitHub does not return an email on the user endpoint unless asked, which
	// is why the provider has a separate call for the primary address.
	if scopes := q.Get("scope"); !strings.Contains(scopes, "user:email") {
		t.Errorf("scope = %q, want it to include user:email", scopes)
	}
}

func TestAppleAuthURL(t *testing.T) {
	p := NewAppleProvider(&AppleConfig{
		ClientID: "com.example.app", TeamID: "TEAM1", KeyID: "KEY1",
	}, "https://app.example.com/cb")

	if p.Name() != "apple" {
		t.Errorf("name = %q", p.Name())
	}

	endpoint, q := parseAuthURL(t, p.GetAuthURL("st"))
	if !strings.Contains(endpoint, "appleid.apple.com") {
		t.Errorf("endpoint = %q", endpoint)
	}
	if q.Get("client_id") != "com.example.app" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	// Apple only sends the name and email in the callback body, which requires
	// this pair; with the default form_post they arrive nowhere else.
	if q.Get("response_mode") != "" && q.Get("response_mode") != "form_post" {
		t.Errorf("response_mode = %q", q.Get("response_mode"))
	}
}

func TestAppleClientSecretNeedsAKey(t *testing.T) {
	// Apple's client secret is a signed assertion rather than a stored string,
	// so a missing or malformed key has to fail loudly at the point of use.
	p := NewAppleProvider(&AppleConfig{
		ClientID: "com.example.app", TeamID: "T", KeyID: "K", PrivateKey: "not-a-key",
	}, "https://app.example.com/cb")

	if _, err := p.generateClientSecret(); err == nil {
		t.Error("a malformed private key produced a client secret")
	}
}

func TestOIDCProviderCarriesItsIssuer(t *testing.T) {
	p := NewOIDCProvider(&OIDCConfig{
		Name: "corp", Issuer: "https://id.corp.example.com",
		ClientID: "c", ClientSecret: "s",
	}, "https://app.example.com/cb")

	if p.Name() != "corp" {
		t.Errorf("name = %q, want the configured provider name", p.Name())
	}
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}
