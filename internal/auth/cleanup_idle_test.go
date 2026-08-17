package auth

import (
	"context"
	"testing"
	"time"
)

// An idle timeout is what ends a session left open on an unattended screen. It
// was configurable, documented, and enforced by nothing: the cleanup parsed the
// value and returned, with a note saying DeleteExpired would handle it — and
// DeleteExpired removes sessions past their absolute lifetime, which a session
// abandoned an hour ago is not.

func idleManager(t *testing.T, idleTimeout string) *Manager {
	t.Helper()
	m, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		Sessions: &SessionsConfig{IdleTimeout: idleTimeout, AbsoluteTimeout: "24h"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestAnIdleSessionIsEnded(t *testing.T) {
	ctx := context.Background()
	m := idleManager(t, "15m")

	active := &Session{
		ID: "active", UserID: "u1",
		CreatedAt: time.Now().Add(-time.Hour), LastActiveAt: time.Now(),
		ExpiresAt: time.Now().Add(23 * time.Hour),
	}
	// Well inside its lifetime, and untouched for an hour.
	abandoned := &Session{
		ID: "abandoned", UserID: "u1",
		CreatedAt: time.Now().Add(-2 * time.Hour), LastActiveAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(22 * time.Hour),
	}
	for _, s := range []*Session{active, abandoned} {
		if err := m.sessionStore.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	if err := NewCleanupService(m, time.Minute).cleanIdleSessions(ctx); err != nil {
		t.Fatalf("cleanIdleSessions: %v", err)
	}

	if _, err := m.sessionStore.FindByID(ctx, "abandoned"); err == nil {
		t.Error("a session untouched for an hour survived a fifteen minute idle timeout")
	}
	if _, err := m.sessionStore.FindByID(ctx, "active"); err != nil {
		t.Errorf("a session in use was ended: %v", err)
	}
}

func TestWithoutAnIdleTimeoutNothingIsEnded(t *testing.T) {
	ctx := context.Background()
	m := idleManager(t, "")

	old := &Session{
		ID: "old", UserID: "u1",
		CreatedAt: time.Now().Add(-48 * time.Hour), LastActiveAt: time.Now().Add(-48 * time.Hour),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := m.sessionStore.Create(ctx, old); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := NewCleanupService(m, time.Minute).cleanIdleSessions(ctx); err != nil {
		t.Fatalf("cleanIdleSessions: %v", err)
	}
	if _, err := m.sessionStore.FindByID(ctx, "old"); err != nil {
		t.Error("a session was ended with no idle timeout configured")
	}
}

func TestTheDefaultStoreCanEnforceAnIdleTimeout(t *testing.T) {
	// The configuration offers the setting, so the store a service gets
	// without asking has to be able to honour it.
	m := idleManager(t, "15m")
	if _, ok := m.sessionStore.(SessionCleanupStore); !ok {
		t.Errorf("the default session store is %T, which cannot end an idle session", m.sessionStore)
	}
}

func TestTouchKeepsASessionAlive(t *testing.T) {
	ctx := context.Background()
	m := idleManager(t, "15m")

	s := &Session{
		ID: "s", UserID: "u1",
		CreatedAt: time.Now().Add(-time.Hour), LastActiveAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(23 * time.Hour),
	}
	if err := m.sessionStore.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.sessionStore.Touch(ctx, "s"); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	if err := NewCleanupService(m, time.Minute).cleanIdleSessions(ctx); err != nil {
		t.Fatalf("cleanIdleSessions: %v", err)
	}
	if _, err := m.sessionStore.FindByID(ctx, "s"); err != nil {
		t.Error("a session that was just used was ended as idle")
	}
}
