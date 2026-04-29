package sync

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSequenceGuard implements SequenceGuard against Redis. Storage is a
// plain GET/SET — atomicity is delegated to the caller's outer Lock, so no
// CAS / WATCH is needed (and the Lock makes it cheaper than a server-side
// Lua script).
type RedisSequenceGuard struct {
	client *redis.Client
	prefix string
}

// NewRedisSequenceGuardFromClient wraps an existing Redis client. The
// prefix is prepended to every key; pass "" to use the default
// "mycel:seqguard:".
func NewRedisSequenceGuardFromClient(client *redis.Client, prefix string) *RedisSequenceGuard {
	if prefix == "" {
		prefix = "mycel:seqguard:"
	}
	return &RedisSequenceGuard{client: client, prefix: prefix}
}

// Read implements SequenceGuard.Read.
func (g *RedisSequenceGuard) Read(ctx context.Context, key string) (int64, bool, error) {
	v, err := g.client.Get(ctx, g.prefix+key).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("seq guard read: %w", err)
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// Stored value got corrupted or hand-edited. Treat as "no value"
		// rather than failing the flow — the next successful execution will
		// overwrite it.
		return 0, false, nil
	}
	return n, true, nil
}

// Write implements SequenceGuard.Write.
func (g *RedisSequenceGuard) Write(ctx context.Context, key string, sequence int64, ttl time.Duration) error {
	if err := g.client.Set(ctx, g.prefix+key, sequence, ttl).Err(); err != nil {
		return fmt.Errorf("seq guard write: %w", err)
	}
	return nil
}

// Close is a no-op — the Redis client is owned by the Manager.
func (g *RedisSequenceGuard) Close() error { return nil }
