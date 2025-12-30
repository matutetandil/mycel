// Package cache provides cache connector factories and interfaces.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
	"github.com/mycel-labs/mycel/internal/connector/cache/memory"
	"github.com/mycel-labs/mycel/internal/connector/cache/redis"
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
func (f *Factory) parseConfig(cfg *connector.Config) *Config {
	config := &Config{
		Driver: cfg.Driver,
	}

	// Get URL (for Redis)
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
		if maxConn, ok := pool["max_connections"].(int); ok {
			config.Pool.MaxConnections = maxConn
		}
		if minIdle, ok := pool["min_idle"].(int); ok {
			config.Pool.MinIdle = minIdle
		}
		if maxIdleTime, ok := pool["max_idle_time"].(string); ok {
			if d, err := time.ParseDuration(maxIdleTime); err == nil {
				config.Pool.MaxIdleTime = d
			}
		}
		if connectTimeout, ok := pool["connect_timeout"].(string); ok {
			if d, err := time.ParseDuration(connectTimeout); err == nil {
				config.Pool.ConnectTimeout = d
			}
		}
	}

	return config
}

// createRedis creates a Redis cache connector.
func (f *Factory) createRedis(name string, config *Config) (connector.Connector, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("redis cache requires 'url' configuration")
	}

	return redis.New(name, config), nil
}

// createMemory creates a memory cache connector.
func (f *Factory) createMemory(name string, config *Config) (connector.Connector, error) {
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
