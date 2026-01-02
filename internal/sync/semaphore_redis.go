package sync

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisSemaphore implements Semaphore interface using Redis sorted sets.
// Uses ZADD with score as expiration timestamp for automatic cleanup.
type RedisSemaphore struct {
	client     *redis.Client
	prefix     string
	maxPermits int
}

// RedisSemaphoreConfig holds configuration for RedisSemaphore.
type RedisSemaphoreConfig struct {
	// URL is the Redis connection URL.
	URL string

	// Prefix is prepended to all semaphore keys.
	Prefix string

	// MaxPermits is the maximum number of concurrent permits.
	MaxPermits int

	// PoolSize is the maximum number of connections.
	PoolSize int
}

// NewRedisSemaphore creates a new Redis-based semaphore manager.
func NewRedisSemaphore(cfg *RedisSemaphoreConfig) (*RedisSemaphore, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, err
	}

	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	}

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "mycel:semaphore:"
	}

	maxPermits := cfg.MaxPermits
	if maxPermits <= 0 {
		maxPermits = 10
	}

	return &RedisSemaphore{
		client:     client,
		prefix:     prefix,
		maxPermits: maxPermits,
	}, nil
}

// NewRedisSemaphoreFromClient creates a RedisSemaphore from an existing Redis client.
func NewRedisSemaphoreFromClient(client *redis.Client, prefix string, maxPermits int) *RedisSemaphore {
	if prefix == "" {
		prefix = "mycel:semaphore:"
	}
	if maxPermits <= 0 {
		maxPermits = 10
	}
	return &RedisSemaphore{
		client:     client,
		prefix:     prefix,
		maxPermits: maxPermits,
	}
}

// acquireScript is the Lua script for atomic semaphore acquisition.
// It cleans expired permits, checks capacity, and adds new permit atomically.
var acquireScript = redis.NewScript(`
	local key = KEYS[1]
	local now = tonumber(ARGV[1])
	local maxPermits = tonumber(ARGV[2])
	local expireAt = tonumber(ARGV[3])
	local permitID = ARGV[4]

	-- Remove expired permits (score < current time)
	redis.call('ZREMRANGEBYSCORE', key, '-inf', now)

	-- Count active permits
	local count = redis.call('ZCARD', key)

	-- Check if we have capacity
	if count >= maxPermits then
		return 0
	end

	-- Add new permit with expiration as score
	redis.call('ZADD', key, expireAt, permitID)

	-- Set key expiration to max lease + buffer
	redis.call('EXPIRE', key, 3600)

	return 1
`)

// Acquire attempts to acquire a permit from the semaphore.
func (r *RedisSemaphore) Acquire(ctx context.Context, key string, timeout, lease time.Duration) (string, error) {
	fullKey := r.prefix + key
	permitID := uuid.New().String()

	now := time.Now().UnixMilli()
	expireAt := time.Now().Add(lease).UnixMilli()

	result, err := acquireScript.Run(ctx, r.client,
		[]string{fullKey},
		now,
		r.maxPermits,
		expireAt,
		permitID,
	).Int()

	if err != nil {
		return "", err
	}

	if result == 0 {
		return "", ErrSemaphoreFull
	}

	return permitID, nil
}

// Release releases a permit back to the semaphore.
func (r *RedisSemaphore) Release(ctx context.Context, key string, permitID string) error {
	fullKey := r.prefix + key

	removed, err := r.client.ZRem(ctx, fullKey, permitID).Result()
	if err != nil {
		return err
	}

	if removed == 0 {
		return ErrPermitNotFound
	}

	return nil
}

// Available returns the number of available permits.
func (r *RedisSemaphore) Available(ctx context.Context, key string) (int, error) {
	fullKey := r.prefix + key

	// Clean expired permits first
	now := time.Now().UnixMilli()
	if err := r.client.ZRemRangeByScore(ctx, fullKey, "-inf", formatScore(now)).Err(); err != nil {
		return 0, err
	}

	// Count active permits
	count, err := r.client.ZCard(ctx, fullKey).Result()
	if err != nil {
		return 0, err
	}

	available := r.maxPermits - int(count)
	if available < 0 {
		available = 0
	}

	return available, nil
}

// Close closes the Redis client.
func (r *RedisSemaphore) Close() error {
	return r.client.Close()
}

// formatScore formats a millisecond timestamp as a string for Redis.
func formatScore(ms int64) string {
	return strconv.FormatInt(ms, 10)
}

// Stats returns statistics about a semaphore.
func (r *RedisSemaphore) Stats(ctx context.Context, key string) (map[string]interface{}, error) {
	fullKey := r.prefix + key

	// Clean expired permits first
	now := time.Now().UnixMilli()
	if err := r.client.ZRemRangeByScore(ctx, fullKey, "-inf", formatScore(now)).Err(); err != nil {
		return nil, err
	}

	count, err := r.client.ZCard(ctx, fullKey).Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"active_permits":    int(count),
		"max_permits":       r.maxPermits,
		"available_permits": r.maxPermits - int(count),
	}, nil
}

// SetMaxPermits dynamically changes the max permits.
func (r *RedisSemaphore) SetMaxPermits(max int) {
	if max > 0 {
		r.maxPermits = max
	}
}
