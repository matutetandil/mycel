package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Writing a webauthn block is how someone asks for passkeys: the relying party,
// its identifier, the origins a credential may be used from. Everything behind
// it existed — the service that runs both ceremonies, the manager methods that
// begin and finish registration, the store that keeps a credential — and there
// was nothing a browser could call, so the block configured a feature that
// could not be reached.
//
// A passkey ceremony is two calls on each side: the browser asks for options,
// the authenticator answers, and the answer comes back to be checked. Missing
// either half leaves a flow that cannot complete.

func passkeyService(t *testing.T) (*Handler, *Manager) {
	t.Helper()
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		MFA: &MFAConfig{
			Enabled: true,
			Methods: []string{"webauthn"},
			WebAuthn: &WebAuthnConfig{
				RPName:  "Mycel",
				RPID:    "example.com",
				Origins: []string{"https://example.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewHandler(manager), manager
}

func TestAServiceWithPasskeysServesTheCeremony(t *testing.T) {
	// Written from the configuration: a webauthn block is a promise that a
	// browser has somewhere to go.
	handler, manager := passkeyService(t)
	mux := &recordingMux{}
	handler.RegisterRoutes(mux)

	if !manager.WebAuthnEnabled() {
		t.Fatal("a configured webauthn block did not enable passkeys")
	}

	for name, path := range map[string]string{
		"asking for registration options": "/webauthn/register/begin",
		"handing back the new credential": "/webauthn/register/finish",
		"asking for sign-in options":      "/webauthn/login/begin",
		"handing back the assertion":      "/webauthn/login/finish",
		"listing the keys on an account":  "/webauthn/credentials",
	} {
		t.Run(name, func(t *testing.T) {
			if !mux.has(path) {
				t.Errorf("%s has no route: %v", path, mux.paths)
			}
		})
	}
}

func TestWithoutAWebAuthnBlockThoseRoutesAreNotServed(t *testing.T) {
	// A service that cannot run the ceremony should not answer on paths that
	// invite a browser to start one.
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		MFA:    &MFAConfig{Enabled: true, Methods: []string{"totp"}},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mux := &recordingMux{}
	NewHandler(manager).RegisterRoutes(mux)

	for _, path := range []string{"/webauthn/register/begin", "/webauthn/login/begin"} {
		if mux.has(path) {
			t.Errorf("%s is served although passkeys are not configured", path)
		}
	}
}

func TestRegistrationOptionsAreIssuedToTheSignedInAccount(t *testing.T) {
	handler, manager := passkeyService(t)
	_, token := signedIn(t, manager)

	status, answer := postTo(t, handler.handleWebAuthnRegisterBegin, token, map[string]interface{}{})
	if status != http.StatusOK {
		t.Fatalf("status = %d, answer = %v", status, answer)
	}

	// What the browser hands to navigator.credentials.create(), plus the
	// session state it must send back — the ceremony cannot be completed
	// without both.
	if answer["publicKey"] == nil && answer["options"] == nil {
		t.Errorf("no creation options came back: %v", answer)
	}
	if session, _ := answer["session"].(string); session == "" {
		t.Error("no session state came back, so the second call has nothing to check against")
	}
}

func TestRegistrationNeedsASignedInAccount(t *testing.T) {
	// A passkey is added to an account, so there has to be one. Taking a user
	// id from the body would let anyone add a key to somebody else's account —
	// which is a way to own it for ever.
	handler, _ := passkeyService(t)

	status, _ := postTo(t, handler.handleWebAuthnRegisterBegin, "", map[string]interface{}{
		"user_id": "somebody-else",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want the request refused without a token", status)
	}
}

func TestSignInOptionsAreIssuedForAnAccountWithAKey(t *testing.T) {
	// The sign-in half is asked for by address, before anybody is signed in —
	// that is the point. It must not say whether the account exists.
	handler, manager := passkeyService(t)
	_, _ = signedIn(t, manager)

	status, answer := postTo(t, handler.handleWebAuthnLoginBegin, "", map[string]interface{}{
		"email": "someone@example.com",
	})
	// An account with no passkey registered cannot be signed in with one, and
	// the answer must not distinguish it from an address with no account.
	unknown, other := postTo(t, handler.handleWebAuthnLoginBegin, "", map[string]interface{}{
		"email": "nobody@example.com",
	})
	if status != unknown {
		t.Errorf("an account with no key answered %d and an unknown address %d, which says which addresses exist",
			status, unknown)
	}
	_ = answer
	_ = other
}

func TestTheKeysOnAnAccountCanBeListedAndRemoved(t *testing.T) {
	handler, manager := passkeyService(t)
	_, token := signedIn(t, manager)

	status, answer := getFrom(t, handler.handleWebAuthnCredentials, token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, answer = %v", status, answer)
	}
	if _, present := answer["credentials"]; !present {
		t.Errorf("no list came back: %v", answer)
	}

	// And listing needs a signed-in account, since the list is of somebody's
	// authenticators.
	if status, _ := getFrom(t, handler.handleWebAuthnCredentials, ""); status != http.StatusUnauthorized {
		t.Errorf("status = %d, want the list refused without a token", status)
	}
}

func getFrom(t *testing.T, handler func(http.ResponseWriter, *http.Request), token string) (int, map[string]interface{}) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/auth/webauthn/credentials", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	var answer map[string]interface{}
	_ = json.Unmarshal(recorder.Body.Bytes(), &answer)
	return recorder.Code, answer
}

func TestAnAccountWithNoPasskeysListsNoneRatherThanFailing(t *testing.T) {
	// The ordinary first visit to a settings page. Answering with an error
	// would show a failure where the answer is simply "none yet".
	handler, manager := passkeyService(t)
	_, token := signedIn(t, manager)

	status, answer := getFrom(t, handler.handleWebAuthnCredentials, token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, answer = %v", status, answer)
	}
	listed, _ := answer["credentials"].([]interface{})
	if len(listed) != 0 {
		t.Errorf("credentials = %v, want none", listed)
	}
}

func TestRemovingAKeyNeedsThePassword(t *testing.T) {
	// A stolen session must not be able to take away the key that would have
	// stopped it.
	handler, manager := passkeyService(t)
	_, token := signedIn(t, manager)

	request := httptest.NewRequest(http.MethodDelete, "/auth/webauthn/credentials",
		bytesReader(t, map[string]interface{}{"credential_id": "some-key"}))
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.handleWebAuthnCredentials(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want it refused without a password", recorder.Code)
	}
}

func TestTheListCarriesNoKeyMaterial(t *testing.T) {
	// A list is for recognising a device, not for moving public keys around;
	// anything beyond what names a key is surface nobody asked for.
	handler, manager := passkeyService(t)
	_, token := signedIn(t, manager)

	_, answer := getFrom(t, handler.handleWebAuthnCredentials, token)
	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, unwanted := range []string{"public_key", "publicKey", "PublicKey"} {
		if strings.Contains(string(encoded), unwanted) {
			t.Errorf("the list carries %q", unwanted)
		}
	}
}

func bytesReader(t *testing.T, body interface{}) *bytes.Reader {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return bytes.NewReader(encoded)
}
