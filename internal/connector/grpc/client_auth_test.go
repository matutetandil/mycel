package grpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// What a Mycel service sends when it is the one calling a gRPC service it does
// not own. Every managed gRPC endpoint wants something: a static token, an API
// key under a header of its choosing, or a client-credentials exchange against
// an authorisation server. None of it was covered, and a credential that never
// leaves is indistinguishable from one the server rejects.

func TestABearerTokenIsSentOnEveryCall(t *testing.T) {
	creds := newBearerCredentials("a-static-token")

	metadata, err := creds.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if metadata["authorization"] != "Bearer a-static-token" {
		t.Errorf("authorization = %q", metadata["authorization"])
	}
	// gRPC refuses to send credentials over a plain connection when this is
	// true, which would rule out every service on an internal network.
	if creds.RequireTransportSecurity() {
		t.Error("credentials cannot be sent over a plain connection")
	}
}

func TestAnAPIKeyGoesUnderTheHeaderTheServerExpects(t *testing.T) {
	// The name matters: a key under the wrong one is a key the server never
	// sees, and the answer is the same as sending nothing.
	creds := newAPIKeyCredentials("k-123", "x-tenant-key")

	metadata, err := creds.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if metadata["x-tenant-key"] != "k-123" {
		t.Errorf("metadata = %v, want the key under the configured name", metadata)
	}
	if creds.RequireTransportSecurity() {
		t.Error("credentials cannot be sent over a plain connection")
	}
}

func TestAnAPIKeyWithNoNameGetsTheUsualOne(t *testing.T) {
	metadata, err := newAPIKeyCredentials("k-123", "").GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if metadata["api-key"] != "k-123" {
		t.Errorf("metadata = %v, want the key under api-key", metadata)
	}
}

// tokenServer stands in for an authorisation server, recording what was asked
// of it and how often.
func tokenServer(t *testing.T, expiresIn int) (*httptest.Server, *int32, *string) {
	t.Helper()
	var exchanges int32
	var lastForm string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&exchanges, 1)
		_ = r.ParseForm()
		lastForm = r.Form.Encode()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "issued-token-" + string(rune('0'+n)),
			"token_type":   "Bearer",
			"expires_in":   expiresIn,
		})
	}))
	t.Cleanup(server.Close)
	return server, &exchanges, &lastForm
}

func TestATokenIsFetchedFromTheAuthorisationServer(t *testing.T) {
	server, exchanges, lastForm := tokenServer(t, 3600)

	creds := newOAuth2Credentials(&OAuth2Config{
		TokenURL:     server.URL,
		ClientID:     "mycel",
		ClientSecret: "s3cret",
		Scopes:       []string{"orders:read", "orders:write"},
	})

	metadata, err := creds.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if !strings.HasPrefix(metadata["authorization"], "Bearer issued-token-") {
		t.Errorf("authorization = %q, want the token the server issued", metadata["authorization"])
	}

	// The exchange itself: the grant, the credentials, and the scopes joined
	// the way the specification asks for them. A scope list sent wrong comes
	// back as a token that is refused by the service, not by this server.
	for _, want := range []string{
		"grant_type=client_credentials",
		"client_id=mycel",
		"client_secret=s3cret",
		"scope=orders%3Aread+orders%3Awrite",
	} {
		if !strings.Contains(*lastForm, want) {
			t.Errorf("the exchange did not carry %s (sent: %s)", want, *lastForm)
		}
	}

	// A second call reuses the token it was given. Without this, every gRPC
	// call becomes two calls, one of them to the authorisation server.
	if _, err := creds.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("the second call failed: %v", err)
	}
	if got := atomic.LoadInt32(exchanges); got != 1 {
		t.Errorf("%d token exchanges, want one: the token is not being reused", got)
	}
}

func TestATokenIsFetchedAgainOnceItHasExpired(t *testing.T) {
	// A token held past its life is a call that fails at the service with
	// nothing in this process to explain it.
	server, exchanges, _ := tokenServer(t, 3600)

	creds := newOAuth2Credentials(&OAuth2Config{TokenURL: server.URL, ClientID: "mycel"})
	if _, err := creds.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}

	// Move its expiry into the past, the way time passing would.
	oauth := creds.(*oauth2Credentials)
	oauth.mu.Lock()
	oauth.expiry = time.Now().Add(-time.Minute)
	oauth.mu.Unlock()

	if _, err := creds.GetRequestMetadata(context.Background()); err != nil {
		t.Fatalf("the call after expiry failed: %v", err)
	}
	if got := atomic.LoadInt32(exchanges); got != 2 {
		t.Errorf("%d token exchanges, want two: an expired token was sent again", got)
	}
}

func TestAnAuthorisationServerThatRefusesIsReported(t *testing.T) {
	// Rather than a call going out with no credentials and failing at the
	// service, where the reason is somebody else's log.
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refusing.Close()

	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not a token response"))
	}))
	defer garbage.Close()

	for name, url := range map[string]string{
		"refusing the credentials": refusing.URL,
		"answering with nonsense":  garbage.URL,
		"not answering at all":     "http://127.0.0.1:1/token",
		"not an address at all":    "://not-a-url",
	} {
		t.Run(name, func(t *testing.T) {
			creds := newOAuth2Credentials(&OAuth2Config{TokenURL: url, ClientID: "mycel"})
			if _, err := creds.GetRequestMetadata(context.Background()); err == nil {
				t.Error("the call went out as though it had credentials")
			}
		})
	}
}

func TestTheCredentialsAServiceIsConfiguredWithAreTheOnesItSends(t *testing.T) {
	server, _, _ := tokenServer(t, 3600)

	for name, config := range map[string]*ClientAuthConfig{
		"a bearer token": {Type: "bearer", Token: "t"},
		"an API key":     {Type: "api_key", APIKey: &ClientAPIKeyConfig{Key: "k"}},
		"oauth2":         {Type: "oauth2", OAuth2: &OAuth2Config{TokenURL: server.URL}},
		"client credentials by its other name": {
			Type: "client_credentials", OAuth2: &OAuth2Config{TokenURL: server.URL},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if BuildClientAuthOption(config) == nil {
				t.Error("nothing was attached to the connection, so the call goes out unauthenticated")
			}
		})
	}
}

func TestIncompleteCredentialsAttachNothing(t *testing.T) {
	// Half-configured authentication attaching a credential with an empty
	// token would be answered by the service with something less clear than
	// no credential at all.
	for name, config := range map[string]*ClientAuthConfig{
		"absent":                      nil,
		"no type":                     {},
		"a type nobody implements":    {Type: "kerberos"},
		"bearer with no token":        {Type: "bearer"},
		"an API key with no key":      {Type: "api_key", APIKey: &ClientAPIKeyConfig{}},
		"an API key with no block":    {Type: "api_key"},
		"oauth2 with nothing to call": {Type: "oauth2"},
	} {
		t.Run(name, func(t *testing.T) {
			if BuildClientAuthOption(config) != nil {
				t.Error("a credential was attached with nothing in it")
			}
		})
	}
}
