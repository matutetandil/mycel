// Package ratelimit provides rate limiting functionality for Mycel services.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Config holds rate limiting configuration.
type Config struct {
	// Enabled enables rate limiting.
	Enabled bool

	// RequestsPerSecond is the rate limit in requests per second.
	RequestsPerSecond float64

	// Burst is the maximum burst size.
	Burst int

	// KeyExtractor specifies how to extract the rate limit key.
	// Options: "ip", "header:<name>", "query:<name>", "combined"
	KeyExtractor string

	// HeaderName is the header to use when KeyExtractor is "header:<name>".
	HeaderName string

	// ExcludePaths are paths that bypass rate limiting.
	ExcludePaths []string

	// EnableHeaders adds rate limit headers to responses.
	EnableHeaders bool
}

// Limiter implements rate limiting logic.
type Limiter struct {
	config   *Config
	limiters map[string]*clientLimiter
	mu       sync.RWMutex
	cleanup  *time.Ticker
	done     chan struct{}
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New creates a new rate limiter.
func New(config *Config) *Limiter {
	if config == nil {
		config = &Config{
			Enabled:           false,
			RequestsPerSecond: 100,
			Burst:             200,
			KeyExtractor:      "ip",
		}
	}

	l := &Limiter{
		config:   config,
		limiters: make(map[string]*clientLimiter),
		done:     make(chan struct{}),
	}

	// Start cleanup goroutine
	l.cleanup = time.NewTicker(time.Minute)
	go l.cleanupLoop()

	return l
}

// Middleware returns an HTTP middleware that applies rate limiting.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Check excluded paths
		for _, path := range l.config.ExcludePaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		key := l.extractKey(r)
		limiter := l.getLimiter(key)

		if l.config.EnableHeaders {
			l.addHeaders(w, limiter)
		}

		if !limiter.Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprintf(w, `{"error": "rate limit exceeded", "retry_after": 1}`)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Allow checks if a request with the given key is allowed.
func (l *Limiter) Allow(key string) bool {
	if !l.config.Enabled {
		return true
	}
	return l.getLimiter(key).Allow()
}

// AllowN checks if n requests with the given key are allowed.
func (l *Limiter) AllowN(key string, n int) bool {
	if !l.config.Enabled {
		return true
	}
	return l.getLimiter(key).AllowN(time.Now(), n)
}

// Wait blocks until a request is allowed or context is cancelled.
func (l *Limiter) Wait(ctx context.Context, key string) error {
	if !l.config.Enabled {
		return nil
	}
	return l.getLimiter(key).Wait(ctx)
}

// Close stops the limiter and releases resources.
func (l *Limiter) Close() {
	close(l.done)
	l.cleanup.Stop()
}

// extractKey extracts the rate limit key from the request.
func (l *Limiter) extractKey(r *http.Request) string {
	switch {
	case l.config.KeyExtractor == "ip":
		return l.getIP(r)

	case strings.HasPrefix(l.config.KeyExtractor, "header:"):
		headerName := strings.TrimPrefix(l.config.KeyExtractor, "header:")
		if val := r.Header.Get(headerName); val != "" {
			return val
		}
		return l.getIP(r)

	case strings.HasPrefix(l.config.KeyExtractor, "query:"):
		paramName := strings.TrimPrefix(l.config.KeyExtractor, "query:")
		if val := r.URL.Query().Get(paramName); val != "" {
			return val
		}
		return l.getIP(r)

	case l.config.KeyExtractor == "combined":
		ip := l.getIP(r)
		userAgent := r.Header.Get("User-Agent")
		return fmt.Sprintf("%s:%s", ip, userAgent)

	default:
		return l.getIP(r)
	}
}

// getIP extracts the client IP from the request.
func (l *Limiter) getIP(r *http.Request) string {
	// Check X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// getLimiter gets or creates a rate limiter for the key.
func (l *Limiter) getLimiter(key string) *rate.Limiter {
	l.mu.RLock()
	cl, exists := l.limiters[key]
	l.mu.RUnlock()

	if exists {
		l.mu.Lock()
		cl.lastSeen = time.Now()
		l.mu.Unlock()
		return cl.limiter
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check
	if cl, exists = l.limiters[key]; exists {
		cl.lastSeen = time.Now()
		return cl.limiter
	}

	limiter := rate.NewLimiter(rate.Limit(l.config.RequestsPerSecond), l.config.Burst)
	l.limiters[key] = &clientLimiter{
		limiter:  limiter,
		lastSeen: time.Now(),
	}

	return limiter
}

// addHeaders adds rate limit headers to the response.
func (l *Limiter) addHeaders(w http.ResponseWriter, limiter *rate.Limiter) {
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", l.config.Burst))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", int(limiter.Tokens())))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Second).Unix()))
}

// cleanupLoop removes stale limiters.
func (l *Limiter) cleanupLoop() {
	for {
		select {
		case <-l.done:
			return
		case <-l.cleanup.C:
			l.mu.Lock()
			threshold := time.Now().Add(-3 * time.Minute)
			for key, cl := range l.limiters {
				if cl.lastSeen.Before(threshold) {
					delete(l.limiters, key)
				}
			}
			l.mu.Unlock()
		}
	}
}

// PerEndpointConfig holds rate limit configuration for specific endpoints.
type PerEndpointConfig struct {
	Path              string
	Method            string
	RequestsPerSecond float64
	Burst             int
}

// EndpointLimiter provides per-endpoint rate limiting.
type EndpointLimiter struct {
	global    *Limiter
	endpoints map[string]*Limiter
	mu        sync.RWMutex
}

// NewEndpointLimiter creates a new endpoint-aware rate limiter.
func NewEndpointLimiter(globalConfig *Config, endpoints []PerEndpointConfig) *EndpointLimiter {
	el := &EndpointLimiter{
		global:    New(globalConfig),
		endpoints: make(map[string]*Limiter),
	}

	for _, ep := range endpoints {
		key := fmt.Sprintf("%s:%s", ep.Method, ep.Path)
		el.endpoints[key] = New(&Config{
			Enabled:           true,
			RequestsPerSecond: ep.RequestsPerSecond,
			Burst:             ep.Burst,
			KeyExtractor:      globalConfig.KeyExtractor,
		})
	}

	return el
}

// Middleware returns an HTTP middleware for endpoint rate limiting.
func (el *EndpointLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check endpoint-specific limiter first
		key := fmt.Sprintf("%s:%s", r.Method, r.URL.Path)

		el.mu.RLock()
		limiter, exists := el.endpoints[key]
		el.mu.RUnlock()

		if exists {
			limiter.Middleware(next).ServeHTTP(w, r)
			return
		}

		// Fall back to global limiter
		el.global.Middleware(next).ServeHTTP(w, r)
	})
}

// Close closes all limiters.
func (el *EndpointLimiter) Close() {
	el.global.Close()
	for _, l := range el.endpoints {
		l.Close()
	}
}

// AllowKey checks if a request with the given key is allowed.
// Returns true if the request is allowed, false if rate limited.
// This method is useful for programmatic rate limiting outside of HTTP middleware.
func (l *Limiter) AllowKey(key string) bool {
	if !l.config.Enabled {
		return true
	}
	limiter := l.getLimiter(key)
	return limiter.Allow()
}

// AllowKeyN checks if n requests with the given key are allowed.
// Returns true if the requests are allowed, false if rate limited.
func (l *Limiter) AllowKeyN(key string, n int) bool {
	if !l.config.Enabled {
		return true
	}
	limiter := l.getLimiter(key)
	return limiter.AllowN(time.Now(), n)
}

// ErrRateLimited is returned when a request is rate limited.
var ErrRateLimited = errors.New("rate limit exceeded")
