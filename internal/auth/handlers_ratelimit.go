package auth

import (
	"net/http"
	"strings"
)

// Rate limiting for the auth endpoints.
//
// This is a different question from the connector's own limit. That one caps a
// whole HTTP server; this one counts per endpoint, so five login attempts a
// minute does not also cap the rest of the API. It is also a different question
// from brute force, which locks one account after repeated failures: this
// refuses a flood spread across many accounts, which is what credential
// stuffing looks like and which no per-account counter sees.
//
// The limiter was written with all of that and nothing built it, so the block
// configuring it did nothing.

// limited wraps a handler with the limit configured for its endpoint.
//
// The endpoint is named here rather than recovered from the request path,
// because the paths are configurable and a limit that stopped applying when
// someone moved an endpoint would be worse than no limit.
func (h *Handler) limited(endpoint string, handler http.HandlerFunc) http.HandlerFunc {
	limiter := h.manager.RateLimiter()
	if limiter == nil {
		return handler
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(endpoint, h.limitKey(r)) {
			// Retry-After is what makes a 429 actionable for a client that
			// respects it, and for one that does not it costs nothing.
			w.Header().Set("Retry-After", "60")
			h.writeError(w, http.StatusTooManyRequests, "rate_limited",
				"Too many requests to "+endpoint)
			return
		}
		handler(w, r)
	}
}

// limitKey is what the limit is counted per.
//
// The address is always part of it. A user can be added, but only where there
// is one to read: a login carries its identity in a body that has not been
// parsed yet, so counting those per user would mean counting them per nothing.
func (h *Handler) limitKey(r *http.Request) string {
	ip := getClientIP(r)

	limiter := h.manager.RateLimiter()
	if limiter == nil || limiter.config == nil {
		return ip
	}

	switch strings.ToLower(limiter.config.KeyBy) {
	case "user", "ip+user":
		if _, claims, err := h.manager.ValidateToken(r.Context(), ExtractTokenFromHeader(r.Header.Get("Authorization"))); err == nil && claims != nil {
			return ip + "|" + claims.UserID
		}
	}
	return ip
}
