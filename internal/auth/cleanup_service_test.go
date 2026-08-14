package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// The idle timeout, which is a different thing from expiry: expiry ends a
// session at its absolute age, and for an eight-hour session that means a screen
// somebody walked away from stays signed in all day. This used to be parsed and
// then dropped with a note saying expiry would handle it.

func sessionAt(id, userID string, lastActive, expires time.Time) *Session {
	return &Session{
		ID: id, UserID: userID,
		CreatedAt:    lastActive.Add(-time.Minute),
		LastActiveAt: lastActive,
		ExpiresAt:    expires,
	}
}

func TestASessionNobodyHasTouchedIsEnded(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySessionStoreWithIdle()

	now := time.Now()
	sessions := []*Session{
		sessionAt("live", "u-1", now, now.Add(time.Hour)),
		sessionAt("abandoned", "u-2", now.Add(-45*time.Minute), now.Add(time.Hour)),
	}
	for _, s := range sessions {
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	deleted, err := store.DeleteIdle(ctx, now.Add(-15*time.Minute))
	if err != nil {
		t.Fatalf("DeleteIdle: %v", err)
	}
	if deleted != 1 {
		t.Errorf("%d sessions ended, want the abandoned one", deleted)
	}

	if _, err := store.FindByID(ctx, "live"); err != nil {
		t.Error("a session somebody is using was ended")
	}
	if _, err := store.FindByID(ctx, "abandoned"); err == nil {
		t.Error("a session nobody has touched is still open")
	}

	// And the account's own list, which is what "sign out everywhere" reads
	// and what a limit on concurrent sessions counts.
	remaining, err := store.FindByUserID(ctx, "u-2")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("the account still lists %d sessions that are gone", len(remaining))
	}
}

func TestExpiryAndIdlenessAreSweptTogether(t *testing.T) {
	// One pass over the sessions rather than two, which is what a store with a
	// lot of them wants — and both reasons a session should end are checked.
	ctx := context.Background()
	store := NewMemorySessionStoreWithIdle()

	now := time.Now()
	for _, s := range []*Session{
		sessionAt("live", "u-1", now, now.Add(time.Hour)),
		sessionAt("expired", "u-2", now, now.Add(-time.Minute)),
		sessionAt("abandoned", "u-3", now.Add(-45*time.Minute), now.Add(time.Hour)),
	} {
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	deleted, err := store.DeleteExpiredAndIdle(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("DeleteExpiredAndIdle: %v", err)
	}
	if deleted != 2 {
		t.Errorf("%d sessions ended, want the expired one and the abandoned one", deleted)
	}
	if _, err := store.FindByID(ctx, "live"); err != nil {
		t.Error("a session that is neither expired nor idle was ended")
	}
}

func TestWithNoIdleTimeoutOnlyExpiryEndsASession(t *testing.T) {
	// A service that does not configure an idle timeout must not have one
	// applied anyway: sessions there end when they expire and not before.
	ctx := context.Background()
	store := NewMemorySessionStoreWithIdle()

	now := time.Now()
	if err := store.Create(ctx, sessionAt("old-but-valid", "u-1",
		now.Add(-8*time.Hour), now.Add(time.Hour))); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := store.DeleteExpiredAndIdle(ctx, 0); err != nil {
		t.Fatalf("DeleteExpiredAndIdle: %v", err)
	}
	if _, err := store.FindByID(ctx, "old-but-valid"); err != nil {
		t.Error("a session was ended for idleness by a service that configured none")
	}
}

func TestTheSweepStopsWithTheServiceAroundIt(t *testing.T) {
	// Otherwise a cancelled runtime leaves a ticker running for the life of
	// the process.
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	service := NewCleanupService(m, 20*time.Millisecond)
	service.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	if err := service.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Starting twice is what a hot reload does, and must not run two sweeps
	// over the same store.
	if err := service.Start(ctx); err != nil {
		t.Fatalf("the second start failed: %v", err)
	}

	cancel()

	select {
	case <-service.doneCh:
	case <-time.After(2 * time.Second):
		t.Error("the sweep is still running after the context was cancelled")
	}
}

func TestAnOptionThatIsNotThereDoesNotBringTheServiceDown(t *testing.T) {
	// The runtime assembles this list from whatever storage the configuration
	// names, so one branch returning nothing used to end the process with a
	// segmentation fault at startup rather than a message.
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
	}, WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if m == nil {
		t.Fatal("no manager was built")
	}
}
