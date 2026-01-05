package auth

import (
	"context"
	"testing"
	"time"
)

func TestBruteForceService(t *testing.T) {
	t.Run("basic brute force protection", func(t *testing.T) {
		store := NewMemoryBruteForceStore()
		config := &BruteForceConfig{
			Enabled:     true,
			MaxAttempts: 3,
			Window:      "15m",
			LockoutTime: "15m",
			TrackBy:     "ip+user",
		}

		svc := NewBruteForceService(config, store)
		ctx := context.Background()
		key := "test@example.com:127.0.0.1"

		// First check should allow access
		allowed, delay, remaining, err := svc.CheckAccess(ctx, key)
		if err != nil {
			t.Fatalf("CheckAccess error: %v", err)
		}
		if !allowed {
			t.Error("expected access to be allowed initially")
		}
		if delay > 0 {
			t.Error("expected no delay initially")
		}
		if remaining > 0 {
			t.Error("expected no remaining lockout initially")
		}

		// Record failed attempts
		for i := 0; i < 2; i++ {
			locked, err := svc.RecordFailedAttempt(ctx, key)
			if err != nil {
				t.Fatalf("RecordFailedAttempt error: %v", err)
			}
			if locked {
				t.Errorf("should not be locked after %d attempts", i+1)
			}
		}

		// Third attempt should lock
		locked, err := svc.RecordFailedAttempt(ctx, key)
		if err != nil {
			t.Fatalf("RecordFailedAttempt error: %v", err)
		}
		if !locked {
			t.Error("expected account to be locked after max attempts")
		}

		// Check access should now be denied
		allowed, _, remaining, err = svc.CheckAccess(ctx, key)
		if err != nil {
			t.Fatalf("CheckAccess error: %v", err)
		}
		if allowed {
			t.Error("expected access to be denied while locked")
		}
		if remaining <= 0 {
			t.Error("expected lockout remaining time > 0")
		}
	})

	t.Run("progressive delay", func(t *testing.T) {
		store := NewMemoryBruteForceStore()
		config := &BruteForceConfig{
			Enabled:     true,
			MaxAttempts: 10,
			Window:      "15m",
			LockoutTime: "15m",
			TrackBy:     "user",
			ProgressiveDelay: &ProgressiveDelayConfig{
				Enabled:    true,
				Initial:    "1s",
				Multiplier: 2,
				Max:        "30s",
			},
		}

		svc := NewBruteForceService(config, store)
		ctx := context.Background()
		key := "test@example.com"

		// Progressive delay increases after each failed attempt
		// Delay formula: initial * (multiplier ^ (attempts - 1))
		// After attempt 1: no delay (first attempt)
		// After attempt 2: 1s * 2^1 = 2s
		// After attempt 3: 1s * 2^2 = 4s
		// etc., capped at 30s

		expectedDelays := []time.Duration{
			0,                // After 1st attempt: no delay
			2 * time.Second,  // After 2nd attempt: 2s
			4 * time.Second,  // After 3rd attempt: 4s
			8 * time.Second,  // After 4th attempt: 8s
			16 * time.Second, // After 5th attempt: 16s
			30 * time.Second, // After 6th attempt: capped at 30s
			30 * time.Second, // After 7th attempt: still capped at 30s
		}

		for i := 0; i < len(expectedDelays); i++ {
			_, err := svc.RecordFailedAttempt(ctx, key)
			if err != nil {
				t.Fatalf("RecordFailedAttempt error: %v", err)
			}

			_, delay, _, err := svc.CheckAccess(ctx, key)
			if err != nil {
				t.Fatalf("CheckAccess error: %v", err)
			}

			if delay != expectedDelays[i] {
				t.Errorf("after attempt %d: expected delay %v, got %v", i+1, expectedDelays[i], delay)
			}
		}
	})

	t.Run("success clears attempts", func(t *testing.T) {
		store := NewMemoryBruteForceStore()
		config := &BruteForceConfig{
			Enabled:     true,
			MaxAttempts: 3,
			Window:      "15m",
			LockoutTime: "15m",
		}

		svc := NewBruteForceService(config, store)
		ctx := context.Background()
		key := "test@example.com"

		// Record some failed attempts
		for i := 0; i < 2; i++ {
			svc.RecordFailedAttempt(ctx, key)
		}

		// Record success
		err := svc.RecordSuccess(ctx, key)
		if err != nil {
			t.Fatalf("RecordSuccess error: %v", err)
		}

		// Stats should show 0 attempts
		stats, err := svc.GetStats(ctx, key)
		if err != nil {
			t.Fatalf("GetStats error: %v", err)
		}
		if stats.Attempts != 0 {
			t.Errorf("expected 0 attempts after success, got %d", stats.Attempts)
		}
	})

	t.Run("disabled brute force", func(t *testing.T) {
		store := NewMemoryBruteForceStore()
		config := &BruteForceConfig{
			Enabled: false,
		}

		svc := NewBruteForceService(config, store)
		ctx := context.Background()
		key := "test@example.com"

		// All operations should succeed when disabled
		allowed, _, _, err := svc.CheckAccess(ctx, key)
		if err != nil {
			t.Fatalf("CheckAccess error: %v", err)
		}
		if !allowed {
			t.Error("expected access to be allowed when brute force is disabled")
		}

		// Recording attempts shouldn't lock
		for i := 0; i < 100; i++ {
			locked, _ := svc.RecordFailedAttempt(ctx, key)
			if locked {
				t.Error("should not lock when brute force is disabled")
			}
		}
	})

	t.Run("get stats", func(t *testing.T) {
		store := NewMemoryBruteForceStore()
		config := &BruteForceConfig{
			Enabled:     true,
			MaxAttempts: 5,
			Window:      "15m",
			LockoutTime: "15m",
		}

		svc := NewBruteForceService(config, store)
		ctx := context.Background()
		key := "test@example.com"

		// Record some attempts
		svc.RecordFailedAttempt(ctx, key)
		svc.RecordFailedAttempt(ctx, key)

		stats, err := svc.GetStats(ctx, key)
		if err != nil {
			t.Fatalf("GetStats error: %v", err)
		}

		if stats.Attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", stats.Attempts)
		}
		if stats.MaxAttempts != 5 {
			t.Errorf("expected max 5, got %d", stats.MaxAttempts)
		}
		if stats.Locked {
			t.Error("should not be locked yet")
		}
	})
}

func TestBruteForceStoreDelay(t *testing.T) {
	t.Run("memory store delay", func(t *testing.T) {
		store := NewMemoryBruteForceStore()
		ctx := context.Background()
		key := "test"

		// Set delay
		err := store.SetDelay(ctx, key, 5*time.Second, 15*time.Minute)
		if err != nil {
			t.Fatalf("SetDelay error: %v", err)
		}

		// Get delay
		delay, err := store.GetDelay(ctx, key)
		if err != nil {
			t.Fatalf("GetDelay error: %v", err)
		}
		if delay != 5*time.Second {
			t.Errorf("expected 5s delay, got %v", delay)
		}

		// Reset should clear delay
		err = store.Reset(ctx, key)
		if err != nil {
			t.Fatalf("Reset error: %v", err)
		}

		delay, err = store.GetDelay(ctx, key)
		if err != nil {
			t.Fatalf("GetDelay error: %v", err)
		}
		if delay != 0 {
			t.Errorf("expected 0 delay after reset, got %v", delay)
		}
	})
}
