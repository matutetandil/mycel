// Package auth provides enterprise-grade authentication for Mycel
package auth

import "time"

// Config holds the complete auth configuration
type Config struct {
	// Preset: strict, standard, relaxed, development
	Preset string `hcl:"preset,optional"`

	// BaseURL is the address this service is reached at from outside, such as
	// https://app.example.com. Identity providers redirect a browser back to
	// it after a sign-in, so it has to be the public address rather than the
	// listening one, and it has to match what is registered with the provider.
	BaseURL string `hcl:"base_url,optional"`

	// Storage for tokens/sessions
	Storage *StorageConfig `hcl:"storage,block"`

	// Users storage configuration (for local provider)
	Users *UsersConfig `hcl:"users,block"`

	// JWT configuration
	JWT *JWTConfig `hcl:"jwt,block"`

	// Password policy
	Password *PasswordConfig `hcl:"password,block"`

	// MFA configuration
	MFA *MFAConfig `hcl:"mfa,block"`

	// Security features
	Security *SecurityConfig `hcl:"security,block"`

	// Session management
	Sessions *SessionsConfig `hcl:"sessions,block"`

	// Social login providers
	Social *SocialConfig `hcl:"social,block"`

	// SSO providers
	SSO *SSOConfig `hcl:"sso,block"`

	// External identity providers
	Providers []*ProviderConfig `hcl:"provider,block"`

	// Account linking
	AccountLinking *AccountLinkingConfig `hcl:"account_linking,block"`

	// Endpoints customization
	Endpoints *EndpointsConfig `hcl:"endpoints,block"`

	// Hooks for custom logic
	Hooks *HooksConfig `hcl:"hooks,block"`

	// Audit logging
	Audit *AuditConfig `hcl:"audit,block"`

	// Quick config: storage connector reference
	StorageConnector string `hcl:"storage,optional"`

	// Quick config: JWT secret
	Secret string `hcl:"secret,optional"`
}

// StorageConfig defines token/session storage
type StorageConfig struct {
	Driver   string `hcl:"driver"` // memory, redis, database
	Address  string `hcl:"address,optional"`
	Password string `hcl:"password,optional"`
	DB       int    `hcl:"db,optional"`

	// For database driver
	Connector string `hcl:"connector,optional"`
	Table     string `hcl:"table,optional"`
}

// UsersConfig defines user storage for local provider
type UsersConfig struct {
	Connector string        `hcl:"connector"`
	Table     string        `hcl:"table,optional"`
	Fields    *FieldsConfig `hcl:"fields,block"`
}

// FieldsConfig maps user table columns
type FieldsConfig struct {
	ID           string `hcl:"id,optional"`
	Email        string `hcl:"email,optional"`
	PasswordHash string `hcl:"password_hash,optional"`
	CreatedAt    string `hcl:"created_at,optional"`
	UpdatedAt    string `hcl:"updated_at,optional"`

	// Roles names the column a user's roles are stored in. Naming one is what
	// turns roles on for a SQL-backed store: without it the column is neither
	// written nor read, so a users table that already exists keeps working.
	Roles string `hcl:"roles,optional"`

	// PasswordChangedAt names the column recording when a password was last
	// set. Naming one is what turns password { max_age } on for a SQL-backed
	// store, on the same terms as roles.
	PasswordChangedAt string `hcl:"password_changed_at,optional"`
}

// JWTConfig defines JWT token settings
type JWTConfig struct {
	// Signing
	Algorithm  string `hcl:"algorithm,optional"`   // HS256, RS256, ES256, etc.
	Secret     string `hcl:"secret,optional"`      // For HS* algorithms
	PrivateKey string `hcl:"private_key,optional"` // For RS*/ES* algorithms
	PublicKey  string `hcl:"public_key,optional"`  // For RS*/ES* algorithms

	// Lifetimes
	AccessLifetime  string `hcl:"access_lifetime,optional"`
	RefreshLifetime string `hcl:"refresh_lifetime,optional"`

	// Claims
	Issuer   string   `hcl:"issuer,optional"`
	Audience []string `hcl:"audience,optional"`

	// Security
	Rotation bool `hcl:"rotation,optional"` // Rotate refresh token on use

	// Custom claims (CEL expressions)
	Claims map[string]string `hcl:"claims,optional"`
}

// PasswordConfig defines password policy
type PasswordConfig struct {
	// Required for registration (false = passwordless allowed)
	Required bool `hcl:"required,optional"`

	// Complexity
	MinLength      int  `hcl:"min_length,optional"`
	MaxLength      int  `hcl:"max_length,optional"`
	RequireUpper   bool `hcl:"require_upper,optional"`
	RequireLower   bool `hcl:"require_lower,optional"`
	RequireNumber  bool `hcl:"require_number,optional"`
	RequireSpecial bool `hcl:"require_special,optional"`

	// Patterns to reject
	RejectPatterns []string `hcl:"reject_patterns,optional"`

	// History
	History int `hcl:"history,optional"` // Can't reuse last N passwords

	// Expiration
	MaxAge     string `hcl:"max_age,optional"`
	WarnBefore string `hcl:"warn_before,optional"`

	// ResetTokenTTL is how long a password reset token is good for. Default
	// one hour: long enough to reach an inbox and be acted on, short enough
	// that a link left in one is not a standing key to the account.
	ResetTokenTTL string `hcl:"reset_token_ttl,optional"`

	// Breach check (HaveIBeenPwned)
	BreachCheck bool `hcl:"breach_check,optional"`

	// Hashing parameters
	Algorithm   string `hcl:"algorithm,optional"`
	Memory      uint32 `hcl:"memory,optional"`
	Iterations  uint32 `hcl:"iterations,optional"`
	Parallelism uint8  `hcl:"parallelism,optional"`
	SaltLength  uint32 `hcl:"salt_length,optional"`
	KeyLength   uint32 `hcl:"key_length,optional"`
}

// MFAConfig defines multi-factor authentication
type MFAConfig struct {
	Enabled    bool     `hcl:"enabled,optional"`  // Enables MFA support
	Required   string   `hcl:"required,optional"` // true, false, optional, admin_only
	Methods    []string `hcl:"methods,optional"`  // totp, webauthn, sms, email, push
	RequireFor []string `hcl:"require_for,optional"`

	// Multiple factors
	RequireMultiple bool `hcl:"require_multiple,optional"`
	MinFactors      int  `hcl:"min_factors,optional"`

	// Grace period for setup
	GracePeriod string `hcl:"grace_period,optional"`

	// Recovery codes
	Recovery *RecoveryConfig `hcl:"recovery,block"`

	// Method-specific configs
	TOTP     *TOTPConfig     `hcl:"totp,block"`
	WebAuthn *WebAuthnConfig `hcl:"webauthn,block"`
	SMS      *SMSConfig      `hcl:"sms,block"`
	Email    *EmailMFAConfig `hcl:"email,block"`
	Push     *PushConfig     `hcl:"push,block"`
}

// RecoveryConfig defines recovery codes settings
type RecoveryConfig struct {
	Enabled    bool `hcl:"enabled,optional"`
	CodeCount  int  `hcl:"code_count,optional"`
	CodeLength int  `hcl:"code_length,optional"`
	GroupSize  int  `hcl:"group_size,optional"` // Groups characters for readability
}

// TOTPConfig defines TOTP settings
type TOTPConfig struct {
	Issuer    string `hcl:"issuer,optional"`
	Digits    int    `hcl:"digits,optional"`
	Period    int    `hcl:"period,optional"`
	Algorithm string `hcl:"algorithm,optional"` // SHA1, SHA256, SHA512
	Skew      int    `hcl:"skew,optional"`      // Clock drift tolerance in periods
}

// WebAuthnConfig defines WebAuthn/FIDO2/Passkeys settings
type WebAuthnConfig struct {
	RPName        string   `hcl:"rp_name"`
	RPDisplayName string   `hcl:"rp_display_name,optional"` // Display name for RP
	RPID          string   `hcl:"rp_id"`
	Origins       []string `hcl:"origins"`
	RPOrigins     []string `hcl:"rp_origins,optional"` // Alias for origins

	// Authenticator requirements
	AuthenticatorAttachment string `hcl:"authenticator_attachment,optional"` // platform, cross-platform, any
	UserVerification        string `hcl:"user_verification,optional"`        // required, preferred, discouraged
	ResidentKey             string `hcl:"resident_key,optional"`             // required, preferred, discouraged

	// Limits
	MaxCredentials int `hcl:"max_credentials,optional"`

	// Attestation
	Attestation    string   `hcl:"attestation,optional"`     // none, indirect, direct
	AllowedAAGUIDs []string `hcl:"allowed_aaguids,optional"` // Whitelist of hardware keys

	// Timeout in milliseconds (default: 60000)
	Timeout int `hcl:"timeout,optional"`
}

// SMSConfig defines SMS MFA settings
type SMSConfig struct {
	Provider   string        `hcl:"provider"`
	Twilio     *TwilioConfig `hcl:"twilio,block"`
	CodeLength int           `hcl:"code_length,optional"`
	Expiry     string        `hcl:"expiry,optional"`
	RateLimit  string        `hcl:"rate_limit,optional"`
}

// TwilioConfig for SMS provider
type TwilioConfig struct {
	AccountSID string `hcl:"account_sid"`
	AuthToken  string `hcl:"auth_token"`
	FromNumber string `hcl:"from_number"`
}

// EmailMFAConfig defines email MFA settings
type EmailMFAConfig struct {
	Connector  string `hcl:"connector"`
	Template   string `hcl:"template,optional"`
	CodeLength int    `hcl:"code_length,optional"`
	Expiry     string `hcl:"expiry,optional"`
	RateLimit  string `hcl:"rate_limit,optional"`
}

// PushConfig defines push notification MFA
type PushConfig struct {
	Provider string          `hcl:"provider"`
	Firebase *FirebaseConfig `hcl:"firebase,block"`
	Expiry   string          `hcl:"expiry,optional"`
}

// FirebaseConfig for push notifications
type FirebaseConfig struct {
	Credentials string `hcl:"credentials"`
}

// SecurityConfig defines security features
type SecurityConfig struct {
	BruteForce       *BruteForceConfig       `hcl:"brute_force,block"`
	ImpossibleTravel *ImpossibleTravelConfig `hcl:"impossible_travel,block"`
	DeviceBinding    *DeviceBindingConfig    `hcl:"device_binding,block"`
	ReplayProtection *ReplayProtectionConfig `hcl:"replay_protection,block"`
	IPRules          *IPRulesConfig          `hcl:"ip_rules,block"`
	RateLimit        *RateLimitConfig        `hcl:"rate_limit,block"`
}

// BruteForceConfig defines brute force protection
type BruteForceConfig struct {
	Enabled     bool   `hcl:"enabled,optional"`
	MaxAttempts int    `hcl:"max_attempts,optional"`
	Window      string `hcl:"window,optional"`
	LockoutTime string `hcl:"lockout_time,optional"`
	TrackBy     string `hcl:"track_by,optional"` // ip, user, ip+user

	// FailDelay is how long a failed sign-in waits before answering, give or
	// take a quarter. Set it to "0" to answer immediately, which is only
	// sensible in a test.
	FailDelay string `hcl:"fail_delay,optional"`

	// Progressive delays
	ProgressiveDelay *ProgressiveDelayConfig `hcl:"progressive_delay,block"`
}

// ProgressiveDelayConfig for increasing delays
type ProgressiveDelayConfig struct {
	Enabled    bool    `hcl:"enabled,optional"`
	Initial    string  `hcl:"initial,optional"`
	Multiplier float64 `hcl:"multiplier,optional"`
	Max        string  `hcl:"max,optional"`
}

// ImpossibleTravelConfig detects suspicious logins
type ImpossibleTravelConfig struct {
	Enabled     bool         `hcl:"enabled,optional"`
	MaxSpeedKMH int          `hcl:"max_speed_kmh,optional"`
	OnDetect    string       `hcl:"on_detect,optional"` // block, challenge, notify
	GeoIP       *GeoIPConfig `hcl:"geoip,block"`
}

// GeoIPConfig for geolocation
type GeoIPConfig struct {
	Database string `hcl:"database,optional"`
	API      string `hcl:"api,optional"`
}

// DeviceBindingConfig for device fingerprinting
type DeviceBindingConfig struct {
	Enabled       bool     `hcl:"enabled,optional"`
	TrustDuration string   `hcl:"trust_duration,optional"`
	MaxDevices    int      `hcl:"max_devices,optional"`
	Fingerprint   []string `hcl:"fingerprint,optional"`
	OnNewDevice   string   `hcl:"on_new_device,optional"` // allow, challenge, block, notify
}

// ReplayProtectionConfig prevents token reuse
type ReplayProtectionConfig struct {
	Enabled bool   `hcl:"enabled,optional"`
	Window  string `hcl:"window,optional"`
}

// IPRulesConfig for allowlist/blocklist
type IPRulesConfig struct {
	Allowlist      []string `hcl:"allowlist,optional"`
	Blocklist      []string `hcl:"blocklist,optional"`
	BlockCountries []string `hcl:"block_countries,optional"`
	AllowCountries []string `hcl:"allow_countries,optional"`
}

// AuthRateLimitConfig for endpoint-specific rate limits
// SessionsConfig defines session management
type SessionsConfig struct {
	MaxActive        int      `hcl:"max_active,optional"`
	IdleTimeout      string   `hcl:"idle_timeout,optional"`
	AbsoluteTimeout  string   `hcl:"absolute_timeout,optional"`
	AllowList        bool     `hcl:"allow_list,optional"`
	AllowRevoke      bool     `hcl:"allow_revoke,optional"`
	Track            []string `hcl:"track,optional"`
	OnMaxReached     string   `hcl:"on_max_reached,optional"` // revoke_oldest, reject_new
	ExtendOnActivity bool     `hcl:"extend_on_activity,optional"`
}

// SocialConfig defines social login providers
type SocialConfig struct {
	Google *OAuthProviderConfig `hcl:"google,block"`
	GitHub *OAuthProviderConfig `hcl:"github,block"`
	Apple  *AppleConfig         `hcl:"apple,block"`
}

// OAuthProviderConfig for OAuth2 providers
type OAuthProviderConfig struct {
	ClientID     string   `hcl:"client_id"`
	ClientSecret string   `hcl:"client_secret"`
	Scopes       []string `hcl:"scopes,optional"`
}

// AppleConfig for Sign in with Apple
type AppleConfig struct {
	ClientID   string `hcl:"client_id"`
	TeamID     string `hcl:"team_id"`
	KeyID      string `hcl:"key_id"`
	PrivateKey string `hcl:"private_key"`
}

// SSOConfig defines SSO providers
type SSOConfig struct {
	OIDC []*OIDCConfig `hcl:"oidc,block"`
	SAML []*SAMLConfig `hcl:"saml,block"`

	// Linking decides what happens when someone signs in through a provider
	// with an address an account already uses. Absent, an identity with a
	// verified address is attached to that account.
	Linking *AccountLinkingConfig `hcl:"linking,block"`
}

// OIDCConfig for OpenID Connect
type OIDCConfig struct {
	Name         string            `hcl:"name,label"`
	Issuer       string            `hcl:"issuer"`
	ClientID     string            `hcl:"client_id"`
	ClientSecret string            `hcl:"client_secret"`
	Scopes       []string          `hcl:"scopes,optional"`
	Claims       map[string]string `hcl:"claims,optional"`
}

// SAMLConfig for SAML 2.0
type SAMLConfig struct {
	Name           string            `hcl:"name,label"`
	MetadataURL    string            `hcl:"metadata_url,optional"`
	IDPSSOURL      string            `hcl:"idp_sso_url,optional"`
	IDPCertificate string            `hcl:"idp_certificate,optional"`
	EntityID       string            `hcl:"entity_id"`
	ACSURL         string            `hcl:"acs_url"`
	Attributes     map[string]string `hcl:"attributes,optional"`
}

// ProviderConfig for external identity providers
type ProviderConfig struct {
	Name     string                  `hcl:"name,label"`
	Type     string                  `hcl:"type"`     // http
	Validate string                  `hcl:"validate"` // URL pattern
	Request  map[string]string       `hcl:"request,optional"`
	Response *ProviderResponseConfig `hcl:"response,block"`
	SyncTo   string                  `hcl:"sync_to,optional"`
}

// ProviderResponseConfig maps provider response
type ProviderResponseConfig struct {
	Success string `hcl:"success"`
	Token   string `hcl:"token,optional"`
	UserID  string `hcl:"user_id,optional"`
	Email   string `hcl:"email,optional"`
	Roles   string `hcl:"roles,optional"`
}

// AccountLinkingConfig for linking accounts
type AccountLinkingConfig struct {
	Enabled             bool   `hcl:"enabled,optional"`
	MatchBy             string `hcl:"match_by,optional"` // email, phone, custom
	RequireVerification bool   `hcl:"require_verification,optional"`
	OnMatch             string `hcl:"on_match,optional"` // link, prompt, reject
	CustomMatch         string `hcl:"custom_match,optional"`
}

// EndpointsConfig customizes endpoint paths
type EndpointsConfig struct {
	Prefix string `hcl:"prefix,optional"`

	Login          *EndpointConfig `hcl:"login,block"`
	Logout         *EndpointConfig `hcl:"logout,block"`
	Register       *EndpointConfig `hcl:"register,block"`
	Refresh        *EndpointConfig `hcl:"refresh,block"`
	Me             *EndpointConfig `hcl:"me,block"`
	PasswordForgot *EndpointConfig `hcl:"password_forgot,block"`
	PasswordReset  *EndpointConfig `hcl:"password_reset,block"`
	PasswordChange *EndpointConfig `hcl:"password_change,block"`
	SessionsList   *EndpointConfig `hcl:"sessions_list,block"`
	SessionsRevoke *EndpointConfig `hcl:"sessions_revoke,block"`
	MFASetup       *EndpointConfig `hcl:"mfa_setup,block"`
	MFAVerify      *EndpointConfig `hcl:"mfa_verify,block"`
	MFADisable     *EndpointConfig `hcl:"mfa_disable,block"`
	MFARecovery    *EndpointConfig `hcl:"mfa_recovery,block"`
	SocialCallback *EndpointConfig `hcl:"social_callback,block"`
	SSOCallback    *EndpointConfig `hcl:"sso_callback,block"`
}

// EndpointConfig for individual endpoint
type EndpointConfig struct {
	Path    string `hcl:"path,optional"`
	Method  string `hcl:"method,optional"`
	Enabled bool   `hcl:"enabled,optional"`
}

// HooksConfig defines lifecycle hooks
type HooksConfig struct {
	BeforeLogin          *HookConfig `hcl:"before_login,block"`
	AfterLogin           *HookConfig `hcl:"after_login,block"`
	AfterRegister        *HookConfig `hcl:"after_register,block"`
	OnFailedLogin        *HookConfig `hcl:"on_failed_login,block"`
	OnSuspiciousActivity *HookConfig `hcl:"on_suspicious_activity,block"`
	OnPasswordReset      *HookConfig `hcl:"on_password_reset,block"`
	BeforePasswordChange *HookConfig `hcl:"before_password_change,block"`
	AfterPasswordChange  *HookConfig `hcl:"after_password_change,block"`
}

// HookConfig defines a single hook: a flow to run when something happens to an
// account, and the terms it runs on.
type HookConfig struct {
	// Flow is the name of the flow to invoke.
	Flow string `hcl:"flow"`

	// Condition is a CEL expression over the event. The flow runs only when it
	// is true, so one hook can serve a case narrower than the event itself:
	// `auth.reason == "new_device"`.
	Condition string `hcl:"condition,optional"`

	// OnError decides what a failing flow means. "ignore", the default, logs
	// it and carries on; "fail" refuses whatever the hook was attached to,
	// which is only useful on a before_ hook — refusing after the fact would
	// leave the account changed and the caller told otherwise.
	OnError string `hcl:"on_error,optional"`
}

// AuditConfig defines audit logging
type AuditConfig struct {
	Enabled   bool     `hcl:"enabled,optional"`
	Connector string   `hcl:"connector"`
	Table     string   `hcl:"table,optional"`
	Events    []string `hcl:"events,optional"`
	Include   []string `hcl:"include,optional"`
	Retention string   `hcl:"retention,optional"`
	StreamTo  string   `hcl:"stream_to,optional"`
}

// User represents a user in the system
type User struct {
	ID           string                 `json:"id"`
	Email        string                 `json:"email"`
	PasswordHash string                 `json:"-"`
	Roles        []string               `json:"roles,omitempty"`
	Permissions  []string               `json:"permissions,omitempty"`
	MFAEnabled   bool                   `json:"mfa_enabled"`
	MFAMethods   []string               `json:"mfa_methods,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	LastLoginAt  *time.Time             `json:"last_login_at,omitempty"`

	// PasswordChangedAt is when the password was last set, which is what
	// password { max_age } is measured from. Nil means it is not known: an
	// account from before this was recorded, or a SQL store whose fields block
	// names no column for it.
	PasswordChangedAt *time.Time `json:"password_changed_at,omitempty"`
}

// Session represents an active session
type Session struct {
	ID           string                 `json:"id"`
	UserID       string                 `json:"user_id"`
	DeviceID     string                 `json:"device_id,omitempty"`
	IP           string                 `json:"ip,omitempty"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	Location     string                 `json:"location,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	LastActiveAt time.Time              `json:"last_active_at"`
	ExpiresAt    time.Time              `json:"expires_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// TokenPair represents access and refresh tokens
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// LoginRequest for login endpoint
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	MFACode  string `json:"mfa_code,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
}

// RegisterRequest for registration endpoint
type RegisterRequest struct {
	Email    string                 `json:"email"`
	Password string                 `json:"password"`
	Name     string                 `json:"name,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// RefreshRequest for token refresh
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// AuthError represents an authentication error
type AuthError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *AuthError) Error() string {
	return e.Message
}

// Common auth errors
var (
	ErrInvalidCredentials = &AuthError{Code: "invalid_credentials", Message: "Invalid email or password"}
	ErrUserNotFound       = &AuthError{Code: "user_not_found", Message: "User not found"}
	ErrUserExists         = &AuthError{Code: "user_exists", Message: "User already exists"}
	ErrUserAlreadyExists  = ErrUserExists // Alias
	ErrInvalidToken       = &AuthError{Code: "invalid_token", Message: "Invalid or expired token"}
	ErrTokenExpired       = &AuthError{Code: "token_expired", Message: "Token has expired"}
	ErrMFARequired        = &AuthError{Code: "mfa_required", Message: "MFA verification required"}
	ErrInvalidMFACode     = &AuthError{Code: "invalid_mfa_code", Message: "Invalid MFA code"}
	ErrAccountLocked      = &AuthError{Code: "account_locked", Message: "Account is temporarily locked"}
	ErrSessionExpired     = &AuthError{Code: "session_expired", Message: "Session has expired"}
	ErrSessionNotFound    = &AuthError{Code: "session_not_found", Message: "Session not found"}
	ErrPasswordExpired    = &AuthError{Code: "password_expired", Message: "Password has expired"}
	ErrWeakPassword       = &AuthError{Code: "weak_password", Message: "Password does not meet requirements"}
	ErrBreachedPassword   = &AuthError{Code: "breached_password", Message: "Password found in data breach"}
	ErrInvalidResetToken  = &AuthError{Code: "invalid_reset_token", Message: "This reset link is not valid or has expired"}
)

// UserFieldsConfig is an alias for FieldsConfig
type UserFieldsConfig = FieldsConfig
