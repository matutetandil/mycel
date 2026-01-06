package sync

import (
	"context"
	"fmt"
	gosync "sync"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/cache"
	"github.com/matutetandil/mycel/internal/connector/cache/redis"
)

// Manager manages sync primitives (Lock, Semaphore, Coordinator).
// It creates instances based on connector configuration.
type Manager struct {
	connectors *connector.Registry

	// Shared memory storage for memory-backed locks
	memoryLockStorage *MemoryLockStorage

	// Cached memory instances (to avoid creating multiple instances)
	memorySemaphore   *MemorySemaphore
	memoryCoordinator *MemoryCoordinator

	mu gosync.RWMutex
}

// NewManager creates a new sync manager.
func NewManager(connectors *connector.Registry) *Manager {
	return &Manager{
		connectors:        connectors,
		memoryLockStorage: NewMemoryLockStorage(),
		memorySemaphore:   NewMemorySemaphore(100), // Default max permits
		memoryCoordinator: NewMemoryCoordinator(time.Second),
	}
}

// GetLock returns a Lock implementation based on the storage connector.
func (m *Manager) GetLock(ctx context.Context, storageName string) (Lock, error) {
	if storageName == "" || storageName == "memory" {
		return NewMemoryLockWithStorage(m.memoryLockStorage), nil
	}

	conn, err := m.connectors.Get(storageName)
	if err != nil {
		return nil, fmt.Errorf("storage connector not found: %s", storageName)
	}

	// Check if it's a Redis cache connector
	if cacheConn := cache.GetCache(conn); cacheConn != nil {
		if redisConn, ok := cacheConn.(*redis.Connector); ok {
			if client := redisConn.Client(); client != nil {
				return NewRedisLockFromClient(client, "mycel:lock:"), nil
			}
		}
	}

	// Default to memory lock
	return NewMemoryLockWithStorage(m.memoryLockStorage), nil
}

// GetSemaphore returns a Semaphore implementation based on the storage connector.
func (m *Manager) GetSemaphore(ctx context.Context, storageName string, maxPermits int) (Semaphore, error) {
	if storageName == "" || storageName == "memory" {
		return m.memorySemaphore, nil
	}

	conn, err := m.connectors.Get(storageName)
	if err != nil {
		return nil, fmt.Errorf("storage connector not found: %s", storageName)
	}

	// Check if it's a Redis cache connector
	if cacheConn := cache.GetCache(conn); cacheConn != nil {
		if redisConn, ok := cacheConn.(*redis.Connector); ok {
			if client := redisConn.Client(); client != nil {
				return NewRedisSemaphoreFromClient(client, "mycel:sem:", maxPermits), nil
			}
		}
	}

	// Default to memory semaphore
	return m.memorySemaphore, nil
}

// GetCoordinator returns a Coordinator implementation based on the storage connector.
func (m *Manager) GetCoordinator(ctx context.Context, storageName string) (Coordinator, error) {
	if storageName == "" || storageName == "memory" {
		return m.memoryCoordinator, nil
	}

	conn, err := m.connectors.Get(storageName)
	if err != nil {
		return nil, fmt.Errorf("storage connector not found: %s", storageName)
	}

	// Check if it's a Redis cache connector
	if cacheConn := cache.GetCache(conn); cacheConn != nil {
		if redisConn, ok := cacheConn.(*redis.Connector); ok {
			if client := redisConn.Client(); client != nil {
				return NewRedisCoordinatorFromClient(client, "mycel:coord:"), nil
			}
		}
	}

	// Default to memory coordinator
	return m.memoryCoordinator, nil
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

	if len(errs) > 0 {
		return fmt.Errorf("errors closing sync manager: %v", errs)
	}
	return nil
}

// FlowLockConfig is the flow-level lock configuration.
type FlowLockConfig struct {
	Storage string
	Key     string
	Timeout string
	Wait    bool
	Retry   string
}

// FlowSemaphoreConfig is the flow-level semaphore configuration.
type FlowSemaphoreConfig struct {
	Storage    string
	Key        string
	MaxPermits int
	Timeout    string
	Lease      string
}

// FlowCoordinateConfig is the flow-level coordinate configuration.
type FlowCoordinateConfig struct {
	Storage            string
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
		Storage: cfg.Storage,
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
		Storage:    cfg.Storage,
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
