package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Enrolling a second factor is a two-step ceremony for a reason: the secret is
// handed over first, and the account is only protected once a code proves the
// app actually holds it. A service that enabled it on the first step would lock
// people out of their own accounts on a mistyped setup.

func mfaService(t *testing.T) (*Handler, *Manager) {
	t.Helper()
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		MFA:    &MFAConfig{Enabled: true, Methods: []string{"totp"}},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewHandler(manager), manager
}

func signedIn(t *testing.T, manager *Manager) (*User, string) {
	t.Helper()
	user, tokens, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.com", Password: "a-long-enough-password-1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return user, tokens.AccessToken
}

func postTo(t *testing.T, handler func(http.ResponseWriter, *http.Request), token string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/mfa/setup", bytes.NewReader(encoded))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	var answer map[string]interface{}
	_ = json.Unmarshal(recorder.Body.Bytes(), &answer)
	return recorder.Code, answer
}

func TestEnrollingHandsOverWhatAnAppNeeds(t *testing.T) {
	handler, manager := mfaService(t)
	_, token := signedIn(t, manager)

	status, answer := postTo(t, handler.handleMFASetup, token, map[string]interface{}{})
	if status != http.StatusOK {
		t.Fatalf("status = %d, answer = %v", status, answer)
	}

	// The secret and the URI an authenticator app is pointed at. Without both,
	// there is nothing for somebody to scan or paste.
	if secret, _ := answer["secret"].(string); secret == "" {
		t.Error("no secret was handed over")
	}
	if uri, _ := answer["provisioning_uri"].(string); uri == "" {
		t.Error("no provisioning URI was handed over")
	}
}

func TestEnrollingDoesNotTurnItOnByItself(t *testing.T) {
	// The second step is what proves the app holds the secret. Turning it on
	// here would lock somebody out on a setup that went wrong.
	handler, manager := mfaService(t)
	user, token := signedIn(t, manager)

	if _, _ = postTo(t, handler.handleMFASetup, token, map[string]interface{}{}); true {
	}

	status, err := manager.GetMFAStatus(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetMFAStatus: %v", err)
	}
	if status.Enabled {
		t.Error("the second factor was turned on before any code was checked")
	}
}

func TestAWrongCodeDoesNotFinishEnrolment(t *testing.T) {
	handler, manager := mfaService(t)
	user, token := signedIn(t, manager)
	_, _ = postTo(t, handler.handleMFASetup, token, map[string]interface{}{})

	code, answer := postTo(t, handler.handleMFAVerify, token, map[string]interface{}{"code": "000000"})
	if code == http.StatusOK {
		t.Fatalf("a wrong code finished enrolment: %v", answer)
	}

	status, _ := manager.GetMFAStatus(context.Background(), user.ID)
	if status.Enabled {
		t.Error("the second factor was turned on by a wrong code")
	}
}

func TestTheRightCodeFinishesEnrolmentAndHandsOverRecoveryCodes(t *testing.T) {
	handler, manager := mfaService(t)
	user, token := signedIn(t, manager)

	_, setup := postTo(t, handler.handleMFASetup, token, map[string]interface{}{})
	secret, _ := setup["secret"].(string)
	if secret == "" {
		t.Fatal("no secret to generate a code from")
	}

	generated := manager.mfaService.totp.GenerateCode(secret)

	status, answer := postTo(t, handler.handleMFAVerify, token, map[string]interface{}{"code": generated})
	if status != http.StatusOK {
		t.Fatalf("status = %d, answer = %v", status, answer)
	}

	// Recovery codes come back here because they are shown once and never
	// again — there is nowhere else to get them.
	codes, _ := answer["recovery_codes"].([]interface{})
	if len(codes) == 0 {
		t.Error("no recovery codes were handed over, so a lost device locks the account out")
	}

	state, _ := manager.GetMFAStatus(context.Background(), user.ID)
	if !state.Enabled {
		t.Error("the second factor was not turned on by a correct code")
	}
}

func TestOnlyTheSignedInAccountCanEnrolOrRemove(t *testing.T) {
	// The account is taken from the token, never from the body: reading a user
	// id out of a request would let anyone enrol a factor on somebody else's
	// account, or take one off.
	handler, manager := mfaService(t)
	_, _ = signedIn(t, manager)

	for name, handle := range map[string]func(http.ResponseWriter, *http.Request){
		"enrolling":      handler.handleMFASetup,
		"confirming":     handler.handleMFAVerify,
		"turning it off": handler.handleMFADisable,
	} {
		t.Run(name, func(t *testing.T) {
			status, _ := postTo(t, handle, "", map[string]interface{}{
				"code": "123456", "password": "a-long-enough-password-1",
				"user_id": "somebody-else",
			})
			if status != http.StatusUnauthorized {
				t.Errorf("status = %d, want the request refused without a token", status)
			}
		})
	}
}

func TestTurningItOffNeedsThePassword(t *testing.T) {
	// A stolen session must not be enough to remove the protection that exists
	// for exactly that case.
	handler, manager := mfaService(t)
	_, token := signedIn(t, manager)

	status, _ := postTo(t, handler.handleMFADisable, token, map[string]interface{}{})
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want it refused without a password", status)
	}

	status, _ = postTo(t, handler.handleMFADisable, token, map[string]interface{}{"password": "not-the-password"})
	if status == http.StatusOK {
		t.Error("the second factor was turned off with a wrong password")
	}
}

func TestARecoveryCodeSignsInAndIsSpent(t *testing.T) {
	// The whole point of a recovery code: it works once, for somebody whose
	// device is gone. One that worked twice would be a password that never
	// expires.
	handler, manager := mfaService(t)
	_, token := signedIn(t, manager)

	_, setup := postTo(t, handler.handleMFASetup, token, map[string]interface{}{})
	secret, _ := setup["secret"].(string)
	generated := manager.mfaService.totp.GenerateCode(secret)
	_, confirmed := postTo(t, handler.handleMFAVerify, token, map[string]interface{}{"code": generated})

	codes, _ := confirmed["recovery_codes"].([]interface{})
	if len(codes) == 0 {
		t.Fatal("no recovery codes to use")
	}
	recovery, _ := codes[0].(string)

	status, answer := postTo(t, handler.handleMFARecovery, "", map[string]interface{}{
		"email": "someone@example.com", "password": "a-long-enough-password-1", "code": recovery,
	})
	if status != http.StatusOK {
		t.Fatalf("signing in with a recovery code: status = %d answer = %v", status, answer)
	}
	if access, _ := answer["access_token"].(string); access == "" {
		t.Error("no token came back from a recovery sign-in")
	}

	// And the same code a second time does not work.
	status, _ = postTo(t, handler.handleMFARecovery, "", map[string]interface{}{
		"email": "someone@example.com", "password": "a-long-enough-password-1", "code": recovery,
	})
	if status == http.StatusOK {
		t.Error("a recovery code worked twice")
	}
}
