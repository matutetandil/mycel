package sync

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemorySemaphore implements Semaphore interface using in-memory storage.
type MemorySemaphore struct {
	mu         sync.RWMutex
	semaphores map[string]*semaphoreState
	maxPermits int
	done       chan struct{}
}

type semaphoreState struct {
	permits map[string]time.Time // permitID -> expiresAt
}

// NewMemorySemaphore creates a new in-memory semaphore manager.
func NewMemorySemaphore(maxPermits int) *MemorySemaphore {
	if maxPermits <= 0 {
		maxPermits = 10
	}

	m := &MemorySemaphore{
		semaphores: make(map[string]*semaphoreState),
		maxPermits: maxPermits,
		done:       make(chan struct{}),
	}

	// Start cleanup goroutine
	go m.cleanupLoop()

	return m
}

// cleanupLoop periodically removes expired permits.
func (m *MemorySemaphore) cleanupLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanExpired()
		case <-m.done:
			return
		}
	}
}

// cleanExpired removes expired permit entries.
func (m *MemorySemaphore) cleanExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for _, state := range m.semaphores {
		for permitID, expiresAt := range state.permits {
			if now.After(expiresAt) {
				delete(state.permits, permitID)
			}
		}
	}
}

// getOrCreateState gets or creates semaphore state for a key.
func (m *MemorySemaphore) getOrCreateState(key string) *semaphoreState {
	if state, exists := m.semaphores[key]; exists {
		return state
	}
	state := &semaphoreState{
		permits: make(map[string]time.Time),
	}
	m.semaphores[key] = state
	return state
}

// countActivePermits counts non-expired permits.
func (m *MemorySemaphore) countActivePermits(state *semaphoreState) int {
	now := time.Now()
	count := 0
	for _, expiresAt := range state.permits {
		if now.Before(expiresAt) {
			count++
		}
	}
	return count
}

// Acquire attempts to acquire a permit from the semaphore.
func (m *MemorySemaphore) Acquire(ctx context.Context, key string, timeout, lease time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.getOrCreateState(key)

	// Clean expired permits first
	now := time.Now()
	for permitID, expiresAt := range state.permits {
		if now.After(expiresAt) {
			delete(state.permits, permitID)
		}
	}

	// Check if we have capacity
	if len(state.permits) >= m.maxPermits {
		return "", ErrSemaphoreFull
	}

	// Acquire permit
	permitID := uuid.New().String()
	state.permits[permitID] = now.Add(lease)

	return permitID, nil
}

// Release releases a permit back to the semaphore.
func (m *MemorySemaphore) Release(ctx context.Context, key string, permitID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.semaphores[key]
	if !exists {
		return ErrPermitNotFound
	}

	if _, exists := state.permits[permitID]; !exists {
		return ErrPermitNotFound
	}

	delete(state.permits, permitID)
	return nil
}

// Available returns the number of available permits.
func (m *MemorySemaphore) Available(ctx context.Context, key string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.semaphores[key]
	if !exists {
		return m.maxPermits, nil
	}

	activeCount := m.countActivePermits(state)
	return m.maxPermits - activeCount, nil
}

// Close stops the cleanup goroutine.
func (m *MemorySemaphore) Close() error {
	close(m.done)
	return nil
}

// Stats returns statistics about the semaphore manager.
func (m *MemorySemaphore) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	for key, state := range m.semaphores {
		stats[key] = map[string]interface{}{
			"active_permits":    m.countActivePermits(state),
			"max_permits":       m.maxPermits,
			"available_permits": m.maxPermits - m.countActivePermits(state),
		}
	}
	return stats
}
