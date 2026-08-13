package auth

import (
	"context"

	"github.com/go-webauthn/webauthn/protocol"
)

// The sign-in half of a passkey ceremony.
//
// The manager could begin and finish a registration, and could do neither for a
// sign-in — the service underneath had both, and nothing above it reached them.
// A passkey that can be registered and never used is a key with no lock.

// WebAuthnEnabled reports whether this service can run the ceremony at all,
// which decides whether the routes for it are worth serving.
func (m *Manager) WebAuthnEnabled() bool {
	return m.mfaService != nil && m.mfaService.WebAuthn() != nil && m.mfaService.WebAuthn().IsConfigured()
}

// BeginWebAuthnLogin issues the options a browser hands to its authenticator.
//
// It is asked for by address, before anybody is signed in — that is what a
// passkey is for. The answer is deliberately the same shape whether or not the
// address has an account with a key on it: a different one would turn this into
// a way of asking which addresses are registered, which is exactly the question
// a sign-in page must not answer.
func (m *Manager) BeginWebAuthnLogin(ctx context.Context, email string) (interface{}, string, error) {
	if !m.WebAuthnEnabled() {
		return nil, "", &AuthError{Code: "webauthn_not_configured", Message: "WebAuthn is not enabled in configuration"}
	}

	user, err := m.userStore.FindByEmail(ctx, email)
	if err != nil {
		return nil, "", ErrWebAuthnNoCredentials
	}

	credentials, err := m.mfaService.GetWebAuthnCredentials(ctx, user.ID)
	if err != nil || len(credentials) == 0 {
		return nil, "", ErrWebAuthnNoCredentials
	}

	return m.mfaService.WebAuthn().BeginLogin(ctx, user.ID, user.Email, credentials)
}

// FinishWebAuthnLogin checks what the authenticator signed and opens a session.
//
// The session is established the same way a password sign-in establishes one,
// so the session limit, the token pair and the last-login record are the same
// for both — two places where a session is born would drift.
func (m *Manager) FinishWebAuthnLogin(ctx context.Context, email, sessionData string, response interface{}, ip, userAgent string) (*User, *TokenPair, error) {
	if !m.WebAuthnEnabled() {
		return nil, nil, &AuthError{Code: "webauthn_not_configured", Message: "WebAuthn is not enabled in configuration"}
	}

	user, err := m.userStore.FindByEmail(ctx, email)
	if err != nil {
		m.auditFailure(ctx, AuditLogin, email, ip, userAgent, ErrInvalidCredentials)
		return nil, nil, ErrInvalidCredentials
	}

	credentials, err := m.mfaService.GetWebAuthnCredentials(ctx, user.ID)
	if err != nil || len(credentials) == 0 {
		m.auditFailure(ctx, AuditLogin, email, ip, userAgent, ErrWebAuthnNoCredentials)
		return nil, nil, ErrInvalidCredentials
	}

	assertion, ok := response.(*protocol.ParsedCredentialAssertionData)
	if !ok {
		return nil, nil, &AuthError{Code: "invalid_response", Message: "Invalid WebAuthn response"}
	}

	// The counter the authenticator keeps is checked here, which is what makes
	// a cloned key detectable.
	used, err := m.mfaService.WebAuthn().FinishLogin(ctx, user.ID, user.Email, credentials, sessionData, assertion)
	if err != nil {
		m.auditFailure(ctx, AuditLogin, email, ip, userAgent, err)
		return nil, nil, err
	}

	// Keep the new signature counter, or a replay would look like a fresh use.
	if used != nil {
		if err := m.mfaService.UpdateWebAuthnCredential(ctx, user.ID, used); err != nil {
			m.logger.Warn("the passkey's counter could not be updated", "user_id", user.ID, "error", err)
		}
	}

	tokens, err := m.EstablishSession(ctx, user, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}

	m.logger.Info("user signed in with a passkey", "user_id", user.ID)
	m.audit(ctx, &AuditEvent{
		Event: AuditLogin, UserID: user.ID, Email: user.Email,
		IP: ip, UserAgent: userAgent, Success: true,
		Metadata: `{"method":"webauthn"}`,
	})

	return user, tokens, nil
}
