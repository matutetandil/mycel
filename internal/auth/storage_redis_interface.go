package auth

import (
	"context"
	"fmt"
	"time"
)

// The Redis stores were written against an earlier shape of the store
// interfaces and never compiled into them, because nothing ever used one: the
// session store answers Get where the manager asks for FindByID, and the token
// store is about refresh tokens where the manager wants a blacklist. So
// `storage { driver = "redis" }` could name them and they could not be handed
// over.
//
// What each does is right; only the names and two missing operations were not.
// The methods below are the interfaces, expressed in terms of what is already
// there rather than as second implementations.

// FindByID returns a session by its identifier.
func (s *RedisSessionStore) FindByID(ctx context.Context, id string) (*Session, error) {
	return s.Get(ctx, id)
}

// FindByUserID returns every session belonging to a user.
func (s *RedisSessionStore) FindByUserID(ctx context.Context, userID string) ([]*Session, error) {
	return s.GetByUserID(ctx, userID)
}

// Count returns how many sessions a user has, which is what a limit on
// concurrent sessions is checked against.
func (s *RedisSessionStore) Count(ctx context.Context, userID string) (int, error) {
	return s.CountByUserID(ctx, userID)
}

// DeleteExpired is a no-op on Redis, which expires the keys itself.
//
// The cleanup loop calls this on every store; here there is nothing to do,
// because each session is written with a TTL and disappears on its own. Saying
// so is better than leaving the method off and the store out.
func (s *RedisSessionStore) DeleteExpired(ctx context.Context) error {
	return nil
}

// Touch records that a session was used, which is what an idle timeout reads.
//
// The key's expiry is rewritten from the session's own ExpiresAt rather than
// pushed further out. That is deliberate: ExpiresAt is the longest a session
// may live, and extending the key past it would keep a session alive beyond
// its own expiry. Idleness is judged from LastActiveAt, which is what this
// updates.
func (s *RedisSessionStore) Touch(ctx context.Context, id string) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	session.LastActiveAt = time.Now()
	return s.Update(ctx, session)
}

// Add records a token identifier as revoked until it would have expired.
//
// The manager's token store is a blacklist: it answers whether a token that is
// still within its lifetime has been withdrawn. Redis expires the entry by
// itself once the token would have expired anyway, so the list cannot grow
// without bound.
func (s *RedisTokenStore) Add(ctx context.Context, tokenID string, expiry time.Time) error {
	return s.BlacklistToken(ctx, tokenID, expiry)
}

// Exists reports whether a token identifier has been revoked.
func (s *RedisTokenStore) Exists(ctx context.Context, tokenID string) (bool, error) {
	return s.IsBlacklisted(ctx, tokenID)
}

// Delete lifts a revocation.
func (s *RedisTokenStore) Delete(ctx context.Context, tokenID string) error {
	if err := s.client.Del(ctx, s.blacklistKey(tokenID)).Err(); err != nil {
		return fmt.Errorf("failed to remove token from the blacklist: %w", err)
	}
	return nil
}

// Cleanup is a no-op on Redis, which expires the entries itself.
func (s *RedisTokenStore) Cleanup(ctx context.Context) error {
	return nil
}
