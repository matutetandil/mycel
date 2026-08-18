package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// required, require_for, require_multiple, min_factors and grace_period were
// read by nothing except a status response: sign-in asked for a second factor
// only when the account had enrolled one voluntarily. So required = "true"
// required nothing, while the status endpoint said it was required.

func mfaPolicyService(t *testing.T, policy *MFAConfig) (*Manager, http.Handler) {
	t.Helper()

	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8},
		MFA:      policy,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mux := http.NewServeMux()
	NewHandler(manager).RegisterRoutes(mux)
	return manager, mux
}

// accountAgedDays registers somebody and backdates the account.
func accountAgedDays(t *testing.T, manager *Manager, days int) (*User, *TokenPair) {
	t.Helper()
	ctx := context.Background()

	user := registered(t, manager, "someone@example.test", "a-good-password")
	stored, err := manager.userStore.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	stored.CreatedAt = time.Now().AddDate(0, 0, -days)
	if err := manager.userStore.Update(ctx, stored); err != nil {
		t.Fatalf("Update: %v", err)
	}

	tokens, err := manager.EstablishSession(ctx, stored, "203.0.113.10", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("EstablishSession: %v", err)
	}
	return stored, tokens
}

func TestAnAccountThatNeverEnrolledStopsWorking(t *testing.T) {
	manager, _ := mfaPolicyService(t, &MFAConfig{Enabled: true, Required: "true", GracePeriod: "7d"})
	_, tokens := accountAgedDays(t, manager, 8)

	_, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken)
	if err == nil {
		t.Fatal("an account eight days into a seven-day grace period was still working")
	}
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != "mfa_enrolment_required" {
		t.Errorf("refusal = %v", err)
	}
}

func TestAnAccountInsideItsGracePeriodStillWorks(t *testing.T) {
	// The whole point of a grace period: somebody has to be able to use the
	// service while they get round to setting it up.
	manager, _ := mfaPolicyService(t, &MFAConfig{Enabled: true, Required: "true", GracePeriod: "7d"})
	_, tokens := accountAgedDays(t, manager, 2)

	if _, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken); err != nil {
		t.Errorf("an account two days into a seven-day grace period was refused: %v", err)
	}
}

func TestNoGracePeriodMeansFromTheStart(t *testing.T) {
	manager, _ := mfaPolicyService(t, &MFAConfig{Enabled: true, Required: "true"})
	_, tokens := accountAgedDays(t, manager, 0)

	if _, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken); err == nil {
		t.Error("a service with no grace period let an unenrolled account through")
	}
}

func TestSigningInStillWorksSoSomebodyCanEnrol(t *testing.T) {
	// Refusing the sign-in would leave somebody unable to do the one thing
	// being asked of them: enrolling needs a token.
	manager, mux := mfaPolicyService(t, &MFAConfig{Enabled: true, Required: "true"})
	accountAgedDays(t, manager, 30)

	rec := postBody(t, mux, "/auth/login",
		`{"email":"someone@example.test","password":"a-good-password"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("signing in answered %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %s", rec.Body.String())
	}
	if body["access_token"] == nil {
		t.Fatal("no token was issued, so nothing can be enrolled")
	}
	// And the client is told why everything else is about to refuse.
	if body["mfa_enrolment_required"] != true {
		t.Errorf("the sign-in did not say a second factor is required: %v", body)
	}
}

func TestTheEnrolmentEndpointItselfKeepsWorking(t *testing.T) {
	manager, mux := mfaPolicyService(t, &MFAConfig{
		Enabled: true, Required: "true", Methods: []string{"totp"},
	})
	_, tokens := accountAgedDays(t, manager, 30)

	req := httptest.NewRequest(http.MethodPost, "/auth/mfa/setup", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Errorf("the endpoint that fixes it refused the account: %s", rec.Body.String())
	}
}

func TestAnAccountThatEnrolledIsFine(t *testing.T) {
	manager, _ := mfaPolicyService(t, &MFAConfig{Enabled: true, Required: "true"})
	user, tokens := accountAgedDays(t, manager, 30)
	ctx := context.Background()

	if err := manager.userStore.UpdateMFAEnabled(ctx, user.ID, true); err != nil {
		t.Fatalf("UpdateMFAEnabled: %v", err)
	}
	if _, _, err := manager.ValidateToken(ctx, tokens.AccessToken); err != nil {
		t.Errorf("an account with a second factor was refused: %v", err)
	}
}

func TestOnlyTheRolesItAppliesTo(t *testing.T) {
	for name, tc := range map[string]struct {
		policy   *MFAConfig
		roles    []string
		required bool
	}{
		"admins only, and this is one": {&MFAConfig{Enabled: true, Required: "admin_only"}, []string{"admin"}, true},
		"admins only, and this is not": {&MFAConfig{Enabled: true, Required: "admin_only"}, []string{"reader"}, false},
		"named roles, and this is one": {&MFAConfig{Enabled: true, RequireFor: []string{"finance"}}, []string{"finance"}, true},
		"named roles, and this is not": {&MFAConfig{Enabled: true, RequireFor: []string{"finance"}}, []string{"reader"}, false},
		"everybody":                    {&MFAConfig{Enabled: true, Required: "true"}, nil, true},
		"offered rather than required": {&MFAConfig{Enabled: true, Required: "optional"}, []string{"admin"}, false},
		"the block is not switched on": {&MFAConfig{Required: "true"}, []string{"admin"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			manager, _ := mfaPolicyService(t, tc.policy)
			user := &User{ID: "user-1", Email: "someone@example.test", Roles: tc.roles}
			if got := manager.mfaRequiredFor(user); got != tc.required {
				t.Errorf("required = %v, want %v", got, tc.required)
			}
		})
	}
}

func TestAskingForMoreThanOneFactor(t *testing.T) {
	manager, _ := mfaPolicyService(t, &MFAConfig{
		Enabled: true, Required: "true", RequireMultiple: true,
		Methods: []string{"totp", "webauthn"},
	})
	user, tokens := accountAgedDays(t, manager, 30)
	ctx := context.Background()

	// One factor is not two.
	if err := manager.userStore.UpdateMFAEnabled(ctx, user.ID, true); err != nil {
		t.Fatalf("UpdateMFAEnabled: %v", err)
	}
	_, _, err := manager.ValidateToken(ctx, tokens.AccessToken)
	if err == nil {
		t.Fatal("one factor satisfied a policy asking for two")
	}
	// And the message says what is missing, not just that something is.
	if authErr, ok := err.(*AuthError); ok {
		if !contains(authErr.Message, "2") {
			t.Errorf("the refusal does not say how many are wanted: %v", err)
		}
	}
}

func TestAPolicyNobodyCouldSatisfyIsRefusedAtStartup(t *testing.T) {
	for name, policy := range map[string]*MFAConfig{
		"more factors than there are methods": {
			Enabled: true, Required: "true", MinFactors: 3, Methods: []string{"totp"},
		},
		"a word that is neither": {Enabled: true, Required: "sometimes"},
		"a grace that is not a time": {
			Enabled: true, Required: "true", GracePeriod: "a fortnight",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewManager(&Config{
				Preset: "development",
				JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
				MFA:    policy,
			})
			if err == nil {
				t.Fatal("the configuration was accepted")
			}
		})
	}
}

func TestNothingRequiredChangesNothing(t *testing.T) {
	// Every deployment offering MFA rather than demanding it.
	manager, _ := mfaPolicyService(t, &MFAConfig{Enabled: true, Required: "optional"})
	_, tokens := accountAgedDays(t, manager, 365)

	if _, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken); err != nil {
		t.Errorf("an account was refused by a policy that only offers MFA: %v", err)
	}
}
