package auth

import (
	"context"
)

// An audit block is written for one reason: somebody has to be able to answer
// who signed in, from where, and what failed. It was parsed into the
// configuration and read by nothing — the stores that write the records exist
// and were constructed only by their own tests — so a service with an audit
// block wrote no record of anything, and said nothing about it. The one kind
// of gap where the discovery usually happens during an investigation, which is
// the worst moment to find out there is nothing to investigate with.

// AuditStore records what happened. Both SQL stores already implement it.
type AuditStore interface {
	Log(ctx context.Context, event *AuditEvent) error
}

// The events a service records. The names are what an `events` list in the
// configuration selects from, so they are part of the configuration surface
// and change with it.
const (
	AuditLogin          = "login"
	AuditLogout         = "logout"
	AuditRegister       = "register"
	AuditPasswordChange = "password_change"
	AuditTokenRefresh   = "token_refresh"
)

// WithAuditStore sets where audit records are written.
func WithAuditStore(store AuditStore) ManagerOption {
	return func(m *Manager) {
		m.auditStore = store
	}
}

// audit records an event, if there is anywhere to record it.
//
// A failure to write one is logged and swallowed. The alternative — failing the
// sign-in because its record could not be written — turns an unreachable audit
// database into an outage of the whole service, which is a worse answer to the
// same problem. The store decides which events it keeps; the caller reports
// everything that happens.
func (m *Manager) audit(ctx context.Context, event *AuditEvent) {
	if m.auditStore == nil || event == nil {
		return
	}
	if err := m.auditStore.Log(ctx, event); err != nil {
		m.logger.Warn("an auth event could not be recorded",
			"event", event.Event, "error", err)
	}
}

// auditFailure records something that did not happen, and why.
//
// The failures are the half that matters: a record of successful sign-ins
// alone answers none of the questions an audit trail is kept for.
func (m *Manager) auditFailure(ctx context.Context, event, email, ip, userAgent string, reason error) {
	record := &AuditEvent{
		Event: event, Email: email, IP: ip, UserAgent: userAgent, Success: false,
	}
	if reason != nil {
		record.ErrorReason = reason.Error()
	}
	m.audit(ctx, record)
}
