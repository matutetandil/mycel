package http

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

func create(t *testing.T, props map[string]interface{}) (*Connector, error) {
	t.Helper()
	conn, err := NewFactory().Create(context.Background(), &connector.Config{
		Name: "api", Type: "http", Properties: props,
	})
	if err != nil {
		return nil, err
	}
	return conn.(*Connector), nil
}

func TestAnAuthTypeTheClientCannotHonourIsRefused(t *testing.T) {
	// The connector used to turn a word it did not recognise into no
	// authentication at all and send every request without credentials. The
	// service on the other end answers 401, the configuration file says
	// authentication is set up, and nothing in between says otherwise.
	_, err := create(t, map[string]interface{}{
		"base_url": "https://example.com",
		"auth":     map[string]interface{}{"type": "beare", "token": "secret"},
	})
	if err == nil {
		t.Fatal("a connector was built that would send every request unauthenticated")
	}
	for _, want := range []string{"beare", "bearer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestEveryAuthTypeTheClientImplements(t *testing.T) {
	for written, want := range map[string]AuthType{
		"bearer":             AuthTypeBearer,
		"oauth2":             AuthTypeOAuth2,
		"client_credentials": AuthTypeClientCredentials,
		"apikey":             AuthTypeAPIKey,
		"api_key":            AuthTypeAPIKey,
		"basic":              AuthTypeBasic,
		// Written the way somebody who read an HTTP header would write it.
		"Bearer": AuthTypeBearer,
	} {
		t.Run(written, func(t *testing.T) {
			conn, err := create(t, map[string]interface{}{
				"base_url": "https://example.com",
				"auth":     map[string]interface{}{"type": written, "token": "t"},
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if conn.auth.Type != want {
				t.Errorf("auth type = %q, want %q", conn.auth.Type, want)
			}
		})
	}
}

func TestAnEmptyAuthBlockIsNotAMistake(t *testing.T) {
	// A block whose contents come from the environment can be empty in the
	// file, and refusing it would break a configuration that works.
	conn, err := create(t, map[string]interface{}{
		"base_url": "https://example.com",
		"auth":     map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conn.auth.Type != AuthTypeNone {
		t.Errorf("auth type = %q", conn.auth.Type)
	}
}

func TestAGrantTypeUpgradesTheFlowItIsWrittenUnder(t *testing.T) {
	// oauth2 with a client_credentials grant is how the two are usually
	// written together, and it has to end up on the client-credentials path
	// rather than the refresh-token one.
	conn, err := create(t, map[string]interface{}{
		"base_url": "https://example.com",
		"auth": map[string]interface{}{
			"type": "oauth2", "grant_type": "client_credentials",
			"token_url": "https://example.com/token",
			"client_id": "id", "client_secret": "secret",
			"scopes": []interface{}{"read", "write"},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conn.auth.Type != AuthTypeClientCredentials {
		t.Errorf("auth type = %q, want the client-credentials flow", conn.auth.Type)
	}
	if len(conn.auth.Scopes) != 2 {
		t.Errorf("scopes = %v", conn.auth.Scopes)
	}
}

func TestABaseURLIsWhatTheConnectorCannotDoWithout(t *testing.T) {
	if _, err := create(t, map[string]interface{}{"timeout": "5s"}); err == nil {
		t.Fatal("a client with nowhere to send requests was built")
	}
}

func TestTheTimeoutCanBeWrittenEitherWay(t *testing.T) {
	for name, props := range map[string]map[string]interface{}{
		"as a duration": {"base_url": "https://example.com", "timeout": "45s"},
		"as seconds":    {"base_url": "https://example.com", "timeout": 45},
	} {
		t.Run(name, func(t *testing.T) {
			conn, err := create(t, props)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if conn.timeout != 45*time.Second {
				t.Errorf("timeout = %v, want 45s", conn.timeout)
			}
		})
	}
}

func TestARetryBlockOverridesTheShorthand(t *testing.T) {
	// Both spellings exist; writing the block is the more specific statement,
	// so it is the one that counts.
	conn, err := create(t, map[string]interface{}{
		"base_url":    "https://example.com",
		"retry_count": 2,
		"retry": map[string]interface{}{
			"attempts": 5, "delay": "200ms", "max_delay": "10s", "backoff": "exponential",
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conn.retryCount != 5 || conn.retry.Attempts != 5 {
		t.Errorf("attempts = %d/%d, want the block's 5", conn.retryCount, conn.retry.Attempts)
	}
	if conn.retry.Delay != 200*time.Millisecond || conn.retry.MaxDelay != 10*time.Second {
		t.Errorf("delays = %v/%v", conn.retry.Delay, conn.retry.MaxDelay)
	}
	if conn.retry.Backoff != "exponential" {
		t.Errorf("backoff = %q", conn.retry.Backoff)
	}
}

func TestRetryCountSurvivesComingFromTheEnvironment(t *testing.T) {
	// env() hands back a string, so a number that arrives spelt out has to be
	// read as a number rather than ignored.
	conn, err := create(t, map[string]interface{}{
		"base_url": "https://example.com", "retry_count": "4",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conn.retryCount != 4 {
		t.Errorf("retry count = %d, want 4", conn.retryCount)
	}
}

func TestTLSIsOnBecauseTheBlockWasWritten(t *testing.T) {
	conn, err := create(t, map[string]interface{}{
		"base_url": "https://example.com",
		"tls":      map[string]interface{}{"ca_file": "/etc/ssl/ca.pem"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conn.tlsConfig == nil {
		t.Fatal("the TLS block was written and nothing came of it")
	}

	// And a block that says otherwise turns it off, which is how the setting
	// is driven from the environment without deleting the certificates.
	off, err := create(t, map[string]interface{}{
		"base_url": "https://example.com",
		"tls":      map[string]interface{}{"enabled": false, "ca_file": "/etc/ssl/ca.pem"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if off.tlsConfig != nil {
		t.Error("TLS stayed on although the block said enabled = false")
	}
}

func TestHeadersReachTheConnector(t *testing.T) {
	conn, err := create(t, map[string]interface{}{
		"base_url": "https://example.com",
		"headers": map[string]interface{}{
			"X-Tenant": "acme",
			"X-Count":  7, // not a header value; left out rather than guessed at
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if conn.headers["X-Tenant"] != "acme" {
		t.Errorf("headers = %v", conn.headers)
	}
}

func TestTheFactoryAnswersForItsOwnType(t *testing.T) {
	f := NewFactory()
	if f.Type() != "http" || !f.Supports("http", "") || f.Supports("rest", "") {
		t.Errorf("Type = %q, Supports(http) = %v, Supports(rest) = %v",
			f.Type(), f.Supports("http", ""), f.Supports("rest", ""))
	}
}
