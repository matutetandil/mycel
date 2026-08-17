package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The endpoints an account uses after it has signed in. Signing out is the one
// that matters most: a session that survives it is a credential somebody
// believes they revoked.

// signedIn registers an account and returns the service and its tokens.
func signedInOverHTTP(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	srv, _ := ssoServer(t, &Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
	})

	body := postJSON(t, srv.URL+"/auth/register", map[string]interface{}{
		"email":    "ada@example.com",
		"password": "a-long-enough-password",
		"name":     "Ada",
	}, "")

	access, _ := body["access_token"].(string)
	refresh, _ := body["refresh_token"].(string)
	if access == "" {
		t.Fatalf("registering answered without an access token: %v", body)
	}
	return srv, access, refresh
}

// post sends JSON and reads JSON back.
func postJSON(t *testing.T, url string, payload map[string]interface{}, token string) map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	body["_status"] = resp.StatusCode
	return body
}

func TestSigningOutEndsTheSession(t *testing.T) {
	srv, access, refresh := signedInOverHTTP(t)

	body := postJSON(t, srv.URL+"/auth/logout", nil, access)
	if body["_status"] != http.StatusOK {
		t.Fatalf("logout = %v", body)
	}

	// The refresh token belonged to that session, so it must not buy another
	// access token — otherwise signing out ends nothing.
	if refresh != "" {
		again := postJSON(t, srv.URL+"/auth/refresh", map[string]interface{}{"refresh_token": refresh}, "")
		if again["_status"] == http.StatusOK {
			t.Error("a refresh token from a session that was signed out still works")
		}
	}
}

func TestSigningOutNeedsToSayWhoIsSigningOut(t *testing.T) {
	// Otherwise anybody could end anybody's session by calling the endpoint.
	srv, _, _ := signedInOverHTTP(t)

	for name, token := range map[string]string{
		"with nothing":        "",
		"with something else": "not-a-token",
	} {
		t.Run(name, func(t *testing.T) {
			body := postJSON(t, srv.URL+"/auth/logout", nil, token)
			if body["_status"] == http.StatusOK {
				t.Error("a session was ended by a caller who did not say whose it was")
			}
		})
	}
}

func TestTheseEndpointsOnlyAnswerTheMethodTheyAreFor(t *testing.T) {
	// A GET that signs somebody out is a link that ends their session when a
	// browser prefetches it.
	srv, access, _ := signedInOverHTTP(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/auth/logout", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+access)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want the method to be refused", resp.StatusCode)
	}
}

func TestAnAccountCanSeeItsOwnSessions(t *testing.T) {
	// What "signed in on three devices" is built from, and it must show only
	// the caller's own.
	srv, access, _ := signedInOverHTTP(t)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/auth/sessions", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+access)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["sessions"] == nil {
		t.Errorf("body = %v, want the caller's sessions", body)
	}
}

func TestChangingAPasswordNeedsTheOldOne(t *testing.T) {
	// Otherwise a stolen access token is enough to take the account
	// permanently, rather than until the token expires.
	srv, access, _ := signedInOverHTTP(t)

	body := postJSON(t, srv.URL+"/auth/change-password", map[string]interface{}{
		"current_password": "not-the-password",
		"new_password":     "another-long-enough-password",
	}, access)
	if body["_status"] == http.StatusOK {
		t.Error("the password was changed without the old one")
	}

	body = postJSON(t, srv.URL+"/auth/change-password", map[string]interface{}{
		"current_password": "a-long-enough-password",
		"new_password":     "another-long-enough-password",
	}, access)
	if body["_status"] != http.StatusOK {
		t.Errorf("changing the password with the right one failed: %v", body)
	}

	// And the new one is what signs in from here.
	body = postJSON(t, srv.URL+"/auth/login", map[string]interface{}{
		"email": "ada@example.com", "password": "another-long-enough-password",
	}, "")
	if body["_status"] != http.StatusOK {
		t.Errorf("the new password does not sign in: %v", body)
	}
}
