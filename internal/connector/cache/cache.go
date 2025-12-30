// Package cache provides cache connector implementations for Mycel.
// Supports Redis and in-memory caching with TTL and pattern-based invalidation.
package cache

import (
	"context"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
	"github.com/mycel-labs/mycel/internal/connector/cache/types"
)

// Cache defines the interface for cache operations.
// All cache implementations (Redis, Memory) must implement this interface.
type Cache interface {
	connector.Connector

	// Get retrieves a value from the cache.
	// Returns the value, a boolean indicating if the key was found, and any error.
	Get(ctx context.Context, key string) ([]byte, bool, error)

	// Set stores a value in the cache with the specified TTL.
	// If ttl is 0, the value never expires (or uses the default TTL).
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes one or more keys from the cache.
	Delete(ctx context.Context, keys ...string) error

	// DeletePattern removes all keys matching the given pattern.
	// Pattern syntax: `*` matches any characters.
	// Note: Only supported by Redis. Memory cache will iterate all keys.
	DeletePattern(ctx context.Context, pattern string) error

	// Exists checks if a key exists in the cache.
	Exists(ctx context.Context, key string) (bool, error)

	// TTL returns the remaining time to live for a key.
	// Returns -1 if the key exists but has no TTL, -2 if the key doesn't exist.
	TTL(ctx context.Context, key string) (time.Duration, error)
}

// Re-export types from the types package for convenience.
type (
	// Config holds cache connector configuration from HCL.
	Config = types.Config

	// PoolConfig holds connection pool configuration.
	PoolConfig = types.PoolConfig
)

// CacheEntry represents a cached value with metadata.
type CacheEntry struct {
	// Value is the cached data
	Value []byte

	// ExpiresAt is when the entry expires (zero means no expiration)
	ExpiresAt time.Time

	// CreatedAt is when the entry was created
	CreatedAt time.Time
}

// IsExpired returns true if the entry has expired.
func (e *CacheEntry) IsExpired() bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.ExpiresAt)
}

// Result represents the result of a cache operation.
type Result struct {
	// Hit indicates if the key was found in cache
	Hit bool

	// Value is the cached value (nil if miss)
	Value []byte

	// TTL is the remaining time to live
	TTL time.Duration
}
