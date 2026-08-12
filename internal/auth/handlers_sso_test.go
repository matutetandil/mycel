package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Single sign-on was configurable, implemented and unreachable: no route led to
// the service, so writing a social or sso block produced provider objects
// nothing could call. These cover the layer that was missing.

func ssoManager(t *testing.T, cfg *Config) *Manager {
	t.Helper()
	if cfg.JWT == nil {
		cfg.JWT = &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"}
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func socialConfig() *Config {
	return &Config{
		Preset:  "development",
		BaseURL: "https://app.example.com",
		Social: &SocialConfig{
			Google: &OAuthProviderConfig{ClientID: "cid", ClientSecret: "secret"},
		},
	}
}

func ssoServer(t *testing.T, cfg *Config) (*httptest.Server, *Manager) {
	t.Helper()
	m := ssoManager(t, cfg)
	mux := http.NewServeMux()
	NewHandler(m).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, m
}

func TestWritingASocialBlockIsEnoughToStartAFlow(t *testing.T) {
	srv, _ := ssoServer(t, socialConfig())

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/auth/social/google")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want a redirect to the provider", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "https://accounts.google.com/") {
		t.Errorf("redirected to %q", location)
	}
	// The address the provider will send the browser back to has to be
	// absolute and has to be the route that is actually mounted, or the flow
	// fails at the provider with an error naming neither.
	if !strings.Contains(location, "redirect_uri=https%3A%2F%2Fapp.example.com%2Fauth%2Fsocial%2Fcallback") {
		t.Errorf("redirect_uri is wrong in %q", location)
	}
}

func TestAnUnknownProviderSaysWhichExist(t *testing.T) {
	srv, _ := ssoServer(t, socialConfig())

	resp, err := http.Get(srv.URL + "/auth/social/gitlab")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	msg, _ := json.Marshal(body)
	if !strings.Contains(string(msg), "google") {
		t.Errorf("the error does not name the configured providers: %s", msg)
	}
}

func TestTheCallbackRefusesWhatItCannotComplete(t *testing.T) {
	srv, _ := ssoServer(t, socialConfig())

	for _, tc := range []struct {
		name  string
		query string
		want  int
	}{
		{"no state and no code", "", http.StatusBadRequest},
		{"a state nobody issued", "?state=invented&code=abc", http.StatusUnauthorized},
		// A provider reports a refusal by returning here, not by failing the
		// request, so a denied consent screen arrives as an ordinary callback.
		{"the user refused", "?error=access_denied&error_description=User+refused", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + "/auth/social/callback" + tc.query)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestWithoutAProviderTheRoutesAreNotMounted(t *testing.T) {
	// A configuration with no social or sso block should not grow endpoints
	// that can only answer that nothing is configured.
	srv, m := ssoServer(t, &Config{Preset: "development"})
	if m.SSO() != nil {
		t.Error("an SSO service was built with no provider configured")
	}

	resp, err := http.Get(srv.URL + "/auth/social/google")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTheCallbackURLFollowsTheConfiguration(t *testing.T) {
	// Written once, used twice: the mounted route and the redirect_uri handed
	// to the provider both come from here, so they cannot disagree.
	svc := NewSSOService(&Config{
		BaseURL: "https://app.example.com/",
		Endpoints: &EndpointsConfig{
			Prefix:         "/identity",
			SocialCallback: &EndpointConfig{Path: "/back", Enabled: true},
		},
	}, NewMemoryLinkedAccountStore(), NewMemoryUserStore(), nil)

	if got := svc.CallbackURL(false); got != "https://app.example.com/identity/back" {
		t.Errorf("callback = %q", got)
	}
	// The OIDC family keeps its own default path when none is configured.
	if got := svc.CallbackURL(true); got != "https://app.example.com/identity/sso/callback" {
		t.Errorf("oidc callback = %q", got)
	}
}
