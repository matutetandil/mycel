// Package ratelimit provides rate limiting functionality for Mycel services.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore implements distributed rate limiting using Redis.
// Uses a fixed-window counter with Redis INCR + EXPIRE.
type RedisStore struct {
	client redis.UniversalClient
	prefix string
}

// NewRedisStore creates a new Redis-backed rate limit store.
func NewRedisStore(client redis.UniversalClient, prefix string) *RedisStore {
	if prefix == "" {
		prefix = "mycel:ratelimit"
	}
	return &RedisStore{
		client: client,
		prefix: prefix,
	}
}

// Allow checks if a request with the given key is allowed under the specified rate limit.
func (s *RedisStore) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	now := time.Now()
	windowSec := int64(window.Seconds())
	if windowSec < 1 {
		windowSec = 1
	}
	windowKey := fmt.Sprintf("%s:%s:%d", s.prefix, key, now.Unix()/windowSec)

	pipe := s.client.Pipeline()
	incr := pipe.Incr(ctx, windowKey)
	pipe.Expire(ctx, windowKey, window+time.Second)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("redis rate limit error: %w", err)
	}

	count := int(incr.Val())
	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	return count <= limit, remaining, nil
}
