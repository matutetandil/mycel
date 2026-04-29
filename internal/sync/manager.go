package sync

import (
	"context"
	"fmt"
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
}

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

	// Ensure lock is released
	defer func() {
		_ = lock.Release(ctx, key)
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

	// Ensure permit is released
	defer func() {
		_ = sem.Release(ctx, key, permitID)
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
		return nil, &SequenceGuardSkippedError{
			Key:             key,
			StoredSequence:  stored,
			CurrentSequence: current,
			Policy:          ParseOnOlder(cfg.OnOlder),
		}
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
		// Log via the manager's logger if we had one; for now, silent.
		// The next message for the same key will read the unchanged stored
		// value and may pass through again — that's acceptable for an
		// idempotent destination.
		_ = writeErr
	}

	return result, nil
}

// ExecuteWithCoordinate handles coordination (signal/wait) for a function execution.
func (m *Manager) ExecuteWithCoordinate(ctx context.Context, cfg *FlowCoordinateConfig, signalKey, waitKey string, fn func() (interface{}, error)) (interface{}, error) {
	if cfg == nil {
		return fn()
	}

	coord, err := m.GetCoordinator(ctx, cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to get coordinator: %w", err)
	}

	// Handle signal after execution if configured
	if cfg.Signal != nil && signalKey != "" {
		result, err := fn()
		if err != nil {
			return result, err
		}

		// Parse TTL
		ttl := 5 * time.Minute
		if cfg.Signal.TTL != "" {
			if d, parseErr := time.ParseDuration(cfg.Signal.TTL); parseErr == nil {
				ttl = d
			}
		}

		if signalErr := coord.Signal(ctx, signalKey, ttl); signalErr != nil {
			// Log but don't fail the operation
			return result, nil
		}

		return result, nil
	}

	// Handle wait before execution if configured
	if cfg.Wait != nil && waitKey != "" {
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
