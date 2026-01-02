package sync

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisLock implements Lock interface using Redis.
// Uses SET NX PX for atomic lock acquisition and Lua script for safe release.
type RedisLock struct {
	client     *redis.Client
	prefix     string
	instanceID string
}

// RedisLockConfig holds configuration for RedisLock.
type RedisLockConfig struct {
	// URL is the Redis connection URL.
	URL string

	// Prefix is prepended to all lock keys.
	Prefix string

	// PoolSize is the maximum number of connections.
	PoolSize int
}

// NewRedisLock creates a new Redis-based lock manager.
func NewRedisLock(cfg *RedisLockConfig) (*RedisLock, error) {
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
		prefix = "mycel:lock:"
	}

	return &RedisLock{
		client:     client,
		prefix:     prefix,
		instanceID: uuid.New().String(),
	}, nil
}

// NewRedisLockFromClient creates a RedisLock from an existing Redis client.
func NewRedisLockFromClient(client *redis.Client, prefix string) *RedisLock {
	if prefix == "" {
		prefix = "mycel:lock:"
	}
	return &RedisLock{
		client:     client,
		prefix:     prefix,
		instanceID: uuid.New().String(),
	}
}

// Acquire attempts to acquire the lock for the given key.
// Uses SET key value NX PX milliseconds for atomic acquisition.
func (r *RedisLock) Acquire(ctx context.Context, key string, timeout time.Duration) (bool, error) {
	fullKey := r.prefix + key

	// SET key value NX PX milliseconds
	// NX - only set if not exists
	// PX - set expiration in milliseconds
	ok, err := r.client.SetNX(ctx, fullKey, r.instanceID, timeout).Result()
	if err != nil {
		return false, err
	}

	return ok, nil
}

// Release releases the lock for the given key.
// Uses Lua script to ensure we only delete if we own the lock.
func (r *RedisLock) Release(ctx context.Context, key string) error {
	fullKey := r.prefix + key

	// Lua script for atomic check-and-delete
	// Only deletes if the value matches our instance ID
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)

	result, err := script.Run(ctx, r.client, []string{fullKey}, r.instanceID).Int()
	if err != nil {
		return err
	}

	if result == 0 {
		return ErrLockNotHeld
	}

	return nil
}

// IsHeld checks if the lock is currently held by this instance.
func (r *RedisLock) IsHeld(ctx context.Context, key string) (bool, error) {
	fullKey := r.prefix + key

	val, err := r.client.Get(ctx, fullKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return val == r.instanceID, nil
}

// Close closes the Redis client.
func (r *RedisLock) Close() error {
	return r.client.Close()
}

// Extend extends the lock timeout if we hold the lock.
func (r *RedisLock) Extend(ctx context.Context, key string, timeout time.Duration) (bool, error) {
	fullKey := r.prefix + key

	// Lua script for atomic check-and-extend
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`)

	result, err := script.Run(ctx, r.client, []string{fullKey}, r.instanceID, timeout.Milliseconds()).Int()
	if err != nil {
		return false, err
	}

	return result == 1, nil
}

// Stats returns statistics about lock usage.
func (r *RedisLock) Stats(ctx context.Context) (map[string]interface{}, error) {
	// Count keys with our prefix
	keys, err := r.client.Keys(ctx, r.prefix+"*").Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"active_locks": len(keys),
		"instance_id":  r.instanceID,
		"prefix":       r.prefix,
	}, nil
}
