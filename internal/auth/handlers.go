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
		Prefix: "/auth",
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
	}
}

// RegisterRoutes registers all auth routes on the given mux
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	prefix := h.config.Prefix
	if prefix == "" {
		prefix = "/auth"
	}

	// Register enabled endpoints
	if h.config.Register != nil && h.config.Register.Enabled {
		path := prefix + getPath(h.config.Register, "/register")
		mux.HandleFunc(path, h.handleRegister)
	}

	if h.config.Login != nil && h.config.Login.Enabled {
		path := prefix + getPath(h.config.Login, "/login")
		mux.HandleFunc(path, h.handleLogin)
	}

	if h.config.Logout != nil && h.config.Logout.Enabled {
		path := prefix + getPath(h.config.Logout, "/logout")
		mux.HandleFunc(path, h.handleLogout)
	}

	if h.config.Refresh != nil && h.config.Refresh.Enabled {
		path := prefix + getPath(h.config.Refresh, "/refresh")
		mux.HandleFunc(path, h.handleRefresh)
	}

	if h.config.Me != nil && h.config.Me.Enabled {
		path := prefix + getPath(h.config.Me, "/me")
		mux.HandleFunc(path, h.handleMe)
	}

	if h.config.PasswordChange != nil && h.config.PasswordChange.Enabled {
		path := prefix + getPath(h.config.PasswordChange, "/change-password")
		mux.HandleFunc(path, h.handleChangePassword)
	}

	if h.config.SessionsList != nil && h.config.SessionsList.Enabled {
		path := prefix + getPath(h.config.SessionsList, "/sessions")
		mux.HandleFunc(path, h.handleSessions)
	}

	// Sessions revoke needs special handling for path param
	if h.config.SessionsRevoke != nil && h.config.SessionsRevoke.Enabled {
		path := prefix + "/sessions/"
		mux.HandleFunc(path, h.handleSessionRevoke)
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

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":          userToResponse(user),
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
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

	// Validate token
	_, claims, err := h.manager.ValidateToken(r.Context(), token)
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

	// Validate token
	user, _, err := h.manager.ValidateToken(r.Context(), token)
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
