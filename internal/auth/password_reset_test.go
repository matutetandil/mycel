package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Resetting a password nobody can remember.
//
// Both endpoints were configured Enabled: true by default and no handler was
// ever registered for either, so the most-used account flow after signing in
// answered 404 while the configuration said it was on.

func resetService(t *testing.T) (*Manager, *recordingFlows, http.Handler) {
	t.Helper()

	flows := newRecordingFlows()
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8},
		Hooks: &HooksConfig{
			OnPasswordReset: &HookConfig{Flow: "send_reset_email"},
		},
	}, WithFlowInvoker(flows))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(manager).RegisterRoutes(mux)
	return manager, flows, mux
}

func postBody(t *testing.T, mux http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// tokenFrom digs the reset token out of what the delivery flow was handed.
func tokenFrom(t *testing.T, flows *recordingFlows) string {
	t.Helper()
	call := flows.invoked("send_reset_email")
	if call == nil {
		t.Fatal("nothing was asked to deliver a reset token")
	}
	event, _ := call.input["auth"].(map[string]interface{})
	token, _ := event["reset_token"].(string)
	if token == "" {
		t.Fatalf("the delivery flow was given no token: %v", event)
	}
	return token
}

func TestSomebodyWhoForgotTheirPasswordCanSetANewOne(t *testing.T) {
	manager, flows, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	if rec := postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`); rec.Code != http.StatusOK {
		t.Fatalf("asking for a reset answered %d: %s", rec.Code, rec.Body.String())
	}

	token := tokenFrom(t, flows)
	rec := postBody(t, mux, "/auth/reset-password",
		`{"token":"`+token+`","new_password":"a-brand-new-password"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("resetting answered %d: %s", rec.Code, rec.Body.String())
	}

	// The new password works and the old one does not.
	ctx := context.Background()
	if _, _, err := manager.Login(ctx, &LoginRequest{
		Email: "someone@example.test", Password: "a-brand-new-password",
	}, "203.0.113.10", "Mozilla/5.0"); err != nil {
		t.Errorf("the new password does not sign in: %v", err)
	}
	if _, _, err := manager.Login(ctx, &LoginRequest{
		Email: "someone@example.test", Password: "the-old-password",
	}, "203.0.113.10", "Mozilla/5.0"); err == nil {
		t.Error("the old password still signs in after a reset")
	}
}

func TestAResetTokenIsGoodOnce(t *testing.T) {
	manager, flows, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	token := tokenFrom(t, flows)

	if rec := postBody(t, mux, "/auth/reset-password",
		`{"token":"`+token+`","new_password":"a-brand-new-password"}`); rec.Code != http.StatusOK {
		t.Fatalf("first reset answered %d", rec.Code)
	}
	rec := postBody(t, mux, "/auth/reset-password",
		`{"token":"`+token+`","new_password":"another-password-again"}`)
	if rec.Code == http.StatusOK {
		t.Error("the same reset link worked twice")
	}
}

func TestAnAddressWithNoAccountIsAnsweredTheSameWay(t *testing.T) {
	// Otherwise this endpoint is a way to find out who has an account here,
	// which is worth more to somebody enumerating a customer list than the
	// reset is to them.
	manager, flows, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	known := postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	unknown := postBody(t, mux, "/auth/forgot-password", `{"email":"nobody@example.test"}`)

	if known.Code != unknown.Code {
		t.Errorf("an address with an account answered %d and one without answered %d",
			known.Code, unknown.Code)
	}
	if known.Body.String() != unknown.Body.String() {
		t.Errorf("the answers differ:\n known: %s\n other: %s", known.Body.String(), unknown.Body.String())
	}
	// And nothing was sent for the address that has no account.
	if flows.count() != 1 {
		t.Errorf("delivery ran %d times for one real address", flows.count())
	}
}

func TestAnExpiredResetTokenIsRefused(t *testing.T) {
	manager, flows, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	token := tokenFrom(t, flows)

	// Wind it back past its life rather than waiting an hour.
	store := manager.passwordReset.(*MemoryPasswordResetStore)
	store.mu.Lock()
	for hash, entry := range store.tokens {
		entry.expiresAt = time.Now().Add(-time.Minute)
		store.tokens[hash] = entry
	}
	store.mu.Unlock()

	rec := postBody(t, mux, "/auth/reset-password",
		`{"token":"`+token+`","new_password":"a-brand-new-password"}`)
	if rec.Code == http.StatusOK {
		t.Error("an expired reset link worked")
	}
}

func TestAMadeUpTokenIsRefused(t *testing.T) {
	manager, _, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	rec := postBody(t, mux, "/auth/reset-password",
		`{"token":"a-token-nobody-issued","new_password":"a-brand-new-password"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a made-up token answered %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestTheTokenIsNotStoredAsItIsSent(t *testing.T) {
	// It is a bearer credential for the length of its life: a store somebody
	// can read must not be a store somebody can reset accounts from.
	manager, flows, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	token := tokenFrom(t, flows)

	store := manager.passwordReset.(*MemoryPasswordResetStore)
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, held := store.tokens[token]; held {
		t.Error("the token is stored exactly as it was sent")
	}
	if len(store.tokens) != 1 {
		t.Fatalf("the store holds %d tokens", len(store.tokens))
	}
}

func TestAResetEndsEverySessionTheAccountHad(t *testing.T) {
	// Somebody resetting a password either forgot it or is taking the account
	// back from whoever did not, and in the second case leaving the other
	// sessions open would defeat the reset.
	manager, flows, mux := resetService(t)
	user := registered(t, manager, "someone@example.test", "the-old-password")
	ctx := context.Background()

	if _, err := manager.EstablishSession(ctx, user, "203.0.113.10", "Mozilla/5.0"); err != nil {
		t.Fatalf("EstablishSession: %v", err)
	}
	before, _ := manager.sessionStore.Count(ctx, user.ID)
	if before == 0 {
		t.Fatal("the account holds no sessions to begin with")
	}

	postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	token := tokenFrom(t, flows)
	postBody(t, mux, "/auth/reset-password", `{"token":"`+token+`","new_password":"a-brand-new-password"}`)

	after, _ := manager.sessionStore.Count(ctx, user.ID)
	if after != 0 {
		t.Errorf("%d sessions survived the reset", after)
	}
}

func TestAResetIsHeldToTheSamePasswordPolicy(t *testing.T) {
	// Otherwise it is a way around the rules a deliberate change obeys.
	flows := newRecordingFlows()
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 12, History: 2},
		Hooks:    &HooksConfig{OnPasswordReset: &HookConfig{Flow: "send_reset_email"}},
	}, WithFlowInvoker(flows))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mux := http.NewServeMux()
	NewHandler(manager).RegisterRoutes(mux)
	registered(t, manager, "someone@example.test", "a-long-enough-password")

	postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	token := tokenFrom(t, flows)

	if rec := postBody(t, mux, "/auth/reset-password",
		`{"token":"`+token+`","new_password":"short"}`); rec.Code == http.StatusOK {
		t.Error("a reset set a password the policy refuses")
	}

	// Same token again, now going back to the password already in use.
	postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	token = tokenFrom(t, flows)
	rec := postBody(t, mux, "/auth/reset-password",
		`{"token":"`+token+`","new_password":"a-long-enough-password"}`)
	if rec.Code == http.StatusOK {
		t.Error("a reset went back to the password already in use, past the history")
	}
}

func TestAResetWithNobodyToDeliverItSaysSo(t *testing.T) {
	// A service with no on_password_reset hook cannot get the token to
	// anybody, and somebody is waiting for an email that is not coming.
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	registered(t, manager, "someone@example.test", "the-old-password")

	// It must not fail the request — the answer is the same either way, and
	// a 500 here would tell a caller which addresses have accounts.
	if err := manager.RequestPasswordReset(context.Background(), "someone@example.test", "203.0.113.10", "curl/8"); err != nil {
		t.Errorf("RequestPasswordReset: %v", err)
	}
}

func TestTheResetEndpointsCanBeTurnedOff(t *testing.T) {
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Endpoints: &EndpointsConfig{
			Login:          &EndpointConfig{Enabled: true},
			PasswordForgot: &EndpointConfig{Enabled: false},
			PasswordReset:  &EndpointConfig{Enabled: false},
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	mounted := &mountedPaths{}
	NewHandler(manager).RegisterRoutes(mounted)
	if mounted.has("forgot-password") || mounted.has("reset-password") {
		t.Errorf("the reset endpoints are served with both turned off: %v", mounted.paths)
	}
}

func TestWhatTheDeliveryFlowIsTold(t *testing.T) {
	manager, flows, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	postBody(t, mux, "/auth/forgot-password", `{"email":"someone@example.test"}`)
	call := flows.invoked("send_reset_email")
	if call == nil {
		t.Fatal("nothing was asked to deliver the token")
	}
	event, _ := call.input["auth"].(map[string]interface{})

	for _, field := range []string{"email", "reset_token", "expires_at", "user_id"} {
		if event[field] == nil || event[field] == "" {
			t.Errorf("the flow was not told %s: %v", field, event)
		}
	}
	// Something a flow can put in a link and a person can read.
	if _, err := time.Parse(time.RFC3339, event["expires_at"].(string)); err != nil {
		t.Errorf("expires_at is not a time a flow can use: %v", event["expires_at"])
	}
}

func TestAskingForAResetWithNoAddressIsARequestError(t *testing.T) {
	manager, _, mux := resetService(t)
	registered(t, manager, "someone@example.test", "the-old-password")

	rec := postBody(t, mux, "/auth/forgot-password", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("answered %d, want 400", rec.Code)
	}
	var answer map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &answer)
	if answer["error"] == nil {
		t.Errorf("no error in the body: %s", rec.Body.String())
	}
}
