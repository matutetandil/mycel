package auth

import (
	"context"
	"testing"
	"time"
)

// Brute-force protection was implemented twice: once inside the manager, which
// counted attempts and locked accounts, and once here, which does that and the
// progressive delays as well. Only the first was ever called, so a
// progressive_delay block parsed, validated and did nothing at all.

func bruteForceService(cfg *BruteForceConfig) *BruteForceService {
	return NewBruteForceService(cfg, NewMemoryBruteForceStore())
}

func TestAccessIsAllowedUntilTheAttemptsRunOut(t *testing.T) {
	ctx := context.Background()
	s := bruteForceService(&BruteForceConfig{
		Enabled: true, MaxAttempts: 3, Window: "15m", LockoutTime: "1h",
	})

	for i := 0; i < 2; i++ {
		locked, err := s.RecordFailedAttempt(ctx, "someone@example.com")
		if err != nil {
			t.Fatalf("RecordFailedAttempt: %v", err)
		}
		if locked {
			t.Fatalf("locked after %d attempts, want 3", i+1)
		}
	}

	allowed, _, _, err := s.CheckAccess(ctx, "someone@example.com")
	if err != nil || !allowed {
		t.Fatalf("access refused before the limit: allowed=%v err=%v", allowed, err)
	}

	locked, err := s.RecordFailedAttempt(ctx, "someone@example.com")
	if err != nil {
		t.Fatalf("RecordFailedAttempt: %v", err)
	}
	if !locked {
		t.Error("the third failure did not lock the account")
	}

	allowed, _, remaining, err := s.CheckAccess(ctx, "someone@example.com")
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if allowed {
		t.Error("access allowed while locked out")
	}
	if remaining <= 0 {
		t.Errorf("lockout remaining = %v, want the caller to be able to say when it ends", remaining)
	}
}

func TestASuccessfulLoginClearsTheCount(t *testing.T) {
	// Otherwise someone who mistypes twice a day is permanently one attempt
	// away from being locked out of their own account.
	ctx := context.Background()
	s := bruteForceService(&BruteForceConfig{
		Enabled: true, MaxAttempts: 3, Window: "15m", LockoutTime: "1h",
	})

	for i := 0; i < 2; i++ {
		if _, err := s.RecordFailedAttempt(ctx, "k"); err != nil {
			t.Fatalf("RecordFailedAttempt: %v", err)
		}
	}
	if err := s.RecordSuccess(ctx, "k"); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}

	// Two more failures must not lock, because the count started over.
	for i := 0; i < 2; i++ {
		locked, err := s.RecordFailedAttempt(ctx, "k")
		if err != nil {
			t.Fatalf("RecordFailedAttempt: %v", err)
		}
		if locked {
			t.Fatal("the count survived a successful login")
		}
	}
}

func TestTheDelayGrowsWithEachFailure(t *testing.T) {
	ctx := context.Background()
	s := bruteForceService(&BruteForceConfig{
		Enabled: true, MaxAttempts: 100, Window: "15m", LockoutTime: "1h",
		ProgressiveDelay: &ProgressiveDelayConfig{
			Enabled: true, Initial: "1s", Max: "10s", Multiplier: 2,
		},
	})

	// The first failure costs nothing; a single typo should not be punished.
	if _, err := s.RecordFailedAttempt(ctx, "k"); err != nil {
		t.Fatalf("RecordFailedAttempt: %v", err)
	}
	_, delay, _, err := s.CheckAccess(ctx, "k")
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if delay != 0 {
		t.Errorf("delay after one failure = %v, want none", delay)
	}

	var previous time.Duration
	for attempt := 2; attempt <= 5; attempt++ {
		if _, err := s.RecordFailedAttempt(ctx, "k"); err != nil {
			t.Fatalf("RecordFailedAttempt: %v", err)
		}
		_, delay, _, err := s.CheckAccess(ctx, "k")
		if err != nil {
			t.Fatalf("CheckAccess: %v", err)
		}
		if delay <= previous && delay < 10*time.Second {
			t.Errorf("after %d failures the delay is %v, which did not grow from %v", attempt, delay, previous)
		}
		previous = delay
	}

	// And it stops growing where the configuration says.
	if previous > 10*time.Second {
		t.Errorf("delay reached %v, past the configured maximum of 10s", previous)
	}
}

func TestProtectionOffMeansNoCountingAtAll(t *testing.T) {
	ctx := context.Background()
	for _, cfg := range []*BruteForceConfig{
		nil,
		{Enabled: false, MaxAttempts: 1},
	} {
		s := bruteForceService(cfg)
		for i := 0; i < 5; i++ {
			locked, err := s.RecordFailedAttempt(ctx, "k")
			if err != nil {
				t.Fatalf("RecordFailedAttempt: %v", err)
			}
			if locked {
				t.Fatal("an account was locked with protection turned off")
			}
		}
		allowed, delay, _, err := s.CheckAccess(ctx, "k")
		if err != nil || !allowed || delay != 0 {
			t.Errorf("allowed=%v delay=%v err=%v, want unimpeded access", allowed, delay, err)
		}
	}
}

func TestStatsReportWhatIsBeingCounted(t *testing.T) {
	ctx := context.Background()
	s := bruteForceService(&BruteForceConfig{
		Enabled: true, MaxAttempts: 5, Window: "15m", LockoutTime: "1h",
	})
	for i := 0; i < 3; i++ {
		if _, err := s.RecordFailedAttempt(ctx, "k"); err != nil {
			t.Fatalf("RecordFailedAttempt: %v", err)
		}
	}

	stats, err := s.GetStats(ctx, "k")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", stats.Attempts)
	}
	if stats.Locked {
		t.Error("reported as locked below the limit")
	}
	if stats.MaxAttempts != 5 {
		t.Errorf("max = %d, want the configured 5 so a caller can say how many are left", stats.MaxAttempts)
	}
}
