package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

// Manager is the main auth service
type Manager struct {
	config *Config

	// Stores
	userStore       UserStore
	sessionStore    SessionStore
	tokenStore      TokenStore
	bruteForceStore BruteForceStore
	mfaStore        MFAStore

	// Components
	tokenManager      *TokenManager
	passwordHasher    *PasswordHasher
	passwordValidator *PasswordValidator
	mfaService        *MFAService

	logger *slog.Logger
}

// ManagerOption is a functional option for Manager
type ManagerOption func(*Manager)

// WithUserStore sets the user store
func WithUserStore(store UserStore) ManagerOption {
	return func(m *Manager) {
		m.userStore = store
	}
}

// WithSessionStore sets the session store
func WithSessionStore(store SessionStore) ManagerOption {
	return func(m *Manager) {
		m.sessionStore = store
	}
}

// WithTokenStore sets the token store
func WithTokenStore(store TokenStore) ManagerOption {
	return func(m *Manager) {
		m.tokenStore = store
	}
}

// WithBruteForceStore sets the brute force store
func WithBruteForceStore(store BruteForceStore) ManagerOption {
	return func(m *Manager) {
		m.bruteForceStore = store
	}
}

// WithLogger sets the logger
func WithLogger(logger *slog.Logger) ManagerOption {
	return func(m *Manager) {
		m.logger = logger
	}
}

// WithMFAStore sets the MFA store
func WithMFAStore(store MFAStore) ManagerOption {
	return func(m *Manager) {
		m.mfaStore = store
	}
}

// NewManager creates a new auth manager
func NewManager(config *Config, opts ...ManagerOption) (*Manager, error) {
	if config == nil {
		config = &Config{}
	}

	// Apply preset and merge defaults
	config = MergeWithPreset(config)

	// Handle quick config
	if config.Secret != "" && config.JWT == nil {
		config.JWT = &JWTConfig{}
	}
	if config.Secret != "" && config.JWT.Secret == "" {
		config.JWT.Secret = config.Secret
	}

	m := &Manager{
		config: config,
		logger: slog.Default(),
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	// Set default stores if not provided
	if m.userStore == nil {
		m.userStore = NewMemoryUserStore()
	}
	if m.sessionStore == nil {
		m.sessionStore = NewMemorySessionStore()
	}
	if m.tokenStore == nil {
		m.tokenStore = NewMemoryTokenStore()
	}
	if m.bruteForceStore == nil {
		m.bruteForceStore = NewMemoryBruteForceStore()
	}

	// Initialize token manager
	var err error
	m.tokenManager, err = NewTokenManager(config.JWT)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize token manager: %w", err)
	}

	// Initialize password components
	m.passwordHasher = NewPasswordHasher(config.Password)
	m.passwordValidator = NewPasswordValidator(config.Password)

	// Initialize MFA components if enabled
	if config.MFA != nil && config.MFA.Enabled {
		if m.mfaStore == nil {
			m.mfaStore = NewMemoryMFAStore()
		}
		m.mfaService = NewMFAService(config.MFA, m.mfaStore)
	}

	return m, nil
}

// Config returns the auth configuration
func (m *Manager) Config() *Config {
	return m.config
}

// Register registers a new user
func (m *Manager) Register(ctx context.Context, req *RegisterRequest) (*User, *TokenPair, error) {
	// Validate password
	if err := m.passwordValidator.Validate(req.Password, nil); err != nil {
		return nil, nil, &AuthError{Code: "weak_password", Message: err.Error()}
	}

	// Check if user exists
	existing, _ := m.userStore.FindByEmail(ctx, req.Email)
	if existing != nil {
		return nil, nil, ErrUserExists
	}

	// Hash password
	passwordHash, err := m.passwordHasher.Hash(req.Password)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Generate user ID
	userID, err := generateID()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	user := &User{
		ID:           userID,
		Email:        req.Email,
		PasswordHash: passwordHash,
		Metadata:     req.Metadata,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Create user
	if err := m.userStore.Create(ctx, user); err != nil {
		return nil, nil, err
	}

	// Generate session and tokens
	session, err := m.createSession(ctx, user, "", "")
	if err != nil {
		return nil, nil, err
	}

	tokens, err := m.tokenManager.GenerateTokenPair(user, session.ID, nil)
	if err != nil {
		return nil, nil, err
	}

	m.logger.Info("user registered", "user_id", user.ID, "email", user.Email)

	return user, tokens, nil
}

// Login authenticates a user
func (m *Manager) Login(ctx context.Context, req *LoginRequest, ip, userAgent string) (*User, *TokenPair, error) {
	// Check brute force protection
	if m.config.Security != nil && m.config.Security.BruteForce != nil && m.config.Security.BruteForce.Enabled {
		key := m.bruteForceKey(req.Email, ip)
		locked, until, _ := m.bruteForceStore.IsLocked(ctx, key)
		if locked {
			return nil, nil, &AuthError{
				Code:    "account_locked",
				Message: fmt.Sprintf("Account is locked until %s", until.Format(time.RFC3339)),
			}
		}
	}

	// Find user
	user, err := m.userStore.FindByEmail(ctx, req.Email)
	if err != nil {
		m.recordFailedLogin(ctx, req.Email, ip)
		return nil, nil, ErrInvalidCredentials
	}

	// Verify password
	valid, err := m.passwordHasher.Verify(req.Password, user.PasswordHash)
	if err != nil || !valid {
		m.recordFailedLogin(ctx, req.Email, ip)
		return nil, nil, ErrInvalidCredentials
	}

	// Check if MFA is required
	if user.MFAEnabled && req.MFACode == "" {
		return nil, nil, ErrMFARequired
	}

	// Verify MFA code if provided
	if user.MFAEnabled && req.MFACode != "" {
		if m.mfaService == nil {
			return nil, nil, &AuthError{Code: "mfa_not_configured", Message: "MFA is enabled but MFA service is not configured"}
		}

		// Try TOTP first, then recovery code
		err := m.mfaService.ValidateTOTP(ctx, user.ID, req.MFACode)
		if err != nil {
			// Try recovery code
			err = m.mfaService.ValidateRecoveryCode(ctx, user.ID, req.MFACode)
			if err != nil {
				m.recordFailedLogin(ctx, req.Email, ip)
				return nil, nil, ErrInvalidMFACode
			}
			m.logger.Warn("user logged in with recovery code", "user_id", user.ID)
		}
	}

	// Reset brute force counter on successful login
	if m.config.Security != nil && m.config.Security.BruteForce != nil && m.config.Security.BruteForce.Enabled {
		key := m.bruteForceKey(req.Email, ip)
		_ = m.bruteForceStore.Reset(ctx, key)
	}

	// Check session limits
	if m.config.Sessions != nil && m.config.Sessions.MaxActive > 0 {
		count, _ := m.sessionStore.Count(ctx, user.ID)
		if count >= m.config.Sessions.MaxActive {
			if m.config.Sessions.OnMaxReached == "reject_new" {
				return nil, nil, &AuthError{Code: "max_sessions", Message: "Maximum number of sessions reached"}
			}
			// revoke_oldest - delete oldest session
			sessions, _ := m.sessionStore.FindByUserID(ctx, user.ID)
			if len(sessions) > 0 {
				oldest := sessions[0]
				for _, s := range sessions[1:] {
					if s.CreatedAt.Before(oldest.CreatedAt) {
						oldest = s
					}
				}
				_ = m.sessionStore.Delete(ctx, oldest.ID)
			}
		}
	}

	// Create session
	session, err := m.createSession(ctx, user, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}

	// Generate tokens
	tokens, err := m.tokenManager.GenerateTokenPair(user, session.ID, nil)
	if err != nil {
		return nil, nil, err
	}

	// Update last login
	_ = m.userStore.UpdateLastLogin(ctx, user.ID, time.Now())

	m.logger.Info("user logged in", "user_id", user.ID, "email", user.Email, "session_id", session.ID)

	return user, tokens, nil
}

// Logout invalidates a session
func (m *Manager) Logout(ctx context.Context, sessionID string) error {
	if err := m.sessionStore.Delete(ctx, sessionID); err != nil {
		return err
	}

	m.logger.Info("user logged out", "session_id", sessionID)
	return nil
}

// LogoutAll invalidates all sessions for a user
func (m *Manager) LogoutAll(ctx context.Context, userID string) error {
	if err := m.sessionStore.DeleteByUserID(ctx, userID); err != nil {
		return err
	}

	m.logger.Info("all sessions revoked", "user_id", userID)
	return nil
}

// RefreshToken refreshes an access token
func (m *Manager) RefreshToken(ctx context.Context, refreshToken string) (*User, *TokenPair, error) {
	// Validate refresh token
	claims, err := m.tokenManager.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	// Check if token is blacklisted (replay protection)
	if m.config.Security != nil && m.config.Security.ReplayProtection != nil && m.config.Security.ReplayProtection.Enabled {
		exists, _ := m.tokenStore.Exists(ctx, claims.ID)
		if exists {
			return nil, nil, ErrInvalidToken
		}
	}

	// Verify session still exists
	session, err := m.sessionStore.FindByID(ctx, claims.SessionID)
	if err != nil {
		return nil, nil, ErrSessionExpired
	}

	// Check session expiry
	if time.Now().After(session.ExpiresAt) {
		_ = m.sessionStore.Delete(ctx, session.ID)
		return nil, nil, ErrSessionExpired
	}

	// Get user
	user, err := m.userStore.FindByID(ctx, claims.UserID)
	if err != nil {
		return nil, nil, ErrUserNotFound
	}

	// Blacklist old refresh token if rotation is enabled
	if m.config.JWT != nil && m.config.JWT.Rotation {
		expiry, _ := claims.GetExpirationTime()
		if expiry != nil {
			_ = m.tokenStore.Add(ctx, claims.ID, expiry.Time)
		}
	}

	// Generate new tokens
	tokens, err := m.tokenManager.GenerateTokenPair(user, session.ID, claims.Custom)
	if err != nil {
		return nil, nil, err
	}

	// Update session activity
	_ = m.sessionStore.Touch(ctx, session.ID)

	m.logger.Debug("token refreshed", "user_id", user.ID, "session_id", session.ID)

	return user, tokens, nil
}

// ValidateToken validates an access token and returns the user
func (m *Manager) ValidateToken(ctx context.Context, accessToken string) (*User, *Claims, error) {
	// Validate access token
	claims, err := m.tokenManager.ValidateAccessToken(accessToken)
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	// Check if token is blacklisted
	if m.config.Security != nil && m.config.Security.ReplayProtection != nil && m.config.Security.ReplayProtection.Enabled {
		exists, _ := m.tokenStore.Exists(ctx, claims.ID)
		if exists {
			return nil, nil, ErrInvalidToken
		}
	}

	// Verify session still exists
	if claims.SessionID != "" {
		session, err := m.sessionStore.FindByID(ctx, claims.SessionID)
		if err != nil {
			return nil, nil, ErrSessionExpired
		}

		// Check session expiry
		if time.Now().After(session.ExpiresAt) {
			_ = m.sessionStore.Delete(ctx, session.ID)
			return nil, nil, ErrSessionExpired
		}

		// Update session activity
		_ = m.sessionStore.Touch(ctx, session.ID)
	}

	// Get user
	user, err := m.userStore.FindByID(ctx, claims.UserID)
	if err != nil {
		return nil, nil, ErrUserNotFound
	}

	return user, claims, nil
}

// GetUser returns the current user
func (m *Manager) GetUser(ctx context.Context, userID string) (*User, error) {
	return m.userStore.FindByID(ctx, userID)
}

// GetSessions returns all sessions for a user
func (m *Manager) GetSessions(ctx context.Context, userID string) ([]*Session, error) {
	return m.sessionStore.FindByUserID(ctx, userID)
}

// RevokeSession revokes a specific session
func (m *Manager) RevokeSession(ctx context.Context, userID, sessionID string) error {
	// Verify session belongs to user
	session, err := m.sessionStore.FindByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.UserID != userID {
		return &AuthError{Code: "forbidden", Message: "Session does not belong to user"}
	}

	return m.sessionStore.Delete(ctx, sessionID)
}

// ChangePassword changes a user's password
func (m *Manager) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	// Get user
	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return err
	}

	// Verify current password
	valid, err := m.passwordHasher.Verify(currentPassword, user.PasswordHash)
	if err != nil || !valid {
		return &AuthError{Code: "invalid_password", Message: "Current password is incorrect"}
	}

	// Validate new password
	if err := m.passwordValidator.Validate(newPassword, user); err != nil {
		return &AuthError{Code: "weak_password", Message: err.Error()}
	}

	// Hash new password
	passwordHash, err := m.passwordHasher.Hash(newPassword)
	if err != nil {
		return err
	}

	// Update password
	if err := m.userStore.UpdatePassword(ctx, userID, passwordHash); err != nil {
		return err
	}

	m.logger.Info("password changed", "user_id", userID)
	return nil
}

// createSession creates a new session for a user
func (m *Manager) createSession(ctx context.Context, user *User, ip, userAgent string) (*Session, error) {
	sessionID, err := generateID()
	if err != nil {
		return nil, err
	}

	now := time.Now()

	// Calculate expiry
	var expiresAt time.Time
	if m.config.Sessions != nil && m.config.Sessions.AbsoluteTimeout != "" {
		duration, err := ParseDuration(m.config.Sessions.AbsoluteTimeout)
		if err != nil {
			duration = 24 * time.Hour
		}
		expiresAt = now.Add(duration)
	} else {
		expiresAt = now.Add(24 * time.Hour)
	}

	session := &Session{
		ID:           sessionID,
		UserID:       user.ID,
		IP:           ip,
		UserAgent:    userAgent,
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    expiresAt,
	}

	if err := m.sessionStore.Create(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

// recordFailedLogin records a failed login attempt
func (m *Manager) recordFailedLogin(ctx context.Context, email, ip string) {
	if m.config.Security == nil || m.config.Security.BruteForce == nil || !m.config.Security.BruteForce.Enabled {
		return
	}

	bf := m.config.Security.BruteForce
	key := m.bruteForceKey(email, ip)

	window, _ := ParseDuration(bf.Window)
	if window == 0 {
		window = 15 * time.Minute
	}

	count, _ := m.bruteForceStore.Increment(ctx, key, window)

	if count >= bf.MaxAttempts {
		lockout, _ := ParseDuration(bf.LockoutTime)
		if lockout == 0 {
			lockout = 15 * time.Minute
		}
		_ = m.bruteForceStore.Lock(ctx, key, lockout)

		m.logger.Warn("account locked due to failed attempts",
			"email", email, "ip", ip, "attempts", count)
	}
}

// bruteForceKey generates a key for brute force tracking
func (m *Manager) bruteForceKey(email, ip string) string {
	if m.config.Security == nil || m.config.Security.BruteForce == nil {
		return email
	}

	switch m.config.Security.BruteForce.TrackBy {
	case "ip":
		return ip
	case "user":
		return email
	case "ip+user":
		return fmt.Sprintf("%s:%s", ip, email)
	default:
		return fmt.Sprintf("%s:%s", ip, email)
	}
}

// generateID generates a random ID
func generateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// Cleanup performs periodic cleanup tasks
func (m *Manager) Cleanup(ctx context.Context) error {
	// Clean expired sessions
	if err := m.sessionStore.DeleteExpired(ctx); err != nil {
		m.logger.Error("failed to cleanup expired sessions", "error", err)
	}

	// Clean expired tokens
	if err := m.tokenStore.Cleanup(ctx); err != nil {
		m.logger.Error("failed to cleanup expired tokens", "error", err)
	}

	return nil
}

// ==================== MFA Methods ====================

// GetMFAStatus returns the MFA status for a user
func (m *Manager) GetMFAStatus(ctx context.Context, userID string) (*MFAStatus, error) {
	if m.mfaService == nil {
		return &MFAStatus{
			Enabled:          false,
			TOTPConfigured:   false,
			RequiredByPolicy: false,
		}, nil
	}
	return m.mfaService.GetStatus(ctx, userID)
}

// BeginTOTPSetup initiates TOTP setup for a user
func (m *Manager) BeginTOTPSetup(ctx context.Context, userID string) (*MFASetup, error) {
	if m.mfaService == nil {
		return nil, &AuthError{Code: "mfa_not_configured", Message: "MFA is not enabled in configuration"}
	}

	// Get user to get their email
	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	return m.mfaService.BeginTOTPSetup(ctx, userID, user.Email)
}

// ConfirmTOTPSetup completes TOTP setup by verifying the code
func (m *Manager) ConfirmTOTPSetup(ctx context.Context, userID, code string) ([]string, error) {
	if m.mfaService == nil {
		return nil, &AuthError{Code: "mfa_not_configured", Message: "MFA is not enabled in configuration"}
	}

	recoveryCodes, err := m.mfaService.ConfirmTOTPSetup(ctx, userID, code)
	if err != nil {
		return nil, err
	}

	// Update user's MFAEnabled flag
	if err := m.userStore.UpdateMFAEnabled(ctx, userID, true); err != nil {
		m.logger.Error("failed to update user MFA status", "user_id", userID, "error", err)
	}

	m.logger.Info("MFA enabled for user", "user_id", userID)
	return recoveryCodes, nil
}

// DisableMFA disables MFA for a user
func (m *Manager) DisableMFA(ctx context.Context, userID, password string) error {
	if m.mfaService == nil {
		return nil // MFA not configured, nothing to disable
	}

	// Verify password before disabling MFA
	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	valid, err := m.passwordHasher.Verify(password, user.PasswordHash)
	if err != nil || !valid {
		return &AuthError{Code: "invalid_password", Message: "Password is incorrect"}
	}

	// Disable TOTP
	if err := m.mfaService.DisableTOTP(ctx, userID); err != nil {
		return err
	}

	// Update user's MFAEnabled flag
	if err := m.userStore.UpdateMFAEnabled(ctx, userID, false); err != nil {
		m.logger.Error("failed to update user MFA status", "user_id", userID, "error", err)
	}

	m.logger.Info("MFA disabled for user", "user_id", userID)
	return nil
}

// RegenerateRecoveryCodes generates new recovery codes for a user
func (m *Manager) RegenerateRecoveryCodes(ctx context.Context, userID, password string) ([]string, error) {
	if m.mfaService == nil {
		return nil, &AuthError{Code: "mfa_not_configured", Message: "MFA is not enabled in configuration"}
	}

	// Verify password before regenerating codes
	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	valid, err := m.passwordHasher.Verify(password, user.PasswordHash)
	if err != nil || !valid {
		return nil, &AuthError{Code: "invalid_password", Message: "Password is incorrect"}
	}

	codes, err := m.mfaService.RegenerateRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, err
	}

	m.logger.Info("recovery codes regenerated", "user_id", userID)
	return codes, nil
}

// ==================== WebAuthn/Passkey Methods ====================

// BeginWebAuthnRegistration starts WebAuthn credential registration
func (m *Manager) BeginWebAuthnRegistration(ctx context.Context, userID string) (interface{}, string, error) {
	if m.mfaService == nil || m.mfaService.WebAuthn() == nil {
		return nil, "", &AuthError{Code: "webauthn_not_configured", Message: "WebAuthn is not enabled in configuration"}
	}

	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return nil, "", ErrUserNotFound
	}

	// Get existing credentials
	existingCreds, _ := m.mfaService.GetWebAuthnCredentials(ctx, userID)

	return m.mfaService.WebAuthn().BeginRegistration(ctx, userID, user.Email, user.Email, existingCreds)
}

// FinishWebAuthnRegistration completes WebAuthn credential registration
func (m *Manager) FinishWebAuthnRegistration(ctx context.Context, userID, sessionData, credentialName string, response interface{}) error {
	if m.mfaService == nil || m.mfaService.WebAuthn() == nil {
		return &AuthError{Code: "webauthn_not_configured", Message: "WebAuthn is not enabled in configuration"}
	}

	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	// Get existing credentials
	existingCreds, _ := m.mfaService.GetWebAuthnCredentials(ctx, userID)

	// Type assert the response - it should be *protocol.ParsedCredentialCreationData
	parsedResponse, ok := response.(*protocol.ParsedCredentialCreationData)
	if !ok {
		return &AuthError{Code: "invalid_response", Message: "Invalid WebAuthn response"}
	}

	// Finish registration
	cred, err := m.mfaService.WebAuthn().FinishRegistration(ctx, userID, user.Email, user.Email, existingCreds, sessionData, parsedResponse)
	if err != nil {
		return err
	}

	// Store the credential
	if err := m.mfaService.AddWebAuthnCredential(ctx, userID, cred, credentialName); err != nil {
		return err
	}

	// Update user's MFAEnabled flag if this is their first MFA method
	status, _ := m.mfaService.GetStatus(ctx, userID)
	if status != nil && status.Enabled {
		if err := m.userStore.UpdateMFAEnabled(ctx, userID, true); err != nil {
			m.logger.Error("failed to update user MFA status", "user_id", userID, "error", err)
		}
	}

	m.logger.Info("WebAuthn credential registered", "user_id", userID, "credential_name", credentialName)
	return nil
}

// GetWebAuthnCredentials returns all WebAuthn credentials for a user
func (m *Manager) GetWebAuthnCredentials(ctx context.Context, userID string) ([]WebAuthnCredential, error) {
	if m.mfaService == nil {
		return nil, nil
	}
	return m.mfaService.GetWebAuthnCredentials(ctx, userID)
}

// RemoveWebAuthnCredential removes a WebAuthn credential
func (m *Manager) RemoveWebAuthnCredential(ctx context.Context, userID, credentialID, password string) error {
	if m.mfaService == nil {
		return &AuthError{Code: "mfa_not_configured", Message: "MFA is not enabled in configuration"}
	}

	// Verify password
	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	valid, err := m.passwordHasher.Verify(password, user.PasswordHash)
	if err != nil || !valid {
		return &AuthError{Code: "invalid_password", Message: "Password is incorrect"}
	}

	if err := m.mfaService.RemoveWebAuthnCredential(ctx, userID, credentialID); err != nil {
		return err
	}

	// Check if user still has any MFA methods enabled
	status, _ := m.mfaService.GetStatus(ctx, userID)
	if status != nil && !status.Enabled {
		if err := m.userStore.UpdateMFAEnabled(ctx, userID, false); err != nil {
			m.logger.Error("failed to update user MFA status", "user_id", userID, "error", err)
		}
	}

	m.logger.Info("WebAuthn credential removed", "user_id", userID, "credential_id", credentialID)
	return nil
}
