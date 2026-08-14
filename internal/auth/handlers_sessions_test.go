package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// Somebody's list of signed-in devices, and the button that ends one of them.
// It is how a person reacts to "we noticed a new sign-in from another country",
// so the two things that matter are that the list is theirs alone and that
// revoking actually ends the session rather than reporting that it did.

// registeredAs signs somebody up and hands back the token their device holds.
func registeredAs(t *testing.T, m *Manager, email string) (string, *User) {
	t.Helper()
	user, tokens, err := m.Register(context.Background(), &RegisterRequest{
		Email: email, Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return tokens.AccessToken, user
}

func sessionsOf(t *testing.T, h *Handler, token string) []map[string]interface{} {
	t.Helper()
	rec := call(h.handleSessions, http.MethodGet, "/auth/sessions", "",
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	var body struct {
		Sessions []map[string]interface{} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the answer was not JSON: %v", err)
	}
	return body.Sessions
}

func TestSomebodyCanSeeWhereTheyAreSignedIn(t *testing.T) {
	h, m := newHandler(t)
	token, _ := registeredAs(t, m, "ada@example.com")

	// A second device.
	if _, _, err := m.Login(context.Background(), &LoginRequest{
		Email: "ada@example.com", Password: "password123",
	}, "203.0.113.7", "another-device"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	sessions := sessionsOf(t, h, token)
	if len(sessions) != 2 {
		t.Fatalf("%d sessions listed, want both devices", len(sessions))
	}

	// One of them is the device asking, and it says so — that is what stops
	// somebody ending the session they are looking at it from.
	current := 0
	for _, s := range sessions {
		if s["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Errorf("%d sessions claim to be the current one", current)
	}
}

func TestOneAccountsSessionsAreNotAnothersBusiness(t *testing.T) {
	h, m := newHandler(t)
	token, _ := registeredAs(t, m, "ada@example.com")
	registeredAs(t, m, "grace@example.com")

	sessions := sessionsOf(t, h, token)
	if len(sessions) != 1 {
		t.Errorf("%d sessions listed, want only this account's", len(sessions))
	}
}

func TestTheListIsRefusedWithoutASignIn(t *testing.T) {
	h, _ := newHandler(t)

	rec := call(h.handleSessions, http.MethodGet, "/auth/sessions", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	rec = call(h.handleSessions, http.MethodGet, "/auth/sessions", "",
		map[string]string{"Authorization": "Bearer not-a-token"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a made-up token listed somebody's sessions: %d", rec.Code)
	}
}

func TestTheListIsReadNotChanged(t *testing.T) {
	h, m := newHandler(t)
	token, _ := registeredAs(t, m, "ada@example.com")

	rec := call(h.handleSessions, http.MethodPost, "/auth/sessions", "",
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestEndingASessionEndsIt(t *testing.T) {
	// The point of the whole screen: the device that was signed in stops being
	// signed in.
	h, m := newHandler(t)
	token, _ := registeredAs(t, m, "ada@example.com")

	_, otherTokens, err := m.Login(context.Background(), &LoginRequest{
		Email: "ada@example.com", Password: "password123",
	}, "203.0.113.7", "another-device")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Find the session that is not the one asking.
	var target string
	for _, s := range sessionsOf(t, h, token) {
		if s["current"] != true {
			target, _ = s["id"].(string)
		}
	}
	if target == "" {
		t.Fatal("no other session to end")
	}

	rec := call(h.handleSessionRevoke, http.MethodDelete, "/auth/sessions/"+target, "",
		map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	if remaining := sessionsOf(t, h, token); len(remaining) != 1 {
		t.Errorf("%d sessions left, want only the one that did the revoking", len(remaining))
	}

	// And the token that belonged to it is no longer a way in. The session is
	// what a refresh hangs off, so this is where a stolen device is cut off.
	if _, _, err := m.RefreshToken(context.Background(), otherTokens.RefreshToken); err == nil {
		t.Error("the revoked session could still be refreshed into a new token")
	}
}

func TestNobodyEndsSomebodyElsesSession(t *testing.T) {
	// Session identifiers are not secrets, so the check has to be that the
	// session belongs to whoever is asking.
	h, m := newHandler(t)
	adaToken, _ := registeredAs(t, m, "ada@example.com")
	graceToken, _ := registeredAs(t, m, "grace@example.com")

	var adaSession string
	for _, s := range sessionsOf(t, h, adaToken) {
		adaSession, _ = s["id"].(string)
	}

	rec := call(h.handleSessionRevoke, http.MethodDelete, "/auth/sessions/"+adaSession, "",
		map[string]string{"Authorization": "Bearer " + graceToken})
	if rec.Code == http.StatusOK {
		t.Error("one account ended another account's session")
	}

	if len(sessionsOf(t, h, adaToken)) != 1 {
		t.Error("the session was ended by somebody it does not belong to")
	}
}

func TestEndingASessionIsRefusedWithoutASignIn(t *testing.T) {
	h, _ := newHandler(t)

	rec := call(h.handleSessionRevoke, http.MethodDelete, "/auth/sessions/s-1", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	rec = call(h.handleSessionRevoke, http.MethodGet, "/auth/sessions/s-1", "",
		map[string]string{"Authorization": "Bearer x"})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
