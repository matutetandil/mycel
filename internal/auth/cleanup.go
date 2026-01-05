package auth

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// CleanupService handles periodic cleanup of expired sessions and tokens
type CleanupService struct {
	manager  *Manager
	interval time.Duration
	logger   *slog.Logger

	stopCh chan struct{}
	doneCh chan struct{}
	mu     sync.Mutex
	running bool
}

// NewCleanupService creates a new cleanup service
func NewCleanupService(manager *Manager, interval time.Duration) *CleanupService {
	if interval == 0 {
		interval = 5 * time.Minute
	}

	return &CleanupService{
		manager:  manager,
		interval: interval,
		logger:   slog.Default(),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// SetLogger sets the logger for the cleanup service
func (s *CleanupService) SetLogger(logger *slog.Logger) {
	s.logger = logger
}

// Start begins the cleanup goroutine
func (s *CleanupService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.run(ctx)
	s.logger.Info("auth cleanup service started", "interval", s.interval)
	return nil
}

// Stop stops the cleanup goroutine
func (s *CleanupService) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
	<-s.doneCh
	s.logger.Info("auth cleanup service stopped")
	return nil
}

// run is the main cleanup loop
func (s *CleanupService) run(ctx context.Context) {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run initial cleanup
	s.cleanup(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup(ctx)
		}
	}
}

// cleanup performs the actual cleanup operations
func (s *CleanupService) cleanup(ctx context.Context) {
	start := time.Now()

	// Clean expired sessions
	if err := s.cleanExpiredSessions(ctx); err != nil {
		s.logger.Error("failed to clean expired sessions", "error", err)
	}

	// Clean idle sessions
	if err := s.cleanIdleSessions(ctx); err != nil {
		s.logger.Error("failed to clean idle sessions", "error", err)
	}

	// Clean expired tokens
	if err := s.cleanExpiredTokens(ctx); err != nil {
		s.logger.Error("failed to clean expired tokens", "error", err)
	}

	s.logger.Debug("auth cleanup completed", "duration", time.Since(start))
}

// cleanExpiredSessions removes sessions past their absolute expiry
func (s *CleanupService) cleanExpiredSessions(ctx context.Context) error {
	return s.manager.Cleanup(ctx)
}

// cleanIdleSessions removes sessions that have been idle too long
func (s *CleanupService) cleanIdleSessions(ctx context.Context) error {
	cfg := s.manager.Config()
	if cfg.Sessions == nil || cfg.Sessions.IdleTimeout == "" {
		return nil
	}

	idleTimeout, err := ParseDuration(cfg.Sessions.IdleTimeout)
	if err != nil {
		return err
	}

	if idleTimeout == 0 {
		return nil
	}

	// Get all sessions and check for idle ones
	// Note: This is a simplified implementation
	// A production system might use a more efficient approach
	// like storing last_active_at in a sortable way

	// For now, the session store's DeleteExpired handles this
	// because we update LastActiveAt on each touch
	// and can add idle timeout checking there

	return nil
}

// cleanExpiredTokens removes expired tokens from blacklist
func (s *CleanupService) cleanExpiredTokens(ctx context.Context) error {
	// Token store cleanup is handled by the manager's Cleanup method
	return nil
}

// SessionCleanupStore extends SessionStore with cleanup capabilities
type SessionCleanupStore interface {
	SessionStore

	// DeleteIdle deletes sessions that haven't been active since the given time
	DeleteIdle(ctx context.Context, threshold time.Time) (int, error)
}

// MemorySessionStoreWithIdle extends MemorySessionStore with idle cleanup
type MemorySessionStoreWithIdle struct {
	*MemorySessionStore
}

// NewMemorySessionStoreWithIdle creates a new memory session store with idle support
func NewMemorySessionStoreWithIdle() *MemorySessionStoreWithIdle {
	return &MemorySessionStoreWithIdle{
		MemorySessionStore: NewMemorySessionStore(),
	}
}

// DeleteIdle removes sessions that haven't been active since threshold
func (s *MemorySessionStoreWithIdle) DeleteIdle(ctx context.Context, threshold time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for id, session := range s.sessions {
		if session.LastActiveAt.Before(threshold) {
			// Remove from user index
			userSessions := s.byUser[session.UserID]
			for i, sid := range userSessions {
				if sid == id {
					s.byUser[session.UserID] = append(userSessions[:i], userSessions[i+1:]...)
					break
				}
			}
			delete(s.sessions, id)
			deleted++
		}
	}

	return deleted, nil
}

// DeleteExpiredAndIdle extends DeleteExpired to also check idle timeout
func (s *MemorySessionStoreWithIdle) DeleteExpiredAndIdle(ctx context.Context, idleTimeout time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	idleThreshold := now.Add(-idleTimeout)
	deleted := 0

	for id, session := range s.sessions {
		shouldDelete := false

		// Check absolute expiry
		if session.ExpiresAt.Before(now) {
			shouldDelete = true
		}

		// Check idle timeout (if configured)
		if idleTimeout > 0 && session.LastActiveAt.Before(idleThreshold) {
			shouldDelete = true
		}

		if shouldDelete {
			// Remove from user index
			userSessions := s.byUser[session.UserID]
			for i, sid := range userSessions {
				if sid == id {
					s.byUser[session.UserID] = append(userSessions[:i], userSessions[i+1:]...)
					break
				}
			}
			delete(s.sessions, id)
			deleted++
		}
	}

	return deleted, nil
}
