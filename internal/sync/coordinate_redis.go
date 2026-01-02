package sync

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCoordinator implements Coordinator interface using Redis.
// Uses a single Pub/Sub subscription hub for efficient waiting.
type RedisCoordinator struct {
	client *redis.Client
	prefix string

	mu      sync.RWMutex
	waiters map[string][]chan struct{} // signal -> waiting channels

	sub  *redis.PubSub
	done chan struct{}
}

// RedisCoordinatorConfig holds configuration for RedisCoordinator.
type RedisCoordinatorConfig struct {
	// URL is the Redis connection URL.
	URL string

	// Prefix is prepended to all signal keys.
	Prefix string

	// PoolSize is the maximum number of connections.
	PoolSize int
}

// NewRedisCoordinator creates a new Redis-based coordinator.
func NewRedisCoordinator(cfg *RedisCoordinatorConfig) (*RedisCoordinator, error) {
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
		prefix = "mycel:coord:"
	}

	c := &RedisCoordinator{
		client:  client,
		prefix:  prefix,
		waiters: make(map[string][]chan struct{}),
		done:    make(chan struct{}),
	}

	// Subscribe to all signals using pattern
	c.sub = client.PSubscribe(ctx, prefix+"signal:*")

	// Start listener goroutine
	go c.listen()

	return c, nil
}

// NewRedisCoordinatorFromClient creates a RedisCoordinator from an existing Redis client.
func NewRedisCoordinatorFromClient(client *redis.Client, prefix string) *RedisCoordinator {
	if prefix == "" {
		prefix = "mycel:coord:"
	}

	ctx := context.Background()

	c := &RedisCoordinator{
		client:  client,
		prefix:  prefix,
		waiters: make(map[string][]chan struct{}),
		done:    make(chan struct{}),
	}

	// Subscribe to all signals using pattern
	c.sub = client.PSubscribe(ctx, prefix+"signal:*")

	// Start listener goroutine
	go c.listen()

	return c
}

// listen listens for published signals and notifies waiters.
func (c *RedisCoordinator) listen() {
	ch := c.sub.Channel()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Extract signal name from channel
			// Channel format: prefix + "signal:" + signal
			signal := strings.TrimPrefix(msg.Channel, c.prefix+"signal:")
			c.notifyWaiters(signal)
		case <-c.done:
			return
		}
	}
}

// notifyWaiters notifies all waiters for a signal.
func (c *RedisCoordinator) notifyWaiters(signal string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if waiters, ok := c.waiters[signal]; ok {
		for _, ch := range waiters {
			select {
			case <-ch:
				// Already closed
			default:
				close(ch)
			}
		}
		delete(c.waiters, signal)
	}
}

// Signal emits a signal that waiting processes can receive.
func (c *RedisCoordinator) Signal(ctx context.Context, signal string, ttl time.Duration) error {
	// Store signal with TTL (for late joiners to check)
	signalKey := c.prefix + signal

	// Use pipeline for atomic set + publish
	pipe := c.client.Pipeline()
	pipe.Set(ctx, signalKey, "1", ttl)
	pipe.Publish(ctx, c.prefix+"signal:"+signal, "ready")

	_, err := pipe.Exec(ctx)
	return err
}

// Wait waits for a signal to be emitted.
func (c *RedisCoordinator) Wait(ctx context.Context, signal string, timeout time.Duration) (bool, error) {
	signalKey := c.prefix + signal

	// 1. Check if signal already exists
	exists, err := c.client.Exists(ctx, signalKey).Result()
	if err != nil {
		return false, err
	}
	if exists > 0 {
		return true, nil
	}

	// 2. Register waiter BEFORE double-check
	ch := make(chan struct{})
	c.mu.Lock()
	c.waiters[signal] = append(c.waiters[signal], ch)
	c.mu.Unlock()

	// Cleanup on exit
	defer c.removeWaiter(signal, ch)

	// 3. Double-check after registering (to avoid race condition)
	exists, err = c.client.Exists(ctx, signalKey).Result()
	if err != nil {
		return false, err
	}
	if exists > 0 {
		return true, nil
	}

	// 4. Wait passively for signal
	select {
	case <-ch:
		return true, nil
	case <-time.After(timeout):
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// removeWaiter removes a waiter channel from the waiters list.
func (c *RedisCoordinator) removeWaiter(signal string, ch chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	waiters := c.waiters[signal]
	for i, w := range waiters {
		if w == ch {
			c.waiters[signal] = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(c.waiters[signal]) == 0 {
		delete(c.waiters, signal)
	}
}

// Exists checks if a signal has been emitted and is still valid.
func (c *RedisCoordinator) Exists(ctx context.Context, signal string) (bool, error) {
	signalKey := c.prefix + signal

	exists, err := c.client.Exists(ctx, signalKey).Result()
	if err != nil {
		return false, err
	}

	return exists > 0, nil
}

// Close closes the Redis client and pub/sub.
func (c *RedisCoordinator) Close() error {
	close(c.done)

	// Close pub/sub
	if c.sub != nil {
		c.sub.Close()
	}

	// Notify all waiters
	c.mu.Lock()
	for signal, waiters := range c.waiters {
		for _, ch := range waiters {
			select {
			case <-ch:
				// Already closed
			default:
				close(ch)
			}
		}
		delete(c.waiters, signal)
	}
	c.mu.Unlock()

	return c.client.Close()
}

// Stats returns statistics about the coordinator.
func (c *RedisCoordinator) Stats(ctx context.Context) (map[string]interface{}, error) {
	c.mu.RLock()
	totalWaiters := 0
	for _, waiters := range c.waiters {
		totalWaiters += len(waiters)
	}
	signalKeys := len(c.waiters)
	c.mu.RUnlock()

	// Count signals in Redis
	keys, err := c.client.Keys(ctx, c.prefix+"*").Result()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"active_signals":    len(keys),
		"active_waiters":    totalWaiters,
		"waiter_signal_keys": signalKeys,
	}, nil
}
