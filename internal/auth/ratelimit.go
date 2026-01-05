package auth

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimitConfig defines rate limiting for auth endpoints
type RateLimitConfig struct {
	// Enabled enables rate limiting
	Enabled bool `hcl:"enabled,optional"`

	// Default rate limit for all endpoints
	DefaultRate  int    `hcl:"rate,optional"`  // requests per window
	DefaultBurst int    `hcl:"burst,optional"` // burst size
	Window       string `hcl:"window,optional"` // time window (e.g., "1m", "1h")

	// Per-endpoint overrides
	Login          *EndpointRateLimit `hcl:"login,block"`
	Register       *EndpointRateLimit `hcl:"register,block"`
	Refresh        *EndpointRateLimit `hcl:"refresh,block"`
	Logout         *EndpointRateLimit `hcl:"logout,block"`
	ChangePassword *EndpointRateLimit `hcl:"change_password,block"`
	Sessions       *EndpointRateLimit `hcl:"sessions,block"`

	// Key extraction
	KeyBy string `hcl:"key_by,optional"` // ip, user, ip+user
}

// EndpointRateLimit defines rate limit for a specific endpoint
type EndpointRateLimit struct {
	Rate   int    `hcl:"rate"`
	Burst  int    `hcl:"burst,optional"`
	Window string `hcl:"window,optional"`
}

// RateLimiter handles per-endpoint rate limiting
type RateLimiter struct {
	config   *RateLimitConfig
	limiters map[string]*endpointLimiter
	mu       sync.RWMutex
}

type endpointLimiter struct {
	limiter *rate.Limiter
	rate    rate.Limit
	burst   int
}

// perKeyLimiter tracks limiters per key (IP, user, etc.)
type perKeyLimiter struct {
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
	mu       sync.RWMutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config *RateLimitConfig) *RateLimiter {
	if config == nil {
		config = &RateLimitConfig{Enabled: false}
	}

	rl := &RateLimiter{
		config:   config,
		limiters: make(map[string]*endpointLimiter),
	}

	// Initialize endpoint limiters
	if config.Enabled {
		rl.initEndpointLimiters()
	}

	return rl
}

// initEndpointLimiters sets up rate limiters for each endpoint
func (rl *RateLimiter) initEndpointLimiters() {
	// Default values
	defaultRate := rl.config.DefaultRate
	if defaultRate == 0 {
		defaultRate = 60 // 60 requests per window
	}

	defaultBurst := rl.config.DefaultBurst
	if defaultBurst == 0 {
		defaultBurst = 10
	}

	defaultWindow := rl.config.Window
	if defaultWindow == "" {
		defaultWindow = "1m"
	}

	// Calculate default rate.Limit
	windowDuration, _ := ParseDuration(defaultWindow)
	if windowDuration == 0 {
		windowDuration = time.Minute
	}
	defaultRateLimit := rate.Limit(float64(defaultRate) / windowDuration.Seconds())

	// Endpoint-specific limits
	endpoints := map[string]*EndpointRateLimit{
		"login":           rl.config.Login,
		"register":        rl.config.Register,
		"refresh":         rl.config.Refresh,
		"logout":          rl.config.Logout,
		"change_password": rl.config.ChangePassword,
		"sessions":        rl.config.Sessions,
	}

	// Stricter defaults for sensitive endpoints
	sensitiveDefaults := map[string]struct{ rate, burst int }{
		"login":           {5, 3},   // 5 per minute, burst 3
		"register":        {10, 5},  // 10 per minute, burst 5
		"change_password": {3, 2},   // 3 per minute, burst 2
		"refresh":         {30, 10}, // 30 per minute, burst 10
		"logout":          {30, 10}, // 30 per minute, burst 10
		"sessions":        {30, 10}, // 30 per minute, burst 10
	}

	for name, cfg := range endpoints {
		var rateLimit rate.Limit
		var burst int

		if cfg != nil {
			// Use configured values
			r := cfg.Rate
			if r == 0 {
				if defaults, ok := sensitiveDefaults[name]; ok {
					r = defaults.rate
				} else {
					r = defaultRate
				}
			}

			burst = cfg.Burst
			if burst == 0 {
				if defaults, ok := sensitiveDefaults[name]; ok {
					burst = defaults.burst
				} else {
					burst = defaultBurst
				}
			}

			window := cfg.Window
			if window == "" {
				window = defaultWindow
			}

			w, _ := ParseDuration(window)
			if w == 0 {
				w = windowDuration
			}

			rateLimit = rate.Limit(float64(r) / w.Seconds())
		} else {
			// Use sensitive defaults or general defaults
			if defaults, ok := sensitiveDefaults[name]; ok {
				rateLimit = rate.Limit(float64(defaults.rate) / windowDuration.Seconds())
				burst = defaults.burst
			} else {
				rateLimit = defaultRateLimit
				burst = defaultBurst
			}
		}

		rl.limiters[name] = &endpointLimiter{
			limiter: rate.NewLimiter(rateLimit, burst),
			rate:    rateLimit,
			burst:   burst,
		}
	}
}

// Allow checks if a request is allowed for a given endpoint
func (rl *RateLimiter) Allow(endpoint string) bool {
	if !rl.config.Enabled {
		return true
	}

	rl.mu.RLock()
	limiter, ok := rl.limiters[endpoint]
	rl.mu.RUnlock()

	if !ok {
		return true // Unknown endpoint, allow
	}

	return limiter.limiter.Allow()
}

// Wait blocks until a request is allowed or context is done
func (rl *RateLimiter) Wait(ctx context.Context, endpoint string) error {
	if !rl.config.Enabled {
		return nil
	}

	rl.mu.RLock()
	limiter, ok := rl.limiters[endpoint]
	rl.mu.RUnlock()

	if !ok {
		return nil
	}

	return limiter.limiter.Wait(ctx)
}

// PerKeyRateLimiter provides per-key (IP, user) rate limiting
type PerKeyRateLimiter struct {
	config   *RateLimitConfig
	limiters map[string]*perKeyLimiter // endpoint -> per-key limiter
	mu       sync.RWMutex
}

// NewPerKeyRateLimiter creates a rate limiter that tracks per key
func NewPerKeyRateLimiter(config *RateLimitConfig) *PerKeyRateLimiter {
	if config == nil {
		config = &RateLimitConfig{Enabled: false}
	}

	return &PerKeyRateLimiter{
		config:   config,
		limiters: make(map[string]*perKeyLimiter),
	}
}

// Allow checks if a request is allowed for a given endpoint and key
func (rl *PerKeyRateLimiter) Allow(endpoint, key string) bool {
	if !rl.config.Enabled {
		return true
	}

	limiter := rl.getLimiter(endpoint, key)
	return limiter.Allow()
}

// getLimiter gets or creates a rate limiter for endpoint+key
func (rl *PerKeyRateLimiter) getLimiter(endpoint, key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	pkl, ok := rl.limiters[endpoint]
	if !ok {
		// Create per-key limiter for this endpoint
		r, burst := rl.getRateForEndpoint(endpoint)
		pkl = &perKeyLimiter{
			limiters: make(map[string]*rate.Limiter),
			rate:     r,
			burst:    burst,
		}
		rl.limiters[endpoint] = pkl
	}

	pkl.mu.Lock()
	defer pkl.mu.Unlock()

	limiter, ok := pkl.limiters[key]
	if !ok {
		limiter = rate.NewLimiter(pkl.rate, pkl.burst)
		pkl.limiters[key] = limiter
	}

	return limiter
}

// getRateForEndpoint returns the rate and burst for an endpoint
func (rl *PerKeyRateLimiter) getRateForEndpoint(endpoint string) (rate.Limit, int) {
	// Default window
	windowDuration := time.Minute
	if rl.config.Window != "" {
		if w, err := ParseDuration(rl.config.Window); err == nil && w > 0 {
			windowDuration = w
		}
	}

	// Endpoint-specific configs
	var cfg *EndpointRateLimit
	switch endpoint {
	case "login":
		cfg = rl.config.Login
	case "register":
		cfg = rl.config.Register
	case "refresh":
		cfg = rl.config.Refresh
	case "logout":
		cfg = rl.config.Logout
	case "change_password":
		cfg = rl.config.ChangePassword
	case "sessions":
		cfg = rl.config.Sessions
	}

	// Sensitive endpoint defaults
	sensitiveDefaults := map[string]struct{ rate, burst int }{
		"login":           {5, 3},
		"register":        {10, 5},
		"change_password": {3, 2},
		"refresh":         {30, 10},
		"logout":          {30, 10},
		"sessions":        {30, 10},
	}

	var r int
	var burst int

	if cfg != nil {
		r = cfg.Rate
		burst = cfg.Burst
		if cfg.Window != "" {
			if w, err := ParseDuration(cfg.Window); err == nil && w > 0 {
				windowDuration = w
			}
		}
	}

	if r == 0 {
		if defaults, ok := sensitiveDefaults[endpoint]; ok {
			r = defaults.rate
		} else if rl.config.DefaultRate > 0 {
			r = rl.config.DefaultRate
		} else {
			r = 60
		}
	}

	if burst == 0 {
		if defaults, ok := sensitiveDefaults[endpoint]; ok {
			burst = defaults.burst
		} else if rl.config.DefaultBurst > 0 {
			burst = rl.config.DefaultBurst
		} else {
			burst = 10
		}
	}

	return rate.Limit(float64(r) / windowDuration.Seconds()), burst
}

// Cleanup removes old limiters that haven't been used recently
func (rl *PerKeyRateLimiter) Cleanup() {
	// In a production system, you'd track last access time
	// and periodically clean up old entries
	// For now, this is a placeholder
}

// RateLimitMiddleware creates HTTP middleware for rate limiting
func RateLimitMiddleware(rl *PerKeyRateLimiter, keyExtractor func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl == nil || !rl.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Extract endpoint from path
			endpoint := extractEndpoint(r.URL.Path)

			// Extract key
			key := keyExtractor(r)
			if key == "" {
				key = getClientIPFromRequest(r)
			}

			if !rl.Allow(endpoint, key) {
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractEndpoint extracts the endpoint name from the path
func extractEndpoint(path string) string {
	// Map paths to endpoint names
	// Assumes paths like /auth/login, /auth/register, etc.
	pathMap := map[string]string{
		"/login":           "login",
		"/register":        "register",
		"/refresh":         "refresh",
		"/logout":          "logout",
		"/change-password": "change_password",
		"/sessions":        "sessions",
		"/me":              "sessions",
	}

	// Try exact match first
	if endpoint, ok := pathMap[path]; ok {
		return endpoint
	}

	// Try with /auth prefix
	if len(path) > 5 && path[:5] == "/auth" {
		if endpoint, ok := pathMap[path[5:]]; ok {
			return endpoint
		}
	}

	// Default to the path itself
	return path
}

// getClientIPFromRequest extracts the client IP from request (wrapper for getClientIP)
func getClientIPFromRequest(r *http.Request) string {
	return getClientIP(r)
}

// DefaultKeyExtractor returns a key extractor based on config
func DefaultKeyExtractor(keyBy string) func(*http.Request) string {
	return func(r *http.Request) string {
		switch keyBy {
		case "ip":
			return getClientIPFromRequest(r)
		case "user":
			// Try to extract user from context or header
			if userID := r.Header.Get("X-User-ID"); userID != "" {
				return userID
			}
			// Fall back to IP
			return getClientIPFromRequest(r)
		case "ip+user":
			ip := getClientIPFromRequest(r)
			userID := r.Header.Get("X-User-ID")
			if userID != "" {
				return fmt.Sprintf("%s:%s", ip, userID)
			}
			return ip
		default:
			return getClientIPFromRequest(r)
		}
	}
}
