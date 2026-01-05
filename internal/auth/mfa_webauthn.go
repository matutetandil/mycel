package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthnService handles WebAuthn/Passkeys operations
type WebAuthnService struct {
	config   *WebAuthnConfig
	webauthn *webauthn.WebAuthn
}

// NewWebAuthnService creates a new WebAuthn service
func NewWebAuthnService(config *WebAuthnConfig) *WebAuthnService {
	if config == nil {
		return nil
	}

	// Set defaults
	displayName := config.RPDisplayName
	if displayName == "" {
		displayName = config.RPName
	}
	if displayName == "" {
		displayName = "Mycel Application"
	}
	if config.Timeout == 0 {
		config.Timeout = 60000 // 60 seconds
	}
	if config.UserVerification == "" {
		config.UserVerification = "preferred"
	}

	// Use RPOrigins if set, otherwise Origins
	origins := config.RPOrigins
	if len(origins) == 0 {
		origins = config.Origins
	}

	svc := &WebAuthnService{config: config}

	// Initialize WebAuthn if RPID is configured
	if config.RPID != "" {
		wa, err := webauthn.New(&webauthn.Config{
			RPDisplayName: displayName,
			RPID:          config.RPID,
			RPOrigins:     origins,
			Timeouts: webauthn.TimeoutsConfig{
				Login: webauthn.TimeoutConfig{
					Enforce:    true,
					Timeout:    time.Duration(config.Timeout) * time.Millisecond,
					TimeoutUVD: time.Duration(config.Timeout) * time.Millisecond,
				},
				Registration: webauthn.TimeoutConfig{
					Enforce:    true,
					Timeout:    time.Duration(config.Timeout) * time.Millisecond,
					TimeoutUVD: time.Duration(config.Timeout) * time.Millisecond,
				},
			},
		})
		if err == nil {
			svc.webauthn = wa
		}
	}

	return svc
}

// IsConfigured returns true if WebAuthn is properly configured
func (s *WebAuthnService) IsConfigured() bool {
	return s != nil && s.webauthn != nil
}

// WebAuthnUser implements webauthn.User interface
type WebAuthnUser struct {
	ID          string
	Name        string
	DisplayName string
	Credentials []webauthn.Credential
}

func (u *WebAuthnUser) WebAuthnID() []byte {
	return []byte(u.ID)
}

func (u *WebAuthnUser) WebAuthnName() string {
	return u.Name
}

func (u *WebAuthnUser) WebAuthnDisplayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}

func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

func (u *WebAuthnUser) WebAuthnIcon() string {
	return ""
}

// BeginRegistration starts the WebAuthn registration ceremony
func (s *WebAuthnService) BeginRegistration(ctx context.Context, userID, userName, displayName string, existingCredentials []WebAuthnCredential) (*protocol.CredentialCreation, string, error) {
	if !s.IsConfigured() {
		return nil, "", ErrWebAuthnNotConfigured
	}

	// Convert existing credentials to webauthn.Credential for user
	var credentials []webauthn.Credential
	for _, cred := range existingCredentials {
		credentials = append(credentials, webauthn.Credential{
			ID:        []byte(cred.ID),
			PublicKey: cred.PublicKey,
		})
	}

	// Convert to CredentialDescriptor for exclusions
	var exclusions []protocol.CredentialDescriptor
	for _, cred := range existingCredentials {
		exclusions = append(exclusions, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: []byte(cred.ID),
		})
	}

	user := &WebAuthnUser{
		ID:          userID,
		Name:        userName,
		DisplayName: displayName,
		Credentials: credentials,
	}

	// Set attestation preference
	attestation := protocol.PreferNoAttestation
	switch s.config.Attestation {
	case "indirect":
		attestation = protocol.PreferIndirectAttestation
	case "direct":
		attestation = protocol.PreferDirectAttestation
	}

	// Set user verification
	userVerification := protocol.VerificationPreferred
	switch s.config.UserVerification {
	case "required":
		userVerification = protocol.VerificationRequired
	case "discouraged":
		userVerification = protocol.VerificationDiscouraged
	}

	options, session, err := s.webauthn.BeginRegistration(
		user,
		webauthn.WithConveyancePreference(attestation),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: userVerification,
		}),
		webauthn.WithExclusions(exclusions),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to begin registration: %w", err)
	}

	// Serialize session for storage
	sessionData, err := json.Marshal(session)
	if err != nil {
		return nil, "", fmt.Errorf("failed to serialize session: %w", err)
	}

	return options, string(sessionData), nil
}

// FinishRegistration completes the WebAuthn registration ceremony
func (s *WebAuthnService) FinishRegistration(ctx context.Context, userID, userName, displayName string, existingCredentials []WebAuthnCredential, sessionData string, response *protocol.ParsedCredentialCreationData) (*WebAuthnCredential, error) {
	if !s.IsConfigured() {
		return nil, ErrWebAuthnNotConfigured
	}

	// Deserialize session
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
		return nil, fmt.Errorf("failed to deserialize session: %w", err)
	}

	// Convert existing credentials
	var credentials []webauthn.Credential
	for _, cred := range existingCredentials {
		credentials = append(credentials, webauthn.Credential{
			ID:        []byte(cred.ID),
			PublicKey: cred.PublicKey,
		})
	}

	user := &WebAuthnUser{
		ID:          userID,
		Name:        userName,
		DisplayName: displayName,
		Credentials: credentials,
	}

	// Verify the response
	credential, err := s.webauthn.CreateCredential(user, session, response)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential: %w", err)
	}

	// Convert to our credential type
	now := time.Now()
	return &WebAuthnCredential{
		ID:              string(credential.ID),
		PublicKey:       credential.PublicKey,
		AttestationType: credential.AttestationType,
		AAGUID:          credential.Authenticator.AAGUID,
		SignCount:       credential.Authenticator.SignCount,
		CreatedAt:       now,
		LastUsedAt:      now,
	}, nil
}

// BeginLogin starts the WebAuthn login ceremony
func (s *WebAuthnService) BeginLogin(ctx context.Context, userID, userName string, credentials []WebAuthnCredential) (*protocol.CredentialAssertion, string, error) {
	if !s.IsConfigured() {
		return nil, "", ErrWebAuthnNotConfigured
	}

	// Convert credentials
	var webauthnCreds []webauthn.Credential
	for _, cred := range credentials {
		webauthnCreds = append(webauthnCreds, webauthn.Credential{
			ID:        []byte(cred.ID),
			PublicKey: cred.PublicKey,
			Authenticator: webauthn.Authenticator{
				AAGUID:    cred.AAGUID,
				SignCount: cred.SignCount,
			},
		})
	}

	user := &WebAuthnUser{
		ID:          userID,
		Name:        userName,
		Credentials: webauthnCreds,
	}

	// Set user verification
	userVerification := protocol.VerificationPreferred
	switch s.config.UserVerification {
	case "required":
		userVerification = protocol.VerificationRequired
	case "discouraged":
		userVerification = protocol.VerificationDiscouraged
	}

	options, session, err := s.webauthn.BeginLogin(
		user,
		webauthn.WithUserVerification(userVerification),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to begin login: %w", err)
	}

	// Serialize session
	sessionData, err := json.Marshal(session)
	if err != nil {
		return nil, "", fmt.Errorf("failed to serialize session: %w", err)
	}

	return options, string(sessionData), nil
}

// FinishLogin completes the WebAuthn login ceremony
func (s *WebAuthnService) FinishLogin(ctx context.Context, userID, userName string, credentials []WebAuthnCredential, sessionData string, response *protocol.ParsedCredentialAssertionData) (*WebAuthnCredential, error) {
	if !s.IsConfigured() {
		return nil, ErrWebAuthnNotConfigured
	}

	// Deserialize session
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
		return nil, fmt.Errorf("failed to deserialize session: %w", err)
	}

	// Convert credentials
	var webauthnCreds []webauthn.Credential
	for _, cred := range credentials {
		webauthnCreds = append(webauthnCreds, webauthn.Credential{
			ID:        []byte(cred.ID),
			PublicKey: cred.PublicKey,
			Authenticator: webauthn.Authenticator{
				AAGUID:    cred.AAGUID,
				SignCount: cred.SignCount,
			},
		})
	}

	user := &WebAuthnUser{
		ID:          userID,
		Name:        userName,
		Credentials: webauthnCreds,
	}

	// Verify the response
	updatedCredential, err := s.webauthn.ValidateLogin(user, session, response)
	if err != nil {
		return nil, fmt.Errorf("failed to validate login: %w", err)
	}

	// Find the updated credential
	for i, cred := range credentials {
		if cred.ID == string(updatedCredential.ID) {
			credentials[i].SignCount = updatedCredential.Authenticator.SignCount
			credentials[i].LastUsedAt = time.Now()
			return &credentials[i], nil
		}
	}

	return nil, fmt.Errorf("credential not found")
}

// AddWebAuthnCredential adds a new WebAuthn credential to user's MFA data
func (s *MFAService) AddWebAuthnCredential(ctx context.Context, userID string, cred *WebAuthnCredential, name string) error {
	if !s.config.Enabled {
		return ErrMFANotEnabled
	}
	if s.webauthn == nil || !s.webauthn.IsConfigured() {
		return ErrWebAuthnNotConfigured
	}

	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil {
		// Create new MFA data
		data = &MFAUserData{
			UserID:    userID,
			CreatedAt: time.Now(),
		}
	}

	cred.Name = name
	data.WebAuthnCredentials = append(data.WebAuthnCredentials, *cred)
	data.UpdatedAt = time.Now()

	// Generate recovery codes if this is the first MFA method
	if !data.TOTPEnabled && len(data.WebAuthnCredentials) == 1 && len(data.RecoveryCodes) == 0 {
		_, hashedCodes, err := s.generateRecoveryCodes()
		if err != nil {
			return fmt.Errorf("failed to generate recovery codes: %w", err)
		}
		data.RecoveryCodes = hashedCodes
		data.RecoveryCodesGen = time.Now()
	}

	return s.store.SaveMFAData(ctx, data)
}

// RemoveWebAuthnCredential removes a WebAuthn credential
func (s *MFAService) RemoveWebAuthnCredential(ctx context.Context, userID, credentialID string) error {
	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil {
		return ErrMFANotEnabled
	}

	found := false
	newCreds := make([]WebAuthnCredential, 0, len(data.WebAuthnCredentials)-1)
	for _, cred := range data.WebAuthnCredentials {
		if cred.ID == credentialID {
			found = true
			continue
		}
		newCreds = append(newCreds, cred)
	}

	if !found {
		return fmt.Errorf("credential not found")
	}

	data.WebAuthnCredentials = newCreds
	data.UpdatedAt = time.Now()

	// If no MFA methods left, clear recovery codes
	if !data.TOTPEnabled && len(data.WebAuthnCredentials) == 0 {
		data.RecoveryCodes = nil
	}

	return s.store.SaveMFAData(ctx, data)
}

// GetWebAuthnCredentials returns all WebAuthn credentials for a user
func (s *MFAService) GetWebAuthnCredentials(ctx context.Context, userID string) ([]WebAuthnCredential, error) {
	data, err := s.store.GetMFAData(ctx, userID)
	if err != nil {
		return nil, err
	}
	return data.WebAuthnCredentials, nil
}
