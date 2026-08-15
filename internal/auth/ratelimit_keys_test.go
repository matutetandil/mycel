package auth

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Rate limiting per caller, which is the only kind that helps on a sign-in
// endpoint: one shared bucket means the first attacker locks out everybody
// else. The cost is a map keyed by whatever the caller sent, and nothing was
// ever letting go of it.

func perKey(t *testing.T) *PerKeyRateLimiter {
	t.Helper()
	return NewPerKeyRateLimiter(&RateLimitConfig{
		Enabled: true,
		Window:  "1m",
		Login:   &EndpointRateLimit{Rate: 3, Burst: 3, Window: "1m"},
	})
}

func TestOneCallerRunningOutDoesNotLockOutTheRest(t *testing.T) {
	rl := perKey(t)

	// Spend one caller's allowance.
	spent := 0
	for i := 0; i < 20; i++ {
		if !rl.Allow("login", "10.0.0.1") {
			break
		}
		spent++
	}
	if spent == 0 {
		t.Fatal("the first request was already refused")
	}
	if rl.Allow("login", "10.0.0.1") {
		t.Error("a caller past its allowance was let through")
	}

	// Somebody else is unaffected, which is the whole point.
	if !rl.Allow("login", "10.0.0.2") {
		t.Error("a caller who has done nothing was refused")
	}
}

func TestKeysNobodyIsUsingAreLetGoOf(t *testing.T) {
	// The map is keyed by whatever the caller sent, on an endpoint facing the
	// internet, and Cleanup used to do nothing at all — so one entry per
	// address that ever tried to sign in, kept for the life of the process.
	rl := perKey(t)

	for i := 0; i < 500; i++ {
		rl.Allow("login", fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}

	if held := heldKeys(rl, "login"); held != 500 {
		t.Fatalf("%d keys held, want the 500 that called", held)
	}

	// Nothing has gone idle yet, so nothing is dropped.
	rl.Cleanup()
	if held := heldKeys(rl, "login"); held != 500 {
		t.Errorf("%d keys after a sweep with nothing idle, want them all kept", held)
	}

	// Age them past the point where their limiter would have refilled: from
	// there, keeping the entry and building a fresh one are the same thing.
	ageKeys(rl, "login", -time.Hour)
	rl.Cleanup()

	if held := heldKeys(rl, "login"); held != 0 {
		t.Errorf("%d keys still held after they all went idle", held)
	}
}

func TestACallerStillInsideItsWindowIsKept(t *testing.T) {
	// Dropping a key early hands back a full allowance, which is the same as
	// having no rate limit at all for anybody patient enough to wait.
	rl := perKey(t)

	for i := 0; i < 5; i++ {
		rl.Allow("login", "10.0.0.1")
	}
	rl.Cleanup()

	if rl.Allow("login", "10.0.0.1") {
		t.Error("a caller past its allowance got a fresh one from a sweep")
	}
}

func TestASweepRunsOnItsOwn(t *testing.T) {
	rl := perKey(t)
	rl.Allow("login", "10.0.0.1")
	ageKeys(rl, "login", -time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rl.StartCleanup(ctx, 10*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if heldKeys(rl, "login") == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("the sweep never ran")
}

func TestNoRateLimitingLetsEverythingThrough(t *testing.T) {
	rl := NewPerKeyRateLimiter(nil)
	for i := 0; i < 100; i++ {
		if !rl.Allow("login", "10.0.0.1") {
			t.Fatal("a service with no rate limiting refused a request")
		}
	}
	// And a sweep over nothing is not a panic.
	rl.Cleanup()
}

func TestWaitingForAnAllowanceRespectsTheCaller(t *testing.T) {
	// Wait blocks rather than refusing, and a caller that gives up has to be
	// let go of rather than held until the allowance comes back.
	rl := NewRateLimiter(&RateLimitConfig{
		Enabled: true,
		Login:   &EndpointRateLimit{Rate: 1, Burst: 1, Window: "1h"},
	})

	if err := rl.Wait(context.Background(), "login"); err != nil {
		t.Fatalf("the first request waited and failed: %v", err)
	}

	// The next one would wait an hour; the caller gives up.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := rl.Wait(ctx, "login"); err == nil {
		t.Error("a caller that gave up was reported as allowed")
	}

	// An endpoint nobody configured is not limited.
	if err := rl.Wait(context.Background(), "an_endpoint_nobody_limits"); err != nil {
		t.Errorf("an unlimited endpoint made the caller wait: %v", err)
	}

	// And a service with rate limiting off never waits at all.
	off := NewRateLimiter(&RateLimitConfig{Enabled: false})
	if err := off.Wait(context.Background(), "login"); err != nil {
		t.Errorf("a service with no rate limiting made the caller wait: %v", err)
	}
}

// heldKeys counts what the limiter is still holding for an endpoint.
func heldKeys(rl *PerKeyRateLimiter, endpoint string) int {
	rl.mu.RLock()
	pkl, ok := rl.limiters[endpoint]
	rl.mu.RUnlock()
	if !ok {
		return 0
	}
	pkl.mu.RLock()
	defer pkl.mu.RUnlock()
	return len(pkl.limiters)
}

// ageKeys moves every key's last use into the past.
func ageKeys(rl *PerKeyRateLimiter, endpoint string, by time.Duration) {
	rl.mu.RLock()
	pkl, ok := rl.limiters[endpoint]
	rl.mu.RUnlock()
	if !ok {
		return
	}
	pkl.mu.Lock()
	defer pkl.mu.Unlock()
	for key := range pkl.lastSeen {
		pkl.lastSeen[key] = time.Now().Add(by)
	}
}
