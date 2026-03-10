package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	if reg == nil {
		t.Fatal("expected registry to be created")
	}

	if reg.reg == nil {
		t.Fatal("expected prometheus registry to be created")
	}
}

func TestRegistry_RecordRequest(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	// Record some requests
	reg.RecordRequest("GET", "/users", "200", 100*time.Millisecond)
	reg.RecordRequest("GET", "/users", "200", 150*time.Millisecond)
	reg.RecordRequest("POST", "/users", "201", 200*time.Millisecond)
	reg.RecordRequest("GET", "/users", "500", 50*time.Millisecond)

	// Check metrics via handler
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	content := string(body)

	// Check that metrics are present
	if !strings.Contains(content, "mycel_requests_total") {
		t.Error("expected mycel_requests_total metric")
	}

	if !strings.Contains(content, "mycel_request_duration_seconds") {
		t.Error("expected mycel_request_duration_seconds metric")
	}

	// Check specific labels
	if !strings.Contains(content, `method="GET"`) {
		t.Error("expected method label")
	}

	if !strings.Contains(content, `path="/users"`) {
		t.Error("expected path label")
	}
}

func TestRegistry_InFlightRequests(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	// Increment in-flight
	reg.IncRequestsInFlight("GET", "/users")
	reg.IncRequestsInFlight("GET", "/users")
	reg.IncRequestsInFlight("POST", "/users")

	// Decrement one
	reg.DecRequestsInFlight("GET", "/users")

	// Check metrics
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	content := string(body)

	if !strings.Contains(content, "mycel_requests_in_flight") {
		t.Error("expected mycel_requests_in_flight metric")
	}
}

func TestRegistry_ConnectorHealth(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	reg.SetConnectorHealth("postgres", "database", true)
	reg.SetConnectorHealth("redis", "cache", false)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	content := string(body)

	if !strings.Contains(content, "mycel_connector_health") {
		t.Error("expected mycel_connector_health metric")
	}

	if !strings.Contains(content, `connector="postgres"`) {
		t.Error("expected postgres connector label")
	}
}

func TestRegistry_FlowMetrics(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	reg.RecordFlowExecution("get_users", "success", 100*time.Millisecond)
	reg.RecordFlowExecution("get_users", "error", 50*time.Millisecond)
	reg.RecordFlowError("get_users", "validation_error")

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	content := string(body)

	if !strings.Contains(content, "mycel_flow_executions_total") {
		t.Error("expected mycel_flow_executions_total metric")
	}

	if !strings.Contains(content, "mycel_flow_duration_seconds") {
		t.Error("expected mycel_flow_duration_seconds metric")
	}

	if !strings.Contains(content, "mycel_flow_errors_total") {
		t.Error("expected mycel_flow_errors_total metric")
	}
}

func TestRegistry_CacheMetrics(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	reg.RecordCacheHit("products")
	reg.RecordCacheHit("products")
	reg.RecordCacheMiss("products")
	reg.SetCacheSize("products", 150)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	content := string(body)

	if !strings.Contains(content, "mycel_cache_hits_total") {
		t.Error("expected mycel_cache_hits_total metric")
	}

	if !strings.Contains(content, "mycel_cache_misses_total") {
		t.Error("expected mycel_cache_misses_total metric")
	}

	if !strings.Contains(content, "mycel_cache_size") {
		t.Error("expected mycel_cache_size metric")
	}
}

func TestRegistry_RuntimeMetrics(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	reg.SetUptime(3600)
	reg.SetGoRoutines(50)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	content := string(body)

	if !strings.Contains(content, "mycel_uptime_seconds") {
		t.Error("expected mycel_uptime_seconds metric")
	}

	if !strings.Contains(content, "mycel_goroutines") {
		t.Error("expected mycel_goroutines metric")
	}
}

func TestRegistry_ServiceInfo(t *testing.T) {
	reg := NewRegistry("my-service", "2.0.0", "1.3.0", "production")

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	reg.Handler().ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	content := string(body)

	if !strings.Contains(content, "mycel_service_info") {
		t.Error("expected mycel_service_info metric")
	}

	if !strings.Contains(content, `service="my-service"`) {
		t.Error("expected service label")
	}

	if !strings.Contains(content, `version="2.0.0"`) {
		t.Error("expected version label")
	}

	if !strings.Contains(content, `mycel_version="1.3.0"`) {
		t.Error("expected mycel_version label")
	}

	if !strings.Contains(content, `environment="production"`) {
		t.Error("expected environment label")
	}
}

func TestRegistry_Handler(t *testing.T) {
	reg := NewRegistry("test-service", "1.0.0", "1.3.0", "development")

	handler := reg.Handler()
	if handler == nil {
		t.Fatal("expected handler to be returned")
	}

	// Check it's a valid HTTP handler
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check content type
	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") && !strings.Contains(contentType, "application/openmetrics-text") {
		t.Errorf("unexpected content type: %s", contentType)
	}
}

func TestDefault(t *testing.T) {
	// Reset default
	defaultRegistry = nil
	initOnce = sync.Once{}

	reg := Default()
	if reg == nil {
		t.Fatal("expected default registry")
	}

	// Should return same instance
	reg2 := Default()
	if reg != reg2 {
		t.Error("expected same instance")
	}
}

func TestSetDefault(t *testing.T) {
	custom := NewRegistry("custom", "1.0.0", "1.3.0", "development")
	SetDefault(custom)

	if defaultRegistry != custom {
		t.Error("expected custom registry to be set as default")
	}
}
