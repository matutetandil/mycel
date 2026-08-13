package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// miniredis speaks the real protocol in-process, so these exercise the commands
// the stores actually send — the keys, the TTLs, the index a user's sessions are
// listed from — without a container. It answers a different question from the
// integration suite: not "does Redis work" but "do we ask it the right things".

func redisStore(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	return server, client
}

func TestASessionSurvivesARoundTrip(t *testing.T) {
	ctx := context.Background()
	_, client := redisStore(t)
	store := NewRedisSessionStore(client, "test:session")

	session := &Session{
		ID: "s-1", UserID: "u-1", IP: "203.0.113.7",
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// FindByID is the name the manager uses; it was Get, which is why the
	// store did not satisfy the interface.
	found, err := store.FindByID(ctx, "s-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.UserID != "u-1" || found.IP != "203.0.113.7" {
		t.Errorf("session = %+v", found)
	}
}

func TestASessionExpiresWithItsTTL(t *testing.T) {
	// The reason to keep sessions in Redis: it forgets them on time, and the
	// cleanup loop has nothing to do.
	ctx := context.Background()
	server, client := redisStore(t)
	store := NewRedisSessionStore(client, "test:session")

	if err := store.Create(ctx, &Session{
		ID: "s-1", UserID: "u-1",
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	server.FastForward(2 * time.Minute)

	if _, err := store.FindByID(ctx, "s-1"); err == nil {
		t.Error("a session outlived its expiry")
	}
}

func TestTouchExtendsTheLifeOfASessionInUse(t *testing.T) {
	// A session being used should not expire out from under the person using
	// it, which is why Touch rewrites it rather than only stamping it.
	ctx := context.Background()
	server, client := redisStore(t)
	store := NewRedisSessionStore(client, "test:session")

	if err := store.Create(ctx, &Session{
		ID: "s-1", UserID: "u-1",
		CreatedAt: time.Now(), LastActiveAt: time.Now().Add(-30 * time.Minute),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	before, err := store.FindByID(ctx, "s-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if err := store.Touch(ctx, "s-1"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	after, err := store.FindByID(ctx, "s-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !after.LastActiveAt.After(before.LastActiveAt) {
		t.Errorf("last active went from %v to %v", before.LastActiveAt, after.LastActiveAt)
	}
	_ = server
}

func TestAUsersSessionsAreListedAndCounted(t *testing.T) {
	ctx := context.Background()
	_, client := redisStore(t)
	store := NewRedisSessionStore(client, "test:session")

	for _, id := range []string{"s-1", "s-2"} {
		if err := store.Create(ctx, &Session{
			ID: id, UserID: "u-1",
			CreatedAt: time.Now(), LastActiveAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := store.Create(ctx, &Session{
		ID: "other", UserID: "u-2",
		CreatedAt: time.Now(), LastActiveAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sessions, err := store.FindByUserID(ctx, "u-1")
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("got %d sessions for u-1", len(sessions))
	}

	// Count is what a limit on concurrent sessions is checked against.
	count, err := store.Count(ctx, "u-1")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d", count)
	}
}

func TestARevokedTokenIsRememberedUntilItWouldExpire(t *testing.T) {
	ctx := context.Background()
	server, client := redisStore(t)
	store := NewRedisTokenStore(client, "test:token")

	if err := store.Add(ctx, "jti-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	revoked, err := store.Exists(ctx, "jti-1")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !revoked {
		t.Fatal("a revoked token was not remembered")
	}

	// Past the point where the token would have expired anyway, the entry goes
	// on its own — which is what keeps a blacklist from growing without bound.
	server.FastForward(2 * time.Minute)
	revoked, err = store.Exists(ctx, "jti-1")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if revoked {
		t.Error("the blacklist entry outlived the token it was about")
	}
}

func TestRevokingSomethingAlreadyExpiredIsNotRecorded(t *testing.T) {
	// A token past its expiry is refused by the token manager anyway, so
	// writing it down is a key that can never matter.
	ctx := context.Background()
	_, client := redisStore(t)
	store := NewRedisTokenStore(client, "test:token")

	if err := store.Add(ctx, "jti-old", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	revoked, err := store.Exists(ctx, "jti-old")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if revoked {
		t.Error("an already expired token was written to the blacklist")
	}
}

func TestFailedAttemptsAreCountedAndLocked(t *testing.T) {
	ctx := context.Background()
	_, client := redisStore(t)
	store := NewRedisBruteForceStore(client, "test:bf")

	for i := 1; i <= 3; i++ {
		count, err := store.Increment(ctx, "person@example.com", time.Minute)
		if err != nil {
			t.Fatalf("Increment: %v", err)
		}
		if count != i {
			t.Errorf("attempt %d counted as %d", i, count)
		}
	}

	if err := store.Lock(ctx, "person@example.com", time.Hour); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	locked, until, err := store.IsLocked(ctx, "person@example.com")
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if !locked {
		t.Fatal("the account was not locked")
	}
	if until.Before(time.Now()) {
		t.Errorf("the lock is already over: %v", until)
	}

	if err := store.Reset(ctx, "person@example.com"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if locked, _, _ := store.IsLocked(ctx, "person@example.com"); locked {
		t.Error("the lock survived a successful login")
	}
}

func TestBothBruteForceBackendsAgreeOnReset(t *testing.T) {
	// Two implementations behind one interface that disagree about what reset
	// means are two behaviours wearing one name. The memory store clears the
	// lock because it deletes the entry holding it; Redis kept the lockout key
	// and did not.
	ctx := context.Background()
	_, client := redisStore(t)

	stores := map[string]BruteForceStore{
		"memory": NewMemoryBruteForceStore(),
		"redis":  NewRedisBruteForceStore(client, "test:agree"),
	}

	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Increment(ctx, "k", time.Minute); err != nil {
				t.Fatalf("Increment: %v", err)
			}
			if err := store.Lock(ctx, "k", time.Hour); err != nil {
				t.Fatalf("Lock: %v", err)
			}
			if err := store.Reset(ctx, "k"); err != nil {
				t.Fatalf("Reset: %v", err)
			}

			locked, _, err := store.IsLocked(ctx, "k")
			if err != nil {
				t.Fatalf("IsLocked: %v", err)
			}
			if locked {
				t.Error("the lock survived a reset")
			}
		})
	}
}
