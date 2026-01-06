// Package types provides shared types for cache connectors.
package types

import (
	"time"
)

// Config holds cache connector configuration from HCL.
type Config struct {
	// Driver is the cache driver: "redis" or "memory"
	Driver string

	// Mode is the Redis mode: "standalone", "cluster", "sentinel"
	Mode string

	// URL is the connection URL (for Redis standalone)
	// Format: redis://[:password@]host:port[/db]
	URL string

	// Cluster configuration
	Cluster *ClusterConfig

	// Sentinel configuration
	Sentinel *SentinelConfig

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

// ClusterConfig holds Redis Cluster configuration.
type ClusterConfig struct {
	// Nodes is the list of cluster nodes (host:port)
	Nodes []string

	// Password for cluster authentication
	Password string

	// MaxRedirects is the maximum number of redirects to follow
	MaxRedirects int

	// RouteByLatency routes read commands to the closest node
	RouteByLatency bool

	// RouteRandomly routes read commands to a random node
	RouteRandomly bool

	// ReadOnly enables read-only mode for replica nodes
	ReadOnly bool
}

// SentinelConfig holds Redis Sentinel configuration.
type SentinelConfig struct {
	// MasterName is the name of the master to monitor
	MasterName string

	// Nodes is the list of Sentinel nodes (host:port)
	Nodes []string

	// Password for Sentinel authentication
	Password string

	// MasterPassword for master authentication
	MasterPassword string

	// DB is the database number to select
	DB int

	// RouteByLatency routes read commands to the closest replica
	RouteByLatency bool

	// RouteRandomly routes read commands to a random replica
	RouteRandomly bool

	// ReplicaOnly only use replicas for read operations
	ReplicaOnly bool
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
