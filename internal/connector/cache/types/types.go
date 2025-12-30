// Package types provides shared types for cache connectors.
package types

import (
	"time"
)

// Config holds cache connector configuration from HCL.
type Config struct {
	// Driver is the cache driver: "redis" or "memory"
	Driver string

	// URL is the connection URL (for Redis)
	// Format: redis://[:password@]host:port[/db]
	URL string

	// Prefix is prepended to all cache keys
	Prefix string

	// MaxItems is the maximum number of items (for memory cache)
	MaxItems int

	// Eviction is the eviction policy for memory cache: "lru", "lfu", "fifo"
	Eviction string

	// DefaultTTL is the default TTL for cache entries
	DefaultTTL time.Duration

	// Pool contains connection pool settings (for Redis)
	Pool PoolConfig
}

// PoolConfig holds connection pool configuration.
type PoolConfig struct {
	// MaxConnections is the maximum number of connections
	MaxConnections int

	// MinIdle is the minimum number of idle connections
	MinIdle int

	// MaxIdleTime is how long connections can be idle before being closed
	MaxIdleTime time.Duration

	// ConnectTimeout is the timeout for establishing new connections
	ConnectTimeout time.Duration
}
