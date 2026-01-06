// Package rest provides authentication middleware for REST connectors.
package rest

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AuthConfig holds authentication configuration for the REST server.
type AuthConfig struct {
	// Type of authentication: jwt, api_key, basic
	Type string

	// JWT configuration (for validating incoming JWTs)
	JWT *JWTAuthConfig

	// API Key configuration
	APIKey *APIKeyAuthConfig

	// Basic auth configuration
	Basic *BasicAuthConfig

	// Public paths that don't require authentication
	Public []string

	// Required headers that must be present
	RequiredHeaders []string

	// Custom response headers to add to all responses
	ResponseHeaders map[string]string
}

// JWTAuthConfig holds JWT validation configuration.
type JWTAuthConfig struct {
	// Secret for HS256/HS384/HS512 algorithms
	Secret string

	// JWKS URL for RS256/ES256 algorithms (fetches public keys)
	JWKSURL string

	// Expected issuer
	Issuer string

	// Expected audience
	Audience []string

	// Allowed algorithms (default: HS256)
	Algorithms []string

	// Header name (default: Authorization)
	Header string

	// Token scheme (default: Bearer)
	Scheme string

	// Clock skew tolerance (default: 5s)
	ClockSkew time.Duration

	// Cached JWKS (fetched from JWKSURL)
	jwks *JWKS
}

// APIKeyAuthConfig holds API key validation configuration.
type APIKeyAuthConfig struct {
	// Static list of valid API keys
	Keys []string

	// Header name to check (default: X-API-Key)
	Header string

	// Query parameter name to check (alternative to header)
	QueryParam string

	// Hash keys before comparison (for security)
	HashKeys bool
}

// BasicAuthConfig holds Basic auth validation configuration.
type BasicAuthConfig struct {
	// Static map of username -> password
	Users map[string]string

	// Realm for WWW-Authenticate header
	Realm string
}

// JWKS represents a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"` // RSA modulus
	E   string `json:"e"` // RSA exponent
	X   string `json:"x"` // EC x coordinate
	Y   string `json:"y"` // EC y coordinate
	Crv string `json:"crv"` // EC curve
}

// AuthContext holds authentication information for the request.
type AuthContext struct {
	// Authenticated indicates if the request is authenticated
	Authenticated bool

	// UserID extracted from token (if available)
	UserID string

	// Claims from JWT (if JWT auth)
	Claims map[string]interface{}

	// APIKey used (if API key auth)
	APIKey string

	// Username (if Basic auth)
	Username string
}

// contextKey is a type for context keys.
type contextKey string

const authContextKey contextKey = "auth"

// GetAuthContext retrieves authentication context from the request.
func GetAuthContext(ctx context.Context) *AuthContext {
	if auth, ok := ctx.Value(authContextKey).(*AuthContext); ok {
		return auth
	}
	return nil
}

// SetAuthConfig sets the authentication configuration for this connector.
func (c *Connector) SetAuthConfig(cfg *AuthConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authConfig = cfg
}

// authMiddleware validates incoming requests based on auth configuration.
func (c *Connector) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add custom response headers
		if c.authConfig.ResponseHeaders != nil {
			for k, v := range c.authConfig.ResponseHeaders {
				w.Header().Set(k, v)
			}
		}

		// Check required headers
		if len(c.authConfig.RequiredHeaders) > 0 {
			for _, header := range c.authConfig.RequiredHeaders {
				if r.Header.Get(header) == "" {
					c.writeJSON(w, http.StatusBadRequest, map[string]string{
						"error": fmt.Sprintf("missing required header: %s", header),
					})
					return
				}
			}
		}

		// Check if path is public
		if c.isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth if no auth type configured
		if c.authConfig.Type == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate based on auth type
		var authCtx *AuthContext
		var err error

		switch c.authConfig.Type {
		case "jwt":
			authCtx, err = c.validateJWT(r)
		case "api_key":
			authCtx, err = c.validateAPIKey(r)
		case "basic":
			authCtx, err = c.validateBasic(r, w)
		default:
			err = fmt.Errorf("unknown auth type: %s", c.authConfig.Type)
		}

		if err != nil {
			c.logger.Warn("authentication failed",
				"path", r.URL.Path,
				"method", r.Method,
				"error", err.Error(),
			)

			status := http.StatusUnauthorized
			if c.authConfig.Type == "basic" && c.authConfig.Basic != nil {
				realm := c.authConfig.Basic.Realm
				if realm == "" {
					realm = "Restricted"
				}
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
			}

			c.writeJSON(w, status, map[string]string{
				"error": "unauthorized",
			})
			return
		}

		// Add auth context to request
		ctx := context.WithValue(r.Context(), authContextKey, authCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicPath checks if the path is in the public paths list.
func (c *Connector) isPublicPath(path string) bool {
	if c.authConfig == nil || len(c.authConfig.Public) == 0 {
		return false
	}

	for _, publicPath := range c.authConfig.Public {
		if publicPath == path {
			return true
		}
		// Simple wildcard support: /path/* matches /path/anything
		if strings.HasSuffix(publicPath, "/*") {
			prefix := strings.TrimSuffix(publicPath, "/*")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}

	return false
}

// validateJWT validates a JWT token from the request.
func (c *Connector) validateJWT(r *http.Request) (*AuthContext, error) {
	cfg := c.authConfig.JWT
	if cfg == nil {
		return nil, fmt.Errorf("JWT configuration not set")
	}

	// Get header name
	headerName := cfg.Header
	if headerName == "" {
		headerName = "Authorization"
	}

	// Get token from header
	authHeader := r.Header.Get(headerName)
	if authHeader == "" {
		return nil, fmt.Errorf("missing authorization header")
	}

	// Get scheme
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "Bearer"
	}

	// Extract token
	var tokenString string
	if strings.HasPrefix(authHeader, scheme+" ") {
		tokenString = strings.TrimPrefix(authHeader, scheme+" ")
	} else {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	// Parse and validate token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Validate algorithm
		alg := token.Method.Alg()
		if len(cfg.Algorithms) > 0 {
			valid := false
			for _, allowed := range cfg.Algorithms {
				if alg == allowed {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("unexpected signing method: %s", alg)
			}
		}

		// Return secret/key based on algorithm
		switch {
		case strings.HasPrefix(alg, "HS"):
			if cfg.Secret == "" {
				return nil, fmt.Errorf("JWT secret not configured")
			}
			return []byte(cfg.Secret), nil

		case strings.HasPrefix(alg, "RS"), strings.HasPrefix(alg, "ES"):
			if cfg.JWKSURL == "" {
				return nil, fmt.Errorf("JWKS URL not configured for %s algorithm", alg)
			}
			return c.getJWKSKey(token)

		default:
			return nil, fmt.Errorf("unsupported algorithm: %s", alg)
		}
	}, jwt.WithLeeway(cfg.ClockSkew))

	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	// Validate claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate issuer
	if cfg.Issuer != "" {
		iss, _ := claims["iss"].(string)
		if iss != cfg.Issuer {
			return nil, fmt.Errorf("invalid issuer")
		}
	}

	// Validate audience
	if len(cfg.Audience) > 0 {
		aud := getAudience(claims)
		found := false
		for _, expected := range cfg.Audience {
			for _, actual := range aud {
				if actual == expected {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("invalid audience")
		}
	}

	// Build auth context
	authCtx := &AuthContext{
		Authenticated: true,
		Claims:        claims,
	}

	// Extract user ID from sub claim
	if sub, ok := claims["sub"].(string); ok {
		authCtx.UserID = sub
	}

	return authCtx, nil
}

// getAudience extracts audience from claims (can be string or []string).
func getAudience(claims jwt.MapClaims) []string {
	aud, ok := claims["aud"]
	if !ok {
		return nil
	}

	switch v := aud.(type) {
	case string:
		return []string{v}
	case []interface{}:
		var result []string
		for _, a := range v {
			if s, ok := a.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// getJWKSKey fetches the appropriate key from JWKS.
func (c *Connector) getJWKSKey(token *jwt.Token) (interface{}, error) {
	cfg := c.authConfig.JWT

	// Fetch JWKS if not cached
	if cfg.jwks == nil {
		jwks, err := fetchJWKS(cfg.JWKSURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
		}
		cfg.jwks = jwks
	}

	// Find key by kid
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("token missing kid header")
	}

	for _, key := range cfg.jwks.Keys {
		if key.Kid == kid {
			return parseJWK(key)
		}
	}

	return nil, fmt.Errorf("key not found in JWKS: %s", kid)
}

// fetchJWKS fetches a JWKS from a URL.
func fetchJWKS(url string) (*JWKS, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS fetch failed with status: %d", resp.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}

	return &jwks, nil
}

// parseJWK converts a JWK to a crypto key.
func parseJWK(key JWK) (interface{}, error) {
	switch key.Kty {
	case "RSA":
		return parseRSAPublicKey(key)
	case "EC":
		return parseECPublicKey(key)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", key.Kty)
	}
}

// parseRSAPublicKey parses an RSA public key from JWK.
func parseRSAPublicKey(key JWK) (interface{}, error) {
	// Decode n and e from base64url
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("invalid RSA n: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("invalid RSA e: %w", err)
	}

	// Convert e to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &struct {
		N []byte
		E int
	}{N: nBytes, E: e}, nil
}

// parseECPublicKey parses an EC public key from JWK.
func parseECPublicKey(key JWK) (interface{}, error) {
	// For EC keys, we need to use crypto/ecdsa
	// This is a simplified implementation
	xBytes, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, fmt.Errorf("invalid EC x: %w", err)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(key.Y)
	if err != nil {
		return nil, fmt.Errorf("invalid EC y: %w", err)
	}

	return &struct {
		Curve string
		X     []byte
		Y     []byte
	}{Curve: key.Crv, X: xBytes, Y: yBytes}, nil
}

// validateAPIKey validates an API key from the request.
func (c *Connector) validateAPIKey(r *http.Request) (*AuthContext, error) {
	cfg := c.authConfig.APIKey
	if cfg == nil {
		return nil, fmt.Errorf("API key configuration not set")
	}

	// Get API key from header or query
	var apiKey string

	header := cfg.Header
	if header == "" {
		header = "X-API-Key"
	}

	apiKey = r.Header.Get(header)
	if apiKey == "" && cfg.QueryParam != "" {
		apiKey = r.URL.Query().Get(cfg.QueryParam)
	}

	if apiKey == "" {
		return nil, fmt.Errorf("missing API key")
	}

	// Validate against known keys
	valid := false
	for _, key := range cfg.Keys {
		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(key)) == 1 {
			valid = true
			break
		}
	}

	if !valid {
		return nil, fmt.Errorf("invalid API key")
	}

	return &AuthContext{
		Authenticated: true,
		APIKey:        apiKey,
	}, nil
}

// validateBasic validates Basic auth credentials.
func (c *Connector) validateBasic(r *http.Request, w http.ResponseWriter) (*AuthContext, error) {
	cfg := c.authConfig.Basic
	if cfg == nil {
		return nil, fmt.Errorf("Basic auth configuration not set")
	}

	// Get Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("missing authorization header")
	}

	// Parse Basic auth
	if !strings.HasPrefix(authHeader, "Basic ") {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHeader, "Basic "))
	if err != nil {
		return nil, fmt.Errorf("invalid base64 encoding")
	}

	pair := strings.SplitN(string(payload), ":", 2)
	if len(pair) != 2 {
		return nil, fmt.Errorf("invalid credentials format")
	}

	username, password := pair[0], pair[1]

	// Validate credentials
	expectedPassword, ok := cfg.Users[username]
	if !ok {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Use constant-time comparison
	if subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) != 1 {
		return nil, fmt.Errorf("invalid credentials")
	}

	return &AuthContext{
		Authenticated: true,
		Username:      username,
	}, nil
}
