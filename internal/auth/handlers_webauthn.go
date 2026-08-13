package auth

import (
	"encoding/json"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
)

// The endpoints a passkey is registered and used through.
//
// A ceremony is two calls on each side: the browser asks for options, hands
// them to the authenticator, and sends the answer back to be checked. Both
// halves existed below — the service, the manager's registration methods, the
// store — and none of them had a route, so a webauthn block configured a
// feature no browser could reach.
//
// The state issued by the first call comes back with the second. It is handed
// to the caller and returned rather than kept on the server, so that a browser
// talking to whichever replica answers next still completes the ceremony it
// started.

// handleWebAuthnRegisterBegin issues the options for adding a passkey.
//
// It is for the account in the token: a passkey added to somebody else's
// account is a way to own it for ever, so there is no reading of a user id
// from the body.
func (h *Handler) handleWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	user, ok := h.callerOf(w, r)
	if !ok {
		return
	}

	options, session, err := h.manager.BeginWebAuthnRegistration(r.Context(), user.ID)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"publicKey": options,
		"session":   session,
	})
}

// handleWebAuthnRegisterFinish checks what the authenticator produced and keeps
// the credential.
func (h *Handler) handleWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	user, ok := h.callerOf(w, r)
	if !ok {
		return
	}

	session := r.Header.Get("X-WebAuthn-Session")
	if session == "" {
		session = r.URL.Query().Get("session")
	}
	if session == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request",
			"The session from the first call is required to complete registration")
		return
	}

	// The body is the authenticator's answer, parsed by the library that knows
	// its shape rather than by us.
	response, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_response", "The authenticator's answer could not be read")
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "passkey"
	}

	if err := h.manager.FinishWebAuthnRegistration(r.Context(), user.ID, session, name, response); err != nil {
		h.writeAuthError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{"registered": true, "name": name})
}

// handleWebAuthnLoginBegin issues the options for signing in with a passkey.
//
// Asked for by address, before anybody is signed in — that is what a passkey is
// for. An address with no passkey and an address with no account get the same
// answer, since a sign-in page that tells them apart is a way of asking which
// addresses are registered.
func (h *Handler) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !h.posted(w, r, &req) {
		return
	}

	if req.Email == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "An address is required")
		return
	}

	options, session, err := h.manager.BeginWebAuthnLogin(r.Context(), req.Email)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "webauthn_unavailable",
			"No passkey can be used for this address")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"publicKey": options,
		"session":   session,
	})
}

// handleWebAuthnLoginFinish checks the signature and opens a session.
func (h *Handler) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	email := r.URL.Query().Get("email")
	session := r.Header.Get("X-WebAuthn-Session")
	if session == "" {
		session = r.URL.Query().Get("session")
	}
	if email == "" || session == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_request",
			"The address and the session from the first call are required")
		return
	}

	response, err := protocol.ParseCredentialRequestResponseBody(r.Body)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_response", "The authenticator's answer could not be read")
		return
	}

	user, tokens, err := h.manager.FinishWebAuthnLogin(r.Context(), email, session, response, getClientIP(r), r.UserAgent())
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "The passkey was not accepted")
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

// handleWebAuthnCredentials lists the keys on the signed-in account, and
// removes one.
//
// Removing asks for the password: a stolen session must not be able to take
// away the key that would have stopped it.
func (h *Handler) handleWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	user, ok := h.callerOf(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		credentials, err := h.manager.GetWebAuthnCredentials(r.Context(), user.ID)
		if err != nil {
			h.writeAuthError(w, err)
			return
		}

		// The public parts only. A list is for recognising a device, not for
		// carrying key material around.
		listed := make([]map[string]interface{}, 0, len(credentials))
		for _, cred := range credentials {
			listed = append(listed, map[string]interface{}{
				"id":           cred.ID,
				"name":         cred.Name,
				"created_at":   cred.CreatedAt,
				"last_used_at": cred.LastUsedAt,
			})
		}
		h.writeJSON(w, http.StatusOK, map[string]interface{}{"credentials": listed})

	case http.MethodDelete:
		var req struct {
			CredentialID string `json:"credential_id"`
			Password     string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
			return
		}
		if req.CredentialID == "" || req.Password == "" {
			h.writeError(w, http.StatusBadRequest, "invalid_request",
				"The key to remove and your password are required")
			return
		}

		if err := h.manager.RemoveWebAuthnCredential(r.Context(), user.ID, req.CredentialID, req.Password); err != nil {
			h.writeAuthError(w, err)
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]interface{}{"removed": true})

	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}
