package auth

import (
	"context"
	"net/http"
	"strings"
)

// Context keys for auth data
type contextKey string

const (
	// UserContextKey is the context key for the authenticated user
	UserContextKey contextKey = "auth_user"
	// ClaimsContextKey is the context key for the JWT claims
	ClaimsContextKey contextKey = "auth_claims"
)

// Middleware provides authentication middleware
type Middleware struct {
	manager *Manager
	config  *MiddlewareConfig
}

// MiddlewareConfig configures the auth middleware
type MiddlewareConfig struct {
	// Required indicates if authentication is required
	Required bool

	// Exclude paths that don't require authentication
	Exclude []string

	// Rules for path-specific authorization
	Rules map[string]*PathRule
}

// PathRule defines authorization rules for a path
type PathRule struct {
	Roles       []string
	Permissions []string
}

// NewMiddleware creates a new auth middleware
func NewMiddleware(manager *Manager, config *MiddlewareConfig) *Middleware {
	if config == nil {
		config = &MiddlewareConfig{
			Required: true,
		}
	}
	return &Middleware{
		manager: manager,
		config:  config,
	}
}

// Handler returns an HTTP middleware handler
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path is excluded
		if m.isExcluded(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from header
		token := ExtractTokenFromHeader(r.Header.Get("Authorization"))

		if token == "" {
			if m.config.Required {
				writeUnauthorized(w, "Missing authorization token")
				return
			}
			// Not required, continue without auth
			next.ServeHTTP(w, r)
			return
		}

		// Validate token
		user, claims, err := m.manager.ValidateToken(r.Context(), token)
		if err != nil {
			writeUnauthorized(w, "Invalid or expired token")
			return
		}

		// Check path-specific rules
		if rule := m.getRule(r.URL.Path); rule != nil {
			if !m.checkAuthorization(user, claims, rule) {
				writeForbidden(w, "Insufficient permissions")
				return
			}
		}

		// Add user and claims to context
		ctx := context.WithValue(r.Context(), UserContextKey, user)
		ctx = context.WithValue(ctx, ClaimsContextKey, claims)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HandlerFunc returns an HTTP middleware handler function
func (m *Middleware) HandlerFunc(next http.HandlerFunc) http.HandlerFunc {
	return m.Handler(next).ServeHTTP
}

// isExcluded checks if a path is excluded from authentication
func (m *Middleware) isExcluded(path string) bool {
	for _, pattern := range m.config.Exclude {
		if matchPath(pattern, path) {
			return true
		}
	}
	return false
}

// getRule gets the authorization rule for a path
func (m *Middleware) getRule(path string) *PathRule {
	for pattern, rule := range m.config.Rules {
		if matchPath(pattern, path) {
			return rule
		}
	}
	return nil
}

// checkAuthorization checks if the user meets the authorization requirements
func (m *Middleware) checkAuthorization(user *User, claims *Claims, rule *PathRule) bool {
	// Check roles
	if len(rule.Roles) > 0 {
		hasRole := false
		for _, required := range rule.Roles {
			for _, userRole := range claims.Roles {
				if userRole == required {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}
		if !hasRole {
			return false
		}
	}

	// Check permissions
	if len(rule.Permissions) > 0 {
		hasPerm := false
		for _, required := range rule.Permissions {
			for _, userPerm := range claims.Permissions {
				if userPerm == required {
					hasPerm = true
					break
				}
			}
			if hasPerm {
				break
			}
		}
		if !hasPerm {
			return false
		}
	}

	return true
}

// matchPath checks if a path matches a pattern
// Supports * for single segment and ** for multiple segments
func matchPath(pattern, path string) bool {
	// Exact match
	if pattern == path {
		return true
	}

	// Handle wildcard patterns
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(path, prefix+"/")
	}

	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(path, prefix+"/")
	}

	// Handle glob patterns with *
	if strings.Contains(pattern, "*") {
		patternParts := strings.Split(pattern, "/")
		pathParts := strings.Split(path, "/")

		if len(patternParts) != len(pathParts) && !strings.Contains(pattern, "**") {
			return false
		}

		j := 0
		for i := 0; i < len(patternParts) && j < len(pathParts); i++ {
			if patternParts[i] == "**" {
				// Match rest of path
				return true
			}
			if patternParts[i] == "*" {
				j++
				continue
			}
			if patternParts[i] != pathParts[j] {
				return false
			}
			j++
		}

		return j == len(pathParts)
	}

	return false
}

// GetUser extracts the user from the request context
func GetUser(ctx context.Context) *User {
	if user, ok := ctx.Value(UserContextKey).(*User); ok {
		return user
	}
	return nil
}

// GetClaims extracts the claims from the request context
func GetClaims(ctx context.Context) *Claims {
	if claims, ok := ctx.Value(ClaimsContextKey).(*Claims); ok {
		return claims
	}
	return nil
}

// RequireAuth is a simple middleware that requires authentication
func RequireAuth(manager *Manager) func(http.Handler) http.Handler {
	m := NewMiddleware(manager, &MiddlewareConfig{Required: true})
	return m.Handler
}

// OptionalAuth is a middleware that allows optional authentication
func OptionalAuth(manager *Manager) func(http.Handler) http.Handler {
	m := NewMiddleware(manager, &MiddlewareConfig{Required: false})
	return m.Handler
}

// RequireRoles is a middleware that requires specific roles
func RequireRoles(manager *Manager, roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r.Context())
			if claims == nil {
				writeUnauthorized(w, "Authentication required")
				return
			}

			hasRole := false
			for _, required := range roles {
				for _, userRole := range claims.Roles {
					if userRole == required {
						hasRole = true
						break
					}
				}
				if hasRole {
					break
				}
			}

			if !hasRole {
				writeForbidden(w, "Insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermissions is a middleware that requires specific permissions
func RequirePermissions(manager *Manager, permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r.Context())
			if claims == nil {
				writeUnauthorized(w, "Authentication required")
				return
			}

			hasPerm := false
			for _, required := range permissions {
				for _, userPerm := range claims.Permissions {
					if userPerm == required {
						hasPerm = true
						break
					}
				}
				if hasPerm {
					break
				}
			}

			if !hasPerm {
				writeForbidden(w, "Insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":{"code":"unauthorized","message":"` + message + `"}}`))
}

func writeForbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":{"code":"forbidden","message":"` + message + `"}}`))
}
