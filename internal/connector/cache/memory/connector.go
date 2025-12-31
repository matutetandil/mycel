// Package memory provides an in-memory cache connector for Mycel.
// Uses LRU eviction and supports TTL-based expiration.
package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/matutetandil/mycel/internal/connector/cache/types"
)

// entry represents a cached value with expiration.
type entry struct {
	value     []byte
	expiresAt time.Time
}

// isExpired checks if the entry has expired.
func (e *entry) isExpired() bool {
	if e.expiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.expiresAt)
}

// Connector implements an in-memory cache connector with LRU eviction.
type Connector struct {
	name   string
	config *types.Config
	cache  *lru.Cache[string, *entry]
	mu     sync.RWMutex

	// For cleanup goroutine
	stopCh chan struct{}
}

// New creates a new memory cache connector.
func New(name string, config *types.Config) *Connector {
	return &Connector{
		name:   name,
		config: config,
		stopCh: make(chan struct{}),
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

// Connect initializes the in-memory cache.
func (c *Connector) Connect(ctx context.Context) error {
	maxItems := c.config.MaxItems
	if maxItems <= 0 {
		maxItems = 10000 // Default max items
	}

	cache, err := lru.New[string, *entry](maxItems)
	if err != nil {
		return fmt.Errorf("failed to create LRU cache: %w", err)
	}

	c.cache = cache

	// Start cleanup goroutine for expired entries
	go c.cleanupLoop()

	return nil
}

// Close stops the cache and cleanup goroutine.
func (c *Connector) Close(ctx context.Context) error {
	close(c.stopCh)
	if c.cache != nil {
		c.cache.Purge()
	}
	return nil
}

// Health checks if the cache is operational.
func (c *Connector) Health(ctx context.Context) error {
	if c.cache == nil {
		return fmt.Errorf("memory cache not initialized")
	}
	return nil
}

// cleanupLoop periodically removes expired entries.
func (c *Connector) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.removeExpired()
		}
	}
}

// removeExpired removes all expired entries from the cache.
func (c *Connector) removeExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		return
	}

	keys := c.cache.Keys()
	for _, key := range keys {
		if e, ok := c.cache.Peek(key); ok && e.isExpired() {
			c.cache.Remove(key)
		}
	}
}

// buildKey constructs the full cache key with prefix.
func (c *Connector) buildKey(key string) string {
	if c.config.Prefix != "" {
		return c.config.Prefix + ":" + key
	}
	return key
}

// Get retrieves a value from the cache.
func (c *Connector) Get(ctx context.Context, key string) ([]byte, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return nil, false, fmt.Errorf("memory cache not initialized")
	}

	fullKey := c.buildKey(key)
	e, ok := c.cache.Get(fullKey)
	if !ok {
		return nil, false, nil
	}

	// Check if expired
	if e.isExpired() {
		// Remove expired entry (upgrade to write lock)
		c.mu.RUnlock()
		c.mu.Lock()
		c.cache.Remove(fullKey)
		c.mu.Unlock()
		c.mu.RLock()
		return nil, false, nil
	}

	return e.value, true, nil
}

// Set stores a value in the cache with TTL.
func (c *Connector) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		return fmt.Errorf("memory cache not initialized")
	}

	fullKey := c.buildKey(key)

	// Use default TTL if not specified
	if ttl == 0 && c.config.DefaultTTL > 0 {
		ttl = c.config.DefaultTTL
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	// Make a copy of the value to prevent external modifications
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	c.cache.Add(fullKey, &entry{
		value:     valueCopy,
		expiresAt: expiresAt,
	})

	return nil
}

// Delete removes one or more keys from the cache.
func (c *Connector) Delete(ctx context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		return fmt.Errorf("memory cache not initialized")
	}

	for _, key := range keys {
		fullKey := c.buildKey(key)
		c.cache.Remove(fullKey)
	}

	return nil
}

// DeletePattern removes all keys matching the pattern.
// Pattern syntax: `*` matches any characters.
// Note: This is less efficient than Redis SCAN as it iterates all keys.
func (c *Connector) DeletePattern(ctx context.Context, pattern string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		return fmt.Errorf("memory cache not initialized")
	}

	fullPattern := c.buildKey(pattern)
	keys := c.cache.Keys()

	for _, key := range keys {
		if matchPattern(fullPattern, key) {
			c.cache.Remove(key)
		}
	}

	return nil
}

// matchPattern checks if a key matches a pattern with * wildcards.
func matchPattern(pattern, key string) bool {
	// Convert * wildcard pattern to simple matching
	if !strings.Contains(pattern, "*") {
		return pattern == key
	}

	// Split by * and check each part
	parts := strings.Split(pattern, "*")
	pos := 0

	for i, part := range parts {
		if part == "" {
			continue
		}

		idx := strings.Index(key[pos:], part)
		if idx == -1 {
			return false
		}

		// First part must match at the beginning if pattern doesn't start with *
		if i == 0 && !strings.HasPrefix(pattern, "*") && idx != 0 {
			return false
		}

		pos += idx + len(part)
	}

	// Last part must match at the end if pattern doesn't end with *
	if !strings.HasSuffix(pattern, "*") && pos != len(key) {
		return false
	}

	return true
}

// Exists checks if a key exists in the cache.
func (c *Connector) Exists(ctx context.Context, key string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return false, fmt.Errorf("memory cache not initialized")
	}

	fullKey := c.buildKey(key)
	e, ok := c.cache.Peek(fullKey)
	if !ok {
		return false, nil
	}

	// Check if expired
	if e.isExpired() {
		return false, nil
	}

	return true, nil
}

// TTL returns the remaining TTL for a key.
// Returns -1 if the key exists but has no TTL, -2 if the key doesn't exist.
func (c *Connector) TTL(ctx context.Context, key string) (time.Duration, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return 0, fmt.Errorf("memory cache not initialized")
	}

	fullKey := c.buildKey(key)
	e, ok := c.cache.Peek(fullKey)
	if !ok {
		return -2 * time.Second, nil
	}

	if e.isExpired() {
		return -2 * time.Second, nil
	}

	if e.expiresAt.IsZero() {
		return -1 * time.Second, nil
	}

	return time.Until(e.expiresAt), nil
}

// Len returns the number of items in the cache.
func (c *Connector) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return 0
	}

	return c.cache.Len()
}

// Keys returns all keys in the cache (for debugging).
func (c *Connector) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return nil
	}

	return c.cache.Keys()
}
