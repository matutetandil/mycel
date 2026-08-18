package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Resetting a password nobody can remember.
//
// The two endpoints for this were configured `Enabled: true` by default and no
// handler was ever registered for either, so the most-used account flow after
// signing in answered 404 while the configuration said it was on. There was
// nothing in the guide about it either — the only trace was the two blocks in
// the endpoints struct.
//
// Delivery is not auth's business. The token goes to a flow, through the same
// hooks any other account event uses, so a deployment sends it by email, SMS or
// anything else it has a connector for.

// PasswordResetStore holds outstanding reset tokens.
//
// What is stored is a hash of the token, never the token: it is a bearer
// credential for the length of its life, and a store somebody can read must not
// be a store somebody can reset accounts from.
type PasswordResetStore interface {
	// Create records a token for a user, good until the given time.
	Create(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error

	// Consume returns the user a token belongs to and spends it, so that one
	// token resets one password. It reports an error when there is no such
	// token or it has expired.
	Consume(ctx context.Context, tokenHash string) (userID string, err error)

	// DeleteForUser drops any outstanding tokens for a user, which is what a
	// completed reset or a deliberate password change should do to them.
	DeleteForUser(ctx context.Context, userID string) error
}

// MemoryPasswordResetStore keeps outstanding tokens in the process.
//
// The default, and the wrong choice for more than one replica: a token issued
// by one process is unknown to the next, so a reset link works only if the same
// replica answers. A service running more than one keeps sessions in Redis for
// the same reason, and this follows them there.
type MemoryPasswordResetStore struct {
	mu     sync.Mutex
	tokens map[string]passwordResetEntry
}

type passwordResetEntry struct {
	userID    string
	expiresAt time.Time
}

// NewMemoryPasswordResetStore creates an in-process reset token store.
func NewMemoryPasswordResetStore() *MemoryPasswordResetStore {
	return &MemoryPasswordResetStore{tokens: make(map[string]passwordResetEntry)}
}

func (s *MemoryPasswordResetStore) Create(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokens[tokenHash] = passwordResetEntry{userID: userID, expiresAt: expiresAt}
	return nil
}

func (s *MemoryPasswordResetStore) Consume(ctx context.Context, tokenHash string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, held := s.tokens[tokenHash]
	if !held {
		return "", ErrInvalidResetToken
	}
	// Spent either way: a token offered after it expired is not one to leave
	// lying around.
	delete(s.tokens, tokenHash)
	if time.Now().After(entry.expiresAt) {
		return "", ErrInvalidResetToken
	}
	return entry.userID, nil
}

func (s *MemoryPasswordResetStore) DeleteForUser(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for hash, entry := range s.tokens {
		if entry.userID == userID {
			delete(s.tokens, hash)
		}
	}
	return nil
}

// hashResetToken is what goes in the store.
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// resetTokenLifetime is how long a reset token is good for.
func (m *Manager) resetTokenLifetime() time.Duration {
	if m.config.Password != nil && m.config.Password.ResetTokenTTL != "" {
		if ttl, err := ParseDuration(m.config.Password.ResetTokenTTL); err == nil && ttl > 0 {
			return ttl
		}
	}
	// Long enough to reach somebody's inbox and be acted on, short enough that
	// a link left in one is not a standing key to the account.
	return time.Hour
}

// RequestPasswordReset issues a reset token for an email address and hands it
// to the flow bound to on_password_reset.
//
// It reports nothing about whether the address belongs to an account. Answering
// differently for one that does turns this endpoint into a way to find out who
// has an account here, which is worth more to somebody enumerating a customer
// list than the reset is to them.
func (m *Manager) RequestPasswordReset(ctx context.Context, email, ip, userAgent string) error {
	user, err := m.userStore.FindByEmail(ctx, email)
	if err != nil {
		m.logger.Debug("password reset asked for an address with no account", "email", email)
		return nil
	}

	token, err := generateID()
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(m.resetTokenLifetime())

	if err := m.passwordReset.Create(ctx, hashResetToken(token), user.ID, expiresAt); err != nil {
		return fmt.Errorf("could not record the reset token: %w", err)
	}

	m.audit(ctx, &AuditEvent{
		Event: AuditPasswordReset, UserID: user.ID, Email: user.Email,
		IP: ip, UserAgent: userAgent, Success: true,
	})

	// The token leaves here exactly once, to the flow that delivers it. If
	// nothing is bound to the hook the reset cannot reach anybody, which is
	// worth saying rather than leaving somebody waiting for an email.
	if m.hookFor(HookOnPasswordReset) == nil {
		m.logger.Warn("a password reset was requested and no on_password_reset hook can deliver it",
			"email", user.Email,
			"hint", "bind a flow: hooks { on_password_reset { flow = \"send_reset_email\" } }")
		return nil
	}

	return m.runHook(ctx, HookOnPasswordReset, map[string]interface{}{
		"user_id":     user.ID,
		"email":       user.Email,
		"reset_token": token,
		"expires_at":  expiresAt.Format(time.RFC3339),
		"ip":          ip,
		"user_agent":  userAgent,
	})
}

// ResetPassword spends a reset token and sets a new password.
//
// Every session the account holds ends with it. Somebody resetting a password
// either forgot it or is taking the account back from whoever did not, and in
// the second case leaving the other sessions open would defeat the reset.
func (m *Manager) ResetPassword(ctx context.Context, token, newPassword, ip, userAgent string) error {
	userID, err := m.passwordReset.Consume(ctx, hashResetToken(token))
	if err != nil {
		return ErrInvalidResetToken
	}

	user, err := m.userStore.FindByID(ctx, userID)
	if err != nil {
		return ErrInvalidResetToken
	}

	if err := m.passwordValidator.Validate(newPassword, user); err != nil {
		return &AuthError{Code: "weak_password", Message: err.Error()}
	}
	if err := m.refuseBreachedPassword(ctx, newPassword); err != nil {
		return err
	}
	// The same policy a deliberate change is held to: a reset is not a way
	// around the history.
	if err := m.refusePasswordReuse(ctx, user, newPassword); err != nil {
		return &AuthError{Code: "password_reused", Message: err.Error()}
	}

	passwordHash, err := m.passwordHasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := m.userStore.UpdatePassword(ctx, userID, passwordHash); err != nil {
		return err
	}

	if keep := m.passwordHistoryDepth(); keep > 0 && user.PasswordHash != "" {
		if err := m.passwordHistory.AddPasswordHash(ctx, userID, user.PasswordHash); err != nil {
			m.logger.Warn("could not record the previous password", "user_id", userID, "error", err)
		} else if err := m.passwordHistory.CleanOldHashes(ctx, userID, keep); err != nil {
			m.logger.Warn("could not trim the password history", "user_id", userID, "error", err)
		}
	}

	// Any other outstanding token, and every session.
	if err := m.passwordReset.DeleteForUser(ctx, userID); err != nil {
		m.logger.Warn("could not drop the remaining reset tokens", "user_id", userID, "error", err)
	}
	if err := m.sessionStore.DeleteByUserID(ctx, userID); err != nil {
		m.logger.Warn("could not end the sessions after a password reset", "user_id", userID, "error", err)
	}

	m.logger.Info("password reset", "user_id", userID)
	m.audit(ctx, &AuditEvent{
		Event: AuditPasswordChange, UserID: userID, Email: user.Email,
		IP: ip, UserAgent: userAgent, Success: true,
	})
	_ = m.runHook(ctx, HookAfterPasswordChange, map[string]interface{}{
		"user_id": userID, "email": user.Email, "reset": true,
	})
	return nil
}
