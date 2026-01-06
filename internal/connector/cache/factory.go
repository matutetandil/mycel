// Package cache provides cache connector factories and interfaces.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/internal/connector/cache/redis"
	"github.com/matutetandil/mycel/internal/connector/cache/types"
)

// Factory creates cache connectors.
type Factory struct{}

// NewFactory creates a new cache connector factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "cache"
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	if connType != "cache" {
		return false
	}
	return driver == "redis" || driver == "memory"
}

// Create creates a new cache connector from config.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	config := f.parseConfig(cfg)

	switch cfg.Driver {
	case "redis":
		return f.createRedis(cfg.Name, config)
	case "memory":
		return f.createMemory(cfg.Name, config)
	default:
		return nil, fmt.Errorf("unsupported cache driver: %s", cfg.Driver)
	}
}

// parseConfig extracts cache configuration from connector config.
func (f *Factory) parseConfig(cfg *connector.Config) *types.Config {
	config := &types.Config{
		Driver: cfg.Driver,
	}

	// Get mode (standalone, cluster, sentinel)
	if mode, ok := cfg.Properties["mode"].(string); ok {
		config.Mode = mode
	}

	// Get URL (for Redis standalone)
	if url, ok := cfg.Properties["url"].(string); ok {
		config.URL = url
	}

	// Get prefix
	if prefix, ok := cfg.Properties["prefix"].(string); ok {
		config.Prefix = prefix
	}

	// Get max_items (for memory)
	if maxItems, ok := cfg.Properties["max_items"].(int); ok {
		config.MaxItems = maxItems
	}

	// Get eviction policy
	if eviction, ok := cfg.Properties["eviction"].(string); ok {
		config.Eviction = eviction
	}

	// Get default TTL
	if ttl, ok := cfg.Properties["default_ttl"].(string); ok {
		if d, err := time.ParseDuration(ttl); err == nil {
			config.DefaultTTL = d
		}
	}

	// Get pool configuration
	if pool, ok := cfg.Properties["pool"].(map[string]interface{}); ok {
		config.Pool = f.parsePoolConfig(pool)
	}

	// Get cluster configuration
	if cluster, ok := cfg.Properties["cluster"].(map[string]interface{}); ok {
		config.Cluster = f.parseClusterConfig(cluster)
	}

	// Get sentinel configuration
	if sentinel, ok := cfg.Properties["sentinel"].(map[string]interface{}); ok {
		config.Sentinel = f.parseSentinelConfig(sentinel)
	}

	return config
}

// parsePoolConfig extracts pool configuration.
func (f *Factory) parsePoolConfig(pool map[string]interface{}) types.PoolConfig {
	config := types.PoolConfig{}

	if maxConn, ok := pool["max_connections"].(int); ok {
		config.MaxConnections = maxConn
	}
	if minIdle, ok := pool["min_idle"].(int); ok {
		config.MinIdle = minIdle
	}
	if maxIdleTime, ok := pool["max_idle_time"].(string); ok {
		if d, err := time.ParseDuration(maxIdleTime); err == nil {
			config.MaxIdleTime = d
		}
	}
	if connectTimeout, ok := pool["connect_timeout"].(string); ok {
		if d, err := time.ParseDuration(connectTimeout); err == nil {
			config.ConnectTimeout = d
		}
	}

	return config
}

// parseClusterConfig extracts Redis Cluster configuration.
func (f *Factory) parseClusterConfig(cluster map[string]interface{}) *types.ClusterConfig {
	config := &types.ClusterConfig{}

	// Get nodes
	if nodes, ok := cluster["nodes"].([]interface{}); ok {
		for _, n := range nodes {
			if s, ok := n.(string); ok {
				config.Nodes = append(config.Nodes, s)
			}
		}
	}
	if nodes, ok := cluster["nodes"].([]string); ok {
		config.Nodes = nodes
	}

	// Get password
	if password, ok := cluster["password"].(string); ok {
		config.Password = password
	}

	// Get max_redirects
	if maxRedirects, ok := cluster["max_redirects"].(int); ok {
		config.MaxRedirects = maxRedirects
	}

	// Get routing options
	if routeByLatency, ok := cluster["route_by_latency"].(bool); ok {
		config.RouteByLatency = routeByLatency
	}
	if routeRandomly, ok := cluster["route_randomly"].(bool); ok {
		config.RouteRandomly = routeRandomly
	}
	if readOnly, ok := cluster["read_only"].(bool); ok {
		config.ReadOnly = readOnly
	}

	return config
}

// parseSentinelConfig extracts Redis Sentinel configuration.
func (f *Factory) parseSentinelConfig(sentinel map[string]interface{}) *types.SentinelConfig {
	config := &types.SentinelConfig{}

	// Get master name
	if masterName, ok := sentinel["master_name"].(string); ok {
		config.MasterName = masterName
	}

	// Get nodes
	if nodes, ok := sentinel["nodes"].([]interface{}); ok {
		for _, n := range nodes {
			if s, ok := n.(string); ok {
				config.Nodes = append(config.Nodes, s)
			}
		}
	}
	if nodes, ok := sentinel["nodes"].([]string); ok {
		config.Nodes = nodes
	}

	// Get passwords
	if password, ok := sentinel["password"].(string); ok {
		config.Password = password
	}
	if masterPassword, ok := sentinel["master_password"].(string); ok {
		config.MasterPassword = masterPassword
	}

	// Get database
	if db, ok := sentinel["db"].(int); ok {
		config.DB = db
	}

	// Get routing options
	if routeByLatency, ok := sentinel["route_by_latency"].(bool); ok {
		config.RouteByLatency = routeByLatency
	}
	if routeRandomly, ok := sentinel["route_randomly"].(bool); ok {
		config.RouteRandomly = routeRandomly
	}
	if replicaOnly, ok := sentinel["replica_only"].(bool); ok {
		config.ReplicaOnly = replicaOnly
	}

	return config
}

// createRedis creates a Redis cache connector.
func (f *Factory) createRedis(name string, config *types.Config) (connector.Connector, error) {
	// Validate configuration based on mode
	mode := config.Mode
	if mode == "" {
		mode = "standalone"
	}

	switch mode {
	case "standalone":
		if config.URL == "" {
			return nil, fmt.Errorf("redis standalone mode requires 'url' configuration")
		}
	case "cluster":
		if config.Cluster == nil || len(config.Cluster.Nodes) == 0 {
			return nil, fmt.Errorf("redis cluster mode requires 'cluster.nodes' configuration")
		}
	case "sentinel":
		if config.Sentinel == nil || config.Sentinel.MasterName == "" {
			return nil, fmt.Errorf("redis sentinel mode requires 'sentinel.master_name' configuration")
		}
		if len(config.Sentinel.Nodes) == 0 {
			return nil, fmt.Errorf("redis sentinel mode requires 'sentinel.nodes' configuration")
		}
	default:
		return nil, fmt.Errorf("unsupported redis mode: %s (use: standalone, cluster, sentinel)", mode)
	}

	return redis.New(name, config), nil
}

// createMemory creates a memory cache connector.
func (f *Factory) createMemory(name string, config *types.Config) (connector.Connector, error) {
	// Set defaults for memory cache
	if config.MaxItems <= 0 {
		config.MaxItems = 10000
	}
	if config.Eviction == "" {
		config.Eviction = "lru"
	}

	return memory.New(name, config), nil
}

// GetCache retrieves a Cache interface from a connector.
// Returns nil if the connector is not a cache.
func GetCache(conn connector.Connector) Cache {
	if c, ok := conn.(Cache); ok {
		return c
	}
	return nil
}
