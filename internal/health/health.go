// Package health provides liveness and readiness probes for Mycel services.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Checker represents something that can be health-checked.
type Checker interface {
	Name() string
	Health(ctx context.Context) error
}

// Status represents the health status of a component.
type Status struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "healthy", "unhealthy", "degraded"
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// Response represents the full health check response.
type Response struct {
	Status     string            `json:"status"` // "healthy", "unhealthy", "degraded"
	Timestamp  string            `json:"timestamp"`
	Version    string            `json:"version,omitempty"`
	Uptime     string            `json:"uptime,omitempty"`
	Components []Status          `json:"components,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Manager manages health checks for the service.
type Manager struct {
	mu         sync.RWMutex
	checkers   []Checker
	startTime  time.Time
	version    string
	timeout    time.Duration
	ready      bool
	readyMu    sync.RWMutex
}

// NewManager creates a new health check manager.
func NewManager(version string) *Manager {
	return &Manager{
		checkers:  make([]Checker, 0),
		startTime: time.Now(),
		version:   version,
		timeout:   5 * time.Second,
		ready:     false,
	}
}

// SetTimeout sets the timeout for individual health checks.
func (m *Manager) SetTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timeout = d
}

// Register adds a checker to the health manager.
func (m *Manager) Register(checker Checker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkers = append(m.checkers, checker)
}

// SetReady marks the service as ready to receive traffic.
func (m *Manager) SetReady(ready bool) {
	m.readyMu.Lock()
	defer m.readyMu.Unlock()
	m.ready = ready
}

// IsReady returns whether the service is ready.
func (m *Manager) IsReady() bool {
	m.readyMu.RLock()
	defer m.readyMu.RUnlock()
	return m.ready
}

// LiveHandler returns an HTTP handler for liveness probes.
// Liveness just checks if the process is alive - always returns 200 unless crashed.
func (m *Manager) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := Response{
			Status:    "healthy",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Version:   m.version,
			Uptime:    time.Since(m.startTime).Round(time.Second).String(),
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// ReadyHandler returns an HTTP handler for readiness probes.
// Readiness checks if the service is ready to receive traffic.
func (m *Manager) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check if marked as ready
		if !m.IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			resp := Response{
				Status:    "unhealthy",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Metadata:  map[string]string{"reason": "service not ready"},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Check all components
		resp := m.checkAll(r.Context())

		if resp.Status == "healthy" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// HealthHandler returns an HTTP handler for detailed health checks.
// This is a more comprehensive check that includes all component statuses.
func (m *Manager) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		resp := m.checkAll(r.Context())
		resp.Uptime = time.Since(m.startTime).Round(time.Second).String()

		if resp.Status == "healthy" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// checkAll checks all registered components and returns the aggregated status.
func (m *Manager) checkAll(ctx context.Context) Response {
	m.mu.RLock()
	checkers := make([]Checker, len(m.checkers))
	copy(checkers, m.checkers)
	timeout := m.timeout
	m.mu.RUnlock()

	resp := Response{
		Status:     "healthy",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Version:    m.version,
		Components: make([]Status, 0, len(checkers)),
	}

	// Check each component concurrently
	var wg sync.WaitGroup
	statusChan := make(chan Status, len(checkers))

	for _, checker := range checkers {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()
			status := m.checkOne(ctx, c, timeout)
			statusChan <- status
		}(checker)
	}

	// Wait for all checks to complete
	wg.Wait()
	close(statusChan)

	// Collect results
	hasUnhealthy := false
	hasDegraded := false

	for status := range statusChan {
		resp.Components = append(resp.Components, status)
		if status.Status == "unhealthy" {
			hasUnhealthy = true
		} else if status.Status == "degraded" {
			hasDegraded = true
		}
	}

	// Determine overall status
	if hasUnhealthy {
		resp.Status = "unhealthy"
	} else if hasDegraded {
		resp.Status = "degraded"
	}

	return resp
}

// checkOne checks a single component with timeout.
func (m *Manager) checkOne(ctx context.Context, checker Checker, timeout time.Duration) Status {
	status := Status{
		Name:   checker.Name(),
		Status: "healthy",
	}

	// Create context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Run health check
	errChan := make(chan error, 1)
	go func() {
		errChan <- checker.Health(checkCtx)
	}()

	select {
	case err := <-errChan:
		status.Latency = time.Since(start).Round(time.Millisecond).String()
		if err != nil {
			status.Status = "unhealthy"
			status.Message = err.Error()
		}
	case <-checkCtx.Done():
		status.Status = "unhealthy"
		status.Message = "health check timed out"
		status.Latency = timeout.String()
	}

	return status
}

// RegisterHandlers registers health check handlers on an HTTP mux.
func (m *Manager) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/health", m.HealthHandler())
	mux.HandleFunc("/health/live", m.LiveHandler())
	mux.HandleFunc("/health/ready", m.ReadyHandler())
}
