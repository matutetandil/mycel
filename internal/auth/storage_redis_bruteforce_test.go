package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// Failed sign-ins counted in Redis.
//
// In memory this count is per replica, so an attacker gets the configured
// number of attempts multiplied by however many replicas are running — which
// is the whole reason the Redis store exists, and it had no tests. Sessions and
// tokens are covered in storage_redis_test.go; this is the third store.

func bruteForceStore(t *testing.T) (*miniredis.Miniredis, *RedisBruteForceStore) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, NewRedisBruteForceStore(client, "")
}

// --- Failed sign-ins ---------------------------------------------------------

func TestFailedSignInsAreCountedAcrossReplicas(t *testing.T) {
	// In memory this is per replica, so an attacker gets the configured number
	// of attempts multiplied by however many replicas are running.
	server, bruteForce := bruteForceStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		count, err := bruteForce.Increment(ctx, "ada@example.com", 15*time.Minute)
		if err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if count != i {
			t.Errorf("attempt %d counted as %d", i, count)
		}
	}

	count, err := bruteForce.Get(ctx, "ada@example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d", count)
	}

	// Somebody who has not failed at all.
	count, err = bruteForce.Get(ctx, "grace@example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want none", count)
	}

	// The count is forgotten after the window, or one failed sign-in a month
	// ago counts towards a lockout today.
	server.FastForward(16 * time.Minute)
	count, err = bruteForce.Get(ctx, "ada@example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d after the window, want it forgotten", count)
	}
}

func TestASuccessfulSignInForgetsTheFailures(t *testing.T) {
	// Otherwise somebody who mistyped twice and then signed in stays two
	// attempts from a lockout for the rest of the window.
	_, bruteForce := bruteForceStore(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := bruteForce.Increment(ctx, "ada@example.com", 15*time.Minute); err != nil {
			t.Fatalf("Increment: %v", err)
		}
	}

	if err := bruteForce.Reset(ctx, "ada@example.com"); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	count, err := bruteForce.Get(ctx, "ada@example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d after a successful sign-in", count)
	}
}
