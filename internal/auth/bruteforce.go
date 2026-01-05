package auth

import (
	"context"
	"fmt"
	"time"
)

// BruteForceService handles brute force protection with progressive delays
type BruteForceService struct {
	config *BruteForceConfig
	store  BruteForceStore
}

// NewBruteForceService creates a new brute force service
func NewBruteForceService(config *BruteForceConfig, store BruteForceStore) *BruteForceService {
	if store == nil {
		store = NewMemoryBruteForceStore()
	}
	return &BruteForceService{
		config: config,
		store:  store,
	}
}

// CheckAccess checks if access is allowed and returns any delay to apply
func (s *BruteForceService) CheckAccess(ctx context.Context, key string) (allowed bool, delay time.Duration, lockoutRemaining time.Duration, err error) {
	if s.config == nil || !s.config.Enabled {
		return true, 0, 0, nil
	}

	// Check if locked out
	locked, lockUntil, err := s.store.IsLocked(ctx, key)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to check lockout: %w", err)
	}
	if locked {
		remaining := time.Until(lockUntil)
		if remaining < 0 {
			remaining = 0
		}
		return false, 0, remaining, nil
	}

	// Get progressive delay if enabled
	if s.config.ProgressiveDelay != nil && s.config.ProgressiveDelay.Enabled {
		delay, err = s.getProgressiveDelay(ctx, key)
		if err != nil {
			return false, 0, 0, fmt.Errorf("failed to get delay: %w", err)
		}
	}

	return true, delay, 0, nil
}

// RecordFailedAttempt records a failed login attempt
func (s *BruteForceService) RecordFailedAttempt(ctx context.Context, key string) (locked bool, err error) {
	if s.config == nil || !s.config.Enabled {
		return false, nil
	}

	window, _ := ParseDuration(s.config.Window)
	if window == 0 {
		window = 15 * time.Minute
	}

	// Increment attempt counter
	count, err := s.store.Increment(ctx, key, window)
	if err != nil {
		return false, fmt.Errorf("failed to increment attempts: %w", err)
	}

	// Update progressive delay if enabled
	if s.config.ProgressiveDelay != nil && s.config.ProgressiveDelay.Enabled {
		if err := s.updateProgressiveDelay(ctx, key, count); err != nil {
			return false, fmt.Errorf("failed to update delay: %w", err)
		}
	}

	// Check if we should lock out
	if count >= s.config.MaxAttempts {
		lockout, _ := ParseDuration(s.config.LockoutTime)
		if lockout == 0 {
			lockout = 15 * time.Minute
		}

		if err := s.store.Lock(ctx, key, lockout); err != nil {
			return false, fmt.Errorf("failed to set lockout: %w", err)
		}
		return true, nil
	}

	return false, nil
}

// RecordSuccess records a successful login (clears attempts)
func (s *BruteForceService) RecordSuccess(ctx context.Context, key string) error {
	if s.config == nil || !s.config.Enabled {
		return nil
	}

	return s.store.Reset(ctx, key)
}

// getProgressiveDelay calculates the current progressive delay
func (s *BruteForceService) getProgressiveDelay(ctx context.Context, key string) (time.Duration, error) {
	pd := s.config.ProgressiveDelay
	if pd == nil || !pd.Enabled {
		return 0, nil
	}

	// Check if we have a stored delay
	storedDelay, err := s.store.GetDelay(ctx, key)
	if err != nil {
		return 0, err
	}
	if storedDelay > 0 {
		return storedDelay, nil
	}

	return 0, nil
}

// updateProgressiveDelay updates the progressive delay after a failed attempt
func (s *BruteForceService) updateProgressiveDelay(ctx context.Context, key string, attemptCount int) error {
	pd := s.config.ProgressiveDelay
	if pd == nil || !pd.Enabled {
		return nil
	}

	// Calculate delay based on attempt count
	initial, _ := ParseDuration(pd.Initial)
	if initial == 0 {
		initial = time.Second
	}

	maxDelay, _ := ParseDuration(pd.Max)
	if maxDelay == 0 {
		maxDelay = 30 * time.Second
	}

	multiplier := pd.Multiplier
	if multiplier == 0 {
		multiplier = 2
	}

	// Calculate: initial * (multiplier ^ (attempts - 1))
	// But only apply delay after the first attempt
	if attemptCount <= 1 {
		return nil
	}

	delay := initial
	for i := 1; i < attemptCount; i++ {
		delay = time.Duration(float64(delay) * multiplier)
		if delay > maxDelay {
			delay = maxDelay
			break
		}
	}

	// Store the delay
	window, _ := ParseDuration(s.config.Window)
	if window == 0 {
		window = 15 * time.Minute
	}

	return s.store.SetDelay(ctx, key, delay, window)
}

// GetStats returns statistics for a key
func (s *BruteForceService) GetStats(ctx context.Context, key string) (*BruteForceStats, error) {
	if s.config == nil || !s.config.Enabled {
		return &BruteForceStats{}, nil
	}

	attempts, err := s.store.GetAttempts(ctx, key)
	if err != nil {
		return nil, err
	}

	locked, lockUntil, err := s.store.IsLocked(ctx, key)
	if err != nil {
		return nil, err
	}

	var lockoutRemaining time.Duration
	if locked {
		lockoutRemaining = time.Until(lockUntil)
		if lockoutRemaining < 0 {
			lockoutRemaining = 0
		}
	}

	var currentDelay time.Duration
	if s.config.ProgressiveDelay != nil && s.config.ProgressiveDelay.Enabled {
		currentDelay, _ = s.store.GetDelay(ctx, key)
	}

	return &BruteForceStats{
		Attempts:         attempts,
		MaxAttempts:      s.config.MaxAttempts,
		Locked:           locked,
		LockoutRemaining: lockoutRemaining,
		CurrentDelay:     currentDelay,
	}, nil
}

// BruteForceStats contains statistics for brute force tracking
type BruteForceStats struct {
	Attempts         int
	MaxAttempts      int
	Locked           bool
	LockoutRemaining time.Duration
	CurrentDelay     time.Duration
}

