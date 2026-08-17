package auth

import (
	"context"
	"testing"
	"time"
)

// Nothing started this loop, and nothing else removes expired sessions or
// tokens: every session and every token a service issued stayed where it was
// written, for as long as the process ran.

func cleanupManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		Sessions: &SessionsConfig{
			AbsoluteTimeout: "1h",
			IdleTimeout:     "15m",
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestCleanupRemovesAnExpiredSession(t *testing.T) {
	ctx := context.Background()
	m := cleanupManager(t)

	live := &Session{
		ID: "live", UserID: "u1",
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	stale := &Session{
		ID: "stale", UserID: "u1",
		CreatedAt: time.Now().Add(-2 * time.Hour), LastActiveAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	for _, s := range []*Session{live, stale} {
		if err := m.sessionStore.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	if err := m.Cleanup(ctx); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := m.sessionStore.FindByID(ctx, "stale"); err == nil {
		t.Error("an expired session survived the cleanup")
	}
	if _, err := m.sessionStore.FindByID(ctx, "live"); err != nil {
		t.Errorf("a live session was removed: %v", err)
	}
}

func TestTheCleanupServiceRunsAndStops(t *testing.T) {
	ctx := context.Background()
	m := cleanupManager(t)

	// An interval short enough that the loop turns over during the test.
	s := NewCleanupService(m, 20*time.Millisecond)

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Starting twice must not launch a second loop or block.
	if err := s.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}

	stale := &Session{
		ID: "stale", UserID: "u1",
		CreatedAt: time.Now().Add(-2 * time.Hour), LastActiveAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if err := m.sessionStore.Create(ctx, stale); err != nil {
		t.Fatalf("Create: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := m.sessionStore.FindByID(ctx, "stale"); err != nil {
			break // removed by the loop
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := m.sessionStore.FindByID(ctx, "stale"); err == nil {
		t.Error("the loop ran without removing an expired session")
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// And stopping twice is not an error either, since shutdown may be reached
	// from more than one path.
	if err := s.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

func TestTheDefaultIntervalIsNotZero(t *testing.T) {
	// A zero interval would make time.NewTicker panic, taking the service down
	// at startup rather than cleaning anything.
	s := NewCleanupService(cleanupManager(t), 0)
	if s.interval <= 0 {
		t.Errorf("interval = %v", s.interval)
	}
}
