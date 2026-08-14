package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// Sessions, refresh tokens and the blacklist are where signing out, revoking
// and locking out actually happen. Redis speaks a protocol, and a server that
// speaks it runs in this process — so these are exercised against a real one
// rather than a stand-in that agrees with whatever the code does.

func redisStores(t *testing.T) (*miniredis.Miniredis, *RedisSessionStore, *RedisTokenStore) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, NewRedisSessionStore(client, ""), NewRedisTokenStore(client, "")
}

func session(id, userID string, lasting time.Duration) *Session {
	return &Session{
		ID: id, UserID: userID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(lasting),
		IP:        "203.0.113.10", UserAgent: "test",
	}
}

func TestASessionComesBackAsItWasStored(t *testing.T) {
	_, sessions, _ := redisStores(t)
	ctx := context.Background()

	if err := sessions.Create(ctx, session("s-1", "user-1", time.Hour)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := sessions.Get(ctx, "s-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != "user-1" || got.IP != "203.0.113.10" {
		t.Errorf("session = %+v", got)
	}
}

func TestASessionNobodyCreatedIsNotFound(t *testing.T) {
	_, sessions, _ := redisStores(t)
	if _, err := sessions.Get(context.Background(), "s-absent"); err != ErrSessionNotFound {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestASessionThatRanOutIsGone(t *testing.T) {
	// The session outliving its expiry is somebody staying signed in after
	// they should have been asked to sign in again.
	server, sessions, _ := redisStores(t)
	ctx := context.Background()

	if err := sessions.Create(ctx, session("s-1", "user-1", 30*time.Minute)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	server.FastForward(31 * time.Minute)

	if _, err := sessions.Get(ctx, "s-1"); err != ErrSessionNotFound {
		t.Errorf("err = %v, want the session to be gone", err)
	}
}

func TestASessionThatAlreadyExpiredIsRefused(t *testing.T) {
	_, sessions, _ := redisStores(t)
	if err := sessions.Create(context.Background(), session("s-1", "user-1", -time.Minute)); err == nil {
		t.Error("a session that had already expired was stored")
	}
}

func TestSigningOutRemovesTheSessionEverywhere(t *testing.T) {
	// Deleting the session and leaving its identifier in the user's index
	// would leave a ghost that still counts against them.
	_, sessions, _ := redisStores(t)
	ctx := context.Background()

	_ = sessions.Create(ctx, session("s-1", "user-1", time.Hour))
	if err := sessions.Delete(ctx, "s-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := sessions.Get(ctx, "s-1"); err != ErrSessionNotFound {
		t.Errorf("the session survived being deleted: %v", err)
	}
	count, err := sessions.CountByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("CountByUserID: %v", err)
	}
	if count != 0 {
		t.Errorf("the user still counts %d sessions after deleting the only one", count)
	}
}

func TestSigningOutEverywhereLeavesNothing(t *testing.T) {
	_, sessions, _ := redisStores(t)
	ctx := context.Background()
	for _, id := range []string{"s-1", "s-2", "s-3"} {
		_ = sessions.Create(ctx, session(id, "user-1", time.Hour))
	}

	if err := sessions.DeleteByUserID(ctx, "user-1"); err != nil {
		t.Fatalf("DeleteByUserID: %v", err)
	}

	found, err := sessions.GetByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetByUserID: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("%d sessions survived signing out everywhere", len(found))
	}
}

func TestOnlyTheUsersOwnSessionsAreListed(t *testing.T) {
	_, sessions, _ := redisStores(t)
	ctx := context.Background()
	_ = sessions.Create(ctx, session("s-1", "user-1", time.Hour))
	_ = sessions.Create(ctx, session("s-2", "user-2", time.Hour))

	found, err := sessions.GetByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetByUserID: %v", err)
	}
	if len(found) != 1 || found[0].ID != "s-1" {
		t.Errorf("sessions = %v", found)
	}
}

func TestSessionsThatRanOutDoNotCountAgainstTheLimit(t *testing.T) {
	// max_active with on_max_reached = "reject_new" is decided by this count.
	// Counting the identifiers in the user's index rather than the sessions
	// that are still there means every session a user has ever had counts
	// forever: after max_active sign-ins over a week they are refused with
	// "Maximum number of sessions reached" while having none at all.
	server, sessions, _ := redisStores(t)
	ctx := context.Background()

	for _, id := range []string{"s-1", "s-2", "s-3"} {
		if err := sessions.Create(ctx, session(id, "user-1", 30*time.Minute)); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if count, _ := sessions.CountByUserID(ctx, "user-1"); count != 3 {
		t.Fatalf("count = %d, want the three that are live", count)
	}

	server.FastForward(31 * time.Minute)

	count, err := sessions.CountByUserID(ctx, "user-1")
	if err != nil {
		t.Fatalf("CountByUserID: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want none: every one of them has expired", count)
	}
}

func TestUpdatingASessionKeepsItAlive(t *testing.T) {
	_, sessions, _ := redisStores(t)
	ctx := context.Background()
	_ = sessions.Create(ctx, session("s-1", "user-1", time.Hour))

	updated := session("s-1", "user-1", 2*time.Hour)
	updated.IP = "198.51.100.4"
	if err := sessions.Update(ctx, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := sessions.Get(ctx, "s-1")
	if got.IP != "198.51.100.4" {
		t.Errorf("session = %+v", got)
	}
}

func TestASessionThatIsNotThereCannotBeUpdated(t *testing.T) {
	_, sessions, _ := redisStores(t)
	if err := sessions.Update(context.Background(), session("s-absent", "user-1", time.Hour)); err == nil {
		t.Error("a session nobody created was updated")
	}
}

// Refresh tokens are what let somebody stay signed in, so revoking one has to
// mean it stops working — that is the whole mechanism behind signing out of a
// stolen session.

func TestARefreshTokenIsGoodUntilItIsRevoked(t *testing.T) {
	_, _, tokens := redisStores(t)
	ctx := context.Background()

	if err := tokens.StoreRefreshToken(ctx, "tok-1", "user-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	userID, err := tokens.ValidateRefreshToken(ctx, "tok-1")
	if err != nil || userID != "user-1" {
		t.Fatalf("validate = %q, %v", userID, err)
	}

	if err := tokens.RevokeRefreshToken(ctx, "tok-1"); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}
	if _, err := tokens.ValidateRefreshToken(ctx, "tok-1"); err == nil {
		t.Error("a revoked refresh token still works")
	}
}

func TestATokenNobodyIssuedIsRefused(t *testing.T) {
	_, _, tokens := redisStores(t)
	if _, err := tokens.ValidateRefreshToken(context.Background(), "made-up"); err == nil {
		t.Error("a token nobody issued was accepted")
	}
}

func TestARefreshTokenStopsWorkingWhenItExpires(t *testing.T) {
	server, _, tokens := redisStores(t)
	ctx := context.Background()
	_ = tokens.StoreRefreshToken(ctx, "tok-1", "user-1", time.Now().Add(30*time.Minute))

	server.FastForward(31 * time.Minute)

	if _, err := tokens.ValidateRefreshToken(ctx, "tok-1"); err == nil {
		t.Error("an expired refresh token still works")
	}
}

func TestRevokingEverythingSignsTheUserOutOfEveryDevice(t *testing.T) {
	_, _, tokens := redisStores(t)
	ctx := context.Background()
	for _, tok := range []string{"tok-1", "tok-2", "tok-3"} {
		_ = tokens.StoreRefreshToken(ctx, tok, "user-1", time.Now().Add(time.Hour))
	}
	_ = tokens.StoreRefreshToken(ctx, "other", "user-2", time.Now().Add(time.Hour))

	if err := tokens.RevokeAllUserTokens(ctx, "user-1"); err != nil {
		t.Fatalf("RevokeAllUserTokens: %v", err)
	}

	for _, tok := range []string{"tok-1", "tok-2", "tok-3"} {
		if _, err := tokens.ValidateRefreshToken(ctx, tok); err == nil {
			t.Errorf("%s still works after revoking everything", tok)
		}
	}
	// And somebody else's session was not taken down with it.
	if _, err := tokens.ValidateRefreshToken(ctx, "other"); err != nil {
		t.Errorf("another user was signed out too: %v", err)
	}
}

func TestABlacklistedTokenIsReportedAsBlacklisted(t *testing.T) {
	// This is what makes an access token stop working before it expires.
	_, _, tokens := redisStores(t)
	ctx := context.Background()

	if blacklisted, _ := tokens.IsBlacklisted(ctx, "jti-1"); blacklisted {
		t.Fatal("a token nobody blacklisted was reported as blacklisted")
	}
	if err := tokens.BlacklistToken(ctx, "jti-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("BlacklistToken: %v", err)
	}

	blacklisted, err := tokens.IsBlacklisted(ctx, "jti-1")
	if err != nil {
		t.Fatalf("IsBlacklisted: %v", err)
	}
	if !blacklisted {
		t.Error("a blacklisted token was not reported as blacklisted")
	}
}

func TestTheBlacklistIsForgottenWhenTheTokenWouldHaveExpiredAnyway(t *testing.T) {
	// Keeping it forever would grow without bound, and after the token has
	// expired the blacklist entry decides nothing.
	server, _, tokens := redisStores(t)
	ctx := context.Background()
	_ = tokens.BlacklistToken(ctx, "jti-1", time.Now().Add(30*time.Minute))

	server.FastForward(31 * time.Minute)

	if blacklisted, _ := tokens.IsBlacklisted(ctx, "jti-1"); blacklisted {
		t.Error("the blacklist entry outlived the token it was about")
	}
}

func TestEverySessionStoreCountsTheSameThing(t *testing.T) {
	// max_active is decided by this number whichever store is configured, so a
	// user must not be refused on one and let in on another.
	ctx := context.Background()
	memory := NewMemorySessionStore()

	live := session("s-1", "user-1", time.Hour)
	expired := session("s-2", "user-1", time.Hour)
	expired.ExpiresAt = time.Now().Add(-time.Minute)

	for _, s := range []*Session{live, expired} {
		if err := memory.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	count, err := memory.Count(ctx, "user-1")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want only the session that is still live", count)
	}
}
