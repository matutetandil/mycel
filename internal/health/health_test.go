package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockChecker implements Checker interface for testing.
type mockChecker struct {
	name string
	err  error
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Health(ctx context.Context) error {
	return m.err
}

func TestManager_LiveHandler(t *testing.T) {
	mgr := NewManager("1.0.0")

	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()

	mgr.LiveHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", resp.Status)
	}

	if resp.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", resp.Version)
	}
}

func TestManager_ReadyHandler_NotReady(t *testing.T) {
	mgr := NewManager("1.0.0")
	// Service not marked as ready

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	mgr.ReadyHandler()(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "unhealthy" {
		t.Errorf("expected status unhealthy, got %s", resp.Status)
	}
}

func TestManager_ReadyHandler_Ready(t *testing.T) {
	mgr := NewManager("1.0.0")
	mgr.SetReady(true)

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()

	mgr.ReadyHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", resp.Status)
	}
}

func TestManager_HealthHandler_AllHealthy(t *testing.T) {
	mgr := NewManager("1.0.0")
	mgr.SetReady(true)

	mgr.Register(&mockChecker{name: "db", err: nil})
	mgr.Register(&mockChecker{name: "cache", err: nil})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	mgr.HealthHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", resp.Status)
	}

	if len(resp.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(resp.Components))
	}
}

func TestManager_HealthHandler_OneUnhealthy(t *testing.T) {
	mgr := NewManager("1.0.0")
	mgr.SetReady(true)

	mgr.Register(&mockChecker{name: "db", err: nil})
	mgr.Register(&mockChecker{name: "cache", err: errors.New("connection refused")})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	mgr.HealthHandler()(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "unhealthy" {
		t.Errorf("expected status unhealthy, got %s", resp.Status)
	}

	// Check that cache is marked as unhealthy
	for _, c := range resp.Components {
		if c.Name == "cache" {
			if c.Status != "unhealthy" {
				t.Errorf("expected cache to be unhealthy, got %s", c.Status)
			}
			if c.Message != "connection refused" {
				t.Errorf("expected message 'connection refused', got %s", c.Message)
			}
		}
	}
}

func TestManager_Timeout(t *testing.T) {
	mgr := NewManager("1.0.0")
	mgr.SetReady(true)
	mgr.SetTimeout(100 * time.Millisecond)

	// Slow checker that takes too long
	slowChecker := &slowMockChecker{name: "slow", delay: 500 * time.Millisecond}
	mgr.Register(slowChecker)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	mgr.HealthHandler()(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 for timeout, got %d", w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "unhealthy" {
		t.Errorf("expected status unhealthy due to timeout, got %s", resp.Status)
	}
}

type slowMockChecker struct {
	name  string
	delay time.Duration
}

func (s *slowMockChecker) Name() string {
	return s.name
}

func (s *slowMockChecker) Health(ctx context.Context) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestManager_RegisterHandlers(t *testing.T) {
	mgr := NewManager("1.0.0")
	mgr.SetReady(true)

	mux := http.NewServeMux()
	mgr.RegisterHandlers(mux)

	// Test /health
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected /health to return 200, got %d", w.Code)
	}

	// Test /health/live
	req = httptest.NewRequest("GET", "/health/live", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected /health/live to return 200, got %d", w.Code)
	}

	// Test /health/ready
	req = httptest.NewRequest("GET", "/health/ready", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected /health/ready to return 200, got %d", w.Code)
	}
}

func TestManager_Uptime(t *testing.T) {
	mgr := NewManager("1.0.0")
	mgr.SetReady(true)

	// Wait a bit so uptime is measurable
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()

	mgr.LiveHandler()(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Uptime == "" {
		t.Error("expected uptime to be set")
	}
}
