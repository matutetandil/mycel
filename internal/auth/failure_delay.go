package auth

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"
)

// A failed sign-in answers slowly, and by an amount that varies.
//
// Two separate reasons, and the second is the one that is easy to miss.
//
// The delay itself is what makes guessing expensive: without it an attacker is
// limited only by the network, and a lockout after three attempts is easy to
// walk around by spreading the guesses across many accounts.
//
// The variation is what stops the answer from being an oracle. Measured before
// this existed: a login for an address with no account answered in 0.4ms and
// one for an address with an account in 46ms, because a missing user returns
// before the password hash is computed. That 100-fold difference is a way to
// harvest which addresses have accounts, at scale, without ever guessing a
// password. A constant delay would not fix it — an attacker subtracts a
// constant — so the wait is randomised and every failure pays it, whether the
// account exists or not.
//
// This is what pam_unix does on Linux, for the same reasons.

// defaultFailureDelay is what a failed sign-in waits when nothing is
// configured. Long enough to matter against a machine, short enough not to
// punish someone who mistyped.
const defaultFailureDelay = 1500 * time.Millisecond

// failureJitter is how much of the delay is random, as a fraction. A quarter
// is what pam_unix uses.
const failureJitter = 0.25

// delayFailure waits before answering a failed sign-in.
//
// It returns when the caller goes away, so a client that disconnects does not
// hold a goroutine for the rest of the wait.
func (m *Manager) delayFailure(ctx context.Context) {
	wait := m.failureDelay()
	if wait <= 0 {
		return
	}

	select {
	case <-time.After(wait):
	case <-ctx.Done():
	}
}

// failureDelay is the configured wait with its jitter applied.
func (m *Manager) failureDelay() time.Duration {
	base := defaultFailureDelay
	if m.config != nil && m.config.Security != nil && m.config.Security.BruteForce != nil {
		bf := m.config.Security.BruteForce
		if bf.FailDelay != "" {
			if parsed, err := ParseDuration(bf.FailDelay); err == nil {
				base = parsed
			}
		}
		// A zero delay is a decision, not an omission: it is how someone turns
		// this off for a test.
		if bf.FailDelay == "0" || bf.FailDelay == "0s" {
			return 0
		}
	}

	return jitter(base, failureJitter)
}

// jitter spreads a duration by a fraction of itself, using the same source of
// randomness as the rest of the security code rather than math/rand.
func jitter(base time.Duration, fraction float64) time.Duration {
	if base <= 0 || fraction <= 0 {
		return base
	}

	span := int64(float64(base) * fraction)
	if span <= 0 {
		return base
	}

	// Centred on the base: half the spread below, half above, so the average
	// wait is the number that was configured.
	offset, err := rand.Int(rand.Reader, big.NewInt(span))
	if err != nil {
		return base
	}
	return base - time.Duration(span/2) + time.Duration(offset.Int64())
}

// decoyHash is a valid Argon2id encoding of a password nobody knows.
//
// It is verified against when the address has no account, so that the work done
// is the same either way. Comparing against an empty string would not do:
// Verify rejects a malformed hash immediately, which is the very shortcut this
// is here to remove.
const decoyHash = "$argon2id$v=19$m=65536,t=3,p=2$" +
	"YXNkZmFzZGZhc2RmYXNkZg$" +
	"c2RmYXNkZmFzZGZhc2RmYXNkZmFzZGZhc2RmYXNkZmFzZGY"
