package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// An audit block is written for one reason: somebody has to be able to answer
// who signed in, from where, and what failed. It was parsed and read by
// nothing, so a service with one wrote no record of anything — the kind of gap
// discovered during an investigation, which is the worst moment to find out
// there is nothing to investigate with.

type recordedAudit struct {
	mu      sync.Mutex
	records []*AuditEvent
	err     error
}

func (r *recordedAudit) Log(_ context.Context, event *AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, event)
	return r.err
}

func (r *recordedAudit) of(event string) []*AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []*AuditEvent
	for _, e := range r.records {
		if e.Event == event {
			found = append(found, e)
		}
	}
	return found
}

func (r *recordedAudit) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

func auditedManager(t *testing.T) (*Manager, *recordedAudit) {
	t.Helper()
	records := &recordedAudit{}
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	}, WithAuditStore(records))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, records
}

func registered(t *testing.T, manager *Manager, email, password string) *User {
	t.Helper()
	user, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: email, Password: password,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return user
}

func TestASignInIsRecorded(t *testing.T) {
	manager, records := auditedManager(t)
	user := registered(t, manager, "someone@example.com", "a-long-enough-password-1")

	_, _, err := manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.com", Password: "a-long-enough-password-1",
	}, "203.0.113.9", "a browser")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	logins := records.of(AuditLogin)
	if len(logins) != 1 {
		t.Fatalf("%d sign-ins recorded, want one", len(logins))
	}
	entry := logins[0]
	if !entry.Success {
		t.Error("a successful sign-in was recorded as a failure")
	}
	if entry.UserID != user.ID || entry.Email != "someone@example.com" {
		t.Errorf("record = %+v, want the account it was for", entry)
	}
	// Where from and with what: the two questions an audit trail is kept to
	// answer, and neither is derivable afterwards.
	if entry.IP != "203.0.113.9" {
		t.Errorf("address = %q", entry.IP)
	}
	if entry.UserAgent != "a browser" {
		t.Errorf("user agent = %q", entry.UserAgent)
	}
}

func TestTheFailuresAreRecordedToo(t *testing.T) {
	// A record of successful sign-ins alone answers none of the questions an
	// audit trail is kept for.
	for name, attempt := range map[string]*LoginRequest{
		"a password that is wrong":   {Email: "someone@example.com", Password: "not-the-password"},
		"an address with no account": {Email: "nobody@example.com", Password: "a-long-enough-password-1"},
	} {
		t.Run(name, func(t *testing.T) {
			manager, records := auditedManager(t)
			registered(t, manager, "someone@example.com", "a-long-enough-password-1")

			if _, _, err := manager.Login(context.Background(), attempt, "203.0.113.9", "a browser"); err == nil {
				t.Fatal("the attempt succeeded")
			}

			logins := records.of(AuditLogin)
			if len(logins) != 1 {
				t.Fatalf("%d attempts recorded, want the failed one", len(logins))
			}
			if logins[0].Success {
				t.Error("a failed attempt was recorded as a success")
			}
			if logins[0].Email != attempt.Email {
				t.Errorf("email = %q, want the address that was tried", logins[0].Email)
			}
			if logins[0].IP != "203.0.113.9" {
				t.Errorf("address = %q, want where it came from", logins[0].IP)
			}
			if logins[0].ErrorReason == "" {
				t.Error("no reason was recorded")
			}
		})
	}
}

func TestAFailedAttemptOnAnAddressWithNoAccountSaysNoMoreThanOneWithAnAccount(t *testing.T) {
	// The record is internal, so it may name the address that was tried — but
	// the reason must not distinguish the two cases, or the audit trail becomes
	// a list of which addresses have accounts.
	manager, records := auditedManager(t)
	registered(t, manager, "someone@example.com", "a-long-enough-password-1")

	for _, attempt := range []*LoginRequest{
		{Email: "someone@example.com", Password: "wrong"},
		{Email: "nobody@example.com", Password: "wrong"},
	} {
		_, _, _ = manager.Login(context.Background(), attempt, "203.0.113.9", "a browser")
	}

	logins := records.of(AuditLogin)
	if len(logins) != 2 {
		t.Fatalf("%d attempts recorded", len(logins))
	}
	if logins[0].ErrorReason != logins[1].ErrorReason {
		t.Errorf("reasons differ (%q vs %q), which tells an inspector which addresses exist",
			logins[0].ErrorReason, logins[1].ErrorReason)
	}
}

func TestRegisteringAndChangingAPasswordAreRecorded(t *testing.T) {
	manager, records := auditedManager(t)
	user := registered(t, manager, "someone@example.com", "a-long-enough-password-1")

	if len(records.of(AuditRegister)) != 1 {
		t.Errorf("%d registrations recorded, want one", len(records.of(AuditRegister)))
	}

	err := manager.ChangePassword(context.Background(), user.ID,
		"a-long-enough-password-1", "a-different-long-password-2")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	changes := records.of(AuditPasswordChange)
	if len(changes) != 1 || !changes[0].Success {
		t.Fatalf("password changes recorded = %+v", changes)
	}
	if changes[0].UserID != user.ID {
		t.Errorf("record = %+v, want the account", changes[0])
	}
}

func TestAPasswordChangeWithoutTheCurrentOneIsRecorded(t *testing.T) {
	// Somebody at a signed-in session trying to take the account over is
	// exactly what this record is for.
	manager, records := auditedManager(t)
	user := registered(t, manager, "someone@example.com", "a-long-enough-password-1")

	err := manager.ChangePassword(context.Background(), user.ID, "not-the-password", "a-new-long-password-2")
	if err == nil {
		t.Fatal("the password was changed without the current one")
	}

	changes := records.of(AuditPasswordChange)
	if len(changes) != 1 {
		t.Fatalf("%d password changes recorded, want the refused one", len(changes))
	}
	if changes[0].Success {
		t.Error("a refused change was recorded as a success")
	}
}

func TestSigningOutIsRecorded(t *testing.T) {
	manager, records := auditedManager(t)
	registered(t, manager, "someone@example.com", "a-long-enough-password-1")
	_, tokens, err := manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.com", Password: "a-long-enough-password-1",
	}, "203.0.113.9", "a browser")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	_, claims, err := manager.ValidateToken(context.Background(), tokens.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if err := manager.Logout(context.Background(), claims.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if len(records.of(AuditLogout)) != 1 {
		t.Errorf("%d sign-outs recorded, want one", len(records.of(AuditLogout)))
	}
}

func TestAnAuditStoreThatCannotWriteDoesNotFailTheSignIn(t *testing.T) {
	// An unreachable audit database would otherwise become an outage of the
	// whole service, which is a worse answer to the same problem.
	records := &recordedAudit{err: errors.New("the audit database is down")}
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	}, WithAuditStore(records))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if _, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.com", Password: "a-long-enough-password-1",
	}); err != nil {
		t.Fatalf("registering failed because its record could not be written: %v", err)
	}

	_, _, err = manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.com", Password: "a-long-enough-password-1",
	}, "203.0.113.9", "a browser")
	if err != nil {
		t.Errorf("signing in failed because its record could not be written: %v", err)
	}
}

func TestWithNoAuditStoreNothingIsAsked(t *testing.T) {
	// The default: a service without an audit block behaves as it always did.
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.com", Password: "a-long-enough-password-1",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestTheStoreDecidesWhichEventsItKeeps(t *testing.T) {
	// The `events` list is applied where the records are written, so the
	// manager reports everything and the store filters. Checking it twice
	// would mean two lists to keep in step.
	store := NewMySQLAuditStore(nil, "auth_audit", []string{"login"})
	if store == nil {
		t.Fatal("no store")
	}

	manager, records := auditedManager(t)
	registered(t, manager, "someone@example.com", "a-long-enough-password-1")
	if records.count() == 0 {
		t.Error("the manager reported nothing for the store to filter")
	}
	if len(records.of(AuditRegister)) == 0 {
		t.Error("a registration was not reported")
	}
}

func TestEveryRecordedEventHasANameTheConfigurationCanSelect(t *testing.T) {
	// The names are the configuration surface: an `events` list selects from
	// them, so one the manager emits under another spelling can never be
	// selected.
	for _, name := range []string{
		AuditLogin, AuditLogout, AuditRegister, AuditPasswordChange, AuditTokenRefresh,
	} {
		if name == "" || strings.ContainsAny(name, " \t") {
			t.Errorf("event name %q cannot be written in a list", name)
		}
		if strings.ToLower(name) != name {
			t.Errorf("event name %q is not written the way the configuration is", name)
		}
	}
}
