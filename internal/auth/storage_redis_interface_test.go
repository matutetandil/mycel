package auth

import "testing"

// The Redis stores were written against an earlier shape of these interfaces
// and nothing ever compiled them into one, because nothing used them. That is
// the whole reason `storage { driver = "redis" }` could not hand them over, and
// it is exactly the kind of thing a compiler catches the moment something
// tries — so this makes something try.

func TestTheRedisStoresSatisfyWhatTheManagerTakes(t *testing.T) {
	var (
		_ SessionStore    = (*RedisSessionStore)(nil)
		_ TokenStore      = (*RedisTokenStore)(nil)
		_ BruteForceStore = (*RedisBruteForceStore)(nil)
	)
}

func TestEveryStoreBackendSatisfiesItsInterface(t *testing.T) {
	// The same for the others, so that a backend cannot drift out of the shape
	// it is meant to fill without the build saying so.
	var (
		_ UserStore    = (*MemoryUserStore)(nil)
		_ UserStore    = (*PostgresUserStore)(nil)
		_ UserStore    = (*MySQLUserStore)(nil)
		_ SessionStore = (*MemorySessionStore)(nil)
		_ SessionStore = (*MySQLSessionStore)(nil)
		_ TokenStore   = (*MemoryTokenStore)(nil)
		_ TokenStore   = (*MySQLTokenStore)(nil)
	)
}
