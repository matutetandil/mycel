package auth

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Requiring a second factor rather than offering one.
//
// `required`, `require_for`, `require_multiple`, `min_factors` and
// `grace_period` were read by nothing except a status response: sign-in asked
// for a second factor only when the account had enrolled one voluntarily. So
// `mfa { required = "true" }` required nothing — somebody who never enrolled
// signed in with a password for ever, and the status endpoint told them it was
// required while nothing made it so.
//
// It is enforced where the password policy is, when a token is validated: the
// account still signs in, because enrolling needs a token, and every other
// endpoint refuses until there is a second factor. Refusing the sign-in itself
// would leave somebody unable to do the one thing being asked of them.

// ErrMFAEnrolmentRequired is what an account past its grace period gets until
// it has enrolled.
var ErrMFAEnrolmentRequired = &AuthError{
	Code:    "mfa_enrolment_required",
	Message: "This account has to set up a second factor before it can be used",
}

// mfaPolicy is the mfa block, or nil.
func (m *Manager) mfaPolicy() *MFAConfig {
	if m.config.MFA == nil || !m.config.MFA.Enabled {
		return nil
	}
	return m.config.MFA
}

// mfaRequiredFor reports whether this account has to have a second factor.
func (m *Manager) mfaRequiredFor(user *User) bool {
	policy := m.mfaPolicy()
	if policy == nil || user == nil {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(policy.Required)) {
	case "true", "required":
		return true
	case "admin_only":
		return hasAnyRole(user, []string{"admin"})
	}
	// require_for names the roles it applies to, which is the same question
	// asked the other way round.
	return len(policy.RequireFor) > 0 && hasAnyRole(user, policy.RequireFor)
}

func hasAnyRole(user *User, roles []string) bool {
	for _, wanted := range roles {
		for _, held := range user.Roles {
			if strings.EqualFold(strings.TrimSpace(wanted), strings.TrimSpace(held)) {
				return true
			}
		}
	}
	return false
}

// mfaFactorsWanted is how many factors an account must have enrolled.
func (m *Manager) mfaFactorsWanted() int {
	policy := m.mfaPolicy()
	if policy == nil {
		return 1
	}
	wanted := 1
	if policy.RequireMultiple {
		wanted = 2
	}
	if policy.MinFactors > wanted {
		wanted = policy.MinFactors
	}
	return wanted
}

// mfaGracePeriod is how long a new account may go without enrolling.
func (m *Manager) mfaGracePeriod() time.Duration {
	policy := m.mfaPolicy()
	if policy == nil || policy.GracePeriod == "" {
		return 0
	}
	grace, err := ParseDuration(policy.GracePeriod)
	if err != nil {
		return 0
	}
	return grace
}

// MFAEnrolment reports what this account still owes the policy.
//
// wanted is how many factors it must have, held is how many it has, and
// graceUntil is when signing in stops working if that gap is not closed. A
// zero graceUntil with a gap means it has stopped already.
func (m *Manager) MFAEnrolment(ctx context.Context, user *User) (wanted, held int, graceUntil time.Time, required bool) {
	if !m.mfaRequiredFor(user) {
		return 0, 0, time.Time{}, false
	}

	wanted = m.mfaFactorsWanted()
	held = m.mfaFactorsHeld(ctx, user)
	if held >= wanted {
		return wanted, held, time.Time{}, true
	}

	if grace := m.mfaGracePeriod(); grace > 0 {
		graceUntil = user.CreatedAt.Add(grace)
	}
	return wanted, held, graceUntil, true
}

// mfaFactorsHeld counts what an account has enrolled.
func (m *Manager) mfaFactorsHeld(ctx context.Context, user *User) int {
	if m.mfaService == nil {
		// Without a service there is nothing to count and nothing to enrol
		// with, so counting the flag on the account is the best that can be
		// said.
		if user.MFAEnabled {
			return 1
		}
		return 0
	}

	status, err := m.mfaService.GetStatus(ctx, user.ID)
	if err != nil || status == nil {
		if user.MFAEnabled {
			return 1
		}
		return 0
	}

	held := 0
	if status.TOTPConfigured {
		held++
	}
	if status.WebAuthnConfigured {
		held++
	}
	// An account marked as having MFA with nothing this can see is counted as
	// having one: better than telling somebody who has already set it up to
	// set it up again.
	if held == 0 && (status.Enabled || user.MFAEnabled) {
		held = 1
	}
	return held
}

// refuseUnenrolledAccount reports whether this account has run out of time to
// enrol a second factor.
func (m *Manager) refuseUnenrolledAccount(ctx context.Context, user *User) error {
	wanted, held, graceUntil, required := m.MFAEnrolment(ctx, user)
	if !required || held >= wanted {
		return nil
	}
	if !graceUntil.IsZero() && time.Now().Before(graceUntil) {
		return nil
	}

	message := ErrMFAEnrolmentRequired.Message
	if wanted > 1 {
		message = fmt.Sprintf("This account has to set up %d factors before it can be used, and has %d", wanted, held)
	}
	return &AuthError{Code: ErrMFAEnrolmentRequired.Code, Message: message}
}

// validateMFAPolicy refuses a block that cannot do what it says.
func validateMFAPolicy(cfg *Config) error {
	if cfg.MFA == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(cfg.MFA.Required)) {
	case "", "false", "optional", "true", "required", "admin_only":
	default:
		return fmt.Errorf("mfa: required = %q is not one of true, false, optional or admin_only", cfg.MFA.Required)
	}
	if cfg.MFA.GracePeriod != "" {
		if _, err := ParseDuration(cfg.MFA.GracePeriod); err != nil {
			return fmt.Errorf("mfa: grace_period %q is not a duration", cfg.MFA.GracePeriod)
		}
	}
	if cfg.MFA.MinFactors < 0 {
		return fmt.Errorf("mfa: min_factors cannot be negative")
	}
	// Asking for more factors than there are methods to enrol them with is a
	// service nobody can finish signing up to.
	if wanted := requestedFactors(cfg.MFA); wanted > 1 && len(cfg.MFA.Methods) > 0 && wanted > len(cfg.MFA.Methods) {
		return fmt.Errorf("mfa: %d factors are required but only %d methods are offered, "+
			"so no account could ever satisfy it", wanted, len(cfg.MFA.Methods))
	}
	return nil
}

func requestedFactors(policy *MFAConfig) int {
	wanted := 1
	if policy.RequireMultiple {
		wanted = 2
	}
	if policy.MinFactors > wanted {
		wanted = policy.MinFactors
	}
	return wanted
}
