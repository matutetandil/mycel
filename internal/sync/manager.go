package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	gosync "sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// SyncStorageConfig defines inline storage configuration for sync primitives.
// This is a mirror of flow.SyncStorageConfig to avoid circular imports.
type SyncStorageConfig struct {
	Driver   string
	URL      string
	Host     string
	Port     int
	Password string
	DB       int
}

// redisURL returns the effective Redis connection URL.
func (c *SyncStorageConfig) redisURL() string {
	if c.URL != "" {
		return c.URL
	}
	host := c.Host
	if host == "" {
		host = "localhost"
	}
	port := c.Port
	if port == 0 {
		port = 6379
	}
	if c.Password != "" {
		return fmt.Sprintf("redis://:%s@%s:%d/%d", c.Password, host, port, c.DB)
	}
	return fmt.Sprintf("redis://%s:%d/%d", host, port, c.DB)
}

// cacheKey returns a string key for client caching.
func (c *SyncStorageConfig) cacheKey() string {
	return c.redisURL()
}

// Manager manages sync primitives (Lock, Semaphore, Coordinator, SequenceGuard).
// It creates Redis clients from inline storage configs.
type Manager struct {
	// Shared memory storage for memory-backed locks
	memoryLockStorage *MemoryLockStorage

	// Cached memory instances (to avoid creating multiple instances)
	memorySemaphore     *MemorySemaphore
	memoryCoordinator   *MemoryCoordinator
	memorySequenceGuard *MemorySequenceGuard

	// Cached Redis clients, keyed by resolved address
	redisClients map[string]*redis.Client

	mu gosync.RWMutex
}

// NewManager creates a new sync manager.
func NewManager() *Manager {
	return &Manager{
		memoryLockStorage:   NewMemoryLockStorage(),
		memorySemaphore:     NewMemorySemaphore(100), // Default max permits
		memoryCoordinator:   NewMemoryCoordinator(time.Second),
		memorySequenceGuard: NewMemorySequenceGuard(time.Minute),
		redisClients:        make(map[string]*redis.Client),
	}
}

// getOrCreateRedisClient returns a cached Redis client or creates a new one.
func (m *Manager) getOrCreateRedisClient(cfg *SyncStorageConfig) (*redis.Client, error) {
	key := cfg.cacheKey()

	m.mu.RLock()
	if client, ok := m.redisClients[key]; ok {
		m.mu.RUnlock()
		return client, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if client, ok := m.redisClients[key]; ok {
		return client, nil
	}

	url := cfg.redisURL()
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL %q: %w", url, err)
	}
	client := redis.NewClient(opts)
	m.redisClients[key] = client
	return client, nil
}

// GetLock returns a Lock implementation based on the storage config.
func (m *Manager) GetLock(ctx context.Context, cfg *SyncStorageConfig) (Lock, error) {
	if cfg == nil || cfg.Driver == "" || cfg.Driver == "memory" {
		return NewMemoryLockWithStorage(m.memoryLockStorage), nil
	}
	if cfg.Driver == "redis" {
		client, err := m.getOrCreateRedisClient(cfg)
		if err != nil {
			return nil, err
		}
		return NewRedisLockFromClient(client, "mycel:lock:"), nil
	}
	return nil, fmt.Errorf("unsupported sync storage driver: %s", cfg.Driver)
}

// GetSemaphore returns a Semaphore implementation based on the storage config.
func (m *Manager) GetSemaphore(ctx context.Context, cfg *SyncStorageConfig, maxPermits int) (Semaphore, error) {
	if cfg == nil || cfg.Driver == "" || cfg.Driver == "memory" {
		return m.memorySemaphore, nil
	}
	if cfg.Driver == "redis" {
		client, err := m.getOrCreateRedisClient(cfg)
		if err != nil {
			return nil, err
		}
		return NewRedisSemaphoreFromClient(client, "mycel:sem:", maxPermits), nil
	}
	return nil, fmt.Errorf("unsupported sync storage driver: %s", cfg.Driver)
}

// GetCoordinator returns a Coordinator implementation based on the storage config.
func (m *Manager) GetCoordinator(ctx context.Context, cfg *SyncStorageConfig) (Coordinator, error) {
	if cfg == nil || cfg.Driver == "" || cfg.Driver == "memory" {
		return m.memoryCoordinator, nil
	}
	if cfg.Driver == "redis" {
		client, err := m.getOrCreateRedisClient(cfg)
		if err != nil {
			return nil, err
		}
		return NewRedisCoordinatorFromClient(client, "mycel:coord:"), nil
	}
	return nil, fmt.Errorf("unsupported sync storage driver: %s", cfg.Driver)
}

// GetSequenceGuard returns a SequenceGuard implementation based on the
// storage config. Memory driver shares one in-process guard across all
// flows; Redis driver shares the underlying client through the manager's
// connection cache.
func (m *Manager) GetSequenceGuard(ctx context.Context, cfg *SyncStorageConfig) (SequenceGuard, error) {
	if cfg == nil || cfg.Driver == "" || cfg.Driver == "memory" {
		return m.memorySequenceGuard, nil
	}
	if cfg.Driver == "redis" {
		client, err := m.getOrCreateRedisClient(cfg)
		if err != nil {
			return nil, err
		}
		return NewRedisSequenceGuardFromClient(client, "mycel:seqguard:"), nil
	}
	return nil, fmt.Errorf("unsupported sync storage driver: %s", cfg.Driver)
}

// Close closes all sync primitive resources.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.memoryLockStorage != nil {
		if err := m.memoryLockStorage.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if m.memorySemaphore != nil {
		if err := m.memorySemaphore.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if m.memoryCoordinator != nil {
		if err := m.memoryCoordinator.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if m.memorySequenceGuard != nil {
		if err := m.memorySequenceGuard.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	for _, client := range m.redisClients {
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	m.redisClients = make(map[string]*redis.Client)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing sync manager: %v", errs)
	}
	return nil
}

// FlowLockConfig is the flow-level lock configuration.
type FlowLockConfig struct {
	Storage *SyncStorageConfig
	Key     string
	Timeout string
	Wait    bool
	Retry   string
}

// FlowSemaphoreConfig is the flow-level semaphore configuration.
type FlowSemaphoreConfig struct {
	Storage    *SyncStorageConfig
	Key        string
	MaxPermits int
	Timeout    string
	Lease      string
}

// FlowCoordinateConfig is the flow-level coordinate configuration.
type FlowCoordinateConfig struct {
	Storage            *SyncStorageConfig
	Wait               *FlowWaitConfig
	Signal             *FlowSignalConfig
	Timeout            string
	OnTimeout          string
	MaxRetries         int
	MaxConcurrentWaits int

	// Preflight, if non-nil, runs before the wait. Returns true to skip
	// the wait (the awaited resource already exists). Returns an error
	// to abort the flow when if_exists="fail" semantics decide so. The
	// caller (the runtime) builds the closure with access to the
	// connector registry so the sync package stays decoupled from
	// connector implementations.
	Preflight FlowPreflightFn
}

// FlowPreflightFn is the closure shape for coordinate.preflight execution.
// Returns (skipWait, err): skipWait=true means the resource the wait was
// going to wait for already exists; err is non-nil when if_exists="fail"
// is configured and the query did find a row, or when the query itself
// errors (logged at WARN by the runtime — the manager still falls through
// to the wait when Preflight reports an error, matching docs).
type FlowPreflightFn func(ctx context.Context) (skipWait bool, err error)

// FlowWaitConfig is the flow-level wait configuration.
type FlowWaitConfig struct {
	When string
	For  string
}

// FlowSignalConfig is the flow-level signal configuration.
type FlowSignalConfig struct {
	When string
	Emit string
	TTL  string
}

// ExecuteWithLock executes a function while holding a lock.
func (m *Manager) ExecuteWithLock(ctx context.Context, cfg *FlowLockConfig, key string, fn func() (interface{}, error)) (interface{}, error) {
	if cfg == nil {
		return fn()
	}

	lock, err := m.GetLock(ctx, cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to get lock: %w", err)
	}

	// Parse timeout
	timeout := 30 * time.Second
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = d
		}
	}

	// Parse retry interval
	retry := 100 * time.Millisecond
	if cfg.Retry != "" {
		if d, err := time.ParseDuration(cfg.Retry); err == nil {
			retry = d
		}
	}

	// Create lock config for acquire
	lockCfg := &LockConfig{
		Key:     key,
		Timeout: timeout,
		Wait:    cfg.Wait,
		Retry:   retry,
	}

	// Acquire lock
	acquired, err := AcquireWithRetry(ctx, lock, key, lockCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !acquired {
		return nil, ErrLockTimeout
	}

	slog.Info("lock acquired", "key", key, "timeout", timeout)

	// Heartbeat: extend the lock TTL while fn() runs. Without this, a
	// flow that takes longer than `timeout` lets the lock auto-expire
	// mid-execution; another worker can then acquire the same key and
	// the supposed mutual-exclusion guarantee silently breaks. The TTL
	// stays as a deadman switch (if this process crashes, no more
	// heartbeats → key expires → recovery), but normal long-running
	// flows are no longer at risk.
	// Heartbeat at timeout/3 — three attempts to extend before the TTL
	// elapses, so a single transient Redis blip doesn't drop the lock.
	// Clamped to ≥50ms to avoid hammering Redis on absurdly short
	// timeouts (mostly tests); production values are typically seconds.
	hbInterval := timeout / 3
	if hbInterval < 50*time.Millisecond {
		hbInterval = 50 * time.Millisecond
	}
	hbCtx, hbCancel := context.WithCancel(context.Background())
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				extendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				ok, err := lock.Extend(extendCtx, key, timeout)
				cancel()
				if err != nil {
					// Transient (e.g. Redis blip) — the next tick gets
					// another shot before the original TTL elapses
					// (we tick at timeout/3).
					slog.Warn("lock heartbeat extend failed, will retry",
						"key", key,
						"error", err)
					continue
				}
				if !ok {
					// Lost ownership: the TTL expired between ticks
					// (slow goroutine scheduling) or some operator
					// manually cleared the key. Stop heartbeating; the
					// release will also fail and surface the loss.
					slog.Error("lock lost during execution — TTL expired or another worker took it",
						"key", key,
						"hint", "increase lock.timeout if your flow takes longer than the configured timeout / 3")
					return
				}
			}
		}
	}()

	// Ensure lock is released even when the parent context is already
	// cancelled. Without a detached context, a coordinate-wait timeout
	// cascades into the defer here and Redis never sees the DEL — the
	// lock then sits at its TTL while every queued worker times out.
	defer func() {
		hbCancel()
		<-hbDone // wait for the heartbeat goroutine to exit before release
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx, key); err != nil {
			slog.Warn("lock release failed", "key", key, "error", err)
			return
		}
		slog.Info("lock released", "key", key)
	}()

	// Execute the function
	return fn()
}

// ExecuteWithSemaphore executes a function while holding a semaphore permit.
func (m *Manager) ExecuteWithSemaphore(ctx context.Context, cfg *FlowSemaphoreConfig, key string, fn func() (interface{}, error)) (interface{}, error) {
	if cfg == nil {
		return fn()
	}

	maxPermits := cfg.MaxPermits
	if maxPermits == 0 {
		maxPermits = 10
	}

	sem, err := m.GetSemaphore(ctx, cfg.Storage, maxPermits)
	if err != nil {
		return nil, fmt.Errorf("failed to get semaphore: %w", err)
	}

	// Parse timeout
	timeout := 30 * time.Second
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = d
		}
	}

	// Parse lease
	lease := 60 * time.Second
	if cfg.Lease != "" {
		if d, err := time.ParseDuration(cfg.Lease); err == nil {
			lease = d
		}
	}

	// Create semaphore config for acquire
	semCfg := &SemaphoreConfig{
		Key:        key,
		MaxPermits: maxPermits,
		Timeout:    timeout,
		Lease:      lease,
	}

	// Acquire permit
	permitID, err := AcquireSemaphoreWithRetry(ctx, sem, key, semCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore permit: %w", err)
	}

	// Ensure permit is released even when parent ctx is cancelled. Same
	// rationale as ExecuteWithLock — without a detached context, releases
	// silently no-op and the semaphore leaks permits.
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sem.Release(releaseCtx, key, permitID)
	}()

	// Execute the function
	return fn()
}

// ExecuteWithSequenceGuard wraps a function with monotonic sequence-number
// dedup. Reads the stored sequence for the resolved key; if current is not
// strictly greater than stored, returns a *SequenceGuardSkippedError without
// calling fn. Otherwise calls fn, and on success bumps the stored sequence
// to current with the configured TTL. Write-back failures are logged but do
// not propagate — the destination side effect already happened.
//
// Atomicity is the caller's responsibility: wrap the same key in an outer
// Lock so the read-decide-write pattern is safe across concurrent workers.
func (m *Manager) ExecuteWithSequenceGuard(ctx context.Context, cfg *FlowSequenceGuardConfig, key string, current int64, fn func() (interface{}, error)) (interface{}, error) {
	if cfg == nil {
		return fn()
	}

	guard, err := m.GetSequenceGuard(ctx, cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to get sequence guard: %w", err)
	}

	stored, exists, err := guard.Read(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("sequence guard read: %w", err)
	}

	if exists && current <= stored {
		// Loud INFO log so the operator can see WHY the flow short-
		// circuited. Without it, the only visible signal is a
		// suspiciously fast "request" log line and the rest of the
		// flow body silently never running — easy to mistake for a bug
		// in an upstream block (e.g. coordinate.preflight skipping
		// "too much").
		slog.Info("sequence guard skipped (current <= stored)",
			"key", key,
			"stored", stored,
			"current", current,
			"policy", string(ParseOnOlder(cfg.OnOlder)),
			"action", "skip_flow")
		return nil, &SequenceGuardSkippedError{
			Key:             key,
			StoredSequence:  stored,
			CurrentSequence: current,
			Policy:          ParseOnOlder(cfg.OnOlder),
		}
	}

	if exists {
		slog.Info("sequence guard passed",
			"key", key,
			"stored", stored,
			"current", current,
			"action", "proceed")
	} else {
		slog.Info("sequence guard initialized",
			"key", key,
			"current", current,
			"action", "proceed")
	}

	result, err := fn()
	if err != nil {
		// Don't bump the sequence on failure — the next retry should be
		// able to process this same message again.
		return result, err
	}

	// Bump the stored sequence. Failures here are logged-but-ignored: the
	// real side effect (Magento POST, etc.) already happened.
	var ttl time.Duration
	if cfg.TTL != "" {
		if d, parseErr := time.ParseDuration(cfg.TTL); parseErr == nil {
			ttl = d
		}
	}
	if writeErr := guard.Write(ctx, key, current, ttl); writeErr != nil {
		slog.Warn("sequence guard write-back failed (destination side effect already happened)",
			"key", key,
			"current", current,
			"error", writeErr)
	}

	return result, nil
}

// SignalKeyBuilder resolves coordinate.signal.emit's CEL expression against
// the flow result post-success. Returns the resolved key plus a bool that
// is false when the expression evaluates to empty / errors out — in that
// case the runtime should skip emitting rather than write a corrupted key.
type SignalKeyBuilder func(result interface{}) (string, bool)

// ExecuteWithCoordinate handles coordination (signal/wait) for a function
// execution. signalKeyFn is invoked AFTER fn() returns, with the flow
// result available so its CEL expression can reference output.* bindings.
// waitKey is evaluated up-front because the wait runs before fn.
func (m *Manager) ExecuteWithCoordinate(ctx context.Context, cfg *FlowCoordinateConfig, signalKeyFn SignalKeyBuilder, waitKey string, fn func() (interface{}, error)) (interface{}, error) {
	if cfg == nil {
		return fn()
	}

	coord, err := m.GetCoordinator(ctx, cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to get coordinator: %w", err)
	}

	// Handle signal after execution if configured
	if cfg.Signal != nil && signalKeyFn != nil {
		result, err := fn()
		if err != nil {
			return result, err
		}

		signalKey, ok := signalKeyFn(result)
		if !ok || signalKey == "" {
			slog.Warn("coordinate.signal.emit evaluated to empty key, skipping emit",
				"emit_expr", cfg.Signal.Emit)
			return result, nil
		}

		// Parse TTL
		ttl := 5 * time.Minute
		if cfg.Signal.TTL != "" {
			if d, parseErr := time.ParseDuration(cfg.Signal.TTL); parseErr == nil {
				ttl = d
			}
		}

		// Use a detached context so the signal fires even if the parent
		// context is being torn down (rare but happens at shutdown).
		signalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if signalErr := coord.Signal(signalCtx, signalKey, ttl); signalErr != nil {
			slog.Warn("coordinate signal emit failed", "key", signalKey, "error", signalErr)
			return result, nil
		}

		slog.Info("coordinate signal emitted", "key", signalKey, "ttl", ttl)
		return result, nil
	}

	// Handle wait before execution if configured
	if cfg.Wait != nil && waitKey != "" {
		// Run preflight first if configured. A successful preflight
		// (skipWait=true) bypasses the wait entirely — the resource the
		// wait was going to wait for already exists, no point blocking.
		// Errors from the preflight closure fall through to the wait
		// (best-effort gate; we don't want a transient DB blip to drop
		// the message).
		if cfg.Preflight != nil {
			skip, pfErr := cfg.Preflight(ctx)
			switch {
			case pfErr != nil && errors.Is(pfErr, ErrPreflightCheckFailed):
				// Explicit policy reject (if_exists="fail" matched a
				// row). Abort the flow — caller surfaces this through
				// the on_error path.
				return nil, pfErr
			case pfErr != nil:
				// Transient error (DB blip, params eval failure). Best
				// effort: fall through to the wait so a single bad
				// check doesn't drop the message.
				slog.Warn("coordinate preflight error, falling through to wait",
					"error", pfErr)
			case skip:
				slog.Info("coordinate preflight passed, skipping wait",
					"key", waitKey,
					"action", "skip_wait")
				return fn()
			default:
				slog.Info("coordinate preflight rejected, entering wait",
					"key", waitKey,
					"action", "enter_wait")
			}
		}

		// Parse timeout
		timeout := 60 * time.Second
		if cfg.Timeout != "" {
			if d, parseErr := time.ParseDuration(cfg.Timeout); parseErr == nil {
				timeout = d
			}
		}

		received, err := coord.Wait(ctx, waitKey, timeout)
		if err != nil {
			return nil, fmt.Errorf("coordinate wait failed: %w", err)
		}

		if !received {
			onTimeout := ParseOnTimeoutAction(cfg.OnTimeout)
			switch onTimeout {
			case OnTimeoutFail:
				return nil, ErrCoordinateTimeout
			case OnTimeoutSkip:
				return nil, ErrCoordinateSkip
			case OnTimeoutRetry:
				return nil, ErrCoordinateRetry
			case OnTimeoutAck:
				// Caller (the runtime) translates this sentinel into a
				// FilteredResultWithPolicy{Policy: "ack"} so the MQ consumer
				// acks the broker delivery cleanly and the rest of the flow
				// (transform / to / aspects) is skipped.
				return nil, ErrCoordinateAck
			case OnTimeoutPass:
				// Continue with execution
			default:
				return nil, ErrCoordinateTimeout
			}
		}
	}

	return fn()
}

// Stats returns statistics about sync primitives.
func (m *Manager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})

	if m.memoryLockStorage != nil {
		m.memoryLockStorage.mu.RLock()
		stats["locks"] = len(m.memoryLockStorage.locks)
		m.memoryLockStorage.mu.RUnlock()
	}

	if m.memorySemaphore != nil {
		stats["semaphores"] = m.memorySemaphore.Stats()
	}

	if m.memoryCoordinator != nil {
		stats["coordinator"] = m.memoryCoordinator.Stats()
	}

	return stats
}
