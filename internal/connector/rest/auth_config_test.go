package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Reading the auth block on a REST connector.
//
// This is the block that decides who may call a service, and it is read into
// three shapes depending on one word. A setting dropped here does not fail
// loudly — it produces a service that checks less than the file says it does,
// which is the failure nobody sees until somebody else does.

func TestAnAuthTypeIsReadHoweverItWasSpelt(t *testing.T) {
	// The word is also the name of a scheme, so people write it the way the
	// documentation prints it. Read strictly, the settings underneath went
	// unparsed and every request was turned away with "unknown auth type"
	// while the file looked right.
	for _, written := range []string{"jwt", "JWT", "Jwt"} {
		cfg, err := AuthConfigFromMap(map[string]interface{}{"type": written, "secret": "s"})
		if err != nil {
			t.Fatalf("%q: %v", written, err)
		}
		if cfg.Type != "jwt" {
			t.Errorf("%q read as %q", written, cfg.Type)
		}
		if cfg.JWT == nil {
			t.Errorf("%q left the settings underneath unread", written)
		}
	}

	// A type the server cannot honour is refused where it is written, not
	// discovered per request.
	if _, err := AuthConfigFromMap(map[string]interface{}{"type": "oauth2"}); err == nil {
		t.Error("an auth type this connector cannot check was accepted")
	} else if !strings.Contains(err.Error(), "jwt") {
		t.Errorf("the error does not say what is available: %v", err)
	}

	// No type at all is a connector that checks nothing, which is allowed:
	// the block may exist only to name public paths.
	cfg, err := AuthConfigFromMap(map[string]interface{}{
		"public": []interface{}{"/health"},
	})
	if err != nil {
		t.Fatalf("AuthConfigFromMap: %v", err)
	}
	if cfg.Type != "" || len(cfg.Public) != 1 {
		t.Errorf("config = %+v", cfg)
	}
}

func TestWhatAJWTIsCheckedAgainst(t *testing.T) {
	cfg, err := AuthConfigFromMap(map[string]interface{}{
		"type":       "jwt",
		"secret":     "shared-secret",
		"jwks_url":   "https://issuer.example.test/.well-known/jwks.json",
		"issuer":     "https://issuer.example.test/",
		"header":     "X-Token",
		"scheme":     "Token",
		"audience":   []interface{}{"orders", "admin"},
		"algorithms": []interface{}{"RS256", "ES256"},
	})
	if err != nil {
		t.Fatalf("AuthConfigFromMap: %v", err)
	}

	jwt := cfg.JWT
	if jwt.Secret != "shared-secret" || jwt.JWKSURL == "" || jwt.Issuer == "" {
		t.Errorf("jwt = %+v", jwt)
	}
	// Which header and scheme the token arrives in: read wrongly, every
	// request looks like it carries no token at all.
	if jwt.Header != "X-Token" || jwt.Scheme != "Token" {
		t.Errorf("header/scheme = %s/%s", jwt.Header, jwt.Scheme)
	}
	// The audience is what stops a token minted for another service being
	// accepted here, and the algorithm list is what stops a token signed
	// with an algorithm nobody expected.
	if len(jwt.Audience) != 2 || len(jwt.Algorithms) != 2 {
		t.Errorf("audience/algorithms = %v / %v", jwt.Audience, jwt.Algorithms)
	}
	// Clocks differ between machines; without any tolerance a token issued a
	// second ago is refused as not yet valid.
	if jwt.ClockSkew != 5*time.Second {
		t.Errorf("clock skew = %v", jwt.ClockSkew)
	}

	// One audience is usually written as a word rather than a list.
	single, err := AuthConfigFromMap(map[string]interface{}{
		"type": "jwt", "secret": "s", "audience": "orders",
	})
	if err != nil {
		t.Fatalf("AuthConfigFromMap: %v", err)
	}
	if len(single.JWT.Audience) != 1 || single.JWT.Audience[0] != "orders" {
		t.Errorf("audience = %v", single.JWT.Audience)
	}
}

func TestWhereAnAPIKeyIsLookedFor(t *testing.T) {
	cfg, err := AuthConfigFromMap(map[string]interface{}{
		"type":        "api_key",
		"header":      "X-API-Key",
		"query_param": "api_key",
		"keys":        []interface{}{"key-1", "key-2"},
	})
	if err != nil {
		t.Fatalf("AuthConfigFromMap: %v", err)
	}
	if cfg.APIKey.Header != "X-API-Key" || cfg.APIKey.QueryParam != "api_key" {
		t.Errorf("api key = %+v", cfg.APIKey)
	}
	if len(cfg.APIKey.Keys) != 2 {
		t.Errorf("keys = %v", cfg.APIKey.Keys)
	}

	// One key written as a word rather than a list of one.
	single, _ := AuthConfigFromMap(map[string]interface{}{"type": "api_key", "keys": "key-1"})
	if len(single.APIKey.Keys) != 1 {
		t.Errorf("keys = %v", single.APIKey.Keys)
	}

	// Keys checked against a database instead of a list, which is how a key
	// is revoked without a deployment.
	dynamic, _ := AuthConfigFromMap(map[string]interface{}{
		"type": "api_key",
		"validate": map[string]interface{}{
			"connector": "orders_db",
			"query":     "SELECT 1 FROM api_keys WHERE key = :key AND revoked_at IS NULL",
		},
	})
	if dynamic.APIKey.ValidateConnector != "orders_db" || dynamic.APIKey.ValidateQuery == "" {
		t.Errorf("validate = %+v", dynamic.APIKey)
	}
}

func TestWhoMayCallWithAPassword(t *testing.T) {
	cfg, err := AuthConfigFromMap(map[string]interface{}{
		"type":  "basic",
		"realm": "Orders",
		"users": map[string]interface{}{"alice": "secret", "bob": "other", "ignored": 42},
	})
	if err != nil {
		t.Fatalf("AuthConfigFromMap: %v", err)
	}
	if cfg.Basic.Realm != "Orders" {
		t.Errorf("realm = %q", cfg.Basic.Realm)
	}
	if len(cfg.Basic.Users) != 2 || cfg.Basic.Users["alice"] != "secret" {
		t.Errorf("users = %v", cfg.Basic.Users)
	}
	// A password that is not text is left out rather than rendered as Go's
	// idea of a number, which would be a password nobody can type.
	if _, present := cfg.Basic.Users["ignored"]; present {
		t.Errorf("a password that is not text was kept: %v", cfg.Basic.Users)
	}
}

func TestHeadersAConnectorInsistsOnOrSendsBack(t *testing.T) {
	cfg, err := AuthConfigFromMap(map[string]interface{}{
		"type":             "api_key",
		"keys":             "key-1",
		"required_headers": []interface{}{"X-Tenant", "X-Request-Id"},
		"response_headers": map[string]interface{}{"X-Frame-Options": "DENY", "ignored": 42},
	})
	if err != nil {
		t.Fatalf("AuthConfigFromMap: %v", err)
	}
	if len(cfg.RequiredHeaders) != 2 {
		t.Errorf("required headers = %v", cfg.RequiredHeaders)
	}
	if cfg.ResponseHeaders["X-Frame-Options"] != "DENY" {
		t.Errorf("response headers = %v", cfg.ResponseHeaders)
	}
	if _, present := cfg.ResponseHeaders["ignored"]; present {
		t.Errorf("a header value that is not text was kept: %v", cfg.ResponseHeaders)
	}
}

func TestWhatAConfiguredConnectorActuallyChecks(t *testing.T) {
	// End to end through the middleware: the block above, applied to
	// requests.
	c := New("api", 0, nil, nil)
	c.authConfig = &AuthConfig{
		Type:            "api_key",
		Public:          []string{"/health"},
		RequiredHeaders: []string{"X-Tenant"},
		ResponseHeaders: map[string]string{"X-Frame-Options": "DENY"},
		APIKey:          &APIKeyAuthConfig{Header: "X-API-Key", Keys: []string{"key-1"}},
	}

	reached := false
	handler := c.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	call := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		reached = false
		r := httptest.NewRequest(http.MethodGet, path, nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	// A public path is answered without a key and without the required
	// headers — this is what keeps a health check working when everything
	// else is locked. A load balancer's probe sends neither, and an instance
	// that answers 400 to it is taken out of rotation.
	if answer := call("/health", nil); !reached {
		t.Errorf("a public path was refused: %d %s", answer.Code, answer.Body.String())
	}

	// A missing required header is a bad request, and it is checked before
	// the key so the caller is told the real problem.
	answer := call("/orders", map[string]string{"X-API-Key": "key-1"})
	if reached || answer.Code != http.StatusBadRequest {
		t.Errorf("a request missing a required header was answered %d", answer.Code)
	}

	// The right key gets through.
	if answer := call("/orders", map[string]string{"X-Tenant": "acme", "X-API-Key": "key-1"}); !reached {
		t.Errorf("a request with the right key was refused: %d", answer.Code)
	}

	// A wrong key, and no key at all, do not.
	for name, headers := range map[string]map[string]string{
		"a key nobody issued": {"X-Tenant": "acme", "X-API-Key": "key-9"},
		"no key at all":       {"X-Tenant": "acme"},
	} {
		t.Run(name, func(t *testing.T) {
			answer := call("/orders", headers)
			if reached {
				t.Error("the request reached the flow")
			}
			if answer.Code != http.StatusUnauthorized {
				t.Errorf("answered %d, want 401", answer.Code)
			}
			// The header the connector was told to send goes out even on a
			// refusal: it is a security header, and a refused request is
			// still a response somebody's browser reads.
			if answer.Header().Get("X-Frame-Options") != "DENY" {
				t.Errorf("headers = %v", answer.Header())
			}
		})
	}
}

func TestAConnectorWithNoAuthBlockChecksNothing(t *testing.T) {
	// Most connectors. The middleware must not stand between a request and
	// its flow at all.
	c := New("api", 0, nil, nil)

	reached := false
	handler := c.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders", nil))

	if !reached {
		t.Error("a connector with no auth block refused a request")
	}
}
