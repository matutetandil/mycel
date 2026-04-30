// Package sync provides synchronization primitives for distributed systems.
package sync

import (
	"context"
	"errors"
	"time"
)

// Common errors for sync primitives.
var (
	ErrLockTimeout  = errors.New("lock acquisition timed out")
	ErrLockBusy     = errors.New("lock is already held")
	ErrLockNotHeld  = errors.New("lock is not held by this instance")
	ErrLockReleased = errors.New("lock was already released")
)

// Lock represents a distributed lock interface.
type Lock interface {
	// Acquire attempts to acquire the lock for the given key.
	// Returns true if the lock was acquired, false if not available.
	// The lock will automatically expire after timeout.
	Acquire(ctx context.Context, key string, timeout time.Duration) (bool, error)

	// Release releases the lock for the given key.
	// Returns an error if the lock is not held by this instance.
	Release(ctx context.Context, key string) error

	// IsHeld checks if the lock is currently held by this instance.
	IsHeld(ctx context.Context, key string) (bool, error)

	// Extend resets the TTL on a lock this instance still owns. Returns
	// true when the extension succeeded, false when the caller no longer
	// owns the lock (TTL expired in the gap, or another worker stole it).
	// Used by ExecuteWithLock to heartbeat long-running flows so the
	// timeout acts as a deadman switch (worker crashed → TTL expires →
	// another worker takes over) instead of a footgun (flow took longer
	// than expected → TTL expires while still holding the critical
	// section → duplicate processing).
	Extend(ctx context.Context, key string, timeout time.Duration) (bool, error)

	// Close cleans up any resources.
	Close() error
}

// LockConfig holds configuration for a lock in a flow.
type LockConfig struct {
	// Key is a CEL expression that evaluates to the lock key.
	Key string `json:"key"`

	// Timeout is the maximum time to hold the lock.
	Timeout time.Duration `json:"timeout"`

	// Wait indicates whether to wait for the lock or fail immediately.
	Wait bool `json:"wait"`

	// Retry is the interval between retry attempts when Wait is true.
	Retry time.Duration `json:"retry"`
}

// DefaultLockConfig returns a LockConfig with sensible defaults.
func DefaultLockConfig() *LockConfig {
	return &LockConfig{
		Timeout: 30 * time.Second,
		Wait:    true,
		Retry:   100 * time.Millisecond,
	}
}

// AcquireWithRetry attempts to acquire a lock with retry logic.
func AcquireWithRetry(ctx context.Context, lock Lock, key string, cfg *LockConfig) (bool, error) {
	if cfg == nil {
		cfg = DefaultLockConfig()
	}

	// If not waiting, try once
	if !cfg.Wait {
		return lock.Acquire(ctx, key, cfg.Timeout)
	}

	// Calculate deadline
	deadline := time.Now().Add(cfg.Timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		acquired, err := lock.Acquire(ctx, key, cfg.Timeout)
		if err != nil {
			return false, err
		}
		if acquired {
			return true, nil
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(cfg.Retry):
		}
	}

	return false, ErrLockTimeout
}
