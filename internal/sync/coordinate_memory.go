package sync

import (
	"context"
	"sync"
	"time"
)

// MemoryCoordinator implements Coordinator interface using in-memory storage.
type MemoryCoordinator struct {
	mu      sync.RWMutex
	signals map[string]time.Time       // signal -> expiresAt
	waiters map[string][]chan struct{} // signal -> waiting channels

	done chan struct{}
}

// NewMemoryCoordinator creates a new in-memory coordinator.
func NewMemoryCoordinator(cleanupInterval time.Duration) *MemoryCoordinator {
	if cleanupInterval <= 0 {
		cleanupInterval = time.Second
	}

	c := &MemoryCoordinator{
		signals: make(map[string]time.Time),
		waiters: make(map[string][]chan struct{}),
		done:    make(chan struct{}),
	}

	// Start cleanup goroutine
	go c.cleanupLoop(cleanupInterval)

	return c
}

// cleanupLoop periodically removes expired signals.
func (c *MemoryCoordinator) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanExpired()
		case <-c.done:
			return
		}
	}
}

// cleanExpired removes expired signal entries.
func (c *MemoryCoordinator) cleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for signal, expiresAt := range c.signals {
		if now.After(expiresAt) {
			delete(c.signals, signal)
		}
	}
}

// Signal emits a signal that waiting processes can receive.
func (c *MemoryCoordinator) Signal(ctx context.Context, signal string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Store signal with expiration
	c.signals[signal] = time.Now().Add(ttl)

	// Notify all waiters
	if waiters, ok := c.waiters[signal]; ok {
		for _, ch := range waiters {
			// Non-blocking send - close the channel to notify
			select {
			case <-ch:
				// Already closed
			default:
				close(ch)
			}
		}
		delete(c.waiters, signal)
	}

	return nil
}

// Wait waits for a signal to be emitted.
func (c *MemoryCoordinator) Wait(ctx context.Context, signal string, timeout time.Duration) (bool, error) {
	// First check if signal already exists
	c.mu.Lock()
	if expiresAt, ok := c.signals[signal]; ok && time.Now().Before(expiresAt) {
		c.mu.Unlock()
		return true, nil
	}

	// Create channel for waiting
	ch := make(chan struct{})
	c.waiters[signal] = append(c.waiters[signal], ch)
	c.mu.Unlock()

	// Double-check after registering (to avoid race condition)
	c.mu.RLock()
	if expiresAt, ok := c.signals[signal]; ok && time.Now().Before(expiresAt) {
		c.mu.RUnlock()
		// Remove ourselves from waiters
		c.removeWaiter(signal, ch)
		return true, nil
	}
	c.mu.RUnlock()

	// Wait for signal or timeout
	select {
	case <-ch:
		return true, nil
	case <-time.After(timeout):
		c.removeWaiter(signal, ch)
		return false, nil
	case <-ctx.Done():
		c.removeWaiter(signal, ch)
		return false, ctx.Err()
	}
}

// removeWaiter removes a waiter channel from the waiters list.
func (c *MemoryCoordinator) removeWaiter(signal string, ch chan struct{}) {
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
func (c *MemoryCoordinator) Exists(ctx context.Context, signal string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	expiresAt, ok := c.signals[signal]
	if !ok {
		return false, nil
	}

	return time.Now().Before(expiresAt), nil
}

// Close stops the cleanup goroutine and notifies all waiters.
func (c *MemoryCoordinator) Close() error {
	close(c.done)

	// Notify all waiters that we're shutting down
	c.mu.Lock()
	defer c.mu.Unlock()

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

	return nil
}

// Stats returns statistics about the coordinator.
func (c *MemoryCoordinator) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalWaiters := 0
	for _, waiters := range c.waiters {
		totalWaiters += len(waiters)
	}

	return map[string]interface{}{
		"active_signals": len(c.signals),
		"active_waiters": totalWaiters,
		"signal_keys":    len(c.waiters),
	}
}
