package sync

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryLockStorage is shared storage for memory locks.
// Multiple MemoryLock instances using the same storage will coordinate.
type MemoryLockStorage struct {
	mu     sync.RWMutex
	locks  map[string]*lockEntry
	done   chan struct{}
	closed bool
}

// NewMemoryLockStorage creates shared storage for memory locks.
func NewMemoryLockStorage() *MemoryLockStorage {
	s := &MemoryLockStorage{
		locks: make(map[string]*lockEntry),
		done:  make(chan struct{}),
	}

	// Start cleanup goroutine
	go s.cleanupLoop()

	return s
}

// cleanupLoop periodically removes expired locks.
func (s *MemoryLockStorage) cleanupLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanExpired()
		case <-s.done:
			return
		}
	}
}

// cleanExpired removes expired lock entries.
func (s *MemoryLockStorage) cleanExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, entry := range s.locks {
		if now.After(entry.expiresAt) {
			delete(s.locks, key)
		}
	}
}

// Close stops the cleanup goroutine.
func (s *MemoryLockStorage) Close() error {
	if !s.closed {
		s.closed = true
		close(s.done)
	}
	return nil
}

// MemoryLock implements Lock interface using in-memory storage.
// Suitable for development and testing, not for production distributed systems.
type MemoryLock struct {
	storage     *MemoryLockStorage
	instanceID  string
	ownsStorage bool
}

type lockEntry struct {
	owner     string
	expiresAt time.Time
}

// NewMemoryLock creates a new in-memory lock manager with its own storage.
func NewMemoryLock() *MemoryLock {
	return &MemoryLock{
		storage:     NewMemoryLockStorage(),
		instanceID:  uuid.New().String(),
		ownsStorage: true,
	}
}

// NewMemoryLockWithStorage creates a new in-memory lock manager with shared storage.
func NewMemoryLockWithStorage(storage *MemoryLockStorage) *MemoryLock {
	return &MemoryLock{
		storage:     storage,
		instanceID:  uuid.New().String(),
		ownsStorage: false,
	}
}

// Acquire attempts to acquire the lock for the given key.
func (m *MemoryLock) Acquire(ctx context.Context, key string, timeout time.Duration) (bool, error) {
	m.storage.mu.Lock()
	defer m.storage.mu.Unlock()

	now := time.Now()

	// Check if lock exists and is not expired
	if entry, exists := m.storage.locks[key]; exists {
		if now.Before(entry.expiresAt) {
			// Lock is held by someone else (or us)
			if entry.owner == m.instanceID {
				// We already hold it, extend the timeout
				entry.expiresAt = now.Add(timeout)
				return true, nil
			}
			return false, nil
		}
		// Lock has expired, remove it
		delete(m.storage.locks, key)
	}

	// Acquire the lock
	m.storage.locks[key] = &lockEntry{
		owner:     m.instanceID,
		expiresAt: now.Add(timeout),
	}

	return true, nil
}

// Release releases the lock for the given key.
func (m *MemoryLock) Release(ctx context.Context, key string) error {
	m.storage.mu.Lock()
	defer m.storage.mu.Unlock()

	entry, exists := m.storage.locks[key]
	if !exists {
		return ErrLockReleased
	}

	if entry.owner != m.instanceID {
		return ErrLockNotHeld
	}

	delete(m.storage.locks, key)
	return nil
}

// Extend resets the lock's expiration if this instance still owns it.
// Returns false when the caller no longer owns the lock — same contract as
// the Redis backend, so ExecuteWithLock's heartbeat path behaves
// identically across drivers.
func (m *MemoryLock) Extend(ctx context.Context, key string, timeout time.Duration) (bool, error) {
	m.storage.mu.Lock()
	defer m.storage.mu.Unlock()

	entry, exists := m.storage.locks[key]
	if !exists {
		return false, nil
	}
	if entry.owner != m.instanceID {
		return false, nil
	}
	if time.Now().After(entry.expiresAt) {
		// TTL already expired; lock is in the cleanup window.
		delete(m.storage.locks, key)
		return false, nil
	}
	entry.expiresAt = time.Now().Add(timeout)
	return true, nil
}

// IsHeld checks if the lock is currently held by this instance.
func (m *MemoryLock) IsHeld(ctx context.Context, key string) (bool, error) {
	m.storage.mu.RLock()
	defer m.storage.mu.RUnlock()

	entry, exists := m.storage.locks[key]
	if !exists {
		return false, nil
	}

	if time.Now().After(entry.expiresAt) {
		return false, nil
	}

	return entry.owner == m.instanceID, nil
}

// Close stops the cleanup goroutine if this lock owns the storage.
func (m *MemoryLock) Close() error {
	if m.ownsStorage {
		return m.storage.Close()
	}
	return nil
}

// Stats returns statistics about the lock manager.
func (m *MemoryLock) Stats() map[string]interface{} {
	m.storage.mu.RLock()
	defer m.storage.mu.RUnlock()

	return map[string]interface{}{
		"active_locks": len(m.storage.locks),
		"instance_id":  m.instanceID,
	}
}
