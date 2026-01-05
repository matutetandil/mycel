package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	t.Run("disabled rate limiting", func(t *testing.T) {
		rl := NewRateLimiter(&RateLimitConfig{Enabled: false})

		// Should always allow
		for i := 0; i < 100; i++ {
			if !rl.Allow("login") {
				t.Error("expected request to be allowed when rate limiting is disabled")
			}
		}
	})

	t.Run("enabled rate limiting", func(t *testing.T) {
		rl := NewRateLimiter(&RateLimitConfig{
			Enabled:      true,
			DefaultRate:  5,
			DefaultBurst: 2,
			Window:       "1m",
		})

		// First few should be allowed (burst)
		allowed := 0
		for i := 0; i < 10; i++ {
			if rl.Allow("unknown_endpoint") {
				allowed++
			}
		}

		// Should have allowed at least the burst amount
		if allowed < 2 {
			t.Errorf("expected at least 2 requests allowed (burst), got %d", allowed)
		}
	})

	t.Run("endpoint-specific limits", func(t *testing.T) {
		rl := NewRateLimiter(&RateLimitConfig{
			Enabled:      true,
			DefaultRate:  100,
			DefaultBurst: 50,
			Window:       "1m",
			Login: &EndpointRateLimit{
				Rate:   2,
				Burst:  1,
				Window: "1m",
			},
		})

		// Login should have stricter limits
		loginAllowed := 0
		for i := 0; i < 10; i++ {
			if rl.Allow("login") {
				loginAllowed++
			}
		}

		if loginAllowed > 2 {
			t.Errorf("expected login to allow max 2 requests, got %d", loginAllowed)
		}
	})
}

func TestPerKeyRateLimiter(t *testing.T) {
	t.Run("different keys get separate limits", func(t *testing.T) {
		rl := NewPerKeyRateLimiter(&RateLimitConfig{
			Enabled:      true,
			DefaultRate:  5,
			DefaultBurst: 2,
			Window:       "1m",
		})

		// Key1 uses its burst
		for i := 0; i < 3; i++ {
			rl.Allow("login", "key1")
		}

		// Key2 should still have full burst available
		allowed := 0
		for i := 0; i < 3; i++ {
			if rl.Allow("login", "key2") {
				allowed++
			}
		}

		if allowed < 2 {
			t.Errorf("expected key2 to have at least 2 requests available, got %d", allowed)
		}
	})

	t.Run("sensitive endpoints have stricter defaults", func(t *testing.T) {
		rl := NewPerKeyRateLimiter(&RateLimitConfig{
			Enabled:      true,
			DefaultRate:  100,
			DefaultBurst: 50,
			Window:       "1m",
		})

		// Login should have stricter limits (5 per minute, burst 3)
		loginRate, loginBurst := rl.getRateForEndpoint("login")
		expectedRate := float64(5) / 60.0 // 5 per minute

		if float64(loginRate) > expectedRate*1.1 {
			t.Errorf("expected login rate ~%v, got %v", expectedRate, loginRate)
		}
		if loginBurst > 5 {
			t.Errorf("expected login burst <= 5, got %d", loginBurst)
		}
	})
}

func TestRateLimitMiddleware(t *testing.T) {
	t.Run("allows requests under limit", func(t *testing.T) {
		rl := NewPerKeyRateLimiter(&RateLimitConfig{
			Enabled:      true,
			DefaultRate:  100,
			DefaultBurst: 10,
			Window:       "1m",
		})

		handler := RateLimitMiddleware(rl, DefaultKeyExtractor("ip"))(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		)

		req := httptest.NewRequest("POST", "/auth/login", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("blocks requests over limit", func(t *testing.T) {
		rl := NewPerKeyRateLimiter(&RateLimitConfig{
			Enabled:      true,
			DefaultRate:  1,
			DefaultBurst: 1,
			Window:       "1m",
			Login: &EndpointRateLimit{
				Rate:   1,
				Burst:  1,
				Window: "1m",
			},
		})

		handler := RateLimitMiddleware(rl, DefaultKeyExtractor("ip"))(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		)

		// First request should succeed
		req1 := httptest.NewRequest("POST", "/auth/login", nil)
		req1.RemoteAddr = "192.168.1.100:12345"
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)

		if rec1.Code != http.StatusOK {
			t.Errorf("first request: expected status 200, got %d", rec1.Code)
		}

		// Second immediate request should be rate limited
		req2 := httptest.NewRequest("POST", "/auth/login", nil)
		req2.RemoteAddr = "192.168.1.100:12345"
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusTooManyRequests {
			t.Errorf("second request: expected status 429, got %d", rec2.Code)
		}
	})

	t.Run("nil rate limiter passes through", func(t *testing.T) {
		handler := RateLimitMiddleware(nil, DefaultKeyExtractor("ip"))(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		)

		req := httptest.NewRequest("POST", "/auth/login", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})
}

func TestExtractEndpoint(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/login", "login"},
		{"/register", "register"},
		{"/auth/login", "login"},
		{"/auth/register", "register"},
		{"/auth/change-password", "change_password"},
		{"/auth/sessions", "sessions"},
		{"/auth/me", "sessions"},
		{"/unknown", "/unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := extractEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("extractEndpoint(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestGetClientIPFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		expected   string
	}{
		{
			name:       "from remote addr",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "from X-Forwarded-For",
			remoteAddr: "127.0.0.1:12345",
			xff:        "203.0.113.195, 70.41.3.18, 150.172.238.178",
			expected:   "203.0.113.195",
		},
		{
			name:       "from X-Real-IP",
			remoteAddr: "127.0.0.1:12345",
			xri:        "203.0.113.195",
			expected:   "203.0.113.195",
		},
		{
			name:       "X-Forwarded-For takes precedence",
			remoteAddr: "127.0.0.1:12345",
			xff:        "10.0.0.1",
			xri:        "20.0.0.1",
			expected:   "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			result := getClientIPFromRequest(req)
			if result != tt.expected {
				t.Errorf("getClientIPFromRequest() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestDefaultKeyExtractor(t *testing.T) {
	t.Run("ip mode", func(t *testing.T) {
		extractor := DefaultKeyExtractor("ip")
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		key := extractor(req)
		if key != "192.168.1.1" {
			t.Errorf("expected IP, got %q", key)
		}
	})

	t.Run("user mode with header", func(t *testing.T) {
		extractor := DefaultKeyExtractor("user")
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		req.Header.Set("X-User-ID", "user123")

		key := extractor(req)
		if key != "user123" {
			t.Errorf("expected user ID, got %q", key)
		}
	})

	t.Run("ip+user mode", func(t *testing.T) {
		extractor := DefaultKeyExtractor("ip+user")
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		req.Header.Set("X-User-ID", "user123")

		key := extractor(req)
		if key != "192.168.1.1:user123" {
			t.Errorf("expected ip:user, got %q", key)
		}
	})
}

func TestRateLimiterWithTime(t *testing.T) {
	t.Run("tokens replenish over time", func(t *testing.T) {
		rl := NewPerKeyRateLimiter(&RateLimitConfig{
			Enabled: true,
			Login: &EndpointRateLimit{
				Rate:   1,
				Burst:  1,
				Window: "100ms", // Very short for testing
			},
		})

		key := "test-key"

		// Use up the burst
		rl.Allow("login", key)

		// Should be rate limited
		if rl.Allow("login", key) {
			t.Error("expected to be rate limited")
		}

		// Wait for token to replenish
		time.Sleep(150 * time.Millisecond)

		// Should be allowed again
		if !rl.Allow("login", key) {
			t.Error("expected to be allowed after waiting")
		}
	})
}
