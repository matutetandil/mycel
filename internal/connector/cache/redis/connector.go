// Package redis provides a Redis cache connector for Mycel.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/matutetandil/mycel/internal/connector/cache/types"
)

// UniversalClient is the interface that all Redis client types share.
type UniversalClient interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Exists(ctx context.Context, keys ...string) *redis.IntCmd
	TTL(ctx context.Context, key string) *redis.DurationCmd
	Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	Ping(ctx context.Context) *redis.StatusCmd
	Close() error
}

// Connector implements a Redis cache connector supporting standalone, cluster, and sentinel modes.
type Connector struct {
	name   string
	config *types.Config
	client UniversalClient
	mode   string // standalone, cluster, sentinel
}

// New creates a new Redis cache connector.
func New(name string, config *types.Config) *Connector {
	mode := config.Mode
	if mode == "" {
		mode = "standalone"
	}
	return &Connector{
		name:   name,
		config: config,
		mode:   mode,
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "cache"
}

// Connect establishes the Redis connection.
func (c *Connector) Connect(ctx context.Context) error {
	var err error

	switch c.mode {
	case "cluster":
		err = c.connectCluster(ctx)
	case "sentinel":
		err = c.connectSentinel(ctx)
	default:
		err = c.connectStandalone(ctx)
	}

	if err != nil {
		return err
	}

	// Test connection
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to redis (%s): %w", c.mode, err)
	}

	return nil
}

// connectStandalone connects to a standalone Redis instance.
func (c *Connector) connectStandalone(ctx context.Context) error {
	opts, err := redis.ParseURL(c.config.URL)
	if err != nil {
		return fmt.Errorf("invalid redis URL: %w", err)
	}

	c.applyPoolConfig(opts)
	c.client = redis.NewClient(opts)
	return nil
}

// connectCluster connects to a Redis Cluster.
func (c *Connector) connectCluster(ctx context.Context) error {
	if c.config.Cluster == nil || len(c.config.Cluster.Nodes) == 0 {
		return fmt.Errorf("cluster nodes are required for cluster mode")
	}

	opts := &redis.ClusterOptions{
		Addrs:          c.config.Cluster.Nodes,
		Password:       c.config.Cluster.Password,
		RouteByLatency: c.config.Cluster.RouteByLatency,
		RouteRandomly:  c.config.Cluster.RouteRandomly,
		ReadOnly:       c.config.Cluster.ReadOnly,
	}

	if c.config.Cluster.MaxRedirects > 0 {
		opts.MaxRedirects = c.config.Cluster.MaxRedirects
	}

	// Apply pool configuration
	if c.config.Pool.MaxConnections > 0 {
		opts.PoolSize = c.config.Pool.MaxConnections
	}
	if c.config.Pool.MinIdle > 0 {
		opts.MinIdleConns = c.config.Pool.MinIdle
	}
	if c.config.Pool.MaxIdleTime > 0 {
		opts.ConnMaxIdleTime = c.config.Pool.MaxIdleTime
	}
	if c.config.Pool.ConnectTimeout > 0 {
		opts.DialTimeout = c.config.Pool.ConnectTimeout
	}

	c.client = redis.NewClusterClient(opts)
	return nil
}

// connectSentinel connects to Redis via Sentinel.
func (c *Connector) connectSentinel(ctx context.Context) error {
	if c.config.Sentinel == nil || c.config.Sentinel.MasterName == "" {
		return fmt.Errorf("sentinel master name is required for sentinel mode")
	}
	if len(c.config.Sentinel.Nodes) == 0 {
		return fmt.Errorf("sentinel nodes are required for sentinel mode")
	}

	opts := &redis.FailoverOptions{
		MasterName:       c.config.Sentinel.MasterName,
		SentinelAddrs:    c.config.Sentinel.Nodes,
		SentinelPassword: c.config.Sentinel.Password,
		Password:         c.config.Sentinel.MasterPassword,
		DB:               c.config.Sentinel.DB,
		RouteByLatency:   c.config.Sentinel.RouteByLatency,
		RouteRandomly:    c.config.Sentinel.RouteRandomly,
		ReplicaOnly:      c.config.Sentinel.ReplicaOnly,
	}

	// Apply pool configuration
	if c.config.Pool.MaxConnections > 0 {
		opts.PoolSize = c.config.Pool.MaxConnections
	}
	if c.config.Pool.MinIdle > 0 {
		opts.MinIdleConns = c.config.Pool.MinIdle
	}
	if c.config.Pool.MaxIdleTime > 0 {
		opts.ConnMaxIdleTime = c.config.Pool.MaxIdleTime
	}
	if c.config.Pool.ConnectTimeout > 0 {
		opts.DialTimeout = c.config.Pool.ConnectTimeout
	}

	c.client = redis.NewFailoverClient(opts)
	return nil
}

// applyPoolConfig applies pool configuration to redis options.
func (c *Connector) applyPoolConfig(opts *redis.Options) {
	if c.config.Pool.MaxConnections > 0 {
		opts.PoolSize = c.config.Pool.MaxConnections
	}
	if c.config.Pool.MinIdle > 0 {
		opts.MinIdleConns = c.config.Pool.MinIdle
	}
	if c.config.Pool.MaxIdleTime > 0 {
		opts.ConnMaxIdleTime = c.config.Pool.MaxIdleTime
	}
	if c.config.Pool.ConnectTimeout > 0 {
		opts.DialTimeout = c.config.Pool.ConnectTimeout
	}
}

// Close closes the Redis connection.
func (c *Connector) Close(ctx context.Context) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Health checks if the Redis connection is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("redis not connected")
	}
	return c.client.Ping(ctx).Err()
}

// Mode returns the current Redis mode.
func (c *Connector) Mode() string {
	return c.mode
}

// buildKey constructs the full cache key with prefix.
func (c *Connector) buildKey(key string) string {
	if c.config.Prefix != "" {
		return c.config.Prefix + ":" + key
	}
	return key
}

// Get retrieves a value from Redis.
func (c *Connector) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if c.client == nil {
		return nil, false, fmt.Errorf("redis not connected")
	}

	fullKey := c.buildKey(key)
	val, err := c.client.Get(ctx, fullKey).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get error: %w", err)
	}

	return val, true, nil
}

// Set stores a value in Redis with TTL.
func (c *Connector) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if c.client == nil {
		return fmt.Errorf("redis not connected")
	}

	fullKey := c.buildKey(key)

	// Use default TTL if not specified
	if ttl == 0 && c.config.DefaultTTL > 0 {
		ttl = c.config.DefaultTTL
	}

	return c.client.Set(ctx, fullKey, value, ttl).Err()
}

// Delete removes one or more keys from Redis.
func (c *Connector) Delete(ctx context.Context, keys ...string) error {
	if c.client == nil {
		return fmt.Errorf("redis not connected")
	}

	if len(keys) == 0 {
		return nil
	}

	// Build full keys with prefix
	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = c.buildKey(key)
	}

	return c.client.Del(ctx, fullKeys...).Err()
}

// DeletePattern removes all keys matching the pattern.
func (c *Connector) DeletePattern(ctx context.Context, pattern string) error {
	if c.client == nil {
		return fmt.Errorf("redis not connected")
	}

	fullPattern := c.buildKey(pattern)

	// Use SCAN to find matching keys (safer than KEYS for large datasets)
	var cursor uint64
	var deleted int64

	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, fullPattern, 100).Result()
		if err != nil {
			return fmt.Errorf("redis scan error: %w", err)
		}

		if len(keys) > 0 {
			n, err := c.client.Del(ctx, keys...).Result()
			if err != nil {
				return fmt.Errorf("redis delete error: %w", err)
			}
			deleted += n
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

// Exists checks if a key exists in Redis.
func (c *Connector) Exists(ctx context.Context, key string) (bool, error) {
	if c.client == nil {
		return false, fmt.Errorf("redis not connected")
	}

	fullKey := c.buildKey(key)
	n, err := c.client.Exists(ctx, fullKey).Result()
	if err != nil {
		return false, fmt.Errorf("redis exists error: %w", err)
	}

	return n > 0, nil
}

// TTL returns the remaining TTL for a key.
func (c *Connector) TTL(ctx context.Context, key string) (time.Duration, error) {
	if c.client == nil {
		return 0, fmt.Errorf("redis not connected")
	}

	fullKey := c.buildKey(key)
	ttl, err := c.client.TTL(ctx, fullKey).Result()
	if err != nil {
		return 0, fmt.Errorf("redis ttl error: %w", err)
	}

	return ttl, nil
}

// Client returns the underlying Redis client for advanced operations.
// Note: Returns nil for cluster/sentinel modes - use ClusterClient() or FailoverClient() instead.
func (c *Connector) Client() *redis.Client {
	if client, ok := c.client.(*redis.Client); ok {
		return client
	}
	return nil
}

// ClusterClient returns the underlying Redis Cluster client (nil if not in cluster mode).
func (c *Connector) ClusterClient() *redis.ClusterClient {
	if client, ok := c.client.(*redis.ClusterClient); ok {
		return client
	}
	return nil
}

// UniversalRedisClient returns the underlying universal client interface.
func (c *Connector) UniversalRedisClient() UniversalClient {
	return c.client
}
