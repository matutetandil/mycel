package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// Sessions, revoked tokens and lockouts held where every replica can see them.
//
// In memory, each of these is per-process: sign out on one replica and the
// session is still live on the other two, revoke a token and it keeps working,
// lock an account after five failed attempts and an attacker gets five more
// per replica. Redis is what makes them one thing — and the Redis stores were
// the ones nothing exercised.

func redisFor(t *testing.T) (*miniredis.Miniredis, *goredis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestASessionIsVisibleToEveryReplica(t *testing.T) {
	server, client := redisFor(t)
	store := NewRedisSessionStore(client, "")
	ctx := context.Background()

	session := &Session{
		ID:        "session-1",
		UserID:    "user-1",
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
		IP:        "203.0.113.10",
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// This is the name the manager asks by; the store answered `Get` and
	// could not be handed over at all until the two were reconciled.
	found, err := store.FindByID(ctx, "session-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil || found.UserID != "user-1" || found.IP != "203.0.113.10" {
		t.Errorf("session = %+v", found)
	}

	// Everything a person has open, which is what "sign out everywhere" acts
	// on and what a limit on concurrent sessions counts.
	second := &Session{ID: "session-2", UserID: "user-1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.Create(ctx, second); err != nil {
		t.Fatalf("Create: %v", err)
	}
	sessions, err := store.FindByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("sessions = %d, want both", len(sessions))
	}
	count, err := store.Count(ctx, "user-1")
	if err != nil || count != 2 {
		t.Errorf("count = %d, %v", count, err)
	}

	// A session left to expire goes on its own: Redis holds the TTL, so
	// there is nothing for the cleanup loop to sweep, and saying so beats
	// leaving the store out of the interface.
	if err := store.DeleteExpired(ctx); err != nil {
		t.Errorf("DeleteExpired: %v", err)
	}
	if ttl := server.TTL(server.Keys()[0]); ttl <= 0 {
		t.Error("a session was written with no expiry, so it outlives the sign-in")
	}
}

func TestUsingASessionKeepsItAlive(t *testing.T) {
	// An idle timeout reads the last time a session was used. Without the
	// TTL being extended with it, a session expires out from under somebody
	// who is using it.
	server, client := redisFor(t)
	store := NewRedisSessionStore(client, "")
	ctx := context.Background()

	created := time.Now().Add(-30 * time.Minute)
	if err := store.Create(ctx, &Session{
		ID:           "session-1",
		UserID:       "user-1",
		ExpiresAt:    time.Now().Add(time.Hour),
		CreatedAt:    created,
		LastActiveAt: created,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Touch(ctx, "session-1"); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	session, err := store.FindByID(ctx, "session-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !session.LastActiveAt.After(created) {
		t.Errorf("last used = %v, want it moved forward", session.LastActiveAt)
	}
	// The key still lives as long as the session does, and no longer: the
	// expiry is rewritten from the session's own, so using a session cannot
	// keep it alive past the point it was meant to end. Idleness is judged
	// from the time above, not from the key.
	if ttl := server.TTL(server.Keys()[0]); ttl <= 0 {
		t.Errorf("using a session left it with no expiry at all: %v", ttl)
	} else if ttl > time.Hour {
		t.Errorf("using a session pushed its expiry past its own: %v", ttl)
	}

	// Touching one that is not there says so rather than creating it.
	if err := store.Touch(ctx, "session-nobody-has"); err == nil {
		t.Error("a session nobody has was touched into existence")
	}
}

func TestARevokedTokenStaysRevokedEverywhere(t *testing.T) {
	// A signed token is valid until it expires — revoking it means writing
	// it down somewhere every replica reads, or signing out does nothing.
	server, client := redisFor(t)
	store := NewRedisTokenStore(client, "")
	ctx := context.Background()

	if used, err := store.Exists(ctx, "token-1"); err != nil || used {
		t.Errorf("a token nobody revoked came back revoked: %v, %v", used, err)
	}

	if err := store.Add(ctx, "token-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	used, err := store.Exists(ctx, "token-1")
	if err != nil || !used {
		t.Errorf("a revoked token was accepted: %v, %v", used, err)
	}

	// The record goes when the token would have expired anyway, so the list
	// cannot grow without bound.
	var found bool
	for _, key := range server.Keys() {
		if server.TTL(key) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("a revocation was written with no expiry")
	}

	if err := store.Delete(ctx, "token-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if used, _ := store.Exists(ctx, "token-1"); used {
		t.Error("lifting a revocation left the token revoked")
	}

	// Nothing to sweep: Redis expires them itself.
	if err := store.Cleanup(ctx); err != nil {
		t.Errorf("Cleanup: %v", err)
	}
}

func TestALockoutAppliesToEveryReplica(t *testing.T) {
	// Five attempts per account, not five per replica.
	server, client := redisFor(t)
	store := NewRedisBruteForceStore(client, "")
	ctx := context.Background()

	locked, until, err := store.IsLocked(ctx, "user-1")
	if err != nil || locked {
		t.Errorf("an account nobody locked was locked: %v, %v", locked, err)
	}

	if err := store.Lock(ctx, "user-1", 15*time.Minute); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	locked, until, err = store.IsLocked(ctx, "user-1")
	if err != nil || !locked {
		t.Fatalf("the lock did not hold: %v, %v", locked, err)
	}
	// When it lifts is what the person signing in is told, so it has to be a
	// real time rather than the zero one.
	if until.Before(time.Now()) {
		t.Errorf("the lock lifts at %v, which is in the past", until)
	}

	// And it lifts on its own, without anything having to remember to unlock.
	server.FastForward(20 * time.Minute)
	if locked, _, _ := store.IsLocked(ctx, "user-1"); locked {
		t.Error("the account was still locked after the lockout had passed")
	}
}

func TestTheWaitBetweenAttemptsIsShared(t *testing.T) {
	// A progressive delay held per replica is a delay an attacker skips by
	// hitting a different one.
	_, client := redisFor(t)
	store := NewRedisBruteForceStore(client, "")
	ctx := context.Background()

	delay, err := store.GetDelay(ctx, "user-1")
	if err != nil || delay != 0 {
		t.Errorf("delay = %v, %v, want none to start", delay, err)
	}

	if err := store.SetDelay(ctx, "user-1", 2500*time.Millisecond, time.Hour); err != nil {
		t.Fatalf("SetDelay: %v", err)
	}
	delay, err = store.GetDelay(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetDelay: %v", err)
	}
	// Sub-second precision matters: the delay doubles each time, and read
	// back as whole seconds the early steps all become the same.
	if delay != 2500*time.Millisecond {
		t.Errorf("delay = %v, want it back as it was set", delay)
	}
}

func TestASuccessfulSignInClearsEverythingItSet(t *testing.T) {
	// Attempts, the delay and the lockout go together. Two backends behind
	// one interface that disagree about what reset means are two different
	// behaviours wearing one name.
	_, client := redisFor(t)
	store := NewRedisBruteForceStore(client, "")
	ctx := context.Background()

	if _, err := store.Increment(ctx, "user-1", time.Hour); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	_ = store.SetDelay(ctx, "user-1", time.Second, time.Hour)
	_ = store.Lock(ctx, "user-1", time.Hour)

	if err := store.Reset(ctx, "user-1"); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if attempts, _ := store.GetAttempts(ctx, "user-1"); attempts != 0 {
		t.Errorf("attempts = %d after a successful sign-in", attempts)
	}
	if delay, _ := store.GetDelay(ctx, "user-1"); delay != 0 {
		t.Errorf("delay = %v after a successful sign-in", delay)
	}
	if locked, _, _ := store.IsLocked(ctx, "user-1"); locked {
		t.Error("the account was still locked after a successful sign-in")
	}
}

func TestATokenCanOnlyBeUsedOnce(t *testing.T) {
	// Replay protection: a password-reset link or a one-time code that works
	// twice is one an attacker can use after the person has.
	server, client := redisFor(t)
	store := NewRedisReplayProtectionStore(client, "")
	ctx := context.Background()

	used, err := store.IsTokenUsed(ctx, "jti-1")
	if err != nil || used {
		t.Errorf("a token nobody used came back used: %v, %v", used, err)
	}

	if err := store.MarkTokenUsed(ctx, "jti-1", time.Hour); err != nil {
		t.Fatalf("MarkTokenUsed: %v", err)
	}
	used, err = store.IsTokenUsed(ctx, "jti-1")
	if err != nil || !used {
		t.Errorf("a token that had been used was accepted again: %v, %v", used, err)
	}

	// The record only has to outlive the token, and it goes on its own.
	for _, key := range server.Keys() {
		if server.TTL(key) <= 0 {
			t.Errorf("%s was written with no expiry", key)
		}
	}

	// A different token is unaffected.
	if used, _ := store.IsTokenUsed(ctx, "jti-2"); used {
		t.Error("marking one token used marked another")
	}
}

func TestTheStoresKeepOutOfEachOthersWay(t *testing.T) {
	// One Redis holds sessions, revocations, lockouts and used tokens, and a
	// service's cache besides. A prefix is what stops one of them reading
	// another's keys — or a second service's.
	server, client := redisFor(t)
	ctx := context.Background()

	sessions := NewRedisSessionStore(client, "orders:sessions:")
	if err := sessions.Create(ctx, &Session{ID: "session-1", UserID: "user-1", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	tokens := NewRedisTokenStore(client, "orders:tokens:")
	if err := tokens.Add(ctx, "token-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for _, key := range server.Keys() {
		if len(key) < len("orders:") || key[:len("orders:")] != "orders:" {
			t.Errorf("%s was written outside the configured prefix", key)
		}
	}

	// A store told no prefix still writes somewhere identifiable rather than
	// at the top level of somebody else's database.
	plainServer, plainClient := redisFor(t)
	plain := NewRedisSessionStore(plainClient, "")
	if err := plain.Create(ctx, &Session{ID: "session-1", UserID: "user-1", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, key := range plainServer.Keys() {
		if len(key) < 6 || key[:6] != "mycel:" {
			t.Errorf("%s was written with no namespace at all", key)
		}
	}
}

func TestBuildingTheClientFromConfiguration(t *testing.T) {
	server, _ := redisFor(t)

	client := NewRedisClient(server.Addr(), "", 0)
	if client == nil {
		t.Fatal("no client was built")
	}
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Errorf("the client cannot reach the server it was given: %v", err)
	}
	if client.Options().Addr != server.Addr() {
		t.Errorf("address = %q", client.Options().Addr)
	}
}
