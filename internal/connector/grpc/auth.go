package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// AuthContext holds authentication information for a request.
type AuthContext struct {
	Authenticated bool
	UserID        string
	Claims        map[string]interface{}
	Method        string // jwt, api_key, mtls
	ClientCert    *x509.Certificate
}

// authContextKey is the context key for auth context.
type authContextKey struct{}

// GetAuthContext retrieves auth context from a gRPC context.
func GetAuthContext(ctx context.Context) *AuthContext {
	if v := ctx.Value(authContextKey{}); v != nil {
		if ac, ok := v.(*AuthContext); ok {
			return ac
		}
	}
	return nil
}

// withAuthContext adds auth context to a gRPC context.
func withAuthContext(ctx context.Context, ac *AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, ac)
}

// AuthInterceptor creates authentication interceptors for the server.
type AuthInterceptor struct {
	config    *AuthConfig
	jwksCache *jwksCache
}

// NewAuthInterceptor creates a new auth interceptor.
func NewAuthInterceptor(config *AuthConfig) *AuthInterceptor {
	return &AuthInterceptor{
		config:    config,
		jwksCache: &jwksCache{},
	}
}

// UnaryInterceptor returns a unary server interceptor for authentication.
func (a *AuthInterceptor) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Check if method is public
		if a.isPublicMethod(info.FullMethod) {
			return handler(ctx, req)
		}

		// Authenticate request
		authCtx, err := a.authenticate(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
		}

		// Add auth context
		ctx = withAuthContext(ctx, authCtx)

		return handler(ctx, req)
	}
}

// StreamInterceptor returns a stream server interceptor for authentication.
func (a *AuthInterceptor) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Check if method is public
		if a.isPublicMethod(info.FullMethod) {
			return handler(srv, ss)
		}

		// Authenticate request
		authCtx, err := a.authenticate(ss.Context())
		if err != nil {
			return status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
		}

		// Wrap stream with auth context
		wrapped := &wrappedServerStream{
			ServerStream: ss,
			ctx:          withAuthContext(ss.Context(), authCtx),
		}

		return handler(srv, wrapped)
	}
}

// wrappedServerStream wraps a ServerStream with a custom context.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the wrapped context.
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// isPublicMethod checks if a method is in the public list.
func (a *AuthInterceptor) isPublicMethod(fullMethod string) bool {
	if a.config == nil || len(a.config.Public) == 0 {
		return false
	}

	for _, pub := range a.config.Public {
		// Exact match
		if pub == fullMethod {
			return true
		}
		// Wildcard match (e.g., "/package.Service/*")
		if strings.HasSuffix(pub, "/*") {
			prefix := strings.TrimSuffix(pub, "/*")
			if strings.HasPrefix(fullMethod, prefix) {
				return true
			}
		}
	}
	return false
}

// authenticate authenticates a request based on configuration.
func (a *AuthInterceptor) authenticate(ctx context.Context) (*AuthContext, error) {
	if a.config == nil || a.config.Type == "" || a.config.Type == "none" {
		return &AuthContext{Authenticated: true}, nil
	}

	switch a.config.Type {
	case "jwt":
		return a.authenticateJWT(ctx)
	case "api_key":
		return a.authenticateAPIKey(ctx)
	case "mtls":
		return a.authenticateMTLS(ctx)
	default:
		return nil, fmt.Errorf("unknown auth type: %s", a.config.Type)
	}
}

// authenticateJWT validates JWT tokens from metadata.
func (a *AuthInterceptor) authenticateJWT(ctx context.Context) (*AuthContext, error) {
	if a.config.JWT == nil {
		return nil, fmt.Errorf("JWT configuration missing")
	}

	// Get token from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing metadata")
	}

	// Try authorization header first
	auth := md.Get("authorization")
	if len(auth) == 0 {
		// Also check grpcgateway-authorization for grpc-gateway
		auth = md.Get("grpcgateway-authorization")
	}

	if len(auth) == 0 {
		return nil, fmt.Errorf("missing authorization header")
	}

	// Extract bearer token
	token := auth[0]
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = token[7:]
	}

	// Parse and validate token
	claims, err := a.validateJWTToken(token)
	if err != nil {
		return nil, err
	}

	authCtx := &AuthContext{
		Authenticated: true,
		Method:        "jwt",
		Claims:        claims,
	}

	// Extract user ID from common claims
	if sub, ok := claims["sub"].(string); ok {
		authCtx.UserID = sub
	}

	return authCtx, nil
}

// validateJWTToken validates a JWT token string.
func (a *AuthInterceptor) validateJWTToken(tokenString string) (map[string]interface{}, error) {
	cfg := a.config.JWT

	// Build parser options
	var parserOpts []jwt.ParserOption
	if cfg.Issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(cfg.Issuer))
	}
	if len(cfg.Audience) > 0 {
		parserOpts = append(parserOpts, jwt.WithAudience(cfg.Audience[0]))
	}

	// Parse token
	parser := jwt.NewParser(parserOpts...)
	token, err := parser.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
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

		// Get verification key
		switch {
		case strings.HasPrefix(alg, "HS"):
			if cfg.Secret == "" {
				return nil, fmt.Errorf("secret required for %s", alg)
			}
			return []byte(cfg.Secret), nil

		case strings.HasPrefix(alg, "RS"), strings.HasPrefix(alg, "ES"):
			if cfg.JWKSURL == "" {
				return nil, fmt.Errorf("JWKS URL required for %s", alg)
			}
			return a.getJWKSKey(token)

		default:
			return nil, fmt.Errorf("unsupported algorithm: %s", alg)
		}
	})

	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims format")
	}

	return claims, nil
}

// getJWKSKey fetches the verification key from JWKS endpoint.
func (a *AuthInterceptor) getJWKSKey(token *jwt.Token) (interface{}, error) {
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("missing key ID in token header")
	}

	return a.jwksCache.GetKey(a.config.JWT.JWKSURL, kid)
}

// authenticateAPIKey validates API keys from metadata.
func (a *AuthInterceptor) authenticateAPIKey(ctx context.Context) (*AuthContext, error) {
	if a.config.APIKey == nil || len(a.config.APIKey.Keys) == 0 {
		return nil, fmt.Errorf("API key configuration missing")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing metadata")
	}

	// Determine metadata key
	metaKey := a.config.APIKey.Metadata
	if metaKey == "" {
		metaKey = "api-key"
	}

	// Also check header name (for grpc-gateway)
	headerKey := a.config.APIKey.Header
	if headerKey == "" {
		headerKey = "x-api-key"
	}

	// Try metadata key first
	keys := md.Get(metaKey)
	if len(keys) == 0 {
		// Try header name (lowercase for gRPC metadata)
		keys = md.Get(strings.ToLower(headerKey))
	}
	if len(keys) == 0 {
		// Try grpcgateway prefix
		keys = md.Get("grpcgateway-" + strings.ToLower(headerKey))
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("missing API key")
	}

	// Validate key
	apiKey := keys[0]
	valid := false
	for _, k := range a.config.APIKey.Keys {
		if k == apiKey {
			valid = true
			break
		}
	}

	if !valid {
		return nil, fmt.Errorf("invalid API key")
	}

	return &AuthContext{
		Authenticated: true,
		Method:        "api_key",
	}, nil
}

// authenticateMTLS validates client certificates.
func (a *AuthInterceptor) authenticateMTLS(ctx context.Context) (*AuthContext, error) {
	// Get peer info
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no peer info")
	}

	// Get TLS info
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil, fmt.Errorf("no TLS connection info")
	}

	// Check for client certificate
	if len(tlsInfo.State.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no client certificate provided")
	}

	cert := tlsInfo.State.PeerCertificates[0]

	// Verify certificate is valid (TLS already verified the chain)
	if time.Now().After(cert.NotAfter) {
		return nil, fmt.Errorf("client certificate expired")
	}
	if time.Now().Before(cert.NotBefore) {
		return nil, fmt.Errorf("client certificate not yet valid")
	}

	return &AuthContext{
		Authenticated: true,
		Method:        "mtls",
		UserID:        cert.Subject.CommonName,
		ClientCert:    cert,
		Claims: map[string]interface{}{
			"cn":           cert.Subject.CommonName,
			"organization": cert.Subject.Organization,
			"serial":       cert.SerialNumber.String(),
		},
	}, nil
}

// jwksCache caches JWKS keys.
type jwksCache struct {
	mu      sync.RWMutex
	keys    map[string]interface{}
	url     string
	expires time.Time
}

// GetKey gets a key from the cache or fetches from JWKS URL.
func (c *jwksCache) GetKey(url, kid string) (interface{}, error) {
	c.mu.RLock()
	if c.url == url && time.Now().Before(c.expires) {
		if key, ok := c.keys[kid]; ok {
			c.mu.RUnlock()
			return key, nil
		}
	}
	c.mu.RUnlock()

	// Fetch JWKS
	if err := c.fetchJWKS(url); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if key, ok := c.keys[kid]; ok {
		return key, nil
	}

	return nil, fmt.Errorf("key %s not found in JWKS", kid)
}

// fetchJWKS fetches and caches JWKS keys.
func (c *jwksCache) fetchJWKS(url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if recently fetched
	if c.url == url && time.Now().Before(c.expires) {
		return nil
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS request failed: %s", resp.Status)
	}

	var jwks struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("failed to decode JWKS: %w", err)
	}

	c.keys = make(map[string]interface{})
	c.url = url
	c.expires = time.Now().Add(5 * time.Minute)

	for _, keyData := range jwks.Keys {
		var keyInfo struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		}
		if err := json.Unmarshal(keyData, &keyInfo); err != nil {
			continue
		}

		switch keyInfo.Kty {
		case "RSA":
			key, err := parseRSAPublicKey(keyInfo.N, keyInfo.E)
			if err == nil {
				c.keys[keyInfo.Kid] = key
			}
		case "EC":
			key, err := parseECPublicKey(keyInfo.Crv, keyInfo.X, keyInfo.Y)
			if err == nil {
				c.keys[keyInfo.Kid] = key
			}
		}
	}

	return nil
}

// parseRSAPublicKey parses RSA public key from JWK parameters.
func parseRSAPublicKey(n, e string) (interface{}, error) {
	// Base64url decode N and E
	nBytes, err := jwt.NewParser().DecodeSegment(n)
	if err != nil {
		return nil, err
	}
	eBytes, err := jwt.NewParser().DecodeSegment(e)
	if err != nil {
		return nil, err
	}

	// Convert E to int
	var eInt int
	for _, b := range eBytes {
		eInt = eInt<<8 + int(b)
	}

	return &struct {
		N []byte
		E int
	}{N: nBytes, E: eInt}, nil
}

// parseECPublicKey parses EC public key from JWK parameters.
func parseECPublicKey(crv, x, y string) (interface{}, error) {
	// For EC keys, we need to parse and create ecdsa.PublicKey
	// This is a simplified implementation
	xBytes, err := jwt.NewParser().DecodeSegment(x)
	if err != nil {
		return nil, err
	}
	yBytes, err := jwt.NewParser().DecodeSegment(y)
	if err != nil {
		return nil, err
	}

	return &struct {
		Curve string
		X     []byte
		Y     []byte
	}{Curve: crv, X: xBytes, Y: yBytes}, nil
}

// BuildMTLSConfig builds TLS config for mTLS authentication.
func BuildMTLSConfig(tlsCfg *TLSConfig) (*tls.Config, error) {
	if tlsCfg == nil || !tlsCfg.Enabled {
		return nil, nil
	}

	config := &tls.Config{}

	// Load server certificate
	if tlsCfg.CertFile != "" && tlsCfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tlsCfg.CertFile, tlsCfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load server certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}

	// Load CA for client verification
	if tlsCfg.CAFile != "" {
		caCert, err := loadCACert(tlsCfg.CAFile)
		if err != nil {
			return nil, err
		}
		config.ClientCAs = caCert
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return config, nil
}

// loadCACert loads CA certificate pool from file.
func loadCACert(caFile string) (*x509.CertPool, error) {
	// Note: Implementation would read the file and parse certificates
	// For now, return system pool as placeholder
	return x509.SystemCertPool()
}
