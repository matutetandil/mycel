package auth

import (
	"context"
	"testing"
	"time"
)

func TestCleanupService(t *testing.T) {
	t.Run("start and stop", func(t *testing.T) {
		manager, err := NewManager(&Config{
			Preset: "development",
			JWT: &JWTConfig{
				Secret: "test-secret-key-for-testing-only",
			},
		})
		if err != nil {
			t.Fatalf("NewManager error: %v", err)
		}

		svc := NewCleanupService(manager, 100*time.Millisecond)
		ctx := context.Background()

		// Start service
		err = svc.Start(ctx)
		if err != nil {
			t.Fatalf("Start error: %v", err)
		}

		// Wait for at least one cleanup cycle
		time.Sleep(250 * time.Millisecond)

		// Stop service
		err = svc.Stop()
		if err != nil {
			t.Fatalf("Stop error: %v", err)
		}
	})

	t.Run("double start is no-op", func(t *testing.T) {
		manager, _ := NewManager(&Config{
			Preset: "development",
			JWT: &JWTConfig{
				Secret: "test-secret-key-for-testing-only",
			},
		})

		svc := NewCleanupService(manager, 100*time.Millisecond)
		ctx := context.Background()

		// Start twice
		svc.Start(ctx)
		err := svc.Start(ctx) // Should be no-op
		if err != nil {
			t.Fatalf("Second Start error: %v", err)
		}

		svc.Stop()
	})

	t.Run("double stop is no-op", func(t *testing.T) {
		manager, _ := NewManager(&Config{
			Preset: "development",
			JWT: &JWTConfig{
				Secret: "test-secret-key-for-testing-only",
			},
		})

		svc := NewCleanupService(manager, 100*time.Millisecond)
		ctx := context.Background()

		svc.Start(ctx)
		svc.Stop()

		// Second stop should be no-op
		err := svc.Stop()
		if err != nil {
			t.Fatalf("Second Stop error: %v", err)
		}
	})

	t.Run("context cancellation stops service", func(t *testing.T) {
		manager, _ := NewManager(&Config{
			Preset: "development",
			JWT: &JWTConfig{
				Secret: "test-secret-key-for-testing-only",
			},
		})

		svc := NewCleanupService(manager, 100*time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())

		svc.Start(ctx)
		time.Sleep(50 * time.Millisecond)

		// Cancel context
		cancel()
		time.Sleep(50 * time.Millisecond)

		// Cleanup - this should be a no-op since already stopped
		svc.Stop()
	})
}

func TestMemorySessionStoreWithIdle(t *testing.T) {
	t.Run("delete idle sessions", func(t *testing.T) {
		store := NewMemorySessionStoreWithIdle()
		ctx := context.Background()

		// Create sessions with different last active times
		now := time.Now()

		sessions := []*Session{
			{
				ID:           "session1",
				UserID:       "user1",
				CreatedAt:    now.Add(-2 * time.Hour),
				LastActiveAt: now.Add(-1 * time.Hour), // Active 1 hour ago
				ExpiresAt:    now.Add(1 * time.Hour),
			},
			{
				ID:           "session2",
				UserID:       "user1",
				CreatedAt:    now.Add(-30 * time.Minute),
				LastActiveAt: now.Add(-5 * time.Minute), // Active 5 minutes ago
				ExpiresAt:    now.Add(1 * time.Hour),
			},
			{
				ID:           "session3",
				UserID:       "user2",
				CreatedAt:    now.Add(-10 * time.Minute),
				LastActiveAt: now, // Just active
				ExpiresAt:    now.Add(1 * time.Hour),
			},
		}

		for _, s := range sessions {
			if err := store.Create(ctx, s); err != nil {
				t.Fatalf("Create error: %v", err)
			}
		}

		// Delete sessions idle for more than 30 minutes
		threshold := now.Add(-30 * time.Minute)
		deleted, err := store.DeleteIdle(ctx, threshold)
		if err != nil {
			t.Fatalf("DeleteIdle error: %v", err)
		}

		if deleted != 1 {
			t.Errorf("expected 1 deleted session, got %d", deleted)
		}

		// Verify session1 is gone
		_, err = store.FindByID(ctx, "session1")
		if err == nil {
			t.Error("expected session1 to be deleted")
		}

		// Verify session2 and session3 still exist
		_, err = store.FindByID(ctx, "session2")
		if err != nil {
			t.Errorf("session2 should still exist: %v", err)
		}

		_, err = store.FindByID(ctx, "session3")
		if err != nil {
			t.Errorf("session3 should still exist: %v", err)
		}
	})

	t.Run("delete expired and idle", func(t *testing.T) {
		store := NewMemorySessionStoreWithIdle()
		ctx := context.Background()

		now := time.Now()

		sessions := []*Session{
			{
				ID:           "expired",
				UserID:       "user1",
				CreatedAt:    now.Add(-2 * time.Hour),
				LastActiveAt: now.Add(-1 * time.Hour),
				ExpiresAt:    now.Add(-30 * time.Minute), // Expired
			},
			{
				ID:           "idle",
				UserID:       "user1",
				CreatedAt:    now.Add(-2 * time.Hour),
				LastActiveAt: now.Add(-1 * time.Hour), // Idle > 30min
				ExpiresAt:    now.Add(1 * time.Hour),  // Not expired
			},
			{
				ID:           "active",
				UserID:       "user2",
				CreatedAt:    now.Add(-10 * time.Minute),
				LastActiveAt: now,                    // Active
				ExpiresAt:    now.Add(1 * time.Hour), // Not expired
			},
		}

		for _, s := range sessions {
			store.Create(ctx, s)
		}

		// Delete expired and idle (idle timeout = 30 minutes)
		deleted, err := store.DeleteExpiredAndIdle(ctx, 30*time.Minute)
		if err != nil {
			t.Fatalf("DeleteExpiredAndIdle error: %v", err)
		}

		if deleted != 2 {
			t.Errorf("expected 2 deleted sessions (expired + idle), got %d", deleted)
		}

		// Only "active" should remain
		_, err = store.FindByID(ctx, "expired")
		if err == nil {
			t.Error("expected 'expired' to be deleted")
		}

		_, err = store.FindByID(ctx, "idle")
		if err == nil {
			t.Error("expected 'idle' to be deleted")
		}

		_, err = store.FindByID(ctx, "active")
		if err != nil {
			t.Errorf("'active' should still exist: %v", err)
		}
	})
}
