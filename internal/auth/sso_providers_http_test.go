package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A social login is a conversation with somebody else's server: an
// authorization code goes out, a token comes back, a profile is fetched and
// mapped onto a user. The provider addresses are fixed — they belong to Google
// and GitHub — so the way to exercise the mapping is to send the requests
// somewhere we control and answer them the way the real one does.
//
// The mapping is where the consequences are. An identifier read from the wrong
// field means every returning user looks new; an email read from the wrong one
// means a link to the wrong account.

// redirectTo sends every request, whatever its host, to the test server.
type redirectTo struct{ target *url.URL }

func (r redirectTo) RoundTrip(req *http.Request) (*http.Response, error) {
	routed := req.Clone(req.Context())
	routed.URL.Scheme = r.target.Scheme
	routed.URL.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(routed)
}

func clientFor(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parsing the test server address: %v", err)
	}
	return &http.Client{Transport: redirectTo{target: target}, Timeout: 5 * time.Second}
}

// providerServer answers the token and profile endpoints of whichever provider
// is being stood in for, keyed on path.
func providerServer(t *testing.T, routes map[string]interface{}) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, ok := routes[req.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no such endpoint"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAnAuthorizationCodeIsExchangedForATokenAndAProfile(t *testing.T) {
	server := providerServer(t, map[string]interface{}{
		"/token": map[string]interface{}{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"token_type":    "Bearer",
			"expires_in":    3600,
		},
		"/userinfo": map[string]interface{}{
			"sub":            "google-user-1",
			"email":          "someone@example.com",
			"email_verified": true,
			"name":           "Someone",
			"given_name":     "Some",
			"family_name":    "One",
			"picture":        "https://example.com/a.png",
			"locale":         "en-NZ",
		},
	})

	service := NewOAuth2Service()
	service.httpClient = clientFor(t, server)

	config := &OAuth2Config{
		ClientID: "id", ClientSecret: "secret",
		TokenURL: "https://accounts.example.com/token", UserInfoURL: "https://accounts.example.com/userinfo",
		RedirectURL: "https://app.example.com/auth/callback",
	}

	token, err := service.ExchangeCode(context.Background(), config, "the-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token.AccessToken != "at-1" || token.RefreshToken != "rt-1" {
		t.Errorf("token = %+v", token)
	}
	if token.ExpiresIn != 3600 {
		t.Errorf("expires_in = %d", token.ExpiresIn)
	}

	raw, err := service.GetUserInfo(context.Background(), config.UserInfoURL, token.AccessToken)
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if raw["email"] != "someone@example.com" {
		t.Errorf("profile = %v", raw)
	}
}

func TestAProviderThatRefusesTheCodeIsReported(t *testing.T) {
	// A code that was already used, or that belongs to another client. Reading
	// this as success would sign somebody in with an empty token.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	service := NewOAuth2Service()
	service.httpClient = clientFor(t, server)

	_, err := service.ExchangeCode(context.Background(), &OAuth2Config{
		TokenURL: "https://accounts.example.com/token",
	}, "a-used-code")
	if err == nil {
		t.Fatal("a refused code was exchanged successfully")
	}
}

func TestTheAuthorizationURLCarriesWhatTheProviderNeeds(t *testing.T) {
	service := NewOAuth2Service()
	raw := service.GetAuthURL(&OAuth2Config{
		ClientID:    "the-client",
		AuthURL:     "https://accounts.example.com/authorize",
		RedirectURL: "https://app.example.com/auth/callback",
		Scopes:      []string{"openid", "email"},
	}, "the-state")

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("the authorization URL does not parse: %v", err)
	}
	query := parsed.Query()

	// The state is what ties the callback to the browser that started it;
	// losing it is what a CSRF on the login flow looks like.
	if query.Get("state") != "the-state" {
		t.Errorf("state = %q", query.Get("state"))
	}
	if query.Get("client_id") != "the-client" {
		t.Errorf("client_id = %q", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "https://app.example.com/auth/callback" {
		t.Errorf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("response_type") != "code" {
		t.Errorf("response_type = %q", query.Get("response_type"))
	}
	if query.Get("scope") != "openid email" {
		t.Errorf("scope = %q, want them space separated as the specification says", query.Get("scope"))
	}
}

func TestGoogleProfileFieldsReachTheUser(t *testing.T) {
	server := providerServer(t, map[string]interface{}{
		"/oauth2/v3/userinfo": map[string]interface{}{
			"sub":            "google-user-1",
			"email":          "someone@example.com",
			"email_verified": true,
			"name":           "Someone",
			"given_name":     "Some",
			"family_name":    "One",
			"picture":        "https://example.com/a.png",
			"locale":         "en-NZ",
		},
	})

	provider := NewGoogleProvider(&OAuthProviderConfig{ClientID: "id", ClientSecret: "secret"},
		"https://app.example.com/auth/callback")
	provider.oauth.httpClient = clientFor(t, server)

	info, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "at-1"})
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}

	// sub, not email: the identifier has to be the one that does not change
	// when somebody changes their address, or a returning user looks new.
	if info.ID != "google-user-1" {
		t.Errorf("id = %q, want the subject claim", info.ID)
	}
	if info.Email != "someone@example.com" || !info.EmailVerified {
		t.Errorf("email = %q verified = %v", info.Email, info.EmailVerified)
	}
	if info.Name != "Someone" || info.GivenName != "Some" || info.FamilyName != "One" {
		t.Errorf("name = %+v", info)
	}
	if info.Provider != ProviderGoogle {
		t.Errorf("provider = %q", info.Provider)
	}
	if info.Raw == nil {
		t.Error("the whole profile was not kept, so nothing else about it can be used")
	}
}

func TestAnUnverifiedGoogleEmailIsReportedAsSuch(t *testing.T) {
	// Not an error — it is the account linking that decides what to do with it.
	server := providerServer(t, map[string]interface{}{
		"/oauth2/v3/userinfo": map[string]interface{}{
			"sub": "google-user-1", "email": "someone@example.com", "email_verified": false,
		},
	})
	provider := NewGoogleProvider(&OAuthProviderConfig{}, "https://app.example.com/cb")
	provider.oauth.httpClient = clientFor(t, server)

	info, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info.EmailVerified {
		t.Error("an unverified address was reported as verified")
	}
}

func TestAGitHubIdentifierIsCarriedAsWrittenDespiteBeingANumber(t *testing.T) {
	// GitHub's id is a JSON number and every other provider's is a string. It
	// has to survive as the same text each time, since it is what a returning
	// user is recognised by.
	server := providerServer(t, map[string]interface{}{
		"/user": map[string]interface{}{
			"id": 12345678, "login": "someone", "email": "someone@example.com",
			"name": "Someone", "avatar_url": "https://example.com/a.png",
		},
	})
	provider := NewGitHubProvider(&OAuthProviderConfig{}, "https://app.example.com/cb")
	provider.oauth.httpClient = clientFor(t, server)
	provider.httpClient = clientFor(t, server)

	info, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info.ID != "12345678" {
		t.Errorf("id = %q, want the number as text", info.ID)
	}
	if strings.Contains(info.ID, "e+") || strings.Contains(info.ID, ".") {
		t.Errorf("id = %q, which is a float rendering and would differ between logins", info.ID)
	}
	if info.Email != "someone@example.com" {
		t.Errorf("email = %q", info.Email)
	}
}

func TestGitHubIsAskedForAnAddressWhenTheProfileHidesOne(t *testing.T) {
	// Most GitHub accounts keep their address private, so the profile comes
	// back with none and the addresses endpoint has to be asked. Without this
	// there is no email at all, and nothing to match an existing account on.
	server := providerServer(t, map[string]interface{}{
		"/user": map[string]interface{}{"id": 1, "login": "someone", "name": "Someone"},
		"/user/emails": []map[string]interface{}{
			{"email": "old@example.com", "primary": false, "verified": true},
			{"email": "unverified@example.com", "primary": true, "verified": false},
			{"email": "someone@example.com", "primary": true, "verified": true},
		},
	})
	provider := NewGitHubProvider(&OAuthProviderConfig{}, "https://app.example.com/cb")
	provider.oauth.httpClient = clientFor(t, server)
	provider.httpClient = clientFor(t, server)

	info, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}

	// Primary and verified, both — a primary address that is not verified is
	// one somebody typed, not one they proved.
	if info.Email != "someone@example.com" {
		t.Errorf("email = %q, want the primary verified one", info.Email)
	}
	if !info.EmailVerified {
		t.Error("the address fetched from the addresses endpoint is not marked verified")
	}
}

func TestGitHubWithNoUsableAddressLeavesItEmpty(t *testing.T) {
	// Rather than picking one that was not verified, which is what an account
	// would then be matched on.
	server := providerServer(t, map[string]interface{}{
		"/user": map[string]interface{}{"id": 1, "login": "someone"},
		"/user/emails": []map[string]interface{}{
			{"email": "unverified@example.com", "primary": true, "verified": false},
		},
	})
	provider := NewGitHubProvider(&OAuthProviderConfig{}, "https://app.example.com/cb")
	provider.oauth.httpClient = clientFor(t, server)
	provider.httpClient = clientFor(t, server)

	info, err := provider.GetUserInfo(context.Background(), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}
	if info.Email != "" {
		t.Errorf("email = %q, want none — no verified address was offered", info.Email)
	}
	if info.EmailVerified {
		t.Error("a user with no address was reported as having a verified one")
	}
}

func TestAProviderThatAnswersWithNonsenseIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>an error page</html>"))
	}))
	defer server.Close()

	service := NewOAuth2Service()
	service.httpClient = clientFor(t, server)

	if _, err := service.GetUserInfo(context.Background(), "https://accounts.example.com/userinfo", "at"); err == nil {
		t.Error("a profile that is not JSON was accepted")
	}
}

func TestEachStateIsDifferent(t *testing.T) {
	// The state is what ties a callback to the browser that started the flow.
	// A repeat would let one login's callback complete another's.
	service := NewOAuth2Service()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		state, err := service.GenerateState()
		if err != nil {
			t.Fatalf("GenerateState: %v", err)
		}
		if len(state) < 32 {
			t.Fatalf("state is %d characters, which is guessable", len(state))
		}
		if seen[state] {
			t.Fatalf("a state was generated twice")
		}
		seen[state] = true
	}
}

func TestAnIdentifierIsRenderedAsTheTextItIs(t *testing.T) {
	// Whatever shape a provider sends it in. The rendering is what the account
	// is stored under, so it has to be the identifier the provider would
	// recognise rather than a formatting of it.
	for name, tc := range map[string]struct {
		raw  interface{}
		want string
	}{
		"a JSON number, which is how GitHub sends it":          {float64(12345678), "12345678"},
		"a small JSON number":                                  {float64(42), "42"},
		"a large one, past where %v switches to exponent form": {float64(1234567890123), "1234567890123"},
		"a string, which is how everyone else sends it":        {"google-user-1", "google-user-1"},
		"nothing at all":                                       {nil, ""},
		"an integer":                                           {7, "7"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := providerID(tc.raw); got != tc.want {
				t.Errorf("providerID(%v) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
