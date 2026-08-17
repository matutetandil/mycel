// Package rest provides authentication middleware for REST connectors.
package rest

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/matutetandil/mycel/v2/internal/identity"
	"github.com/matutetandil/mycel/v2/internal/jwks"
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

	// Dynamic validation: validate API key against a database or service
	// If set, Keys list is ignored and validation is done via this function
	ValidateFunc APIKeyValidateFunc

	// For HCL config: connector name and query for dynamic validation
	ValidateConnector string // e.g., "connector.db"
	ValidateQuery     string // e.g., "SELECT 1 FROM api_keys WHERE key = :key AND active = true"
}

// APIKeyValidateFunc is a function type for validating API keys dynamically.
// Returns (valid, userID, metadata, error)
type APIKeyValidateFunc func(ctx context.Context, apiKey string) (bool, string, map[string]interface{}, error)

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
	N   string `json:"n"`   // RSA modulus
	E   string `json:"e"`   // RSA exponent
	X   string `json:"x"`   // EC x coordinate
	Y   string `json:"y"`   // EC y coordinate
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

// Authenticator checks a request against an AuthConfig.
//
// This was six methods on the REST connector, which meant the only way to have
// a request checked the way a rest connector checks one was to be a rest
// connector. The workflow API needs exactly that — the same block, the same
// words, the same validators — so it lives on its own here and the connector
// delegates to it. Nothing about how a REST connector authenticates changed.
type Authenticator struct {
	config *AuthConfig
	logger *slog.Logger
	mu     sync.RWMutex
}

// NewAuthenticator returns something that can check requests against cfg.
func NewAuthenticator(cfg *AuthConfig, logger *slog.Logger) *Authenticator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Authenticator{config: cfg, logger: logger}
}

// Config returns what this checks against.
func (a *Authenticator) Config() *AuthConfig { return a.config }

func (a *Authenticator) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			a.logger.Error("Failed to encode response", slog.Any("error", err))
		}
	}
}

// SetAuthConfig sets the authentication configuration for this connector.
func (c *Connector) SetAuthConfig(cfg *AuthConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authConfig = cfg
	c.authenticator = NewAuthenticator(cfg, c.logger)
}

// authMiddleware checks a request the way this connector was configured to.
//
// No locking here: Start already holds c.mu when it wraps the handler, and the
// mutex is not reentrant — taking it again deadlocked the whole startup, with
// every connector after this one in the map never starting and the admin
// server never coming up. The authenticator is set by SetAuthConfig before
// Start and not written afterwards, so there is nothing to guard.
func (c *Connector) authMiddleware(next http.Handler) http.Handler {
	if c.authenticator == nil {
		if c.authConfig == nil {
			return next
		}
		// A connector whose config was set without going through
		// SetAuthConfig still gets checked.
		c.authenticator = NewAuthenticator(c.authConfig, c.logger)
	}
	return c.authenticator.Middleware(next)
}

// authMiddleware validates incoming requests based on auth configuration.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add custom response headers
		if a.config.ResponseHeaders != nil {
			for k, v := range a.config.ResponseHeaders {
				w.Header().Set(k, v)
			}
		}

		// A public path is exempt from all of it, headers included.
		//
		// The required headers used to be checked first, so a path listed as
		// public — the documented example is /health — was still refused with
		// 400 unless the caller sent them. A load balancer's health probe
		// sends no headers of its own, so the instance read as unhealthy and
		// was taken out of rotation, which is a long way from a header list
		// in an auth block.
		if a.isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Check required headers
		if len(a.config.RequiredHeaders) > 0 {
			for _, header := range a.config.RequiredHeaders {
				if r.Header.Get(header) == "" {
					a.writeJSON(w, http.StatusBadRequest, map[string]string{
						"error": fmt.Sprintf("missing required header: %s", header),
					})
					return
				}
			}
		}

		// Skip auth if no auth type configured
		if a.config.Type == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate based on auth type
		var authCtx *AuthContext
		var err error

		switch a.config.Type {
		case "jwt":
			authCtx, err = a.validateJWT(r)
		case "api_key":
			authCtx, err = a.validateAPIKey(r)
		case "basic":
			authCtx, err = a.validateBasic(r, w)
		default:
			err = fmt.Errorf("unknown auth type: %s", a.config.Type)
		}

		if err != nil {
			a.logger.Warn("authentication failed",
				"path", r.URL.Path,
				"method", r.Method,
				"error", err.Error(),
			)

			status := http.StatusUnauthorized
			if a.config.Type == "basic" && a.config.Basic != nil {
				realm := a.config.Basic.Realm
				if realm == "" {
					realm = "Restricted"
				}
				w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
			}

			a.writeJSON(w, status, map[string]string{
				"error": "unauthorized",
			})
			return
		}

		// Add auth context to request, and publish the identity in the shape
		// expressions read. The context above was written and never read by
		// anything; without this, `auth.user_id` in a flow could not compile,
		// let alone resolve.
		ctx := context.WithValue(r.Context(), authContextKey, authCtx)
		ctx = identity.With(ctx, authCtx.identity())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicPath checks if the path is in the public paths list.
func (a *Authenticator) isPublicPath(path string) bool {
	if a.config == nil || len(a.config.Public) == 0 {
		return false
	}

	for _, publicPath := range a.config.Public {
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
func (a *Authenticator) validateJWT(r *http.Request) (*AuthContext, error) {
	cfg := a.config.JWT
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
			return a.getJWKSKey(token)

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

// getJWKSKey finds the key a token was signed with.
//
// The set is fetched once and kept, since it changes rarely and fetching it
// per request would put an HTTP call in front of every authenticated one. But
// issuers do rotate: a token arrives signed with a key that was published
// after the set was cached, and its identifier is not in what we hold. Kept
// for ever, that would refuse every request from the moment of a rotation
// until somebody restarted the service. So an unknown identifier is a reason
// to look again — once, and only then.
func (a *Authenticator) getJWKSKey(token *jwt.Token) (interface{}, error) {
	cfg := a.config.JWT

	// The identifier first: a token that names no key cannot be matched
	// against any set, so there is nothing to fetch for it.
	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, fmt.Errorf("the token names no signing key, so it cannot be checked against %s", cfg.JWKSURL)
	}

	if cfg.jwks == nil {
		jwks, err := fetchJWKS(cfg.JWKSURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
		}
		cfg.jwks = jwks
	}

	if key, found := findKey(cfg.jwks, kid); found {
		return parseJWK(key)
	}

	// Not in what we hold. The issuer may have rotated since.
	refreshed, err := fetchJWKS(cfg.JWKSURL)
	if err != nil {
		return nil, fmt.Errorf("token names key %q, which is not in the set we hold, and it could not be fetched again: %w", kid, err)
	}
	cfg.jwks = refreshed

	if key, found := findKey(refreshed, kid); found {
		return parseJWK(key)
	}

	return nil, fmt.Errorf("no key named %q is published at %s", kid, cfg.JWKSURL)
}

// findKey looks up one key of a set by its identifier.
func findKey(jwks *JWKS, kid string) (JWK, bool) {
	if jwks == nil {
		return JWK{}, false
	}
	for _, key := range jwks.Keys {
		if key.Kid == kid {
			return key, true
		}
	}
	return JWK{}, false
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

// parseRSAPublicKey and parseECPublicKey build keys through internal/jwks,
// which both connectors share — the defect this replaces existed in each of
// them separately, written the same wrong way twice.
func parseRSAPublicKey(key JWK) (interface{}, error) {
	return jwks.PublicKey(asJWKSKey(key))
}

func parseECPublicKey(key JWK) (interface{}, error) {
	return jwks.PublicKey(asJWKSKey(key))
}

// asJWKSKey is this connector's JWK in the shared shape.
func asJWKSKey(key JWK) jwks.Key {
	return jwks.Key{
		Kty: key.Kty, Kid: key.Kid, Use: key.Use, Alg: key.Alg,
		N: key.N, E: key.E, X: key.X, Y: key.Y, Crv: key.Crv,
	}
}

// validateAPIKey validates an API key from the request.
func (a *Authenticator) validateAPIKey(r *http.Request) (*AuthContext, error) {
	cfg := a.config.APIKey
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

	// If dynamic validation function is configured, use it
	if cfg.ValidateFunc != nil {
		valid, userID, metadata, err := cfg.ValidateFunc(r.Context(), apiKey)
		if err != nil {
			a.logger.Error("API key validation error", "error", err)
			return nil, fmt.Errorf("API key validation failed")
		}
		if !valid {
			return nil, fmt.Errorf("invalid API key")
		}

		authCtx := &AuthContext{
			Authenticated: true,
			APIKey:        apiKey,
			UserID:        userID,
		}
		// Store metadata in claims for access
		if metadata != nil {
			authCtx.Claims = metadata
		}
		return authCtx, nil
	}

	// Validate against static keys list
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

// CreateAPIKeyValidator creates an APIKeyValidateFunc from a database connector.
// The query should return at least one row if the key is valid.
// Optional columns: user_id, and any additional columns will be added to metadata.
func CreateAPIKeyValidator(reader interface {
	Read(ctx context.Context, query interface{}) (interface{}, error)
}, queryTemplate string) APIKeyValidateFunc {
	return func(ctx context.Context, apiKey string) (bool, string, map[string]interface{}, error) {
		// Replace :key placeholder with actual key
		query := strings.ReplaceAll(queryTemplate, ":key", apiKey)
		query = strings.ReplaceAll(query, "${key}", apiKey)

		result, err := reader.Read(ctx, query)
		if err != nil {
			return false, "", nil, err
		}

		// Check if we got results
		if result == nil {
			return false, "", nil, nil
		}

		// Try to extract rows from result
		var rows []map[string]interface{}
		switch r := result.(type) {
		case *struct {
			Rows     []map[string]interface{}
			Affected int64
		}:
			rows = r.Rows
		case map[string]interface{}:
			if rowsData, ok := r["rows"].([]map[string]interface{}); ok {
				rows = rowsData
			} else if rowsData, ok := r["Rows"].([]map[string]interface{}); ok {
				rows = rowsData
			}
		}

		if len(rows) == 0 {
			return false, "", nil, nil
		}

		// Key is valid - extract user_id and metadata
		firstRow := rows[0]
		var userID string
		metadata := make(map[string]interface{})

		for k, v := range firstRow {
			switch strings.ToLower(k) {
			case "user_id", "userid", "user":
				if s, ok := v.(string); ok {
					userID = s
				} else if s, ok := v.(fmt.Stringer); ok {
					userID = s.String()
				}
			default:
				metadata[k] = v
			}
		}

		return true, userID, metadata, nil
	}
}

// validateBasic validates Basic auth credentials.
func (a *Authenticator) validateBasic(r *http.Request, w http.ResponseWriter) (*AuthContext, error) {
	cfg := a.config.Basic
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

// identity renders what was learned about the caller in the shape the rest of
// the service reads it, so that a flow can say auth.user_id without knowing
// which of the three credential types answered.
func (a *AuthContext) identity() *identity.Identity {
	if a == nil || !a.Authenticated {
		return nil
	}

	id := &identity.Identity{
		UserID: a.UserID,
		Claims: a.Claims,
	}
	if id.UserID == "" {
		// Basic auth has no subject beyond the name it authenticated.
		id.UserID = a.Username
	}
	if a.Claims != nil {
		if email, ok := a.Claims["email"].(string); ok {
			id.Email = email
		}
		id.Roles = claimStrings(a.Claims, "roles")
	}
	return id
}

// claimStrings reads a claim that carries a list of strings, which arrives from
// JSON as a list of anything.
func claimStrings(claims map[string]interface{}, name string) []string {
	raw, ok := claims[name]
	if !ok {
		return nil
	}

	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		// A single role is a common enough shape to accept.
		return []string{v}
	}
	return nil
}
