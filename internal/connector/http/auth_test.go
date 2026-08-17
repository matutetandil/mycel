package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Outgoing authentication has one failure mode worth designing around: it does
// not fail. A credential that is not applied produces a request that goes out
// bare, and the answer is a 401 from somewhere else — reported against the
// destination, not against the configuration that dropped it.

// recorder answers every request and remembers what it was sent.
type recorder struct {
	*httptest.Server
	last atomic.Value // *http.Request
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()
	r := &recorder{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		clone := req.Clone(context.Background())
		r.last.Store(clone)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *recorder) request(t *testing.T) *http.Request {
	t.Helper()
	req, _ := r.last.Load().(*http.Request)
	if req == nil {
		t.Fatal("the destination was never called")
	}
	return req
}

func callWith(t *testing.T, auth *AuthConfig, baseURL string) *Connector {
	t.Helper()
	c := New("api", baseURL, time.Second, auth, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	if _, err := c.Read(context.Background(), connector.Query{Target: "GET /orders"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	return c
}

func TestEachKindOfCredentialReachesTheRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		auth  *AuthConfig
		check func(*testing.T, *http.Request)
	}{
		"a bearer token": {
			auth: &AuthConfig{Type: AuthTypeBearer, Token: "t0ken"},
			check: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("Authorization"); got != "Bearer t0ken" {
					t.Errorf("Authorization = %q", got)
				}
			},
		},
		"basic credentials": {
			auth: &AuthConfig{Type: AuthTypeBasic, Username: "someone", Password: "s3cret"},
			check: func(t *testing.T, req *http.Request) {
				user, pass, ok := req.BasicAuth()
				if !ok || user != "someone" || pass != "s3cret" {
					t.Errorf("basic auth = %q/%q ok=%v", user, pass, ok)
				}
			},
		},
		"an api key in the default header": {
			auth: &AuthConfig{Type: AuthTypeAPIKey, APIKey: "k3y"},
			check: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("X-API-Key"); got != "k3y" {
					t.Errorf("X-API-Key = %q, want the key under the default header", got)
				}
			},
		},
		"an api key in a header the destination names": {
			auth: &AuthConfig{Type: AuthTypeAPIKey, APIKey: "k3y", APIKeyHeader: "X-Mercury-Token"},
			check: func(t *testing.T, req *http.Request) {
				if got := req.Header.Get("X-Mercury-Token"); got != "k3y" {
					t.Errorf("the named header carried %q", got)
				}
				if req.Header.Get("X-API-Key") != "" {
					t.Error("the key was also sent under the default header")
				}
			},
		},
		"an api key in the query string": {
			auth: &AuthConfig{Type: AuthTypeAPIKey, APIKey: "k3y", APIKeyQuery: "api_key"},
			check: func(t *testing.T, req *http.Request) {
				if got := req.URL.Query().Get("api_key"); got != "k3y" {
					t.Errorf("query = %q, want the key", req.URL.RawQuery)
				}
				if req.Header.Get("X-API-Key") != "" {
					t.Error("a key meant for the query string was also sent as a header")
				}
			},
		},
		"no credentials at all": {
			auth: &AuthConfig{Type: AuthTypeNone},
			check: func(t *testing.T, req *http.Request) {
				if req.Header.Get("Authorization") != "" {
					t.Error("an unauthenticated connector sent an Authorization header")
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := newRecorder(t)
			callWith(t, tc.auth, server.URL)
			tc.check(t, server.request(t))
		})
	}
}

func TestAConnectorWithNoAuthBlockSendsNothing(t *testing.T) {
	server := newRecorder(t)
	callWith(t, nil, server.URL)
	if got := server.request(t).Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none", got)
	}
}

// tokenServer stands in for an OAuth2 provider and counts how often it is asked.
type tokenServer struct {
	*httptest.Server
	issued    atomic.Int64
	lastGrant atomic.Value // string
	lastForm  atomic.Value // url.Values
	expiresIn int
	status    int
}

func newTokenServer(t *testing.T, expiresIn int) *tokenServer {
	t.Helper()
	ts := &tokenServer{expiresIn: expiresIn, status: http.StatusOK}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		ts.lastGrant.Store(req.Form.Get("grant_type"))
		ts.lastForm.Store(req.Form)

		if ts.status != http.StatusOK {
			w.WriteHeader(ts.status)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}

		n := ts.issued.Add(1)
		w.Header().Set("Content-Type", "application/json")
		body := map[string]interface{}{
			"access_token": fmt.Sprintf("token-%d", n),
			"token_type":   "Bearer",
		}
		if ts.expiresIn != 0 {
			body["expires_in"] = ts.expiresIn
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestAClientCredentialsTokenIsFetchedAtStartupAndSent(t *testing.T) {
	// Fetching at startup is what makes a bad client secret a startup failure
	// rather than a 401 on the first real request, hours later.
	tokens := newTokenServer(t, 3600)
	destination := newRecorder(t)

	callWith(t, &AuthConfig{
		Type:         AuthTypeClientCredentials,
		TokenURL:     tokens.URL,
		ClientID:     "id",
		ClientSecret: "secret",
		Scopes:       []string{"orders:read", "orders:write"},
	}, destination.URL)

	if tokens.issued.Load() != 1 {
		t.Errorf("the provider was asked %d times for one request", tokens.issued.Load())
	}
	if got := tokens.lastGrant.Load(); got != "client_credentials" {
		t.Errorf("grant_type = %v", got)
	}
	if got := destination.request(t).Header.Get("Authorization"); got != "Bearer token-1" {
		t.Errorf("the destination was sent %q", got)
	}

	// The scopes are what the token is granted for; dropping them produces a
	// token that authenticates and is refused by the destination.
	form, _ := tokens.lastForm.Load().(interface{ Get(string) string })
	if form != nil && !strings.Contains(form.Get("scope"), "orders:read") {
		t.Errorf("scope = %q, want the scopes that were configured", form.Get("scope"))
	}
}

func TestAValidTokenIsReusedRatherThanRefetched(t *testing.T) {
	// A provider asked for a token on every request is both slow and a good way
	// to be rate limited by an identity provider.
	tokens := newTokenServer(t, 3600)
	destination := newRecorder(t)

	c := New("api", destination.URL, time.Second, &AuthConfig{
		Type: AuthTypeClientCredentials, TokenURL: tokens.URL,
		ClientID: "id", ClientSecret: "secret",
	}, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, err := c.Read(context.Background(), connector.Query{Target: "GET /orders"}); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if tokens.issued.Load() != 1 {
		t.Errorf("the provider was asked %d times for five requests", tokens.issued.Load())
	}
}

func TestAnExpiredTokenIsReplacedBeforeTheRequestGoesOut(t *testing.T) {
	tokens := newTokenServer(t, 3600)
	destination := newRecorder(t)

	c := New("api", destination.URL, time.Second, &AuthConfig{
		Type: AuthTypeClientCredentials, TokenURL: tokens.URL,
		ClientID: "id", ClientSecret: "secret",
	}, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Age the token past its lifetime.
	c.mu.Lock()
	c.tokenExpiry = time.Now().Add(-time.Minute)
	c.mu.Unlock()

	if _, err := c.Read(context.Background(), connector.Query{Target: "GET /orders"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if tokens.issued.Load() != 2 {
		t.Errorf("the provider was asked %d times, want a second token", tokens.issued.Load())
	}
	if got := destination.request(t).Header.Get("Authorization"); got != "Bearer token-2" {
		t.Errorf("the destination was sent %q, want the fresh token", got)
	}
}

func TestATokenWithNoStatedLifetimeIsStillRefreshedEventually(t *testing.T) {
	// A provider that states no lifetime is not saying the token lasts for
	// ever. Treating it that way means the service works until the token
	// quietly expires and then fails every request until it is restarted.
	for name, auth := range map[string]*AuthConfig{
		"client credentials": {Type: AuthTypeClientCredentials, ClientID: "id", ClientSecret: "secret"},
		"refresh token":      {Type: AuthTypeOAuth2, RefreshToken: "r3fresh"},
	} {
		t.Run(name, func(t *testing.T) {
			tokens := newTokenServer(t, 0) // no expires_in in the response
			destination := newRecorder(t)
			auth.TokenURL = tokens.URL

			c := New("api", destination.URL, time.Second, auth, nil, 1)
			if err := c.Connect(context.Background()); err != nil {
				t.Fatalf("Connect: %v", err)
			}

			c.mu.RLock()
			expiry := c.tokenExpiry
			c.mu.RUnlock()
			if expiry.IsZero() {
				t.Fatal("the token was recorded as never expiring, so it would never be refreshed")
			}
			if until := time.Until(expiry); until > 2*time.Hour {
				t.Errorf("the token is treated as good for %v", until)
			}
		})
	}
}

func TestARefreshUsesTheRefreshGrantAndKeepsANewRefreshToken(t *testing.T) {
	// A provider that rotates refresh tokens invalidates the old one, so
	// keeping the original means the next refresh fails and the connector is
	// locked out until restart.
	var issued atomic.Int64
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		if req.Form.Get("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n := issued.Add(1)
		// The second call must present the token handed out by the first.
		if n > 1 && req.Form.Get("refresh_token") != "rotated-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"the old refresh token was reused"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  fmt.Sprintf("token-%d", n),
			"refresh_token": fmt.Sprintf("rotated-%d", n),
			"expires_in":    3600,
		})
	}))
	defer tokens.Close()
	destination := newRecorder(t)

	c := New("api", destination.URL, time.Second, &AuthConfig{
		Type: AuthTypeOAuth2, TokenURL: tokens.URL, RefreshToken: "original",
	}, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	c.mu.Lock()
	c.tokenExpiry = time.Now().Add(-time.Minute)
	c.mu.Unlock()

	if _, err := c.Read(context.Background(), connector.Query{Target: "GET /orders"}); err != nil {
		t.Fatalf("the second refresh failed, so the rotated token was not kept: %v", err)
	}
	if got := destination.request(t).Header.Get("Authorization"); got != "Bearer token-2" {
		t.Errorf("the destination was sent %q", got)
	}
}

func TestAProviderThatRefusesStopsTheServiceStarting(t *testing.T) {
	// Starting anyway would produce a service that answers every request with
	// somebody else's 401.
	tokens := newTokenServer(t, 3600)
	tokens.status = http.StatusUnauthorized

	for name, auth := range map[string]*AuthConfig{
		"client credentials": {Type: AuthTypeClientCredentials, TokenURL: tokens.URL, ClientID: "id", ClientSecret: "wrong"},
		"refresh token":      {Type: AuthTypeOAuth2, TokenURL: tokens.URL, RefreshToken: "expired"},
	} {
		t.Run(name, func(t *testing.T) {
			c := New("api", "http://example.invalid", time.Second, auth, nil, 1)
			err := c.Connect(context.Background())
			if err == nil {
				t.Fatal("the connector started although the provider refused it")
			}
			if !strings.Contains(err.Error(), "401") && !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
				t.Errorf("error = %q, want the provider's answer", err)
			}
		})
	}
}

func TestClientCredentialsWithoutCredentialsIsRefused(t *testing.T) {
	c := New("api", "http://example.invalid", time.Second, &AuthConfig{
		Type: AuthTypeClientCredentials, TokenURL: "http://provider.invalid/token",
	}, nil, 1)
	err := c.Connect(context.Background())
	if err == nil {
		t.Fatal("a client credentials grant with no client was accepted")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Errorf("error = %q, want it to name what is missing", err)
	}
}

func TestAGrantWithNoProviderIsRefused(t *testing.T) {
	for name, auth := range map[string]*AuthConfig{
		"client credentials": {Type: AuthTypeClientCredentials, ClientID: "id", ClientSecret: "s"},
		"refresh token":      {Type: AuthTypeOAuth2, RefreshToken: "r"},
	} {
		t.Run(name, func(t *testing.T) {
			c := New("api", "http://example.invalid", time.Second, auth, nil, 1)
			err := c.Connect(context.Background())
			if err == nil {
				t.Fatal("a grant with no token URL was accepted")
			}
			if !strings.Contains(err.Error(), "token URL") {
				t.Errorf("error = %q, want it to name the missing address", err)
			}
		})
	}
}

// A 4xx says the request was the problem and a 5xx says the destination was.
// The runtime asks the difference to decide whether replaying could ever help:
// the retry budget stops on one, and an MQ consumer drops rather than
// redelivers, which is what keeps a rejected payload out of an endless loop.

func TestTheStatusDecidesWhetherReplayingCouldHelp(t *testing.T) {
	for status, permanent := range map[int]bool{
		400: true, 401: true, 403: true, 404: true, 422: true, 429: true,
		500: false, 502: false, 503: false, 504: false,
	} {
		err := &HTTPError{StatusCode: status, Status: fmt.Sprintf("%d", status)}
		if got := err.IsPermanent(); got != permanent {
			t.Errorf("%d permanent = %v, want %v", status, got, permanent)
		}
		if got := connector.IsPermanent(fmt.Errorf("calling the API: %w", err)); got != permanent {
			t.Errorf("%d wrapped permanent = %v, want %v", status, got, permanent)
		}
		if got := isClientError(err); got != permanent {
			t.Errorf("%d client error = %v, want %v", status, got, permanent)
		}
	}

	// Anything that is not an answer at all — a refused connection — is worth
	// another attempt, since the destination may simply be restarting.
	if connector.IsPermanent(fmt.Errorf("connection refused")) {
		t.Error("a connection failure was treated as the request's own fault")
	}
	if isClientError(fmt.Errorf("connection refused")) {
		t.Error("a connection failure was reported as a client error")
	}
}

func TestTheConnectorDescribesItself(t *testing.T) {
	c := New("orders_api", "http://example.invalid", time.Second, nil, nil, 1)
	if c.Name() != "orders_api" {
		t.Errorf("name = %q", c.Name())
	}
	if c.Type() != "http" {
		t.Errorf("type = %q", c.Type())
	}
}

func TestHealthFollowsTheDestination(t *testing.T) {
	server := newRecorder(t)
	c := New("api", server.URL, time.Second, nil, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("a reachable destination reported unhealthy: %v", err)
	}

	server.Close()
	if err := c.Health(context.Background()); err == nil {
		t.Error("a destination that is gone reported healthy")
	}
}
