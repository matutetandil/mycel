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

// How old a password is allowed to get.
//
// max_age and warn_before were read by nothing, so a service requiring a
// password to be changed every ninety days never asked anybody to change one.

func managerWithMaxAge(t *testing.T, maxAge, warnBefore string) *Manager {
	t.Helper()

	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8, MaxAge: maxAge, WarnBefore: warnBefore},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager
}

// signedInUser registers somebody whose password was set the given time ago.
func signedInUser(t *testing.T, manager *Manager, passwordSet time.Time) (*User, *TokenPair) {
	t.Helper()
	ctx := context.Background()

	hash, err := manager.passwordHasher.Hash("the-current-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	user := &User{
		ID:                "user-1",
		Email:             "someone@example.test",
		PasswordHash:      hash,
		PasswordChangedAt: &passwordSet,
	}
	if err := manager.userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	tokens, err := manager.EstablishSession(ctx, user, "203.0.113.10", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("EstablishSession: %v", err)
	}
	return user, tokens
}

func TestAPasswordPastItsAgeStopsWorking(t *testing.T) {
	manager := managerWithMaxAge(t, "90d", "")
	_, tokens := signedInUser(t, manager, time.Now().Add(-91*24*time.Hour))

	_, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken)
	if err == nil {
		t.Fatal("a password three months past a ninety-day policy was accepted")
	}
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != "password_expired" {
		t.Errorf("the refusal is %v, which a client cannot tell from a bad token", err)
	}
}

func TestAPasswordWithinItsAgeIsFine(t *testing.T) {
	manager := managerWithMaxAge(t, "90d", "")
	_, tokens := signedInUser(t, manager, time.Now().Add(-89*24*time.Hour))

	if _, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken); err != nil {
		t.Errorf("a password inside the policy was refused: %v", err)
	}
}

func TestAnUnknownPasswordAgeIsNotAnExpiredOne(t *testing.T) {
	// Every account that existed before the policy was configured, and every
	// SQL store whose fields block names no column for it. Locking all of them
	// out the moment somebody writes max_age is not what writing it means.
	manager := managerWithMaxAge(t, "90d", "")
	ctx := context.Background()

	hash, err := manager.passwordHasher.Hash("the-current-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	user := &User{ID: "user-2", Email: "older@example.test", PasswordHash: hash}
	// Straight into the store, so that nothing stamps an age on the way in.
	if err := manager.userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	stored, err := manager.userStore.FindByID(ctx, "user-2")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	stored.PasswordChangedAt = nil

	if _, expired, known := manager.PasswordExpiry(stored); known || expired {
		t.Errorf("a password of unknown age reported known=%v expired=%v", known, expired)
	}
}

func TestNoPolicyExpiresNothing(t *testing.T) {
	manager := managerWithMaxAge(t, "", "")
	_, tokens := signedInUser(t, manager, time.Now().Add(-10*365*24*time.Hour))

	if _, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken); err != nil {
		t.Errorf("a ten-year-old password was refused by a service with no policy: %v", err)
	}
}

func TestSomebodyWithAnExpiredPasswordCanStillChangeIt(t *testing.T) {
	// Otherwise the policy is a lockout: the endpoint that fixes it needs a
	// token, and every token this account holds is refused everywhere else.
	manager := managerWithMaxAge(t, "90d", "")
	_, tokens := signedInUser(t, manager, time.Now().Add(-91*24*time.Hour))

	mux := http.NewServeMux()
	NewHandler(manager).RegisterRoutes(mux)

	body := strings.NewReader(`{"current_password":"the-current-password","new_password":"a-brand-new-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/change-password", body)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("changing an expired password answered %d: %s", rec.Code, rec.Body.String())
	}

	// And afterwards the account works again, which is the point.
	if _, _, err := manager.ValidateToken(context.Background(), tokens.AccessToken); err != nil {
		t.Errorf("the account is still refused after changing the password: %v", err)
	}
}

func TestSigningOutWorksWithAnExpiredPassword(t *testing.T) {
	manager := managerWithMaxAge(t, "90d", "")
	_, tokens := signedInUser(t, manager, time.Now().Add(-91*24*time.Hour))

	mux := http.NewServeMux()
	NewHandler(manager).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("signing out with an expired password answered %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAProtectedRouteSaysWhyItRefused(t *testing.T) {
	// A client that cannot tell an expired password from a bad token sends
	// somebody back to the sign-in screen to get the same answer again.
	manager := managerWithMaxAge(t, "90d", "")
	_, tokens := signedInUser(t, manager, time.Now().Add(-91*24*time.Hour))

	protected := NewMiddleware(manager, &MiddlewareConfig{}).Handler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("answered %d, want 403: %s", rec.Code, rec.Body.String())
	}
	var answer struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("the refusal is not JSON: %s", rec.Body.String())
	}
	if answer.Error.Code != "password_expired" {
		t.Errorf("code = %q, want password_expired", answer.Error.Code)
	}
}

func TestSigningInSaysWhenThePasswordExpires(t *testing.T) {
	// What warn_before is for. A week's notice is no use if nothing says it.
	for name, tc := range map[string]struct {
		setAgo      time.Duration
		warnBefore  string
		wantWarning bool
		wantExpired bool
	}{
		"plenty of time left, nothing said": {10 * 24 * time.Hour, "7d", false, false},
		"inside the warning window":         {85 * 24 * time.Hour, "7d", true, false},
		"already expired":                   {91 * 24 * time.Hour, "7d", true, true},
		"no warning configured":             {85 * 24 * time.Hour, "", false, false},
	} {
		t.Run(name, func(t *testing.T) {
			manager := managerWithMaxAge(t, "90d", tc.warnBefore)
			user, _ := signedInUser(t, manager, time.Now().Add(-tc.setAgo))
			_ = user

			mux := http.NewServeMux()
			NewHandler(manager).RegisterRoutes(mux)

			body := strings.NewReader(`{"email":"someone@example.test","password":"the-current-password"}`)
			req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("signing in answered %d: %s", rec.Code, rec.Body.String())
			}
			var answer map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
				t.Fatalf("not JSON: %s", rec.Body.String())
			}

			_, said := answer["password_expires_at"]
			if said != tc.wantWarning {
				t.Errorf("password_expires_at present = %v, want %v", said, tc.wantWarning)
			}
			expired, _ := answer["password_expired"].(bool)
			if expired != tc.wantExpired {
				t.Errorf("password_expired = %v, want %v", expired, tc.wantExpired)
			}
			// Signing in itself always works: the change-password endpoint
			// needs a token, so refusing here would be a lockout.
			if answer["access_token"] == nil {
				t.Error("signing in did not return a token")
			}
		})
	}
}

func TestChangingAPasswordResetsItsAge(t *testing.T) {
	manager := managerWithMaxAge(t, "90d", "")
	_, _ = signedInUser(t, manager, time.Now().Add(-91*24*time.Hour))
	ctx := context.Background()

	if err := manager.ChangePassword(ctx, "user-1", "the-current-password", "a-brand-new-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	user, err := manager.userStore.FindByID(ctx, "user-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if _, expired, known := manager.PasswordExpiry(user); !known || expired {
		t.Errorf("after changing it, known=%v expired=%v", known, expired)
	}
}
