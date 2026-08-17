package auth

import (
	"context"
	"testing"
	"time"
)

// A failed sign-in answers slowly and by an amount that varies, for two
// different reasons: the wait makes guessing expensive, and the variation stops
// the answer from telling an attacker anything.
//
// Measured before this existed: an address with no account answered in 0.4ms
// and an address with one in 46ms, because the missing case returned before the
// password hash was computed. A hundredfold difference is a way to harvest
// which addresses have accounts without guessing a single password.

func delayManager(t *testing.T, failDelay string) *Manager {
	t.Helper()
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		Security: &SecurityConfig{BruteForce: &BruteForceConfig{
			Enabled: true, MaxAttempts: 3, Window: "15m", LockoutTime: "10m",
			FailDelay: failDelay,
		}},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestADecoyCostsWhatARealHashCosts(t *testing.T) {
	// The equalisation has to hold on its own, not only because the delay is
	// long enough to hide the difference — otherwise turning the delay off
	// brings the oracle back.
	h := NewPasswordHasher(&PasswordConfig{
		Algorithm: "argon2id", Memory: 65536, Iterations: 3, Parallelism: 2,
		SaltLength: 16, KeyLength: 32,
	})
	real, err := h.Hash("correct-horse")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	measure := func(hash string) time.Duration {
		start := time.Now()
		for i := 0; i < 3; i++ {
			h.Verify("guess", hash)
		}
		return time.Since(start) / 3
	}

	realCost, decoyCost := measure(real), measure(decoyHash)
	if decoyCost < realCost/2 {
		t.Errorf("the decoy costs %v against %v for a real hash, so a missing account is still the faster answer",
			decoyCost, realCost)
	}
}

func TestAFailedSignInWaits(t *testing.T) {
	m := delayManager(t, "200ms")

	start := time.Now()
	_, _, err := m.Login(context.Background(), &LoginRequest{
		Email: "nobody@example.com", Password: "guess",
	}, "1.2.3.4", "test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a sign-in for an account that does not exist succeeded")
	}
	// Allowing for the jitter, which takes a quarter off the bottom.
	if elapsed < 140*time.Millisecond {
		t.Errorf("the failure answered in %v, so the wait did not apply", elapsed)
	}
}

func TestTheWaitVaries(t *testing.T) {
	// A constant is something an attacker subtracts. What makes the timing
	// useless is that it moves.
	m := delayManager(t, "100ms")

	seen := map[time.Duration]bool{}
	for i := 0; i < 8; i++ {
		seen[m.failureDelay()] = true
	}
	if len(seen) < 4 {
		t.Errorf("eight waits produced %d distinct values, which is close enough to constant", len(seen))
	}

	// And it stays near what was configured, so the setting means something.
	for wait := range seen {
		if wait < 80*time.Millisecond || wait > 120*time.Millisecond {
			t.Errorf("a wait of %v is outside the quarter either side of 100ms", wait)
		}
	}
}

func TestTheWaitCanBeTurnedOff(t *testing.T) {
	// Tests need this; nothing else should.
	m := delayManager(t, "0")
	if got := m.failureDelay(); got != 0 {
		t.Errorf("fail_delay = \"0\" produced a wait of %v", got)
	}
}

func TestWithoutConfigurationThereIsStillAWait(t *testing.T) {
	// The default protects a service whose author never thought about this,
	// which is the one that needs protecting.
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if got := m.failureDelay(); got <= 0 {
		t.Errorf("a service that configured nothing answers failures instantly (%v)", got)
	}
}

func TestJitterStaysCentredOnTheBase(t *testing.T) {
	var total time.Duration
	const runs = 200
	for i := 0; i < runs; i++ {
		total += jitter(time.Second, 0.25)
	}
	average := total / runs

	// The average is the number someone configured; the spread is around it.
	if average < 900*time.Millisecond || average > 1100*time.Millisecond {
		t.Errorf("the average wait is %v, want it near the second that was asked for", average)
	}
}

func TestLockingHappensAtTheConfiguredAttempt(t *testing.T) {
	ctx := context.Background()
	m := delayManager(t, "0")

	if _, _, err := m.Register(ctx, &RegisterRequest{
		Email: "person@example.com", Password: "correct-horse-battery",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 1; i <= 3; i++ {
		_, _, err := m.Login(ctx, &LoginRequest{Email: "person@example.com", Password: "wrong"}, "1.2.3.4", "t")
		if err == nil {
			t.Fatalf("attempt %d with a wrong password succeeded", i)
		}
	}

	// The right password afterwards is refused too — the account is locked,
	// not the guess.
	_, _, err := m.Login(ctx, &LoginRequest{
		Email: "person@example.com", Password: "correct-horse-battery",
	}, "1.2.3.4", "t")
	if err == nil {
		t.Fatal("the account was not locked after three failures")
	}
	var authErr *AuthError
	if !asAuthError(err, &authErr) || authErr.Code != "account_locked" {
		t.Errorf("err = %v, want an account_locked error", err)
	}
}

func asAuthError(err error, target **AuthError) bool {
	if e, ok := err.(*AuthError); ok {
		*target = e
		return true
	}
	return false
}
