package auth

import (
	"net/http"
	"strings"
)

// The endpoints for the identities attached to an account.
//
// Linking during a sign-in worked: an identity whose address matches an
// existing account is attached to it. Everything after that did not — the
// service can list, confirm and unlink, and no route led to any of it, so an
// account could accumulate identities and never show or lose one.

// handleLinkedAccounts lists the identities attached to the caller's account.
func (h *Handler) handleLinkedAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	user, linking, ok := h.linkingRequest(w, r)
	if !ok {
		return
	}

	accounts, err := linking.GetLinkedAccounts(r.Context(), user.ID)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	// The tokens a provider issued are the account's business and nobody
	// else's; what a caller needs is which identities exist.
	listed := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		listed = append(listed, map[string]interface{}{
			"provider":   account.Provider,
			"email":      account.Email,
			"name":       account.Name,
			"picture":    account.Picture,
			"linked_at":  account.CreatedAt,
			"updated_at": account.UpdatedAt,
		})
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{"linked_accounts": listed})
}

// handleUnlink detaches one identity from the caller's account.
func (h *Handler) handleUnlink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	provider := r.PathValue("provider")
	if provider == "" {
		h.writeError(w, http.StatusBadRequest, "provider_required", "No provider was named in the path")
		return
	}

	user, linking, ok := h.linkingRequest(w, r)
	if !ok {
		return
	}

	if err := linking.Unlink(r.Context(), user.ID, provider); err != nil {
		// Removing the last way into an account is refused by the service, and
		// that refusal is the caller's answer rather than a server error.
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		h.writeError(w, status, "unlink_failed", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{"unlinked": provider})
}

// linkingRequest resolves the caller and the linking service, answering the
// request itself when either is missing.
func (h *Handler) linkingRequest(w http.ResponseWriter, r *http.Request) (*User, *AccountLinkingService, bool) {
	sso := h.manager.SSO()
	if sso == nil || sso.Linking() == nil {
		h.writeError(w, http.StatusNotFound, "sso_not_configured", "No identity provider is configured")
		return nil, nil, false
	}

	token := ExtractTokenFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Missing authorization token")
		return nil, nil, false
	}

	user, _, err := h.manager.ValidateToken(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid or expired token")
		return nil, nil, false
	}

	return user, sso.Linking(), true
}
