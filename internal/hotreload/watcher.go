// Package hotreload provides configuration hot-reload functionality for Mycel.
// Like nginx, Mycel can reload configuration without restarting the service.
package hotreload

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Config holds hot reload configuration.
type Config struct {
	// Enabled enables hot reload functionality.
	Enabled bool

	// Paths are the directories to watch for changes.
	Paths []string

	// Extensions are file extensions to watch (default: .mycel).
	Extensions []string

	// Debounce is the debounce duration to prevent rapid reloads.
	Debounce time.Duration

	// ValidateOnly validates config without applying (useful for testing).
	ValidateOnly bool
}

// DefaultConfig returns a default hot reload configuration.
func DefaultConfig(configPath string) *Config {
	return &Config{
		Enabled:    true,
		Paths:      []string{configPath},
		Extensions: []string{".mycel"},
		Debounce:   500 * time.Millisecond,
	}
}

// ReloadFunc is called when a reload is triggered.
// It should return an error if the reload fails.
type ReloadFunc func(ctx context.Context) error

// ValidateFunc is called to validate configuration before reloading.
type ValidateFunc func(ctx context.Context) error

// Watcher watches for configuration changes and triggers reloads.
type Watcher struct {
	config     *Config
	logger     *slog.Logger
	watcher    *fsnotify.Watcher
	reload     ReloadFunc
	validate   ValidateFunc
	done       chan struct{}
	reloading  bool
	reloadLock sync.Mutex
	lastReload time.Time
	debounce   *time.Timer
	debounceMu sync.Mutex
}

// NewWatcher creates a new hot reload watcher.
func NewWatcher(config *Config, logger *slog.Logger, reload ReloadFunc, validate ValidateFunc) (*Watcher, error) {
	if config == nil {
		config = DefaultConfig(".")
	}

	if logger == nil {
		logger = slog.Default()
	}

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	w := &Watcher{
		config:   config,
		logger:   logger,
		watcher:  fsWatcher,
		reload:   reload,
		validate: validate,
		done:     make(chan struct{}),
	}

	return w, nil
}

// Start starts watching for file changes.
func (w *Watcher) Start(ctx context.Context) error {
	if !w.config.Enabled {
		w.logger.Info("hot reload disabled")
		return nil
	}

	// Add paths to watch
	for _, path := range w.config.Paths {
		if err := w.addPath(path); err != nil {
			return fmt.Errorf("failed to watch path %s: %w", path, err)
		}
	}

	w.logger.Info("hot reload enabled",
		"paths", w.config.Paths,
		"extensions", w.config.Extensions,
		"debounce", w.config.Debounce,
	)

	// Start watching
	go w.watch(ctx)

	return nil
}

// Stop stops the watcher.
func (w *Watcher) Stop() error {
	close(w.done)
	return w.watcher.Close()
}

// TriggerReload manually triggers a reload.
func (w *Watcher) TriggerReload(ctx context.Context) error {
	return w.doReload(ctx)
}

// IsReloading returns true if a reload is in progress.
func (w *Watcher) IsReloading() bool {
	w.reloadLock.Lock()
	defer w.reloadLock.Unlock()
	return w.reloading
}

// LastReload returns the time of the last successful reload.
func (w *Watcher) LastReload() time.Time {
	w.reloadLock.Lock()
	defer w.reloadLock.Unlock()
	return w.lastReload
}

// addPath recursively adds a path to watch.
func (w *Watcher) addPath(path string) error {
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Watch directories
		if info.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
				return filepath.SkipDir
			}

			if err := w.watcher.Add(p); err != nil {
				return fmt.Errorf("failed to add %s to watcher: %w", p, err)
			}
			w.logger.Debug("watching directory", "path", p)
		}

		return nil
	})
}

// watch runs the file watcher loop.
func (w *Watcher) watch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("watcher error", "error", err)
		}
	}
}

// handleEvent handles a file system event.
func (w *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	// Only handle write and create events
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
		return
	}

	// Check if this is a file we care about
	if !w.shouldWatch(event.Name) {
		return
	}

	w.logger.Debug("file changed",
		"file", event.Name,
		"op", event.Op.String(),
	)

	// Debounce reloads
	w.scheduleReload(ctx)
}

// shouldWatch returns true if the file should trigger a reload.
func (w *Watcher) shouldWatch(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range w.config.Extensions {
		if ext == e {
			return true
		}
	}
	return false
}

// scheduleReload schedules a reload with debouncing.
func (w *Watcher) scheduleReload(ctx context.Context) {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()

	// Cancel existing timer
	if w.debounce != nil {
		w.debounce.Stop()
	}

	// Schedule new reload
	w.debounce = time.AfterFunc(w.config.Debounce, func() {
		if err := w.doReload(ctx); err != nil {
			w.logger.Error("reload failed", "error", err)
		}
	})
}

// doReload performs the actual reload.
func (w *Watcher) doReload(ctx context.Context) error {
	w.reloadLock.Lock()
	if w.reloading {
		w.reloadLock.Unlock()
		w.logger.Warn("reload already in progress, skipping")
		return nil
	}
	w.reloading = true
	w.reloadLock.Unlock()

	defer func() {
		w.reloadLock.Lock()
		w.reloading = false
		w.reloadLock.Unlock()
	}()

	w.logger.Info("configuration reload triggered")
	start := time.Now()

	// Validate first
	if w.validate != nil {
		w.logger.Debug("validating configuration")
		if err := w.validate(ctx); err != nil {
			w.logger.Error("configuration validation failed", "error", err)
			return fmt.Errorf("validation failed: %w", err)
		}
		w.logger.Debug("configuration validated successfully")
	}

	// Stop here if validate-only mode
	if w.config.ValidateOnly {
		w.logger.Info("validate-only mode, not applying changes")
		return nil
	}

	// Perform reload
	if w.reload != nil {
		if err := w.reload(ctx); err != nil {
			w.logger.Error("reload failed", "error", err)
			return fmt.Errorf("reload failed: %w", err)
		}
	}

	w.reloadLock.Lock()
	w.lastReload = time.Now()
	w.reloadLock.Unlock()

	w.logger.Info("configuration reloaded successfully",
		"duration", time.Since(start).Round(time.Millisecond),
	)

	return nil
}
