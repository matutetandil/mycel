package auth

import (
	"time"
)

// How old a password is allowed to get.
//
// `password { max_age }` and `warn_before` were read by nothing, so a service
// requiring a password to be changed every ninety days never asked anybody to
// change one — the setting parsed, appeared in the guide, and expired nothing.
//
// Expiry is enforced when a token is validated rather than at sign-in. Signing
// in has to keep working: the endpoint for changing a password needs a token,
// so refusing the sign-in would leave somebody with an expired password unable
// to do the one thing that would fix it. What they get instead is a token that
// every other endpoint refuses.

// passwordMaxAge is how long a password may be used for, or zero when there is
// no policy.
func (m *Manager) passwordMaxAge() time.Duration {
	if m.config.Password == nil || m.config.Password.MaxAge == "" {
		return 0
	}
	age, err := ParseDuration(m.config.Password.MaxAge)
	if err != nil {
		return 0
	}
	return age
}

// passwordWarnBefore is how long before expiry somebody should be told, or
// zero when nothing is configured.
func (m *Manager) passwordWarnBefore() time.Duration {
	if m.config.Password == nil || m.config.Password.WarnBefore == "" {
		return 0
	}
	warn, err := ParseDuration(m.config.Password.WarnBefore)
	if err != nil {
		return 0
	}
	return warn
}

// PasswordExpiry reports when a user's password stops being usable.
//
// known is false when there is no policy, or when nothing recorded the age of
// this password — an account from before it was recorded, or a SQL store whose
// fields block names no column for it. An unknown age is not an expired one:
// locking out every existing account the moment a policy is configured is not
// what configuring one means.
func (m *Manager) PasswordExpiry(user *User) (expiresAt time.Time, expired bool, known bool) {
	maxAge := m.passwordMaxAge()
	if maxAge <= 0 || user == nil || user.PasswordChangedAt == nil {
		return time.Time{}, false, false
	}

	expiresAt = user.PasswordChangedAt.Add(maxAge)
	return expiresAt, !time.Now().Before(expiresAt), true
}

// PasswordExpiryWarning reports how long is left when that is little enough to
// be worth telling somebody, and whether it is worth telling them at all.
func (m *Manager) PasswordExpiryWarning(user *User) (expiresAt time.Time, warn bool) {
	expiresAt, expired, known := m.PasswordExpiry(user)
	if !known || expired {
		return expiresAt, false
	}

	warnBefore := m.passwordWarnBefore()
	if warnBefore <= 0 {
		return expiresAt, false
	}
	return expiresAt, time.Until(expiresAt) <= warnBefore
}
