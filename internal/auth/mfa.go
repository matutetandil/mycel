package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MFA errors
var (
	ErrMFANotEnabled       = errors.New("MFA is not enabled for this user")
	ErrMFAAlreadyEnabled   = errors.New("MFA is already enabled")
	ErrInvalidRecoveryCode = errors.New("invalid recovery code")
	ErrNoRecoveryCodesLeft = errors.New("no recovery codes remaining")
	ErrMFASetupIncomplete  = errors.New("MFA setup not completed")
	ErrWebAuthnNotConfigured = errors.New("WebAuthn not configured")
)

// MFAMethod represents the type of MFA
type MFAMethod string

const (
	MFAMethodTOTP     MFAMethod = "totp"
	MFAMethodWebAuthn MFAMethod = "webauthn"
	MFAMethodRecovery MFAMethod = "recovery"
)

// MFAStatus represents a user's MFA status
type MFAStatus struct {
	Enabled          bool        `json:"enabled"`
	Methods          []MFAMethod `json:"methods"`
	TOTPConfigured   bool        `json:"totp_configured"`
	WebAuthnConfigured bool      `json:"webauthn_configured"`
	RecoveryCodesLeft int        `json:"recovery_codes_left"`
	RequiredByPolicy bool        `json:"required_by_policy"`
}

// MFASetup represents MFA setup data
type MFASetup struct {
	Method        MFAMethod `json:"method"`
	Secret        string    `json:"secret,omitempty"`        // For TOTP
	QRCode        string    `json:"qr_code,omitempty"`       // Base64 PNG for TOTP
	ProvisioningURI string  `json:"provisioning_uri,omitempty"` // otpauth:// URI
	RecoveryCodes []string  `json:"recovery_codes,omitempty"` // For recovery
}

// MFAUserData stores MFA data for a user
type MFAUserData struct {
	UserID           string    `json:"user_id"`
	TOTPSecret       string    `json:"totp_secret,omitempty"`
	TOTPEnabled      bool      `json:"totp_enabled"`
	TOTPVerifiedAt   time.Time `json:"totp_verified_at,omitempty"`
	RecoveryCodes    []string  `json:"recovery_codes,omitempty"` // Hashed codes
	RecoveryCodesGen time.Time `json:"recovery_codes_generated,omitempty"`
	WebAuthnCredentials []WebAuthnCredential `json:"webauthn_credentials,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WebAuthnCredential stores a WebAuthn credential
type WebAuthnCredential struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	PublicKey       []byte    `json:"public_key"`
	AttestationType string    `json:"attestation_type"`
	AAGUID          []byte    `json:"aaguid"`
	SignCount       uint32    `json:"sign_count"`
	CreatedAt       time.Time `json:"created_at"`
	LastUsedAt      time.Time `json:"last_used_at"`
}

// MFAStore defines the interface for MFA data storage
type MFAStore interface {
	// GetMFAData retrieves MFA data for a user
	GetMFAData(ctx context.Context, userID string) (*MFAUserData, error)

	// SaveMFAData saves MFA data for a user
	SaveMFAData(ctx context.Context, data *MFAUserData) error

	// DeleteMFAData removes all MFA data for a user
	DeleteMFAData(ctx context.Context, userID string) error
}

// MFAService provides MFA functionality
type MFAService struct {
	config   *MFAConfig
	store    MFAStore
	totp     *TOTPService
	webauthn *WebAuthnService
	hasher   *PasswordHasher
	mu       sync.RWMutex
}

// NewMFAService creates a new MFA service
func NewMFAService(config *MFAConfig, store MFAStore) *MFAService {
	if config == nil {
		config = &MFAConfig{Enabled: false}
	}

	// Set defaults
	if config.TOTP == nil {
		config.TOTP = &TOTPConfig{}
	}
	if config.TOTP.Issuer == "" {
		config.TOTP.Issuer = "Mycel"
	}
	if config.TOTP.Algorithm == "" {
		config.TOTP.Algorithm = "SHA1"
	}
	if config.TOTP.Digits == 0 {
		config.TOTP.Digits = 6
	}
	if config.TOTP.Period == 0 {
		config.TOTP.Period = 30
	}
	if config.TOTP.Skew == 0 {
		config.TOTP.Skew = 1
	}

	if config.Recovery == nil {
		config.Recovery = &RecoveryConfig{}
	}
	if config.Recovery.CodeCount == 0 {
		config.Recovery.CodeCount = 10
	}
	if config.Recovery.CodeLength == 0 {
		config.Recovery.CodeLength = 8
	}
	if config.Recovery.GroupSize == 0 {
		config.Recovery.GroupSize = 4
	}

	svc := &MFAService{
		config: config,
		store:  store,
		totp:   NewTOTPService(config.TOTP),
		hasher: NewPasswordHasher(nil),
	}

	if config.WebAuthn != nil {
		svc.webauthn = NewWebAuthnService(config.WebAuthn)
	}

	return svc
}

// GetStatus returns the MFA status for a user
func (s *MFAService) GetStatus(ctx context.Context, userID string) (*MFAStatus, error) {
	if !s.config.Enabled {
		return &MFAStatus{Enabled: false}, nil
	}

	isRequired := s.config.Required == "true" || s.config.Required == "admin_only"

	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil {
		// No MFA data yet
		return &MFAStatus{
			Enabled:          false,
			Methods:          []MFAMethod{},
			RequiredByPolicy: isRequired,
		}, nil
	}

	status := &MFAStatus{
		Enabled:            data.TOTPEnabled || len(data.WebAuthnCredentials) > 0,
		Methods:            []MFAMethod{},
		TOTPConfigured:     data.TOTPEnabled,
		WebAuthnConfigured: len(data.WebAuthnCredentials) > 0,
		RecoveryCodesLeft:  len(data.RecoveryCodes),
		RequiredByPolicy:   isRequired,
	}

	if data.TOTPEnabled {
		status.Methods = append(status.Methods, MFAMethodTOTP)
	}
	if len(data.WebAuthnCredentials) > 0 {
		status.Methods = append(status.Methods, MFAMethodWebAuthn)
	}

	return status, nil
}

// BeginTOTPSetup starts TOTP setup for a user
func (s *MFAService) BeginTOTPSetup(ctx context.Context, userID, email string) (*MFASetup, error) {
	if !s.config.Enabled {
		return nil, ErrMFANotEnabled
	}

	// Check if TOTP is already enabled
	data, _ := s.store.GetMFAData(ctx, userID)
	if data != nil && data.TOTPEnabled {
		return nil, ErrMFAAlreadyEnabled
	}

	// Generate new secret
	secret, err := s.totp.GenerateSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to generate TOTP secret: %w", err)
	}

	// Generate provisioning URI
	uri := s.totp.GenerateURI(secret, email)

	// Generate QR code
	qrCode, err := s.totp.GenerateQRCode(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code: %w", err)
	}

	// Store the secret (not yet enabled)
	now := time.Now()
	if data == nil {
		data = &MFAUserData{
			UserID:    userID,
			CreatedAt: now,
		}
	}
	data.TOTPSecret = secret
	data.UpdatedAt = now

	if err := s.store.SaveMFAData(ctx, data); err != nil {
		return nil, fmt.Errorf("failed to save MFA data: %w", err)
	}

	return &MFASetup{
		Method:          MFAMethodTOTP,
		Secret:          secret,
		QRCode:          qrCode,
		ProvisioningURI: uri,
	}, nil
}

// ConfirmTOTPSetup verifies and enables TOTP
func (s *MFAService) ConfirmTOTPSetup(ctx context.Context, userID, code string) ([]string, error) {
	if !s.config.Enabled {
		return nil, ErrMFANotEnabled
	}

	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil || data == nil || data.TOTPSecret == "" {
		return nil, ErrMFASetupIncomplete
	}

	// Verify the code
	if !s.totp.Validate(data.TOTPSecret, code) {
		return nil, ErrInvalidMFACode
	}

	// Generate recovery codes
	recoveryCodes, hashedCodes, err := s.generateRecoveryCodes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate recovery codes: %w", err)
	}

	// Enable TOTP and save recovery codes
	now := time.Now()
	data.TOTPEnabled = true
	data.TOTPVerifiedAt = now
	data.RecoveryCodes = hashedCodes
	data.RecoveryCodesGen = now
	data.UpdatedAt = now

	if err := s.store.SaveMFAData(ctx, data); err != nil {
		return nil, fmt.Errorf("failed to save MFA data: %w", err)
	}

	return recoveryCodes, nil
}

// ValidateTOTP validates a TOTP code
func (s *MFAService) ValidateTOTP(ctx context.Context, userID, code string) error {
	if !s.config.Enabled {
		return ErrMFANotEnabled
	}

	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil || data == nil || !data.TOTPEnabled {
		return ErrMFANotEnabled
	}

	if !s.totp.Validate(data.TOTPSecret, code) {
		return ErrInvalidMFACode
	}

	return nil
}

// ValidateRecoveryCode validates and consumes a recovery code
func (s *MFAService) ValidateRecoveryCode(ctx context.Context, userID, code string) error {
	if !s.config.Enabled {
		return ErrMFANotEnabled
	}

	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil || data == nil {
		return ErrMFANotEnabled
	}

	if len(data.RecoveryCodes) == 0 {
		return ErrNoRecoveryCodesLeft
	}

	// Normalize code
	normalizedCode := normalizeRecoveryCode(code)

	// Find and verify the code
	foundIdx := -1
	for i, hashedCode := range data.RecoveryCodes {
		if valid, _ := s.hasher.Verify(normalizedCode, hashedCode); valid {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		return ErrInvalidRecoveryCode
	}

	// Remove the used code
	data.RecoveryCodes = append(data.RecoveryCodes[:foundIdx], data.RecoveryCodes[foundIdx+1:]...)
	data.UpdatedAt = time.Now()

	if err := s.store.SaveMFAData(ctx, data); err != nil {
		return fmt.Errorf("failed to update MFA data: %w", err)
	}

	return nil
}

// RegenerateRecoveryCodes generates new recovery codes
func (s *MFAService) RegenerateRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	if !s.config.Enabled {
		return nil, ErrMFANotEnabled
	}

	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil || data == nil {
		return nil, ErrMFANotEnabled
	}

	// User must have MFA enabled to regenerate codes
	if !data.TOTPEnabled && len(data.WebAuthnCredentials) == 0 {
		return nil, ErrMFANotEnabled
	}

	// Generate new codes
	recoveryCodes, hashedCodes, err := s.generateRecoveryCodes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate recovery codes: %w", err)
	}

	// Save new codes
	now := time.Now()
	data.RecoveryCodes = hashedCodes
	data.RecoveryCodesGen = now
	data.UpdatedAt = now

	if err := s.store.SaveMFAData(ctx, data); err != nil {
		return nil, fmt.Errorf("failed to save MFA data: %w", err)
	}

	return recoveryCodes, nil
}

// DisableTOTP disables TOTP for a user
func (s *MFAService) DisableTOTP(ctx context.Context, userID string) error {
	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil || data == nil {
		return ErrMFANotEnabled
	}

	data.TOTPEnabled = false
	data.TOTPSecret = ""
	data.TOTPVerifiedAt = time.Time{}
	data.UpdatedAt = time.Now()

	// If no other MFA methods, clear recovery codes too
	if len(data.WebAuthnCredentials) == 0 {
		data.RecoveryCodes = nil
	}

	return s.store.SaveMFAData(ctx, data)
}

// generateRecoveryCodes generates recovery codes and their hashes
func (s *MFAService) generateRecoveryCodes() (plainCodes []string, hashedCodes []string, err error) {
	count := s.config.Recovery.CodeCount
	length := s.config.Recovery.CodeLength
	groupSize := s.config.Recovery.GroupSize

	plainCodes = make([]string, count)
	hashedCodes = make([]string, count)

	for i := 0; i < count; i++ {
		code, err := generateRandomCode(length)
		if err != nil {
			return nil, nil, err
		}

		// Format with groups for display
		formatted := formatCodeWithGroups(code, groupSize)
		plainCodes[i] = formatted

		// Hash the normalized code for storage
		hash, err := s.hasher.Hash(code)
		if err != nil {
			return nil, nil, err
		}
		hashedCodes[i] = hash
	}

	return plainCodes, hashedCodes, nil
}

// generateRandomCode generates a random alphanumeric code
func generateRandomCode(length int) (string, error) {
	// Use base32 alphabet (A-Z, 2-7) for readability
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	for i := range bytes {
		bytes[i] = alphabet[bytes[i]%byte(len(alphabet))]
	}

	return string(bytes), nil
}

// formatCodeWithGroups formats a code with separator groups
func formatCodeWithGroups(code string, groupSize int) string {
	if groupSize <= 0 {
		return code
	}

	var groups []string
	for i := 0; i < len(code); i += groupSize {
		end := i + groupSize
		if end > len(code) {
			end = len(code)
		}
		groups = append(groups, code[i:end])
	}

	return strings.Join(groups, "-")
}

// normalizeRecoveryCode removes formatting from a recovery code
func normalizeRecoveryCode(code string) string {
	// Remove dashes and spaces, uppercase
	code = strings.ToUpper(code)
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// MemoryMFAStore implements MFAStore in memory
type MemoryMFAStore struct {
	mu   sync.RWMutex
	data map[string]*MFAUserData
}

// NewMemoryMFAStore creates a new in-memory MFA store
func NewMemoryMFAStore() *MemoryMFAStore {
	return &MemoryMFAStore{
		data: make(map[string]*MFAUserData),
	}
}

func (s *MemoryMFAStore) GetMFAData(ctx context.Context, userID string) (*MFAUserData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, exists := s.data[userID]
	if !exists {
		return nil, ErrMFANotEnabled
	}

	// Return a copy
	copy := *data
	return &copy, nil
}

func (s *MemoryMFAStore) SaveMFAData(ctx context.Context, data *MFAUserData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy
	copy := *data
	s.data[data.UserID] = &copy
	return nil
}

func (s *MemoryMFAStore) DeleteMFAData(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, userID)
	return nil
}

// GenerateSecret generates a random base32-encoded secret
func GenerateSecret(length int) (string, error) {
	if length <= 0 {
		length = 20 // 160 bits, recommended for TOTP
	}

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes), nil
}
