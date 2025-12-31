package hotreload

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Reloader manages graceful configuration reloading.
// It coordinates the reload process to minimize downtime.
type Reloader struct {
	logger     *slog.Logger
	configPath string

	// Hooks
	onLoad      func(ctx context.Context, configPath string) error
	onValidate  func(ctx context.Context) error
	onPrepare   func(ctx context.Context) error
	onSwitch    func(ctx context.Context) error
	onRollback  func(ctx context.Context, err error)
	onComplete  func(ctx context.Context)

	// State
	mu          sync.RWMutex
	version     int
	lastSuccess time.Time
	lastError   error
}

// ReloaderConfig holds configuration for the reloader.
type ReloaderConfig struct {
	ConfigPath string
	Logger     *slog.Logger

	// OnLoad is called to load new configuration.
	OnLoad func(ctx context.Context, configPath string) error

	// OnValidate is called to validate the new configuration.
	OnValidate func(ctx context.Context) error

	// OnPrepare is called to prepare new resources (connectors, etc).
	OnPrepare func(ctx context.Context) error

	// OnSwitch is called to switch to the new configuration.
	OnSwitch func(ctx context.Context) error

	// OnRollback is called if the reload fails after OnPrepare.
	OnRollback func(ctx context.Context, err error)

	// OnComplete is called after a successful reload.
	OnComplete func(ctx context.Context)
}

// NewReloader creates a new reloader.
func NewReloader(config *ReloaderConfig) *Reloader {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &Reloader{
		logger:     config.Logger,
		configPath: config.ConfigPath,
		onLoad:     config.OnLoad,
		onValidate: config.OnValidate,
		onPrepare:  config.OnPrepare,
		onSwitch:   config.OnSwitch,
		onRollback: config.OnRollback,
		onComplete: config.OnComplete,
	}
}

// Reload performs a graceful configuration reload.
func (r *Reloader) Reload(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	start := time.Now()
	r.logger.Info("starting configuration reload", "version", r.version+1)

	// Step 1: Load new configuration
	if r.onLoad != nil {
		r.logger.Debug("loading new configuration")
		if err := r.onLoad(ctx, r.configPath); err != nil {
			r.lastError = fmt.Errorf("load failed: %w", err)
			return r.lastError
		}
	}

	// Step 2: Validate configuration
	if r.onValidate != nil {
		r.logger.Debug("validating configuration")
		if err := r.onValidate(ctx); err != nil {
			r.lastError = fmt.Errorf("validation failed: %w", err)
			return r.lastError
		}
	}

	// Step 3: Prepare new resources
	prepared := false
	if r.onPrepare != nil {
		r.logger.Debug("preparing new resources")
		if err := r.onPrepare(ctx); err != nil {
			r.lastError = fmt.Errorf("prepare failed: %w", err)
			return r.lastError
		}
		prepared = true
	}

	// Step 4: Switch to new configuration
	if r.onSwitch != nil {
		r.logger.Debug("switching to new configuration")
		if err := r.onSwitch(ctx); err != nil {
			r.lastError = fmt.Errorf("switch failed: %w", err)
			// Rollback if we prepared resources
			if prepared && r.onRollback != nil {
				r.logger.Warn("rolling back due to switch failure")
				r.onRollback(ctx, err)
			}
			return r.lastError
		}
	}

	// Success
	r.version++
	r.lastSuccess = time.Now()
	r.lastError = nil

	if r.onComplete != nil {
		r.onComplete(ctx)
	}

	r.logger.Info("configuration reload completed",
		"version", r.version,
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return nil
}

// Validate validates the configuration without applying it.
func (r *Reloader) Validate(ctx context.Context) error {
	r.logger.Debug("validating configuration (dry run)")

	// Load configuration
	if r.onLoad != nil {
		if err := r.onLoad(ctx, r.configPath); err != nil {
			return fmt.Errorf("load failed: %w", err)
		}
	}

	// Validate
	if r.onValidate != nil {
		if err := r.onValidate(ctx); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	r.logger.Info("configuration validation passed")
	return nil
}

// Version returns the current configuration version.
func (r *Reloader) Version() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// LastSuccess returns the time of the last successful reload.
func (r *Reloader) LastSuccess() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastSuccess
}

// LastError returns the last reload error.
func (r *Reloader) LastError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

// Stats returns reload statistics.
func (r *Reloader) Stats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := map[string]interface{}{
		"version":     r.version,
		"config_path": r.configPath,
	}

	if !r.lastSuccess.IsZero() {
		stats["last_success"] = r.lastSuccess
	}

	if r.lastError != nil {
		stats["last_error"] = r.lastError.Error()
	}

	return stats
}
