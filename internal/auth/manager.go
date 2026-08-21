package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/matutetandil/mycel/v3/internal/transform"
)

// Manager is the main auth service
type Manager struct {
	config *Config

	// Stores
	userStore       UserStore
	sessionStore    SessionStore
	tokenStore      TokenStore
	bruteForceStore BruteForceStore
	bruteForce      *BruteForceService
	sso             *SSOService
	rateLimiter     *PerKeyRateLimiter
	mfaStore        MFAStore
	auditStore      AuditStore
	passwordHistory PasswordHistoryStore
	passwordReset   PasswordResetStore
	devices         DeviceStore
	geoip           GeoIPLookup
	breaches        BreachChecker
	travel          *travelHistory
	flows           FlowInvoker
	hookConditions  *transform.CELTransformer

	// Components
	tokenManager      *TokenManager
	passwordHasher    *PasswordHasher
	passwordValidator *PasswordValidator
	mfaService        *MFAService
	providerValidator *ProviderValidator

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

// WithPasswordResetStore sets where outstanding reset tokens are kept.
func WithPasswordResetStore(store PasswordResetStore) ManagerOption {
	return func(m *Manager) {
		m.passwordReset = store
	}
}

// WithPasswordHistoryStore sets where previously used password hashes are kept.
func WithPasswordHistoryStore(store PasswordHistoryStore) ManagerOption {
	return func(m *Manager) {
		m.passwordHistory = store
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

	if err := validateHooks(config); err != nil {
		return nil, err
	}
	if err := validateDeviceBinding(config); err != nil {
		return nil, err
	}
	if err := validateImpossibleTravel(config); err != nil {
		return nil, err
	}
	if err := validateMFAPolicy(config); err != nil {
		return nil, err
	}

	// What to do at the session limit has to be understood before a service
	// starts, not at the moment somebody is turned away. The word for refusing
	// was published as `deny` for a long time and the code only ever knew
	// `reject_new`, so `deny` revoked the oldest session instead — the opposite
	// of what it says. It is read as refusing now, and anything unrecognised is
	// refused here rather than quietly meaning revoke_oldest.
	if config.Sessions != nil {
		switch config.Sessions.OnMaxReached {
		case "", "revoke_oldest":
		case "reject_new", "deny":
			config.Sessions.OnMaxReached = "reject_new"
		default:
			return nil, fmt.Errorf("sessions: on_max_reached = %q is not something this understands; use reject_new or revoke_oldest",
				config.Sessions.OnMaxReached)
		}
	}

	m := &Manager{
		config: config,
		logger: slog.Default(),
	}

	// Apply options. An option that is nil is skipped rather than crashing the
	// process: the runtime assembles this list from whatever storage the
	// configuration names, and one branch returning nothing would otherwise
	// take the whole service down at startup with a segmentation fault instead
	// of a message.
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(m)
	}

	// Set default stores if not provided
	if m.userStore == nil {
		m.userStore = NewMemoryUserStore()
	}
	if m.sessionStore == nil {
		// The idle-capable one, so that sessions { idle_timeout } is enforced
		// out of the box rather than only when a store is supplied by hand.
		m.sessionStore = NewMemorySessionStoreWithIdle()
	}
	if m.tokenStore == nil {
		m.tokenStore = NewMemoryTokenStore()
	}
	if m.passwordHistory == nil {
		m.passwordHistory = NewMemoryPasswordHistoryStore()
	}
	if m.passwordReset == nil {
		m.passwordReset = NewMemoryPasswordResetStore()
	}
	if m.devices == nil {
		m.devices = NewMemoryDeviceStore()
	}
	m.travel = newTravelHistory()
	// The public list unless something else was supplied. Nothing is asked of
	// it until a service turns breach_check on.
	if m.breaches == nil {
		m.breaches = NewPwnedPasswords()
	}
	if m.geoip == nil {
		lookup, err := buildGeoIP(config)
		if err != nil {
			return nil, err
		}
		m.geoip = lookup
	}
	if m.bruteForceStore == nil {
		m.bruteForceStore = NewMemoryBruteForceStore()
	}

	// Brute-force protection lives in one place. The manager used to carry its
	// own copy of the counting and locking, which worked but knew nothing about
	// progressive delays — so a progressive_delay block parsed, validated and
	// did nothing.
	if config.Security != nil {
		m.bruteForce = NewBruteForceService(config.Security.BruteForce, m.bruteForceStore)
	} else {
		m.bruteForce = NewBruteForceService(nil, m.bruteForceStore)
	}

	// Rate limiting for the auth endpoints, which is a different question from
	// the connector's own limit: this one counts per endpoint and per caller,
	// so five login attempts a minute does not also cap the rest of the API.
	// Brute force locks one account; this refuses a flood across many.
	if config.Security != nil && config.Security.RateLimit != nil {
		m.rateLimiter = NewPerKeyRateLimiter(config.Security.RateLimit)
	}

	// Single sign-on, when a provider is configured. Writing the block is the
	// whole of the setup: the providers are built from it, and the endpoints
	// that drive them are mounted from the same configuration.
	if config.Social != nil || config.SSO != nil {
		m.sso = NewSSOService(config, NewMemoryLinkedAccountStore(), m.userStore, m.logger)
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

	// Initialize external identity providers if configured. A bad provider
	// (unsupported type, invalid CEL, missing validate/success) fails startup.
	if len(config.Providers) > 0 {
		pv, err := NewProviderValidator(config.Providers, nil, m.logger)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize auth providers: %w", err)
		}
		m.providerValidator = pv
	}

	return m, nil
}

// Config returns the auth configuration
// RateLimiter returns the auth endpoint rate limiter, or nil when none is
// configured.
func (m *Manager) RateLimiter() *PerKeyRateLimiter {
	return m.rateLimiter
}

// SSO returns the single sign-on service, or nil when no provider is
// configured.
func (m *Manager) SSO() *SSOService {
	return m.sso
}

func (m *Manager) Config() *Config {
	return m.config
}

// Register registers a new user
func (m *Manager) Register(ctx context.Context, req *RegisterRequest) (*User, *TokenPair, error) {
	// Validate password. The cheap rules first: a password that is too short
	// is refused without asking anybody else about it.
	if err := m.passwordValidator.Validate(req.Password, nil); err != nil {
		return nil, nil, &AuthError{Code: "weak_password", Message: err.Error()}
	}
	if err := m.refuseBreachedPassword(ctx, req.Password); err != nil {
		return nil, nil, err
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
	m.audit(ctx, &AuditEvent{
		Event: AuditRegister, UserID: user.ID, Email: user.Email, Success: true,
	})
	_ = m.runHook(ctx, HookAfterRegister, map[string]interface{}{
		"user_id": user.ID, "email": user.Email,
	})

	return user, tokens, nil
}

// Login authenticates a user
func (m *Manager) Login(ctx context.Context, req *LoginRequest, ip, userAgent string) (*User, *TokenPair, error) {
	// Before anything is checked, so that a hook refusing a sign-in refuses it
	// without the password being verified at all.
	if err := m.runHook(ctx, HookBeforeLogin, map[string]interface{}{
		"email": req.Email, "ip": ip, "user_agent": userAgent,
	}); err != nil {
		return nil, nil, &AuthError{Code: "login_refused", Message: err.Error()}
	}

	// Check brute force protection
	if m.bruteForceEnabled() {
		key := m.bruteForceKey(req.Email, ip)
		allowed, delay, remaining, err := m.bruteForce.CheckAccess(ctx, key)
		if err != nil {
			return nil, nil, err
		}
		if !allowed {
			locked := &AuthError{
				Code:    "account_locked",
				Message: fmt.Sprintf("Account is locked until %s", time.Now().Add(remaining).Format(time.RFC3339)),
			}
			m.auditFailure(ctx, AuditLogin, req.Email, ip, userAgent, locked)
			return nil, nil, locked
		}
		// A progressive delay is the point of the feature: each failure makes
		// the next attempt slower. The wait is abandoned if the caller goes
		// away, so a disconnecting client does not hold a goroutine for it.
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
	}

	// Find user
	user, err := m.userStore.FindByEmail(ctx, req.Email)
	if err != nil {
		// The password is verified against a hash that matches nothing, so an
		// address with no account costs what an address with one costs. Without
		// it this path returns before any hashing and answers a hundred times
		// faster, which is a way to find out which addresses have accounts
		// without guessing a single password.
		_, _ = m.passwordHasher.Verify(req.Password, decoyHash)
		m.recordFailedLogin(ctx, req.Email, ip)
		m.auditFailure(ctx, AuditLogin, req.Email, ip, userAgent, ErrInvalidCredentials)
		m.delayFailure(ctx)
		return nil, nil, ErrInvalidCredentials
	}

	// Verify password
	valid, err := m.passwordHasher.Verify(req.Password, user.PasswordHash)
	if err != nil || !valid {
		m.recordFailedLogin(ctx, req.Email, ip)
		m.auditFailure(ctx, AuditLogin, req.Email, ip, userAgent, ErrInvalidCredentials)
		m.delayFailure(ctx)
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
				m.auditFailure(ctx, AuditLogin, req.Email, ip, userAgent, ErrInvalidMFACode)
				return nil, nil, ErrInvalidMFACode
			}
			m.logger.Warn("user logged in with recovery code", "user_id", user.ID)
		}
	}

	// Whether this is a machine the account has used before. After the
	// password is accepted, so that somebody guessing at it does not teach the
	// service a new device, and before a session exists, so that a refusal
	// here refuses to open one.
	if err := m.checkDevice(ctx, user, req, ip, userAgent); err != nil {
		return nil, nil, err
	}
	if err := m.checkTravel(ctx, user, req, ip, userAgent); err != nil {
		return nil, nil, err
	}

	// Reset brute force counter on successful login
	if m.bruteForceEnabled() {
		_ = m.bruteForce.RecordSuccess(ctx, m.bruteForceKey(req.Email, ip))
	}

	tokens, err := m.EstablishSession(ctx, user, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}

	m.logger.Info("user logged in", "user_id", user.ID, "email", user.Email)
	m.audit(ctx, &AuditEvent{
		Event: AuditLogin, UserID: user.ID, Email: user.Email,
		IP: ip, UserAgent: userAgent, Success: true,
	})
	_ = m.runHook(ctx, HookAfterLogin, map[string]interface{}{
		"user_id": user.ID, "email": user.Email, "ip": ip, "user_agent": userAgent,
	})

	return user, tokens, nil
}

// EstablishSession opens a session for a user who has already been
// authenticated, and returns the tokens for it.
//
// It is the tail of Login, extracted because signing in through an identity
// provider ends the same way: the session limit applies, a session is created,
// tokens are issued against it and the last login is recorded. Repeating that
// for SSO would mean two places where a session is born, and they would drift.
func (m *Manager) EstablishSession(ctx context.Context, user *User, ip, userAgent string) (*TokenPair, error) {
	// Check session limits
	if m.config.Sessions != nil && m.config.Sessions.MaxActive > 0 {
		count, _ := m.sessionStore.Count(ctx, user.ID)
		if count >= m.config.Sessions.MaxActive {
			if m.config.Sessions.OnMaxReached == "reject_new" {
				return nil, &AuthError{Code: "max_sessions", Message: "Maximum number of sessions reached"}
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

	session, err := m.createSession(ctx, user, ip, userAgent)
	if err != nil {
		return nil, err
	}

	tokens, err := m.tokenManager.GenerateTokenPair(user, session.ID, nil)
	if err != nil {
		return nil, err
	}

	_ = m.userStore.UpdateLastLogin(ctx, user.ID, time.Now())

	return tokens, nil
}

// Logout invalidates a session
func (m *Manager) Logout(ctx context.Context, sessionID string) error {
	if err := m.sessionStore.Delete(ctx, sessionID); err != nil {
		return err
	}

	m.logger.Info("user logged out", "session_id", sessionID)
	m.audit(ctx, &AuditEvent{
		Event: AuditLogout, Success: true,
		Metadata: `{"session_id":"` + sessionID + `"}`,
	})
	return nil
}

// LogoutAll invalidates all sessions for a user
func (m *Manager) LogoutAll(ctx context.Context, userID string) error {
	if err := m.sessionStore.DeleteByUserID(ctx, userID); err != nil {
		return err
	}

	m.logger.Info("all sessions revoked", "user_id", userID)
	m.audit(ctx, &AuditEvent{Event: AuditLogout, UserID: userID, Success: true})
	return nil
}

// replayChecked reports whether a token has to be checked against the list of
// spent ones: because rotation puts them there, or because replay protection
// was asked for.
func (m *Manager) replayChecked() bool {
	if m.config.JWT != nil && m.config.JWT.Rotation {
		return true
	}
	return m.config.Security != nil &&
		m.config.Security.ReplayProtection != nil &&
		m.config.Security.ReplayProtection.Enabled
}

// RefreshToken refreshes an access token
func (m *Manager) RefreshToken(ctx context.Context, refreshToken string) (*User, *TokenPair, error) {
	// Validate refresh token
	claims, err := m.tokenManager.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	// A refresh token that has already been spent is refused.
	//
	// Rotation is what puts it on the list, a few lines below, and the list was
	// read only when replay protection was separately switched on — so
	// `rotation = true` on its own issued a new token and went on honouring the
	// one it replaced, which is not rotation at all. The field says "rotate on
	// use"; being good once is the whole of what that buys.
	if m.replayChecked() {
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

// ValidateToken validates an access token and returns the user.
//
// A password past its max_age is refused here, which is what makes the policy
// a policy rather than a note in the sign-in response.
func (m *Manager) ValidateToken(ctx context.Context, accessToken string) (*User, *Claims, error) {
	return m.validateToken(ctx, accessToken, false)
}

// ValidateTokenAllowingUnfinishedSetup is ValidateToken for the endpoints an
// account still has to be able to reach when a policy has put it in a state it
// must get out of: an expired password, or a second factor it has not enrolled
// yet. Changing a password, enrolling a factor and signing out are those
// endpoints; refusing them would leave somebody with no way out of the state
// they are being asked to leave.
func (m *Manager) ValidateTokenAllowingUnfinishedSetup(ctx context.Context, accessToken string) (*User, *Claims, error) {
	return m.validateToken(ctx, accessToken, true)
}

func (m *Manager) validateToken(ctx context.Context, accessToken string, allowUnfinishedSetup bool) (*User, *Claims, error) {
	// Validate access token
	claims, err := m.tokenManager.ValidateAccessToken(accessToken)
	if err != nil {
		// Not a valid local JWT — fall back to external identity providers,
		// which validate the raw credential (e.g. an API key) over HTTP.
		if m.providerValidator.HasProviders() {
			if user, pClaims, pErr := m.providerValidator.Validate(ctx, accessToken); pErr == nil {
				return user, pClaims, nil
			}
		}
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

	if !allowUnfinishedSetup {
		if _, expired, known := m.PasswordExpiry(user); known && expired {
			return nil, nil, ErrPasswordExpired
		}
		if err := m.refuseUnenrolledAccount(ctx, user); err != nil {
			return nil, nil, err
		}
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

	if err := m.runHook(ctx, HookBeforePasswordChange, map[string]interface{}{
		"user_id": user.ID, "email": user.Email,
	}); err != nil {
		return &AuthError{Code: "password_change_refused", Message: err.Error()}
	}

	// Verify current password
	valid, err := m.passwordHasher.Verify(currentPassword, user.PasswordHash)
	if err != nil || !valid {
		refused := &AuthError{Code: "invalid_password", Message: "Current password is incorrect"}
		// Somebody trying to change a password without knowing the current one
		// is worth a record whether or not they are the account's owner.
		m.audit(ctx, &AuditEvent{
			Event: AuditPasswordChange, UserID: userID, Email: user.Email,
			Success: false, ErrorReason: refused.Message,
		})
		return refused
	}

	// Validate new password
	if err := m.passwordValidator.Validate(newPassword, user); err != nil {
		return &AuthError{Code: "weak_password", Message: err.Error()}
	}
	if err := m.refuseBreachedPassword(ctx, newPassword); err != nil {
		return err
	}

	// Somebody returning to a password they have used before.
	//
	// `password { history = N }` was read by nothing, so an account could
	// alternate between two passwords for ever — which is what somebody
	// required to change theirs every ninety days will do unless this stops
	// them. The current one counts as the most recent of the N, so history = 1
	// means "not the one you already have".
	if err := m.refusePasswordReuse(ctx, user, newPassword); err != nil {
		m.audit(ctx, &AuditEvent{
			Event: AuditPasswordChange, UserID: userID, Email: user.Email,
			Success: false, ErrorReason: err.Error(),
		})
		return &AuthError{Code: "password_reused", Message: err.Error()}
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

	// The one being replaced joins the record, and the record is trimmed to
	// what the policy actually looks at.
	if keep := m.passwordHistoryDepth(); keep > 0 && user.PasswordHash != "" {
		if err := m.passwordHistory.AddPasswordHash(ctx, userID, user.PasswordHash); err != nil {
			m.logger.Warn("could not record the previous password", "user_id", userID, "error", err)
		} else if err := m.passwordHistory.CleanOldHashes(ctx, userID, keep); err != nil {
			m.logger.Warn("could not trim the password history", "user_id", userID, "error", err)
		}
	}

	m.logger.Info("password changed", "user_id", userID)
	m.audit(ctx, &AuditEvent{
		Event: AuditPasswordChange, UserID: userID, Email: user.Email, Success: true,
	})
	_ = m.runHook(ctx, HookAfterPasswordChange, map[string]interface{}{
		"user_id": userID, "email": user.Email,
	})
	return nil
}

// passwordHistoryDepth is how many past passwords the policy looks at.
func (m *Manager) passwordHistoryDepth() int {
	if m.config.Password == nil {
		return 0
	}
	return m.config.Password.History
}

// refusePasswordReuse reports whether a password is one this account has used
// within the configured depth.
//
// Each stored hash is checked with the hasher rather than compared as a
// string: every hash carries its own salt, so the same password hashed twice
// does not produce the same text, and a string comparison would let every
// reuse through while looking like it worked.
func (m *Manager) refusePasswordReuse(ctx context.Context, user *User, candidate string) error {
	depth := m.passwordHistoryDepth()
	if depth <= 0 {
		return nil
	}

	// The password in use counts as the most recent one.
	if user.PasswordHash != "" {
		if same, _ := m.passwordHasher.Verify(candidate, user.PasswordHash); same {
			return fmt.Errorf("this is the password already in use; choose one of the last %d you have not used", depth)
		}
	}
	if depth == 1 {
		return nil
	}

	previous, err := m.passwordHistory.GetRecentHashes(ctx, user.ID, depth-1)
	if err != nil {
		// A history that cannot be read must not become a way to get a reuse
		// past the policy, nor a way to lock somebody out of changing their
		// password. It is reported and the change goes ahead.
		m.logger.Warn("could not read the password history", "user_id", user.ID, "error", err)
		return nil
	}
	for _, hash := range previous {
		if same, _ := m.passwordHasher.Verify(candidate, hash); same {
			return fmt.Errorf("this is one of your last %d passwords; choose one you have not used", depth)
		}
	}
	return nil
}

// Close releases what the manager holds open.
//
// Today that is the geoip database, which is a file mapped into memory for as
// long as it is open. Everything else here is owned by whoever supplied it.
func (m *Manager) Close() error {
	if m.geoip != nil {
		return m.geoip.Close()
	}
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

	// A session that is not extended by use ends a fixed time after it began.
	//
	// extend_on_activity was read by nothing, so every session slid forward:
	// each validated request refreshes the last-active time, which is what the
	// idle sweep reads, and a session in constant use never reached its idle
	// timeout. A deployment asking for a fixed window — sign in again after
	// thirty minutes however busy you were — got a sliding one.
	//
	// Writing it into the session's own expiry rather than teaching the sweep
	// a second rule means every store already enforces it, including Redis,
	// where the session is a key with a lifetime and no sweep runs at all. It
	// also leaves the last-active time truthful, so a session listing still
	// says when it was really used.
	if m.config.Sessions != nil && !m.config.Sessions.ExtendOnActivity && m.config.Sessions.IdleTimeout != "" {
		if idle, err := ParseDuration(m.config.Sessions.IdleTimeout); err == nil && idle > 0 {
			if fixed := now.Add(idle); fixed.Before(expiresAt) {
				expiresAt = fixed
			}
		}
	}

	session := &Session{
		ID:           sessionID,
		UserID:       user.ID,
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    expiresAt,
	}

	// What is kept about the person on the other end.
	//
	// The address and the browser string were recorded whatever the
	// configuration said, and `track` — the list naming what to record — was
	// read by nothing. That is the wrong way round for the one setting here
	// that is about not keeping something: an address is personal data in
	// most of the places this runs, and a deployment that listed only the
	// browser was storing addresses anyway.
	if m.sessionTracks("ip") {
		session.IP = ip
	}
	if m.sessionTracks("user_agent") {
		session.UserAgent = userAgent
	}

	if err := m.sessionStore.Create(ctx, session); err != nil {
		return nil, err
	}

	return session, nil
}

// sessionTracks reports whether an attribute is recorded on a session.
//
// Nothing configured means everything is recorded, which is what happened
// before the setting was honoured — turning it into an opt-in list would
// silently stop recording for every deployment that never asked.
func (m *Manager) sessionTracks(attribute string) bool {
	if m.config.Sessions == nil || len(m.config.Sessions.Track) == 0 {
		return true
	}
	for _, tracked := range m.config.Sessions.Track {
		if strings.EqualFold(strings.TrimSpace(tracked), attribute) {
			return true
		}
	}
	return false
}

// recordFailedLogin records a failed login attempt
func (m *Manager) recordFailedLogin(ctx context.Context, email, ip string) {
	if !m.bruteForceEnabled() {
		return
	}

	key := m.bruteForceKey(email, ip)
	locked, err := m.bruteForce.RecordFailedAttempt(ctx, key)
	if err != nil {
		m.logger.Warn("failed to record login attempt", "email", email, "ip", ip, "error", err)
		return
	}
	if locked {
		m.logger.Warn("account locked due to failed attempts", "email", email, "ip", ip)
	}
}

// bruteForceEnabled reports whether the configuration asked for protection.
func (m *Manager) bruteForceEnabled() bool {
	return m.bruteForce != nil && m.config.Security != nil &&
		m.config.Security.BruteForce != nil && m.config.Security.BruteForce.Enabled
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

// MFAEnabled reports whether this service can enrol and check a second factor
// at all — which decides whether the endpoints for it are worth serving.
func (m *Manager) MFAEnabled() bool {
	return m.mfaService != nil
}

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

// GetWebAuthnCredentials returns the passkeys on an account.
//
// An account with none has an empty list, not an error. Asking what keys I
// have before I have added any is the ordinary first visit to a settings page,
// and answering "MFA is not enabled for this user" would show a failure where
// the answer is simply "none yet".
func (m *Manager) GetWebAuthnCredentials(ctx context.Context, userID string) ([]WebAuthnCredential, error) {
	if m.mfaService == nil {
		return nil, nil
	}

	credentials, err := m.mfaService.GetWebAuthnCredentials(ctx, userID)
	if errors.Is(err, ErrMFANotEnabled) {
		return nil, nil
	}
	return credentials, err
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
