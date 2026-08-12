package auth

import (
	"net/http"
	"strings"
)

// The endpoints that make single sign-on work from configuration alone.
//
// Everything below them was already written — the providers, the state
// handling, the account linking — and none of it was reachable, because no
// route led to it. A person who wrote a social block got working provider
// objects inside a service nothing could call.
//
// Two routes per family, which is what an OAuth2 flow needs: one to send the
// browser to the provider, one for the provider to send it back to.

// handleSSOBegin starts an authorisation flow and redirects to the provider.
//
// The provider is named in the path — /auth/social/google — so that a single
// route serves every provider the configuration declares, rather than one
// route per provider name.
func (h *Handler) handleSSOBegin(w http.ResponseWriter, r *http.Request) {
	sso := h.manager.SSO()
	if sso == nil {
		h.writeError(w, http.StatusNotFound, "sso_not_configured", "No identity provider is configured")
		return
	}

	provider := r.PathValue("provider")
	if provider == "" {
		h.writeError(w, http.StatusBadRequest, "provider_required", "No provider was named in the path")
		return
	}

	authURL, _, err := sso.BeginAuth(r.Context(), provider)
	if err != nil {
		// Naming the ones that exist turns a typo into an answer rather than a
		// hunt through the configuration.
		h.writeError(w, http.StatusNotFound, "provider_not_found",
			"Unknown provider "+provider+"; configured: "+strings.Join(sso.GetAvailableProviders(), ", "))
		return
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleSSOCallback completes the flow the provider is returning from.
//
// The user is signed in on this service, not merely on the provider: the
// callback ends in the same session and token pair a password login produces,
// because that is what the caller has to carry afterwards.
func (h *Handler) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	sso := h.manager.SSO()
	if sso == nil {
		h.writeError(w, http.StatusNotFound, "sso_not_configured", "No identity provider is configured")
		return
	}

	query := r.URL.Query()

	// A provider reports a refusal here rather than by failing the request, so
	// a denied consent screen arrives as an ordinary callback.
	if providerErr := query.Get("error"); providerErr != "" {
		description := query.Get("error_description")
		if description == "" {
			description = providerErr
		}
		h.writeError(w, http.StatusUnauthorized, providerErr, description)
		return
	}

	state, code := query.Get("state"), query.Get("code")
	if state == "" || code == "" {
		h.writeError(w, http.StatusBadRequest, "invalid_callback", "The callback carried no state or no code")
		return
	}

	result, err := sso.HandleCallback(r.Context(), state, code)
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "sso_failed", err.Error())
		return
	}

	// Linking an identity to an existing account can need the account owner's
	// say-so. Signing them in at that point would be deciding it for them.
	if result.NeedsConfirmation {
		h.writeJSON(w, http.StatusOK, map[string]interface{}{
			"action":             result.Action,
			"needs_confirmation": true,
			"provider":           result.Provider,
			"email":              result.UserInfo.Email,
		})
		return
	}

	tokens, err := h.manager.EstablishSession(r.Context(), result.User, getClientIP(r), r.UserAgent())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "session_failed", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
		"action":        result.Action,
		"provider":      result.Provider,
		"user": map[string]interface{}{
			"id":    result.User.ID,
			"email": result.User.Email,
		},
	})
}
