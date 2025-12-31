package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestLimiter_Basic(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             10,
		KeyExtractor:      "ip",
	})
	defer limiter.Close()

	// First 10 requests should be allowed (burst)
	for i := 0; i < 10; i++ {
		if !limiter.Allow("test-key") {
			t.Errorf("request %d should be allowed", i)
		}
	}

	// 11th request should be denied
	if limiter.Allow("test-key") {
		t.Error("11th request should be denied")
	}
}

func TestLimiter_Disabled(t *testing.T) {
	limiter := New(&Config{
		Enabled:           false,
		RequestsPerSecond: 1,
		Burst:             1,
	})
	defer limiter.Close()

	// All requests should be allowed when disabled
	for i := 0; i < 100; i++ {
		if !limiter.Allow("test-key") {
			t.Error("request should be allowed when rate limiting is disabled")
		}
	}
}

func TestLimiter_DifferentKeys(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 1,
		Burst:             2,
	})
	defer limiter.Close()

	// Different keys should have separate limits
	if !limiter.Allow("key1") {
		t.Error("first request for key1 should be allowed")
	}
	if !limiter.Allow("key1") {
		t.Error("second request for key1 should be allowed")
	}
	if limiter.Allow("key1") {
		t.Error("third request for key1 should be denied")
	}

	// key2 should have its own limit
	if !limiter.Allow("key2") {
		t.Error("first request for key2 should be allowed")
	}
	if !limiter.Allow("key2") {
		t.Error("second request for key2 should be allowed")
	}
}

func TestLimiter_Middleware(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             2,
		KeyExtractor:      "ip",
		EnableHeaders:     true,
	})
	defer limiter.Close()

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make requests
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d should return 200, got %d", i, rec.Code)
		}

		// Check headers
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Error("X-RateLimit-Limit header should be set")
		}
	}

	// Third request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("rate limited request should return 429, got %d", rec.Code)
	}
}

func TestLimiter_ExcludePaths(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 1,
		Burst:             1,
		KeyExtractor:      "ip",
		ExcludePaths:      []string{"/health", "/metrics"},
	})
	defer limiter.Close()

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Health endpoint should not be rate limited
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("health request %d should return 200, got %d", i, rec.Code)
		}
	}

	// Metrics endpoint should not be rate limited
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/metrics", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("metrics request %d should return 200, got %d", i, rec.Code)
		}
	}
}

func TestLimiter_KeyExtractor_Header(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             2,
		KeyExtractor:      "header:X-API-Key",
	})
	defer limiter.Close()

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Same API key should share limits
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "key-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d should return 200, got %d", i, rec.Code)
		}
	}

	// Third request with same key should be limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("rate limited request should return 429, got %d", rec.Code)
	}

	// Different key should have separate limit
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "key-2")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("different key should return 200, got %d", rec.Code)
	}
}

func TestLimiter_XForwardedFor(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             2,
		KeyExtractor:      "ip",
	})
	defer limiter.Close()

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use X-Forwarded-For
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d should return 200, got %d", i, rec.Code)
		}
	}

	// Should use first IP from X-Forwarded-For
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("rate limited request should return 429, got %d", rec.Code)
	}

	// Different X-Forwarded-For should have separate limit
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.2")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("different IP should return 200, got %d", rec.Code)
	}
}

func TestLimiter_Concurrent(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 100,
		Burst:             100,
		KeyExtractor:      "ip",
	})
	defer limiter.Close()

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	// Launch 200 concurrent requests
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- limiter.Allow("concurrent-key")
		}()
	}

	wg.Wait()
	close(allowed)

	allowedCount := 0
	for a := range allowed {
		if a {
			allowedCount++
		}
	}

	// Should allow approximately burst number
	if allowedCount < 90 || allowedCount > 110 {
		t.Errorf("expected ~100 allowed requests, got %d", allowedCount)
	}
}

func TestLimiter_Recovery(t *testing.T) {
	limiter := New(&Config{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             2,
	})
	defer limiter.Close()

	// Exhaust burst
	limiter.Allow("recovery-key")
	limiter.Allow("recovery-key")

	if limiter.Allow("recovery-key") {
		t.Error("should be rate limited")
	}

	// Wait for tokens to recover
	time.Sleep(250 * time.Millisecond)

	// Should have recovered some tokens
	if !limiter.Allow("recovery-key") {
		t.Error("should have recovered after waiting")
	}
}

func TestEndpointLimiter(t *testing.T) {
	el := NewEndpointLimiter(
		&Config{
			Enabled:           true,
			RequestsPerSecond: 10,
			Burst:             5,
			KeyExtractor:      "ip",
		},
		[]PerEndpointConfig{
			{
				Path:              "/api/expensive",
				Method:            "POST",
				RequestsPerSecond: 1,
				Burst:             1,
			},
		},
	)
	defer el.Close()

	handler := el.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Expensive endpoint should have stricter limit
	req := httptest.NewRequest("POST", "/api/expensive", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("first expensive request should return 200, got %d", rec.Code)
	}

	// Second request to expensive endpoint should be limited
	req = httptest.NewRequest("POST", "/api/expensive", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second expensive request should return 429, got %d", rec.Code)
	}

	// Regular endpoint should use global limit
	for i := 0; i < 5; i++ {
		req = httptest.NewRequest("GET", "/api/regular", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("regular request %d should return 200, got %d", i, rec.Code)
		}
	}
}

func TestNew_NilConfig(t *testing.T) {
	limiter := New(nil)
	defer limiter.Close()

	// Should not panic and should be disabled
	if !limiter.Allow("test") {
		t.Error("should allow when config is nil (disabled)")
	}
}
