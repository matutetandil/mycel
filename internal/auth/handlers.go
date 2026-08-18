package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Handler provides HTTP handlers for auth endpoints
type Handler struct {
	manager *Manager
	config  *EndpointsConfig
}

// NewHandler creates a new auth handler
func NewHandler(manager *Manager) *Handler {
	config := manager.Config().Endpoints
	if config == nil {
		config = DefaultEndpointsConfig()
	}
	return &Handler{
		manager: manager,
		config:  config,
	}
}

// DefaultEndpointsConfig returns the default endpoints configuration
func DefaultEndpointsConfig() *EndpointsConfig {
	return &EndpointsConfig{
		Prefix:         "/auth",
		Login:          &EndpointConfig{Path: "/login", Method: "POST", Enabled: true},
		Logout:         &EndpointConfig{Path: "/logout", Method: "POST", Enabled: true},
		Register:       &EndpointConfig{Path: "/register", Method: "POST", Enabled: true},
		Refresh:        &EndpointConfig{Path: "/refresh", Method: "POST", Enabled: true},
		Me:             &EndpointConfig{Path: "/me", Method: "GET", Enabled: true},
		PasswordForgot: &EndpointConfig{Path: "/forgot-password", Method: "POST", Enabled: true},
		PasswordReset:  &EndpointConfig{Path: "/reset-password", Method: "POST", Enabled: true},
		PasswordChange: &EndpointConfig{Path: "/change-password", Method: "POST", Enabled: true},
		SessionsList:   &EndpointConfig{Path: "/sessions", Method: "GET", Enabled: true},
		SessionsRevoke: &EndpointConfig{Path: "/sessions/:id", Method: "DELETE", Enabled: true},
		MFASetup:       &EndpointConfig{Path: "/mfa/setup", Method: "POST", Enabled: true},
		MFAVerify:      &EndpointConfig{Path: "/mfa/verify", Method: "POST", Enabled: true},
		MFADisable:     &EndpointConfig{Path: "/mfa/disable", Method: "POST", Enabled: true},
		MFARecovery:    &EndpointConfig{Path: "/mfa/recovery", Method: "POST", Enabled: true},
		// The single sign-on callbacks. They were missing here as well as from
		// the routing, so even a configuration that named them had nowhere to
		// send a provider.
		SocialCallback: &EndpointConfig{Path: "/social/callback", Method: "GET", Enabled: true},
		SSOCallback:    &EndpointConfig{Path: "/sso/callback", Method: "GET", Enabled: true},
	}
}

// Mux is whatever the routes are mounted on. *http.ServeMux satisfies it, and
// so does an adapter over a connector that owns its own router.
type Mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// RegisterRoutes mounts the endpoints the configuration asks for.
//
// Nothing called this until 2.19.0. The manager and the handler were both
// built at startup, the log said "auth system initialized", and every endpoint
// the auth block promises — login, register, refresh, me, the session and MFA
// routes — answered 404, because the routes were never attached to a server.
func (h *Handler) RegisterRoutes(mux Mux) {
	prefix := h.config.Prefix
	if prefix == "" {
		prefix = "/auth"
	}

	// Register enabled endpoints
	if h.config.Register != nil && h.config.Register.Enabled {
		path := prefix + getPath(h.config.Register, "/register")
		mux.HandleFunc(path, h.limited("register", h.handleRegister))
	}

	if h.config.Login != nil && h.config.Login.Enabled {
		path := prefix + getPath(h.config.Login, "/login")
		mux.HandleFunc(path, h.limited("login", h.handleLogin))
	}

	if h.config.Logout != nil && h.config.Logout.Enabled {
		path := prefix + getPath(h.config.Logout, "/logout")
		mux.HandleFunc(path, h.limited("logout", h.handleLogout))
	}

	if h.config.Refresh != nil && h.config.Refresh.Enabled {
		path := prefix + getPath(h.config.Refresh, "/refresh")
		mux.HandleFunc(path, h.limited("refresh", h.handleRefresh))
	}

	if h.config.Me != nil && h.config.Me.Enabled {
		path := prefix + getPath(h.config.Me, "/me")
		mux.HandleFunc(path, h.limited("sessions", h.handleMe))
	}

	if h.config.PasswordChange != nil && h.config.PasswordChange.Enabled {
		path := prefix + getPath(h.config.PasswordChange, "/change-password")
		mux.HandleFunc(path, h.limited("change_password", h.handleChangePassword))
	}

	// The most-used account flow after signing in, and until 2.19.0 the only
	// two endpoints in this block with no handler at all: both were on by
	// default and both answered 404.
	if h.config.PasswordForgot != nil && h.config.PasswordForgot.Enabled {
		path := prefix + getPath(h.config.PasswordForgot, "/forgot-password")
		mux.HandleFunc(path, h.limited("password_forgot", h.handlePasswordForgot))
	}

	if h.config.PasswordReset != nil && h.config.PasswordReset.Enabled {
		path := prefix + getPath(h.config.PasswordReset, "/reset-password")
		mux.HandleFunc(path, h.limited("password_reset", h.handlePasswordReset))
	}

	// Two ways of saying no, and either of them counts.
	//
	// The endpoints block turns an endpoint off; the sessions block's
	// allow_list and allow_revoke say whether the feature is offered at all.
	// Only the first was consulted, so a deployment that wrote
	// `allow_list = false` — meaning nobody may list sessions — still served
	// the endpoint, and had no way to know.
	if h.config.SessionsList != nil && h.config.SessionsList.Enabled && h.sessionsMayBeListed() {
		path := prefix + getPath(h.config.SessionsList, "/sessions")
		mux.HandleFunc(path, h.limited("sessions", h.handleSessions))
	}

	// Sessions revoke needs special handling for path param
	if h.config.SessionsRevoke != nil && h.config.SessionsRevoke.Enabled && h.sessionsMayBeRevoked() {
		path := prefix + "/sessions/"
		mux.HandleFunc(path, h.limited("sessions", h.handleSessionRevoke))
	}

	h.registerMFARoutes(mux, prefix)
	h.registerWebAuthnRoutes(mux, prefix)
	h.registerSSORoutes(mux, prefix)
}

// registerMFARoutes mounts the four routes a second factor is set up and used
// through.
//
// They exist only when a second factor does: paths that answer for a service
// that cannot enrol anybody would be worse than none, since they invite a
// client to build the flow.
// sessionsMayBeListed reports whether the sessions block allows listing.
//
// Nothing configured is a yes: this is the behaviour every deployment has
// today, and the setting exists to take it away rather than to grant it.
func (h *Handler) sessionsMayBeListed() bool {
	sessions := h.manager.Config().Sessions
	return sessions == nil || sessions.AllowList
}

// sessionsMayBeRevoked reports whether the sessions block allows revocation.
func (h *Handler) sessionsMayBeRevoked() bool {
	sessions := h.manager.Config().Sessions
	return sessions == nil || sessions.AllowRevoke
}

func (h *Handler) registerMFARoutes(mux Mux, prefix string) {
	if !h.manager.MFAEnabled() {
		return
	}

	if path := endpointPath(h.config.MFASetup, "/mfa/setup"); path != "" {
		mux.HandleFunc(prefix+path, h.limited("mfa_setup", h.handleMFASetup))
	}
	if path := endpointPath(h.config.MFAVerify, "/mfa/verify"); path != "" {
		mux.HandleFunc(prefix+path, h.limited("mfa_verify", h.handleMFAVerify))
	}
	if path := endpointPath(h.config.MFADisable, "/mfa/disable"); path != "" {
		mux.HandleFunc(prefix+path, h.limited("mfa_disable", h.handleMFADisable))
	}
	if path := endpointPath(h.config.MFARecovery, "/mfa/recovery"); path != "" {
		mux.HandleFunc(prefix+path, h.limited("mfa_recovery", h.handleMFARecovery))
	}
}

// endpointPath returns the path an endpoint is served on, or "" when it is
// turned off. An endpoint nobody configured takes the default, since leaving
// the block out is not a decision to remove it.
func endpointPath(cfg *EndpointConfig, fallback string) string {
	if cfg == nil {
		return fallback
	}
	if !cfg.Enabled {
		return ""
	}
	if cfg.Path != "" {
		return cfg.Path
	}
	return fallback
}

// registerWebAuthnRoutes mounts the four calls a passkey ceremony needs, plus
// the list of keys on an account.
//
// Only when the service can actually run one: paths that answer for a service
// with no relying party configured invite a browser to start a ceremony that
// cannot finish.
func (h *Handler) registerWebAuthnRoutes(mux Mux, prefix string) {
	if !h.manager.WebAuthnEnabled() {
		return
	}

	mux.HandleFunc(prefix+"/webauthn/register/begin", h.limited("webauthn_register", h.handleWebAuthnRegisterBegin))
	mux.HandleFunc(prefix+"/webauthn/register/finish", h.limited("webauthn_register", h.handleWebAuthnRegisterFinish))
	mux.HandleFunc(prefix+"/webauthn/login/begin", h.limited("login", h.handleWebAuthnLoginBegin))
	mux.HandleFunc(prefix+"/webauthn/login/finish", h.limited("login", h.handleWebAuthnLoginFinish))
	mux.HandleFunc(prefix+"/webauthn/credentials", h.handleWebAuthnCredentials)
}

// registerSSORoutes mounts the two routes each sign-on family needs: one that
// sends the browser to the provider, one the provider returns to.
//
// The begin route carries the provider in the path, so one route serves every
// provider the configuration declares. The literal callback path is registered
// as well, and takes precedence over the wildcard by Go's own routing rules.
func (h *Handler) registerSSORoutes(mux Mux, prefix string) {
	// The identities attached to an account, once a sign-in has attached them.
	// These exist whenever sign-on does, since an account that can gain an
	// identity should be able to show and lose one.
	if h.config.SocialCallback != nil && h.config.SocialCallback.Enabled ||
		h.config.SSOCallback != nil && h.config.SSOCallback.Enabled {
		mux.HandleFunc(prefix+"/linked-accounts", h.handleLinkedAccounts)
		mux.HandleFunc(prefix+"/unlink/{provider}", h.handleUnlink)
	}

	if h.config.SocialCallback != nil && h.config.SocialCallback.Enabled {
		mux.HandleFunc(prefix+getPath(h.config.SocialCallback, "/social/callback"), h.handleSSOCallback)
		mux.HandleFunc(prefix+"/social/{provider}", h.handleSSOBegin)
	}

	if h.config.SSOCallback != nil && h.config.SSOCallback.Enabled {
		mux.HandleFunc(prefix+getPath(h.config.SSOCallback, "/sso/callback"), h.handleSSOCallback)
		mux.HandleFunc(prefix+"/sso/{provider}", h.handleSSOBegin)
	}
}

func getPath(cfg *EndpointConfig, defaultPath string) string {
	if cfg != nil && cfg.Path != "" {
		return cfg.Path
	}
	return defaultPath
}

// handleRegister handles POST /auth/register
func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Email == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Email is required")
		return
	}
	if req.Password == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Password is required")
		return
	}

	user, tokens, err := h.manager.Register(r.Context(), &req)
	if err != nil {
		if authErr, ok := err.(*AuthError); ok {
			h.writeError(w, http.StatusBadRequest, authErr.Code, authErr.Message)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internal_error", "Registration failed")
		return
	}

	h.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user":          userToResponse(user),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
	})
}

// handleLogin handles POST /auth/login
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Email and password are required")
		return
	}

	ip := getClientIP(r)
	userAgent := r.UserAgent()

	user, tokens, err := h.manager.Login(r.Context(), &req, ip, userAgent)
	if err != nil {
		if authErr, ok := err.(*AuthError); ok {
			status := http.StatusUnauthorized
			if authErr.Code == "account_locked" {
				status = http.StatusTooManyRequests
			} else if authErr.Code == "mfa_required" {
				status = http.StatusPreconditionRequired
			}
			h.writeError(w, status, authErr.Code, authErr.Message)
			return
		}
		h.writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
		return
	}

	body := map[string]interface{}{
		"user":          userToResponse(user),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
	}

	// What a client needs to send somebody to the change-password endpoint
	// before every other one starts refusing them, and what warn_before is
	// for: a week's notice is no use if nothing says it.
	// What a client needs to send somebody to the enrolment endpoint before
	// every other one starts refusing them.
	if wanted, held, graceUntil, required := h.manager.MFAEnrolment(r.Context(), user); required && held < wanted {
		body["mfa_enrolment_required"] = true
		body["mfa_factors_wanted"] = wanted
		body["mfa_factors_held"] = held
		if !graceUntil.IsZero() {
			body["mfa_grace_until"] = graceUntil
		}
	}

	if expiresAt, expired, known := h.manager.PasswordExpiry(user); known {
		if expired {
			body["password_expired"] = true
			body["password_expires_at"] = expiresAt
		} else if _, warn := h.manager.PasswordExpiryWarning(user); warn {
			body["password_expires_at"] = expiresAt
		}
	}

	h.writeJSON(w, http.StatusOK, body)
}

// handlePasswordForgot handles POST /auth/forgot-password.
//
// It answers the same way whether or not the address belongs to an account.
// Answering differently turns this into a way to find out who has an account
// here, which is worth more to somebody enumerating a customer list than the
// reset is to them.
func (h *Handler) handlePasswordForgot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	if req.Email == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Email is required")
		return
	}

	if err := h.manager.RequestPasswordReset(r.Context(), req.Email, getClientIP(r), r.UserAgent()); err != nil {
		// Something here is broken — the store, or the flow that delivers the
		// token. That is worth reporting as a failure, unlike an address with
		// no account behind it.
		h.writeError(w, http.StatusInternalServerError, "reset_failed", "Could not start a password reset")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "If that address has an account, a reset link is on its way",
	})
}

// handlePasswordReset handles POST /auth/reset-password.
func (h *Handler) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	if req.Token == "" || req.NewPassword == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Token and new password are required")
		return
	}

	if err := h.manager.ResetPassword(r.Context(), req.Token, req.NewPassword, getClientIP(r), r.UserAgent()); err != nil {
		if authErr, ok := err.(*AuthError); ok {
			status := http.StatusBadRequest
			if authErr.Code == ErrInvalidResetToken.Code {
				status = http.StatusUnauthorized
			}
			h.writeError(w, status, authErr.Code, authErr.Message)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "reset_failed", "Could not reset the password")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Password reset. Every session that account had is now signed out",
	})
}

// handleLogout handles POST /auth/logout
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Get token from header
	token := ExtractTokenFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Missing authorization token")
		return
	}

	// Signing out has to work whatever state the account is in, including a
	// password the policy has expired.
	_, claims, err := h.manager.ValidateTokenAllowingUnfinishedSetup(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid token")
		return
	}

	// Logout
	if err := h.manager.Logout(r.Context(), claims.SessionID); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "Logout failed")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Successfully logged out",
	})
}

// handleRefresh handles POST /auth/refresh
func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.RefreshToken == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Refresh token is required")
		return
	}

	user, tokens, err := h.manager.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		if authErr, ok := err.(*AuthError); ok {
			h.writeError(w, http.StatusUnauthorized, authErr.Code, authErr.Message)
			return
		}
		h.writeError(w, http.StatusUnauthorized, "invalid_token", "Invalid refresh token")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":          userToResponse(user),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
	})
}

// handleMe handles GET /auth/me
func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Get token from header
	token := ExtractTokenFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Missing authorization token")
		return
	}

	// Validate token
	user, _, err := h.manager.ValidateToken(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid token")
		return
	}

	h.writeJSON(w, http.StatusOK, userToResponse(user))
}

// handleChangePassword handles POST /auth/change-password
func (h *Handler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Get token from header
	token := ExtractTokenFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Missing authorization token")
		return
	}

	// The one endpoint an expired password must not be refused at: it is what
	// somebody in that state is being sent here to do.
	user, _, err := h.manager.ValidateTokenAllowingUnfinishedSetup(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid token")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Current and new password are required")
		return
	}

	if err := h.manager.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword); err != nil {
		if authErr, ok := err.(*AuthError); ok {
			h.writeError(w, http.StatusBadRequest, authErr.Code, authErr.Message)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internal_error", "Password change failed")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Password changed successfully",
	})
}

// handleSessions handles GET /auth/sessions
func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Get token from header
	token := ExtractTokenFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Missing authorization token")
		return
	}

	// Validate token
	user, claims, err := h.manager.ValidateToken(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid token")
		return
	}

	sessions, err := h.manager.GetSessions(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal_error", "Failed to get sessions")
		return
	}

	// Convert to response format
	sessionsResp := make([]map[string]interface{}, len(sessions))
	for i, s := range sessions {
		sessionsResp[i] = map[string]interface{}{
			"id":             s.ID,
			"ip":             s.IP,
			"user_agent":     s.UserAgent,
			"location":       s.Location,
			"created_at":     s.CreatedAt,
			"last_active_at": s.LastActiveAt,
			"current":        s.ID == claims.SessionID,
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessionsResp,
	})
}

// handleSessionRevoke handles DELETE /auth/sessions/:id
func (h *Handler) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// Get token from header
	token := ExtractTokenFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Missing authorization token")
		return
	}

	// Validate token
	user, _, err := h.manager.ValidateToken(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid token")
		return
	}

	// Extract session ID from path
	path := r.URL.Path
	prefix := h.config.Prefix + "/sessions/"
	if !strings.HasPrefix(path, prefix) {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid session ID")
		return
	}
	sessionID := strings.TrimPrefix(path, prefix)
	if sessionID == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Session ID is required")
		return
	}

	if err := h.manager.RevokeSession(r.Context(), user.ID, sessionID); err != nil {
		if authErr, ok := err.(*AuthError); ok {
			h.writeError(w, http.StatusForbidden, authErr.Code, authErr.Message)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke session")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Session revoked successfully",
	})
}

// writeJSON writes a JSON response
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes an error response
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// userToResponse converts a User to a response map
func userToResponse(u *User) map[string]interface{} {
	resp := map[string]interface{}{
		"id":         u.ID,
		"email":      u.Email,
		"created_at": u.CreatedAt,
		"updated_at": u.UpdatedAt,
	}

	if len(u.Roles) > 0 {
		resp["roles"] = u.Roles
	}
	if len(u.Permissions) > 0 {
		resp["permissions"] = u.Permissions
	}
	if u.MFAEnabled {
		resp["mfa_enabled"] = true
		resp["mfa_methods"] = u.MFAMethods
	}
	if u.LastLoginAt != nil {
		resp["last_login_at"] = u.LastLoginAt
	}
	if u.Metadata != nil {
		resp["metadata"] = u.Metadata
	}

	return resp
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the list
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	// Remove port if present
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}
