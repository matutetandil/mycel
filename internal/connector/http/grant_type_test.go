package http

import (
	"strings"
	"testing"
)

// Which OAuth2 grant a connector uses.
//
// grant_type was stored in the config and read by nothing: which grant ran was
// decided by the auth type alone. So a connector written for a grant this does
// not implement was accepted in silence and ran a different one, against a
// token endpoint that would refuse it — and the error, when it came, named the
// endpoint rather than the configuration.

func TestTheGrantTypeChoosesTheGrant(t *testing.T) {
	for name, tc := range map[string]struct {
		config map[string]interface{}
		want   AuthType
	}{
		"client credentials": {
			map[string]interface{}{
				"type": "oauth2", "grant_type": "client_credentials",
				"token_url": "https://example.test/token",
				"client_id": "id", "client_secret": "secret",
			},
			AuthTypeClientCredentials,
		},
		"a refresh token": {
			map[string]interface{}{
				"type": "oauth2", "grant_type": "refresh_token",
				"token_url": "https://example.test/token", "refresh_token": "a-refresh-token",
			},
			AuthTypeOAuth2,
		},
		"nothing said, so the type decides as it always did": {
			map[string]interface{}{
				"type": "oauth2", "token_url": "https://example.test/token",
				"refresh_token": "a-refresh-token",
			},
			AuthTypeOAuth2,
		},
	} {
		t.Run(name, func(t *testing.T) {
			auth, err := parseAuthConfig(tc.config)
			if err != nil {
				t.Fatalf("parseAuthConfig: %v", err)
			}
			if auth.Type != tc.want {
				t.Errorf("auth type = %q, want %q", auth.Type, tc.want)
			}
		})
	}
}

func TestAGrantNobodyImplementedIsNamed(t *testing.T) {
	// Rather than quietly running a different one.
	for _, grant := range []string{"password", "authorization_code", "implicit", "device_code"} {
		t.Run(grant, func(t *testing.T) {
			_, err := parseAuthConfig(map[string]interface{}{
				"type": "oauth2", "grant_type": grant,
				"token_url": "https://example.test/token",
			})
			if err == nil {
				t.Fatalf("grant_type = %q was accepted", grant)
			}
			if !strings.Contains(err.Error(), "client_credentials") {
				t.Errorf("the error does not say which grants work: %v", err)
			}
		})
	}
}

func TestTheGrantTypeIsKeptAsWritten(t *testing.T) {
	// It is reported, and a value that came back different from what somebody
	// wrote is worse than no value at all.
	auth, err := parseAuthConfig(map[string]interface{}{
		"type": "oauth2", "grant_type": "client_credentials",
		"token_url": "https://example.test/token",
		"client_id": "id", "client_secret": "secret",
	})
	if err != nil {
		t.Fatalf("parseAuthConfig: %v", err)
	}
	if auth.GrantType != "client_credentials" {
		t.Errorf("grant type = %q", auth.GrantType)
	}
}
