// Package circuitbreaker provides circuit breaker functionality for Mycel services.
package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed means the circuit is closed (normal operation).
	StateClosed State = iota
	// StateOpen means the circuit is open (failing fast).
	StateOpen
	// StateHalfOpen means the circuit is testing if service recovered.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Common errors.
var (
	ErrCircuitOpen   = errors.New("circuit breaker is open")
	ErrTooManyErrors = errors.New("too many errors")
)

// Config holds circuit breaker configuration.
type Config struct {
	// Name identifies this circuit breaker.
	Name string

	// FailureThreshold is the number of failures before opening the circuit.
	FailureThreshold int

	// SuccessThreshold is the number of successes needed to close the circuit from half-open.
	SuccessThreshold int

	// Timeout is how long the circuit stays open before transitioning to half-open.
	Timeout time.Duration

	// MaxConcurrent limits concurrent requests (0 = unlimited).
	MaxConcurrent int

	// OnStateChange is called when the state changes.
	OnStateChange func(name string, from, to State)
}

// DefaultConfig returns a default configuration.
func DefaultConfig(name string) *Config {
	return &Config{
		Name:             name,
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxConcurrent:    0,
	}
}

// Breaker implements the circuit breaker pattern.
type Breaker struct {
	config *Config

	mu           sync.RWMutex
	state        State
	failures     int
	successes    int
	lastFailure  time.Time
	concurrent   int
	stateChanged time.Time
}

// New creates a new circuit breaker.
func New(config *Config) *Breaker {
	if config == nil {
		config = DefaultConfig("default")
	}
	return &Breaker{
		config:       config,
		state:        StateClosed,
		stateChanged: time.Now(),
	}
}

// Execute runs a function through the circuit breaker.
func (b *Breaker) Execute(ctx context.Context, fn func() error) error {
	if err := b.beforeRequest(); err != nil {
		return err
	}

	err := fn()
	b.afterRequest(err)
	return err
}

// ExecuteWithResult runs a function that returns a result through the circuit breaker.
func (b *Breaker) ExecuteWithResult(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	if err := b.beforeRequest(); err != nil {
		return nil, err
	}

	result, err := fn()
	b.afterRequest(err)
	return result, err
}

// State returns the current state.
func (b *Breaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Failures returns the current failure count.
func (b *Breaker) Failures() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.failures
}

// Successes returns the current success count (relevant in half-open state).
func (b *Breaker) Successes() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.successes
}

// Stats returns current circuit breaker statistics.
func (b *Breaker) Stats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return map[string]interface{}{
		"name":          b.config.Name,
		"state":         b.state.String(),
		"failures":      b.failures,
		"successes":     b.successes,
		"concurrent":    b.concurrent,
		"last_failure":  b.lastFailure,
		"state_changed": b.stateChanged,
	}
}

// Reset resets the circuit breaker to closed state.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	oldState := b.state
	b.state = StateClosed
	b.failures = 0
	b.successes = 0
	b.stateChanged = time.Now()
	if oldState != StateClosed && b.config.OnStateChange != nil {
		b.config.OnStateChange(b.config.Name, oldState, StateClosed)
	}
}

// beforeRequest checks if the request can proceed.
func (b *Breaker) beforeRequest() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()

	switch b.state {
	case StateClosed:
		// Check concurrent limit
		if b.config.MaxConcurrent > 0 && b.concurrent >= b.config.MaxConcurrent {
			return fmt.Errorf("max concurrent requests reached: %d", b.config.MaxConcurrent)
		}
		b.concurrent++
		return nil

	case StateOpen:
		// Check if timeout has passed
		if now.Sub(b.stateChanged) >= b.config.Timeout {
			b.transitionTo(StateHalfOpen)
			b.concurrent++
			return nil
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		// Allow limited requests in half-open state
		if b.config.MaxConcurrent > 0 && b.concurrent >= 1 {
			return ErrCircuitOpen
		}
		b.concurrent++
		return nil
	}

	return nil
}

// afterRequest records the result of a request.
func (b *Breaker) afterRequest(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.concurrent--

	if err != nil {
		b.recordFailure()
	} else {
		b.recordSuccess()
	}
}

// recordFailure records a failure.
func (b *Breaker) recordFailure() {
	b.failures++
	b.lastFailure = time.Now()
	b.successes = 0

	switch b.state {
	case StateClosed:
		if b.failures >= b.config.FailureThreshold {
			b.transitionTo(StateOpen)
		}
	case StateHalfOpen:
		// Single failure in half-open goes back to open
		b.transitionTo(StateOpen)
	}
}

// recordSuccess records a success.
func (b *Breaker) recordSuccess() {
	switch b.state {
	case StateClosed:
		// Reset failures on success in closed state
		b.failures = 0
	case StateHalfOpen:
		b.successes++
		if b.successes >= b.config.SuccessThreshold {
			b.transitionTo(StateClosed)
			b.failures = 0
			b.successes = 0
		}
	}
}

// transitionTo changes the state.
func (b *Breaker) transitionTo(newState State) {
	oldState := b.state
	b.state = newState
	b.stateChanged = time.Now()

	if b.config.OnStateChange != nil {
		go b.config.OnStateChange(b.config.Name, oldState, newState)
	}
}

// Manager manages multiple circuit breakers.
type Manager struct {
	breakers map[string]*Breaker
	mu       sync.RWMutex
	config   *Config
}

// NewManager creates a new circuit breaker manager.
func NewManager(defaultConfig *Config) *Manager {
	if defaultConfig == nil {
		defaultConfig = DefaultConfig("default")
	}
	return &Manager{
		breakers: make(map[string]*Breaker),
		config:   defaultConfig,
	}
}

// Get gets or creates a circuit breaker for a service.
func (m *Manager) Get(name string) *Breaker {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if cb, exists = m.breakers[name]; exists {
		return cb
	}

	// Create new with default config
	cfg := *m.config
	cfg.Name = name
	cb = New(&cfg)
	m.breakers[name] = cb
	return cb
}

// GetOrCreate gets or creates a circuit breaker with custom config.
func (m *Manager) GetOrCreate(name string, config *Config) *Breaker {
	m.mu.RLock()
	cb, exists := m.breakers[name]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if cb, exists = m.breakers[name]; exists {
		return cb
	}

	if config == nil {
		config = m.config
	}
	config.Name = name
	cb = New(config)
	m.breakers[name] = cb
	return cb
}

// Stats returns statistics for all circuit breakers.
func (m *Manager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	for name, cb := range m.breakers {
		stats[name] = cb.Stats()
	}
	return stats
}

// Reset resets all circuit breakers.
func (m *Manager) Reset() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cb := range m.breakers {
		cb.Reset()
	}
}
