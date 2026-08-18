package auth

import (
	"encoding/json"
	"net/http"
)

// The endpoints a second factor is set up and used through.
//
// The manager could already enrol, confirm, disable and check a code, and the
// configuration declared paths for all four with defaults — and none of them
// was mounted. So `mfa_setup` named a path that answered 404: the feature read
// as broken rather than absent, which sends whoever is looking to the client
// first.
//
// Every one of these acts on the account the caller is signed in as, taken from
// the token rather than from the body. Reading a user id out of a request would
// let anyone enrol a second factor on somebody else's account, or turn one off.

// callerOf returns the account a request is signed in as.
func (h *Handler) callerOf(w http.ResponseWriter, r *http.Request) (*User, bool) {
	token := ExtractTokenFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Missing authorization token")
		return nil, false
	}

	// Tolerant on purpose: an account being told it must enrol a second factor
	// has to be able to reach the endpoint that enrols one, and one whose
	// password has expired has to be able to keep the factor it already has.
	user, _, err := h.manager.ValidateTokenAllowingUnfinishedSetup(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid token")
		return nil, false
	}
	return user, true
}

// posted reads a JSON body, answering the caller if it is not one.
func (h *Handler) posted(w http.ResponseWriter, r *http.Request, into interface{}) bool {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return false
	}
	return true
}

// handleMFASetup starts enrolment and returns what an authenticator app needs.
//
// The secret is shown once, here, because the app has to store it and we do
// not show it again. Enrolment is not finished until a code proves the app
// holds it — otherwise a mistyped setup would lock the account out of itself.
func (h *Handler) handleMFASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	user, ok := h.callerOf(w, r)
	if !ok {
		return
	}

	setup, err := h.manager.BeginTOTPSetup(r.Context(), user.ID)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"method":           setup.Method,
		"secret":           setup.Secret,
		"qr_code":          setup.QRCode,
		"provisioning_uri": setup.ProvisioningURI,
	})
}

// handleMFAVerify finishes enrolment with a code from the app, and answers with
// the recovery codes.
//
// They are shown once and never again, which is why they are the answer to
// this call rather than something to be fetched later.
func (h *Handler) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !h.posted(w, r, &req) {
		return
	}

	user, ok := h.callerOf(w, r)
	if !ok {
		return
	}

	if req.Code == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "A code from your authenticator app is required")
		return
	}

	recoveryCodes, err := h.manager.ConfirmTOTPSetup(r.Context(), user.ID, req.Code)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":        true,
		"recovery_codes": recoveryCodes,
		"note":           "These recovery codes are shown once. Keep them somewhere you can reach without this device.",
	})
}

// handleMFADisable turns the second factor off, on proof of the password.
//
// The password is asked for because a stolen session should not be enough to
// remove the protection that exists for exactly that case.
func (h *Handler) handleMFADisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !h.posted(w, r, &req) {
		return
	}

	user, ok := h.callerOf(w, r)
	if !ok {
		return
	}

	if req.Password == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Your password is required to turn off two-factor authentication")
		return
	}

	if err := h.manager.DisableMFA(r.Context(), user.ID, req.Password); err != nil {
		h.writeAuthError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": false})
}

// handleMFARecovery signs in with a recovery code, for somebody who has lost
// the device their codes come from.
//
// It goes through the ordinary sign-in rather than around it, which is what
// keeps the brute-force counters, the delays and the audit record in one place.
// Login already accepts a recovery code where a generated one would go: this
// endpoint exists because the configuration declares it and because "sign in
// with a recovery code" is a different thing to explain to a client than
// "sign in, but put this in the code field".
//
// A recovery code is spent when it is used, which the service below enforces:
// one that worked twice would be a password that never expires.
func (h *Handler) handleMFARecovery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !h.posted(w, r, &req) {
		return
	}

	if req.Email == "" || req.Code == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "An address and a recovery code are required")
		return
	}

	user, tokens, err := h.manager.Login(r.Context(), &LoginRequest{
		Email:    req.Email,
		Password: req.Password,
		MFACode:  req.Code,
	}, getClientIP(r), r.UserAgent())
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
		"user": map[string]interface{}{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}

// writeAuthError answers with the status an auth error carries, rather than
// turning every refusal into a 500.
func (h *Handler) writeAuthError(w http.ResponseWriter, err error) {
	if authErr, ok := err.(*AuthError); ok {
		status := http.StatusBadRequest
		switch authErr.Code {
		case "mfa_not_configured":
			status = http.StatusNotFound
		case "unauthorized", "invalid_password", "invalid_mfa_code":
			status = http.StatusUnauthorized
		}
		h.writeError(w, status, authErr.Code, authErr.Message)
		return
	}
	h.writeError(w, http.StatusBadRequest, "mfa_error", err.Error())
}
