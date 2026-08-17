package auth

import (
	"context"

	"github.com/go-webauthn/webauthn/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The manager's side of passkeys, and the small pieces around authentication
// that nothing reached.

func passkeyManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
		MFA: &MFAConfig{
			Enabled: true,
			WebAuthn: &WebAuthnConfig{
				RPID:    "orders.example.com",
				RPName:  "Orders",
				Origins: []string{"https://orders.example.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func registeredAccount(t *testing.T, m *Manager) *User {
	t.Helper()
	user, _, err := m.Register(context.Background(), &RegisterRequest{
		Email:    "ada@example.com",
		Password: "a-long-enough-password",
		Name:     "Ada",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return user
}

func TestEnrollingAPasskeyStartsFromTheAccount(t *testing.T) {
	m := passkeyManager(t)
	user := registeredAccount(t, m)

	options, session, err := m.BeginWebAuthnRegistration(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BeginWebAuthnRegistration: %v", err)
	}
	if options == nil || session == "" {
		t.Fatal("the ceremony started with nothing to sign")
	}

	// The account is who the passkey will belong to, so the prompt has to name
	// it rather than an anonymous user.
	creation, ok := options.(*protocol.CredentialCreation)
	if !ok {
		t.Fatalf("options = %T, want something the browser can be handed", options)
	}
	if creation.Response.User.Name != user.Email {
		t.Errorf("the prompt names %q, want the account", creation.Response.User.Name)
	}
}

func TestEnrollingForAnAccountNobodyHasIsRefused(t *testing.T) {
	m := passkeyManager(t)

	if _, _, err := m.BeginWebAuthnRegistration(context.Background(), "u-nobody"); err == nil {
		t.Error("a passkey ceremony started for an account that does not exist")
	}
}

func TestFinishingWithSomethingThatIsNotAnAssertionIsRefused(t *testing.T) {
	// The response arrives as an interface from the HTTP layer, so the type
	// assertion is the only thing between a forged body and a registered key.
	m := passkeyManager(t)
	user := registeredAccount(t, m)

	_, session, err := m.BeginWebAuthnRegistration(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("BeginWebAuthnRegistration: %v", err)
	}

	err = m.FinishWebAuthnRegistration(context.Background(), user.ID, session, "my laptop",
		map[string]interface{}{"id": "forged"})
	if err == nil {
		t.Fatal("something that is not an assertion was registered as a passkey")
	}
	if !strings.Contains(err.Error(), "Invalid") {
		t.Errorf("error = %q", err)
	}
}

func TestRemovingAPasskeyNeedsThePassword(t *testing.T) {
	// A stolen access token should not be enough to take away the second
	// factor — that is the whole point of having one.
	m := passkeyManager(t)
	user := registeredAccount(t, m)

	err := m.RemoveWebAuthnCredential(context.Background(), user.ID, "some-key", "not-the-password")
	if err == nil {
		t.Error("a passkey was removed without the account's password")
	}
}

func TestWithoutPasskeysConfiguredTheManagerSaysSo(t *testing.T) {
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	user := registeredAccount(t, m)

	if _, _, err := m.BeginWebAuthnRegistration(context.Background(), user.ID); err == nil {
		t.Error("a passkey ceremony started on a service with none configured")
	}
	if err := m.FinishWebAuthnRegistration(context.Background(), user.ID, "{}", "k", nil); err == nil {
		t.Error("a passkey was registered on a service with none configured")
	}
	if err := m.RemoveWebAuthnCredential(context.Background(), user.ID, "k", "a-long-enough-password"); err == nil {
		t.Error("a passkey was removed on a service with none configured")
	}
}

// --- Around authentication ---------------------------------------------------

func TestAuthenticationCanBeOptional(t *testing.T) {
	// Some routes want the caller's identity when there is one and serve
	// anybody when there is not — a public page that greets you by name.
	m := passkeyManager(t)

	optional := NewMiddleware(m, &MiddlewareConfig{Required: false})
	var reached bool
	guarded := optional.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	guarded(recorder, httptest.NewRequest(http.MethodGet, "/public", nil))
	if !reached {
		t.Error("a route that does not require a credential refused a caller with none")
	}

	// A credential that is not one is still refused: sending something
	// invalid is not the same as sending nothing.
	request := httptest.NewRequest(http.MethodGet, "/public", nil)
	request.Header.Set("Authorization", "Bearer not-a-token")
	recorder = httptest.NewRecorder()
	guarded(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want a bad credential to be refused", recorder.Code)
	}
}

func TestTheMiddlewareWrapsAPlainHandlerToo(t *testing.T) {
	// Most routes are written as a function rather than a Handler, and a
	// middleware that only wrapped one of the two would leave the other open.
	m := passkeyManager(t)
	user := registeredAccount(t, m)

	_, tokens, err := m.Login(context.Background(), &LoginRequest{
		Email: user.Email, Password: "a-long-enough-password",
	}, "10.0.0.1", "test")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// RequireAuth is the wrapper the runtime mounts; the zero-value config
	// means optional, which is a different question.
	middleware := NewMiddleware(m, &MiddlewareConfig{Required: true})
	var reached bool
	guarded := middleware.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	// Without a credential.
	recorder := httptest.NewRecorder()
	guarded(recorder, httptest.NewRequest(http.MethodGet, "/orders", nil))
	if reached {
		t.Error("the handler ran for a request with no credential")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", recorder.Code)
	}

	// And with one.
	request := httptest.NewRequest(http.MethodGet, "/orders", nil)
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	recorder = httptest.NewRecorder()
	guarded(recorder, request)
	if !reached {
		t.Error("the handler did not run for an authenticated request")
	}
}

func TestAPasswordRefusalSaysEverythingThatIsWrong(t *testing.T) {
	// One rule at a time means somebody fixes the length, tries again, and is
	// told about the digit.
	validator := NewPasswordValidator(&PasswordConfig{
		MinLength:      12,
		RequireUpper:   true,
		RequireNumber:  true,
		RequireSpecial: true,
	})

	err := validator.Validate("short", nil)
	if err == nil {
		t.Fatal("a password breaking every rule was accepted")
	}

	message := err.Error()
	for _, want := range []string{"12", "uppercase", "number"} {
		if !strings.Contains(strings.ToLower(message), strings.ToLower(want)) {
			t.Errorf("the refusal does not mention %q: %s", want, message)
		}
	}
	if !strings.Contains(message, ";") {
		t.Errorf("the reasons are not listed together: %s", message)
	}
}
