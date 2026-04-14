package sync

import (
	"context"
	"errors"
	"time"
)

// Semaphore errors.
var (
	ErrSemaphoreTimeout = errors.New("semaphore acquisition timed out")
	ErrSemaphoreFull    = errors.New("semaphore has no available permits")
	ErrPermitNotFound   = errors.New("permit not found")
)

// Semaphore represents a distributed counting semaphore interface.
type Semaphore interface {
	// Acquire attempts to acquire a permit from the semaphore.
	// Returns a permit ID that must be used for Release.
	// The permit will automatically expire after lease duration.
	Acquire(ctx context.Context, key string, timeout, lease time.Duration) (string, error)

	// Release releases a permit back to the semaphore.
	Release(ctx context.Context, key string, permitID string) error

	// Available returns the number of available permits.
	Available(ctx context.Context, key string) (int, error)

	// Close cleans up any resources.
	Close() error
}

// SemaphoreConfig holds configuration for a semaphore in a flow.
type SemaphoreConfig struct {
	// Key is a CEL expression that evaluates to the semaphore key.
	Key string `json:"key"`

	// MaxPermits is the maximum number of concurrent permits.
	MaxPermits int `json:"max_permits"`

	// Timeout is the maximum time to wait for a permit.
	Timeout time.Duration `json:"timeout"`

	// Lease is the maximum time to hold a permit before auto-release.
	Lease time.Duration `json:"lease"`
}

// DefaultSemaphoreConfig returns a SemaphoreConfig with sensible defaults.
func DefaultSemaphoreConfig() *SemaphoreConfig {
	return &SemaphoreConfig{
		MaxPermits: 10,
		Timeout:    30 * time.Second,
		Lease:      60 * time.Second,
	}
}

// AcquireSemaphoreWithRetry attempts to acquire a semaphore permit with retry logic.
func AcquireSemaphoreWithRetry(ctx context.Context, sem Semaphore, key string, cfg *SemaphoreConfig) (string, error) {
	if cfg == nil {
		cfg = DefaultSemaphoreConfig()
	}

	deadline := time.Now().Add(cfg.Timeout)
	retryInterval := 50 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		permitID, err := sem.Acquire(ctx, key, cfg.Timeout, cfg.Lease)
		if err == nil {
			return permitID, nil
		}
		if err != ErrSemaphoreFull {
			return "", err
		}

		// Wait before retry
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(retryInterval):
		}
	}

	return "", ErrSemaphoreTimeout
}
