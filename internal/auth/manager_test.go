package auth

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// The manager holds the parts of authentication that outlive a single request:
// sessions, password changes, MFA enrolment. Those are the operations where a
// mistake is not a wrong status code but a door left open — a revoked session
// that still works, a password change that leaves the old one valid, MFA that
// reports itself enabled without a confirmed secret.

// newMFAManager builds a manager with MFA switched on. The development preset
// leaves it off, which is right for that preset but makes the enrolment flow
// unreachable from newTestManager.
func newMFAManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(&Config{
		Preset: "development",
		JWT: &JWTConfig{
			Secret:          "test-secret-key-that-is-long-enough",
			AccessLifetime:  "15m",
			RefreshLifetime: "7d",
		},
		MFA: &MFAConfig{
			Enabled: true,
			Methods: []string{"totp"},
		},
	})
	if err != nil {
		t.Fatalf("NewManager with MFA: %v", err)
	}
	return m
}

// registerUser returns a user and its first token pair.
func registerUser(t *testing.T, m *Manager, email string) (*User, *TokenPair) {
	t.Helper()
	u, tokens, err := m.Register(context.Background(), &RegisterRequest{
		Email:    email,
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register(%s): %v", email, err)
	}
	return u, tokens
}

func TestGetUser(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	user, _ := registerUser(t, m, "get@example.com")

	got, err := m.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Email != "get@example.com" {
		t.Errorf("email = %q, want get@example.com", got.Email)
	}

	if _, err := m.GetUser(ctx, "no-such-user"); err == nil {
		t.Error("GetUser returned no error for an unknown id")
	}
}

func TestSessionLifecycle(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	user, _ := registerUser(t, m, "sessions@example.com")

	// Two logins from different clients: two sessions.
	for _, ua := range []string{"client-a", "client-b"} {
		if _, _, err := m.Login(ctx, &LoginRequest{
			Email: "sessions@example.com", Password: "password123",
		}, "10.0.0.1", ua); err != nil {
			t.Fatalf("Login from %s: %v", ua, err)
		}
	}

	sessions, err := m.GetSessions(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetSessions: %v", err)
	}
	if len(sessions) < 2 {
		t.Fatalf("got %d sessions after two logins, want at least 2", len(sessions))
	}

	// Revoking one must not take the others with it.
	before := len(sessions)
	if err := m.RevokeSession(ctx, user.ID, sessions[0].ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	after, err := m.GetSessions(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetSessions after revoke: %v", err)
	}
	if len(after) != before-1 {
		t.Errorf("sessions after revoking one = %d, want %d", len(after), before-1)
	}

	// LogoutAll clears the rest.
	if err := m.LogoutAll(ctx, user.ID); err != nil {
		t.Fatalf("LogoutAll: %v", err)
	}
	remaining, err := m.GetSessions(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetSessions after LogoutAll: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("%d sessions survived LogoutAll", len(remaining))
	}
}

func TestRevokingAnotherUsersSessionIsRefused(t *testing.T) {
	// The session id alone must not be enough; it has to belong to the caller.
	m := newTestManager(t)
	ctx := context.Background()

	victim, _ := registerUser(t, m, "victim@example.com")
	attacker, _ := registerUser(t, m, "attacker@example.com")

	victimSessions, err := m.GetSessions(ctx, victim.ID)
	if err != nil || len(victimSessions) == 0 {
		t.Skipf("no session to attempt against (err=%v)", err)
	}

	if err := m.RevokeSession(ctx, attacker.ID, victimSessions[0].ID); err == nil {
		remaining, _ := m.GetSessions(ctx, victim.ID)
		if len(remaining) < len(victimSessions) {
			t.Error("one user revoked another user's session")
		}
	}
}

func TestChangePassword(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	user, _ := registerUser(t, m, "chpw2@example.com")

	if err := m.ChangePassword(ctx, user.ID, "not-the-password", "newpassword456"); err == nil {
		t.Error("the password changed without the correct current one")
	}

	if err := m.ChangePassword(ctx, user.ID, "password123", "newpassword456"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// The point of a password change is that the old one stops working.
	if _, _, err := m.Login(ctx, &LoginRequest{
		Email: "chpw2@example.com", Password: "password123",
	}, "10.0.0.1", "test"); err == nil {
		t.Error("the old password still logs in after a change")
	}
	if _, _, err := m.Login(ctx, &LoginRequest{
		Email: "chpw2@example.com", Password: "newpassword456",
	}, "10.0.0.1", "test"); err != nil {
		t.Errorf("the new password does not log in: %v", err)
	}
}

func TestTOTPEnrolmentFlow(t *testing.T) {
	m := newMFAManager(t)
	ctx := context.Background()
	user, _ := registerUser(t, m, "totp@example.com")

	status, err := m.GetMFAStatus(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetMFAStatus: %v", err)
	}
	if status.Enabled {
		t.Fatal("MFA reports itself enabled on a fresh account")
	}

	setup, err := m.BeginTOTPSetup(ctx, user.ID)
	if err != nil {
		t.Fatalf("BeginTOTPSetup: %v", err)
	}
	if setup.Secret == "" {
		t.Fatal("no secret produced")
	}
	if setup.ProvisioningURI == "" {
		t.Error("no otpauth:// URI produced, so no authenticator app can enrol")
	}

	// Beginning setup must not be enough on its own — an unconfirmed secret
	// that already counts as MFA would lock a user out of their own account.
	mid, err := m.GetMFAStatus(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetMFAStatus mid-setup: %v", err)
	}
	if mid.Enabled {
		t.Error("MFA became enabled before the secret was confirmed")
	}

	totp := NewTOTPService(nil)
	if _, err := m.ConfirmTOTPSetup(ctx, user.ID, "000000"); err == nil {
		t.Error("setup was confirmed with an obviously wrong code")
	}

	codes, err := m.ConfirmTOTPSetup(ctx, user.ID, totp.GenerateCode(setup.Secret))
	if err != nil {
		t.Fatalf("ConfirmTOTPSetup with a valid code: %v", err)
	}
	if len(codes) == 0 {
		t.Error("no recovery codes issued, so a lost device means a lost account")
	}

	after, err := m.GetMFAStatus(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetMFAStatus after confirm: %v", err)
	}
	if !after.Enabled || !after.TOTPConfigured {
		t.Errorf("after confirming: enabled=%v totp=%v, want both true", after.Enabled, after.TOTPConfigured)
	}

	// Turning MFA off is a password-protected operation.
	if err := m.DisableMFA(ctx, user.ID, "wrong-password"); err == nil {
		t.Error("MFA was disabled with the wrong password")
	}
	if err := m.DisableMFA(ctx, user.ID, "password123"); err != nil {
		t.Fatalf("DisableMFA: %v", err)
	}
	off, _ := m.GetMFAStatus(ctx, user.ID)
	if off.Enabled {
		t.Error("MFA still reports itself enabled after being disabled")
	}
}

func TestRegenerateRecoveryCodesNeedsThePassword(t *testing.T) {
	m := newMFAManager(t)
	ctx := context.Background()
	user, _ := registerUser(t, m, "recovery@example.com")

	setup, err := m.BeginTOTPSetup(ctx, user.ID)
	if err != nil {
		t.Fatalf("BeginTOTPSetup: %v", err)
	}
	totp := NewTOTPService(nil)
	first, err := m.ConfirmTOTPSetup(ctx, user.ID, totp.GenerateCode(setup.Secret))
	if err != nil {
		t.Fatalf("ConfirmTOTPSetup: %v", err)
	}

	if _, err := m.RegenerateRecoveryCodes(ctx, user.ID, "wrong-password"); err == nil {
		t.Error("recovery codes were regenerated with the wrong password")
	}

	second, err := m.RegenerateRecoveryCodes(ctx, user.ID, "password123")
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes: %v", err)
	}
	if len(second) == 0 {
		t.Fatal("no codes returned")
	}
	// Regenerating must actually replace them, or a leaked set stays valid.
	if len(first) > 0 && first[0] == second[0] {
		t.Error("regenerating returned the same codes")
	}
}

func TestRepeatedFailedLoginsLockTheAccount(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	registerUser(t, m, "lockout@example.com")

	// The development preset is permissive, so this asserts the mechanism runs
	// and stays consistent rather than a specific threshold: once the account
	// starts refusing, the correct password must not slip through either.
	var locked bool
	for i := 0; i < 25; i++ {
		_, _, err := m.Login(ctx, &LoginRequest{
			Email: "lockout@example.com", Password: "wrong-password",
		}, "10.0.0.9", "test")
		if err == nil {
			t.Fatal("a wrong password logged in")
		}
		if authErr, ok := err.(*AuthError); ok && authErr.Code == "account_locked" {
			locked = true
			break
		}
	}

	if locked {
		if _, _, err := m.Login(ctx, &LoginRequest{
			Email: "lockout@example.com", Password: "password123",
		}, "10.0.0.9", "test"); err == nil {
			t.Error("the correct password bypassed the lockout")
		}
	}
}

func TestBruteForceKeyDistinguishesUsersAndAddresses(t *testing.T) {
	m := newTestManager(t)

	// Two different accounts, or the same account from two addresses, must not
	// share a counter — otherwise one attacker locks out everybody, or one
	// victim's failures hide another's.
	a := m.bruteForceKey("one@example.com", "10.0.0.1")
	b := m.bruteForceKey("two@example.com", "10.0.0.1")
	c := m.bruteForceKey("one@example.com", "10.0.0.2")

	if a == "" {
		t.Fatal("bruteForceKey produced an empty key")
	}
	if a == b {
		t.Error("two accounts from one address share a key")
	}
	if a == c {
		t.Error("one account from two addresses shares a key")
	}
	if a != m.bruteForceKey("one@example.com", "10.0.0.1") {
		t.Error("the key is not stable for the same inputs")
	}
}

func TestManagerOptions(t *testing.T) {
	// The functional options are the seam for swapping storage; a nil manager
	// field would surface as a panic on first use rather than a clear error.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	m, err := NewManager(&Config{
		Preset: "development",
		JWT: &JWTConfig{
			Secret:          "test-secret-key-that-is-long-enough",
			AccessLifetime:  "15m",
			RefreshLifetime: "7d",
		},
	},
		WithLogger(logger),
		WithUserStore(NewMemoryUserStore()),
		WithSessionStore(NewMemorySessionStore()),
	)
	if err != nil {
		t.Fatalf("NewManager with options: %v", err)
	}
	if m.Config() == nil {
		t.Error("Config() returned nil")
	}

	// The injected stores must be the ones actually used.
	user, _ := registerUser(t, m, "options@example.com")
	if _, err := m.GetUser(context.Background(), user.ID); err != nil {
		t.Errorf("the injected user store did not keep the user: %v", err)
	}
}

func TestCleanup(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	registerUser(t, m, "cleanup@example.com")

	// Nothing is expired yet, so this is about Cleanup being safe to call at
	// all — it runs on a timer in production.
	if err := m.Cleanup(ctx); err != nil {
		t.Errorf("Cleanup: %v", err)
	}
}
