package auth

import (
	"context"
	"sync"
)

// PasswordHistoryStore keeps the hashes of passwords somebody has used before,
// so that `password { history = N }` can refuse a password they are returning
// to.
//
// Two implementations of this have existed since the auth system was written,
// one for PostgreSQL and one for MySQL, each with its own tests. Nothing ever
// built them: the manager had no field to hold one, so a service configuring a
// history let people cycle between two passwords indefinitely, which is the
// one thing the setting exists to stop.
type PasswordHistoryStore interface {
	// AddPasswordHash records a hash the account has used.
	AddPasswordHash(ctx context.Context, userID, hash string) error

	// GetRecentHashes returns the most recently used hashes, newest first.
	GetRecentHashes(ctx context.Context, userID string, count int) ([]string, error)

	// CleanOldHashes drops everything but the most recent keepCount, so the
	// record does not grow for the life of the account.
	CleanOldHashes(ctx context.Context, userID string, keepCount int) error
}

// MemoryPasswordHistoryStore keeps the history in the process.
//
// It is the default, matching the memory user store: a service that has not
// configured a database still gets the behaviour it asked for, and loses it on
// a restart along with the accounts themselves.
type MemoryPasswordHistoryStore struct {
	mu      sync.RWMutex
	hashes  map[string][]string // newest first
	maxKeep int
}

// NewMemoryPasswordHistoryStore creates an in-process password history.
func NewMemoryPasswordHistoryStore() *MemoryPasswordHistoryStore {
	return &MemoryPasswordHistoryStore{hashes: make(map[string][]string)}
}

func (s *MemoryPasswordHistoryStore) AddPasswordHash(ctx context.Context, userID, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hashes[userID] = append([]string{hash}, s.hashes[userID]...)
	return nil
}

func (s *MemoryPasswordHistoryStore) GetRecentHashes(ctx context.Context, userID string, count int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	held := s.hashes[userID]
	if count > 0 && len(held) > count {
		held = held[:count]
	}
	return append([]string(nil), held...), nil
}

func (s *MemoryPasswordHistoryStore) CleanOldHashes(ctx context.Context, userID string, keepCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if keepCount <= 0 {
		delete(s.hashes, userID)
		return nil
	}
	if held := s.hashes[userID]; len(held) > keepCount {
		s.hashes[userID] = held[:keepCount]
	}
	return nil
}
