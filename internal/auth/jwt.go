package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenManager handles JWT token generation and validation
type TokenManager struct {
	config     *JWTConfig
	signingKey interface{}
	verifyKey  interface{}
}

// Claims represents the JWT claims
type Claims struct {
	jwt.RegisteredClaims
	UserID      string                 `json:"user_id"`
	Email       string                 `json:"email,omitempty"`
	Roles       []string               `json:"roles,omitempty"`
	Permissions []string               `json:"permissions,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	TokenType   string                 `json:"token_type"` // access, refresh
	Custom      map[string]interface{} `json:"custom,omitempty"`
}

// NewTokenManager creates a new token manager
func NewTokenManager(config *JWTConfig) (*TokenManager, error) {
	if config == nil {
		return nil, errors.New("JWT config is required")
	}

	tm := &TokenManager{config: config}

	// Set up signing key based on algorithm
	switch config.Algorithm {
	case "HS256", "HS384", "HS512":
		if config.Secret == "" {
			return nil, errors.New("secret is required for HMAC algorithms")
		}
		tm.signingKey = []byte(config.Secret)
		tm.verifyKey = []byte(config.Secret)

	case "RS256", "RS384", "RS512":
		if config.PrivateKey == "" || config.PublicKey == "" {
			return nil, errors.New("private_key and public_key are required for RSA algorithms")
		}
		privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(config.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse RSA private key: %w", err)
		}
		publicKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(config.PublicKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse RSA public key: %w", err)
		}
		tm.signingKey = privateKey
		tm.verifyKey = publicKey

	case "ES256", "ES384", "ES512":
		if config.PrivateKey == "" || config.PublicKey == "" {
			return nil, errors.New("private_key and public_key are required for ECDSA algorithms")
		}
		privateKey, err := jwt.ParseECPrivateKeyFromPEM([]byte(config.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse ECDSA private key: %w", err)
		}
		publicKey, err := jwt.ParseECPublicKeyFromPEM([]byte(config.PublicKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse ECDSA public key: %w", err)
		}
		tm.signingKey = privateKey
		tm.verifyKey = publicKey

	default:
		// Default to HS256
		if config.Secret == "" {
			return nil, errors.New("secret is required")
		}
		config.Algorithm = "HS256"
		tm.signingKey = []byte(config.Secret)
		tm.verifyKey = []byte(config.Secret)
	}

	return tm, nil
}

// GenerateTokenPair generates access and refresh tokens
func (tm *TokenManager) GenerateTokenPair(user *User, sessionID string, customClaims map[string]interface{}) (*TokenPair, error) {
	now := time.Now()

	// Parse durations
	accessDuration, err := ParseDuration(tm.config.AccessLifetime)
	if err != nil {
		accessDuration = 15 * time.Minute
	}
	if accessDuration == 0 {
		accessDuration = 15 * time.Minute
	}

	refreshDuration, err := ParseDuration(tm.config.RefreshLifetime)
	if err != nil {
		refreshDuration = 7 * 24 * time.Hour
	}
	if refreshDuration == 0 {
		refreshDuration = 7 * 24 * time.Hour
	}

	accessExpiry := now.Add(accessDuration)
	refreshExpiry := now.Add(refreshDuration)

	// Generate access token
	accessToken, err := tm.generateToken(user, sessionID, "access", accessExpiry, customClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	// Generate refresh token
	refreshToken, err := tm.generateToken(user, sessionID, "refresh", refreshExpiry, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(accessDuration.Seconds()),
		ExpiresAt:    accessExpiry,
	}, nil
}

// generateToken generates a single token
func (tm *TokenManager) generateToken(user *User, sessionID, tokenType string, expiry time.Time, customClaims map[string]interface{}) (string, error) {
	now := time.Now()

	// Generate unique token ID
	jti, err := generateTokenID()
	if err != nil {
		return "", err
	}

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   user.ID,
			Issuer:    tm.config.Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiry),
		},
		UserID:      user.ID,
		Email:       user.Email,
		Roles:       user.Roles,
		Permissions: user.Permissions,
		SessionID:   sessionID,
		TokenType:   tokenType,
		Custom:      customClaims,
	}

	// Add audience if configured
	if len(tm.config.Audience) > 0 {
		claims.Audience = tm.config.Audience
	}

	// Create token with appropriate signing method
	var signingMethod jwt.SigningMethod
	switch tm.config.Algorithm {
	case "HS256":
		signingMethod = jwt.SigningMethodHS256
	case "HS384":
		signingMethod = jwt.SigningMethodHS384
	case "HS512":
		signingMethod = jwt.SigningMethodHS512
	case "RS256":
		signingMethod = jwt.SigningMethodRS256
	case "RS384":
		signingMethod = jwt.SigningMethodRS384
	case "RS512":
		signingMethod = jwt.SigningMethodRS512
	case "ES256":
		signingMethod = jwt.SigningMethodES256
	case "ES384":
		signingMethod = jwt.SigningMethodES384
	case "ES512":
		signingMethod = jwt.SigningMethodES512
	default:
		signingMethod = jwt.SigningMethodHS256
	}

	token := jwt.NewWithClaims(signingMethod, claims)
	return token.SignedString(tm.signingKey)
}

// ValidateToken validates a token and returns the claims
func (tm *TokenManager) ValidateToken(tokenString string) (*Claims, error) {
	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify algorithm matches
		alg := token.Method.Alg()
		if alg != tm.config.Algorithm {
			return nil, fmt.Errorf("unexpected signing method: %v", alg)
		}
		return tm.verifyKey, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrInvalidToken
		}
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}

	// Verify issuer if configured
	if tm.config.Issuer != "" {
		issuer, err := claims.GetIssuer()
		if err != nil || issuer != tm.config.Issuer {
			return nil, ErrInvalidToken
		}
	}

	// Verify audience if configured
	if len(tm.config.Audience) > 0 {
		aud, err := claims.GetAudience()
		if err != nil {
			return nil, ErrInvalidToken
		}
		found := false
		for _, a := range aud {
			for _, expected := range tm.config.Audience {
				if a == expected {
					found = true
					break
				}
			}
		}
		if !found {
			return nil, ErrInvalidToken
		}
	}

	return claims, nil
}

// ValidateAccessToken validates an access token
func (tm *TokenManager) ValidateAccessToken(tokenString string) (*Claims, error) {
	claims, err := tm.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	if claims.TokenType != "access" {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ValidateRefreshToken validates a refresh token
func (tm *TokenManager) ValidateRefreshToken(tokenString string) (*Claims, error) {
	claims, err := tm.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	if claims.TokenType != "refresh" {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// RefreshTokens generates new tokens from a refresh token
func (tm *TokenManager) RefreshTokens(refreshToken string, user *User) (*TokenPair, error) {
	claims, err := tm.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, err
	}

	// Generate new token pair
	return tm.GenerateTokenPair(user, claims.SessionID, claims.Custom)
}

// generateTokenID generates a random token ID
func generateTokenID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// ExtractTokenFromHeader extracts the token from Authorization header
func ExtractTokenFromHeader(header string) string {
	if header == "" {
		return ""
	}

	// Check for Bearer prefix
	const prefix = "Bearer "
	if len(header) > len(prefix) && header[:len(prefix)] == prefix {
		return header[len(prefix):]
	}

	return header
}
