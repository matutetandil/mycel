package auth

import (
	"context"
	"strings"
	"testing"
)

// A passkey is the credential somebody's phone or security key holds. What this
// side has to get right is the bookkeeping around it: which keys an account has,
// what happens to the recovery codes when the first one is added and the last
// one removed, and the counter that is the only defence against a cloned
// authenticator.

func passkeyStore(t *testing.T) (*MFAService, MFAStore) {
	t.Helper()
	store := NewMemoryMFAStore()
	service := NewMFAService(&MFAConfig{
		Enabled: true,
		WebAuthn: &WebAuthnConfig{
			RPName: "Example", RPID: "example.com",
			Origins: []string{"https://example.com"},
		},
	}, store)
	return service, store
}

func passkey(id, name string, count uint32) *WebAuthnCredential {
	return &WebAuthnCredential{
		ID: id, Name: name, SignCount: count,
		PublicKey: []byte("a key"), AttestationType: "none",
	}
}

func TestAPasskeyIsRememberedForTheAccountThatAddedIt(t *testing.T) {
	service, store := passkeyStore(t)
	ctx := context.Background()

	if err := service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-1", "", 0), "Ada's phone"); err != nil {
		t.Fatalf("AddWebAuthnCredential: %v", err)
	}

	data, err := store.GetMFAData(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetMFAData: %v", err)
	}
	if len(data.WebAuthnCredentials) != 1 {
		t.Fatalf("credentials = %v", data.WebAuthnCredentials)
	}
	// The name is what somebody recognises in a list of devices.
	if data.WebAuthnCredentials[0].Name != "Ada's phone" {
		t.Errorf("name = %q", data.WebAuthnCredentials[0].Name)
	}
}

func TestTheFirstPasskeyBringsRecoveryCodes(t *testing.T) {
	// Losing the only key that can sign in is how somebody locks themselves
	// out of an account for good, so the codes arrive with the first one.
	service, store := passkeyStore(t)
	ctx := context.Background()

	if err := service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-1", "", 0), "phone"); err != nil {
		t.Fatalf("AddWebAuthnCredential: %v", err)
	}

	data, _ := store.GetMFAData(ctx, "user-1")
	if len(data.RecoveryCodes) == 0 {
		t.Fatal("the first passkey was added with no way back in if it is lost")
	}
	before := len(data.RecoveryCodes)

	// A second key does not regenerate them: that would invalidate the codes
	// somebody has already written down.
	if err := service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-2", "", 0), "laptop"); err != nil {
		t.Fatalf("AddWebAuthnCredential: %v", err)
	}
	data, _ = store.GetMFAData(ctx, "user-1")
	if len(data.RecoveryCodes) != before {
		t.Errorf("the recovery codes changed when a second key was added: %d then %d", before, len(data.RecoveryCodes))
	}
}

func TestRemovingTheLastPasskeyTakesTheRecoveryCodesWithIt(t *testing.T) {
	// They exist to recover a second factor. With no second factor left they
	// are a list of valid credentials to nothing, sitting in a database.
	service, store := passkeyStore(t)
	ctx := context.Background()
	_ = service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-1", "", 0), "phone")
	_ = service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-2", "", 0), "laptop")

	if err := service.RemoveWebAuthnCredential(ctx, "user-1", "cred-1"); err != nil {
		t.Fatalf("RemoveWebAuthnCredential: %v", err)
	}
	data, _ := store.GetMFAData(ctx, "user-1")
	if len(data.WebAuthnCredentials) != 1 || data.WebAuthnCredentials[0].ID != "cred-2" {
		t.Fatalf("credentials = %v", data.WebAuthnCredentials)
	}
	if len(data.RecoveryCodes) == 0 {
		t.Error("the recovery codes went with a key that was not the last one")
	}

	if err := service.RemoveWebAuthnCredential(ctx, "user-1", "cred-2"); err != nil {
		t.Fatalf("RemoveWebAuthnCredential: %v", err)
	}
	data, _ = store.GetMFAData(ctx, "user-1")
	if len(data.WebAuthnCredentials) != 0 || len(data.RecoveryCodes) != 0 {
		t.Errorf("after the last key: credentials = %v, codes = %d",
			data.WebAuthnCredentials, len(data.RecoveryCodes))
	}
}

func TestRemovingAPasskeyNobodyRegisteredIsReported(t *testing.T) {
	service, _ := passkeyStore(t)
	ctx := context.Background()
	_ = service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-1", "", 0), "phone")

	if err := service.RemoveWebAuthnCredential(ctx, "user-1", "cred-nobody-has"); err == nil {
		t.Error("removing a credential that does not exist was reported as done")
	}
	if err := service.RemoveWebAuthnCredential(ctx, "user-nobody", "cred-1"); err == nil {
		t.Error("removing a credential from an account with none was reported as done")
	}
}

func TestTheCounterAPasskeyReportsIsKept(t *testing.T) {
	// An authenticator counts its own uses and the count only goes up. Keeping
	// the new one is what lets a cloned key be noticed later — a copy carries a
	// count that has fallen behind. Not storing it would leave every check
	// comparing against the count from the day it was registered.
	service, store := passkeyStore(t)
	ctx := context.Background()
	_ = service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-1", "", 4), "phone")

	used := passkey("cred-1", "phone", 5)
	if err := service.UpdateWebAuthnCredential(ctx, "user-1", used); err != nil {
		t.Fatalf("UpdateWebAuthnCredential: %v", err)
	}

	data, _ := store.GetMFAData(ctx, "user-1")
	if data.WebAuthnCredentials[0].SignCount != 5 {
		t.Errorf("sign count = %d, want the one the authenticator reported",
			data.WebAuthnCredentials[0].SignCount)
	}
	// And the moment it was last used, which is what a list of devices shows.
	if data.WebAuthnCredentials[0].LastUsedAt.IsZero() {
		t.Error("the key was used and nothing recorded when")
	}
}

func TestUpdatingAPasskeyThatIsNotThereChangesNothing(t *testing.T) {
	service, store := passkeyStore(t)
	ctx := context.Background()
	_ = service.AddWebAuthnCredential(ctx, "user-1", passkey("cred-1", "", 4), "phone")

	if err := service.UpdateWebAuthnCredential(ctx, "user-1", passkey("cred-other", "", 99)); err != nil {
		t.Fatalf("UpdateWebAuthnCredential: %v", err)
	}
	data, _ := store.GetMFAData(ctx, "user-1")
	if len(data.WebAuthnCredentials) != 1 || data.WebAuthnCredentials[0].SignCount != 4 {
		t.Errorf("credentials = %+v", data.WebAuthnCredentials)
	}

	// Nothing to update is not a failure: a login with no credential attached
	// must not fail on the bookkeeping afterwards.
	if err := service.UpdateWebAuthnCredential(ctx, "user-1", nil); err != nil {
		t.Errorf("updating with nothing reported a failure: %v", err)
	}
}

func TestPasskeysNeedTheFeatureToBeConfigured(t *testing.T) {
	// A service with no relying party configured cannot register one, and has
	// to say so rather than storing a credential nothing can verify.
	store := NewMemoryMFAStore()
	service := NewMFAService(&MFAConfig{Enabled: true}, store)

	err := service.AddWebAuthnCredential(context.Background(), "user-1", passkey("cred-1", "", 0), "phone")
	if err == nil {
		t.Fatal("a passkey was registered against a service that cannot check one")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "webauthn") {
		t.Errorf("error = %q, want it to name what is not configured", err)
	}
}

func TestPasskeysNeedMFAToBeOn(t *testing.T) {
	store := NewMemoryMFAStore()
	service := NewMFAService(&MFAConfig{
		Enabled:  false,
		WebAuthn: &WebAuthnConfig{RPName: "Example", RPID: "example.com"},
	}, store)

	if err := service.AddWebAuthnCredential(context.Background(), "user-1", passkey("c", "", 0), "phone"); err == nil {
		t.Error("a passkey was registered on a service with multi-factor turned off")
	}
}
