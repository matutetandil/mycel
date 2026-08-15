package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
)

// Passkeys. A registration or a login is a ceremony in two halves — the server
// issues a challenge, the authenticator signs it, the server checks the
// signature — and the half that matters is the second one: a service that
// accepts an assertion it did not verify authenticates anybody at all.
//
// Signing a real assertion needs an authenticator, which a test does not have.
// What is checked here is everything around it: that the challenge says who is
// asking, that it is not reused, and that anything not signed by the key the
// account registered is refused.

func webauthnService(t *testing.T) *WebAuthnService {
	t.Helper()
	svc := NewWebAuthnService(&WebAuthnConfig{
		RPID:          "orders.example.com",
		RPName:        "Orders",
		RPDisplayName: "Orders",
		Origins:       []string{"https://orders.example.com"},
	})
	if svc == nil || !svc.IsConfigured() {
		t.Fatal("the service was not configured")
	}
	return svc
}

func TestAChallengeSaysWhoIsAskingForIt(t *testing.T) {
	// The relying party is what stops a passkey registered for one site being
	// used on another: the browser refuses to sign for an id that does not
	// match where it is.
	svc := webauthnService(t)

	options, session, err := svc.BeginRegistration(context.Background(),
		"u-1", "ada@example.com", "Ada", nil)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	if options.Response.RelyingParty.ID != "orders.example.com" {
		t.Errorf("relying party = %q", options.Response.RelyingParty.ID)
	}
	if len(options.Response.Challenge) == 0 {
		t.Error("the challenge is empty, so anything would sign it")
	}
	if session == "" {
		t.Fatal("no session was carried, so the second half has nothing to check against")
	}

	// What the server has to remember between the halves: the challenge it
	// issued and who it issued it to.
	var carried map[string]interface{}
	if err := json.Unmarshal([]byte(session), &carried); err != nil {
		t.Fatalf("the session is not something the server can read back: %v", err)
	}
	if carried["challenge"] == nil {
		t.Errorf("the session carries no challenge: %v", carried)
	}
}

func TestEveryChallengeIsANewOne(t *testing.T) {
	// A reused challenge is a replayed assertion: an attacker who saw one
	// signature could use it again.
	svc := webauthnService(t)

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		options, _, err := svc.BeginRegistration(context.Background(), "u-1", "ada@example.com", "Ada", nil)
		if err != nil {
			t.Fatalf("BeginRegistration: %v", err)
		}
		challenge := options.Response.Challenge.String()
		if seen[challenge] {
			t.Fatal("the same challenge was issued twice")
		}
		seen[challenge] = true
	}
}

func TestAKeyAlreadyRegisteredIsNotOfferedAgain(t *testing.T) {
	// The browser reads this list and refuses to enrol a key that is already
	// on the account, which is how somebody avoids registering the same
	// device twice without noticing.
	svc := webauthnService(t)

	options, _, err := svc.BeginRegistration(context.Background(), "u-1", "ada@example.com", "Ada",
		[]WebAuthnCredential{{ID: "already-enrolled", PublicKey: []byte("key")}})
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	if len(options.Response.CredentialExcludeList) != 1 {
		t.Errorf("%d keys excluded, want the one already on the account",
			len(options.Response.CredentialExcludeList))
	}
}

func TestASignInOffersTheKeysTheAccountRegistered(t *testing.T) {
	svc := webauthnService(t)

	options, session, err := svc.BeginLogin(context.Background(), "u-1", "ada@example.com",
		[]WebAuthnCredential{{ID: "the-key", PublicKey: []byte("key")}})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if len(options.Response.AllowedCredentials) != 1 {
		t.Errorf("%d keys offered, want the one registered", len(options.Response.AllowedCredentials))
	}
	if session == "" {
		t.Error("no session was carried")
	}
}

func TestSigningInWithNoKeyRegisteredIsRefused(t *testing.T) {
	// Otherwise the ceremony starts for an account that has no passkey, and
	// whatever comes back has nothing to be checked against.
	svc := webauthnService(t)

	if _, _, err := svc.BeginLogin(context.Background(), "u-1", "ada@example.com", nil); err == nil {
		t.Error("a passkey sign-in started for an account with no passkey")
	}
}

func TestAnAssertionNobodyValidSignedIsRefused(t *testing.T) {
	// The half that matters. None of these can be verified, and each has to be
	// refused rather than treated as a signature the service could not read.
	svc := webauthnService(t)

	_, session, err := svc.BeginRegistration(context.Background(), "u-1", "ada@example.com", "Ada", nil)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	forged := `{
	  "id": "AAAA",
	  "rawId": "AAAA",
	  "type": "public-key",
	  "response": {
	    "attestationObject": "AAAA",
	    "clientDataJSON": "eyJ0eXBlIjoid2ViYXV0aG4uY3JlYXRlIn0"
	  }
	}`

	parsed, parseErr := protocol.ParseCredentialCreationResponseBody(bytes.NewReader([]byte(forged)))
	if parseErr == nil {
		if _, err := svc.FinishRegistration(context.Background(), "u-1", "ada@example.com", "Ada",
			nil, session, parsed); err == nil {
			t.Error("a credential nobody signed was registered")
		}
	}

	// And a session the server never issued.
	if _, err := svc.FinishRegistration(context.Background(), "u-1", "ada@example.com", "Ada",
		nil, "not-a-session", nil); err == nil {
		t.Error("a registration completed against a session the server never issued")
	}
}

func TestNoWebAuthnConfiguredRefusesEveryCeremony(t *testing.T) {
	// A service that did not configure passkeys must refuse them by name,
	// rather than starting a ceremony nothing can finish.
	svc := NewWebAuthnService(&WebAuthnConfig{RPName: "Orders"}) // no RPID

	if svc.IsConfigured() {
		t.Fatal("a service with no relying party id reported itself configured")
	}

	if _, _, err := svc.BeginRegistration(context.Background(), "u-1", "ada@example.com", "Ada", nil); err == nil {
		t.Error("a registration started with no configuration")
	}
	if _, _, err := svc.BeginLogin(context.Background(), "u-1", "ada@example.com",
		[]WebAuthnCredential{{ID: "k"}}); err == nil {
		t.Error("a sign-in started with no configuration")
	}
	if _, err := svc.FinishRegistration(context.Background(), "u-1", "a", "A", nil, "{}", nil); err == nil {
		t.Error("a registration completed with no configuration")
	}
	if _, err := svc.FinishLogin(context.Background(), "u-1", "a", nil, "{}", nil); err == nil {
		t.Error("a sign-in completed with no configuration")
	}

	// And a service that configured nothing at all.
	if NewWebAuthnService(nil) != nil {
		t.Error("a service was built from no configuration")
	}
}

func TestAConfigurationThatCannotBeUsedSaysWhy(t *testing.T) {
	// Passkeys need the addresses the browser is on, and forgetting them is
	// the ordinary mistake. The error used to be swallowed: the service
	// started without a word and then refused every passkey with "webauthn is
	// not configured", which is true and names none of the settings that
	// would fix it.
	svc := NewWebAuthnService(&WebAuthnConfig{RPID: "orders.example.com", RPName: "Orders"})

	if svc.IsConfigured() {
		t.Fatal("a service with no origins reported itself usable")
	}
	err := svc.ConfigError()
	if err == nil {
		t.Fatal("nothing was kept to say what is wrong")
	}
	if !strings.Contains(err.Error(), "orders.example.com") {
		t.Errorf("error = %q, want it to name what was configured", err)
	}
}

func TestTheDisplayNameFallsBackToSomethingReadable(t *testing.T) {
	// It is what the browser shows in the prompt, and an empty one reads as a
	// site asking for a passkey without saying who it is.
	svc := webauthnService(t)

	options, _, err := svc.BeginRegistration(context.Background(), "u-1", "ada@example.com", "", nil)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if strings.TrimSpace(options.Response.RelyingParty.Name) == "" {
		t.Error("the prompt names nobody")
	}
	// The user's own display name falls back to the account name.
	user := &WebAuthnUser{ID: "u-1", Name: "ada@example.com"}
	if user.WebAuthnDisplayName() != "ada@example.com" {
		t.Errorf("display name = %q", user.WebAuthnDisplayName())
	}
	if user.WebAuthnIcon() != "" {
		t.Errorf("icon = %q, want none", user.WebAuthnIcon())
	}
	if len(user.WebAuthnCredentials()) != 0 {
		t.Error("an account with no keys reported some")
	}
}
