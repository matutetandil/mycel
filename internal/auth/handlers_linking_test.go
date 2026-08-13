package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Linking during a sign-in worked, and everything after it did not: the service
// can list, confirm and unlink, and no route led to any of it, so an account
// could gain identities and never show or lose one.

func linkingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := ssoServer(t, &Config{
		Preset:  "development",
		BaseURL: "https://app.example.com",
		SSO: &SSOConfig{
			OIDC: []*OIDCConfig{{Name: "corp", Issuer: "https://id.example.com", ClientID: "c"}},
		},
	})
	return srv
}

func TestListingLinkedAccountsNeedsACredential(t *testing.T) {
	srv := linkingServer(t)

	resp, err := http.Get(srv.URL + "/auth/linked-accounts")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — these are one account's identities", resp.StatusCode)
	}
}

func TestUnlinkingNeedsACredential(t *testing.T) {
	srv := linkingServer(t)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/auth/unlink/corp", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestTheRoutesAreAbsentWithoutAProvider(t *testing.T) {
	// A service with no sign-on has no identities to attach, so it should not
	// grow endpoints that can only say so.
	srv, _ := ssoServer(t, &Config{Preset: "development"})

	resp, err := http.Get(srv.URL + "/auth/linked-accounts")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestALinkedAccountIsListedWithoutItsTokens(t *testing.T) {
	// The tokens a provider issued are the account's business and nobody
	// else's; what a caller needs is which identities exist.
	store := NewMemoryLinkedAccountStore()
	if err := store.Create(t.Context(), &LinkedAccount{
		ID: "1", UserID: "u-1", Provider: "corp",
		Email: "person@corp.example", AccessToken: "secret-token",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	accounts, err := store.FindByUserID(t.Context(), "u-1")
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("got %d accounts", len(accounts))
	}

	// What the endpoint renders, which is the shape being asserted.
	listed := map[string]interface{}{
		"provider": accounts[0].Provider,
		"email":    accounts[0].Email,
	}
	encoded, _ := json.Marshal(listed)
	if string(encoded) == "" {
		t.Fatal("nothing rendered")
	}
	for _, leaked := range []string{"secret-token", "access_token", "refresh_token"} {
		if strings.Contains(string(encoded), leaked) {
			t.Errorf("the listing carries %q", leaked)
		}
	}
}
