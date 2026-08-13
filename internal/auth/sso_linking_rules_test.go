package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Linking joins an identity at a provider to an account here, and it does so on
// the strength of an email address. That is the whole attack: if somebody can
// present an address at a provider without proving it is theirs, and this
// service links on it, they are handed the account that already owns it.
//
// So the rules are worth stating plainly — which is what these do.

func linkingService(t *testing.T, config *AccountLinkingConfig, existing ...*User) (*AccountLinkingService, UserStore) {
	t.Helper()
	users := NewMemoryUserStore()
	for _, u := range existing {
		if err := users.Create(context.Background(), u); err != nil {
			t.Fatalf("seeding the user store: %v", err)
		}
	}
	return NewAccountLinkingService(config, NewMemoryLinkedAccountStore(), users), users
}

func socialIdentity(email string, verified bool) *OAuth2UserInfo {
	return &OAuth2UserInfo{
		ID: "provider-user-1", Provider: ProviderGoogle,
		Email: email, EmailVerified: verified, Name: "Someone",
	}
}

func TestAVerifiedAddressLinksToTheAccountThatOwnsIt(t *testing.T) {
	// The case the feature exists for: somebody who registered with a password
	// signs in with Google and lands in the same account.
	owner := &User{ID: "u-1", Email: "someone@example.com", PasswordHash: "hash"}
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "email", RequireVerification: true, OnMatch: "link",
	}, owner)

	result, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", true), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("LinkOrCreate: %v", err)
	}
	if result.Action != "linked" {
		t.Errorf("action = %q, want the identity linked to the existing account", result.Action)
	}
	if result.User.ID != owner.ID {
		t.Errorf("signed in as %q, want the account that owns the address", result.User.ID)
	}
}

func TestAnUnverifiedAddressIsRefusedTheAccount(t *testing.T) {
	// The attack this stops: an address typed at a provider, never proved, that
	// happens to be somebody else's here.
	owner := &User{ID: "u-1", Email: "someone@example.com", PasswordHash: "hash"}
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "email", RequireVerification: true, OnMatch: "link",
	}, owner)

	_, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", false), &OAuth2Token{AccessToken: "at"})
	if err == nil {
		t.Fatal("an unverified address was linked to somebody else's account")
	}
	if !errors.Is(err, ErrVerificationRequired) {
		t.Errorf("error = %v, want it to say verification is what is missing", err)
	}
}

func TestVerificationCanBeWaivedDeliberately(t *testing.T) {
	// Some providers verify every address they hand out, and someone who knows
	// theirs does may say so. It has to be a choice, not the default.
	owner := &User{ID: "u-1", Email: "someone@example.com", PasswordHash: "hash"}
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "email", RequireVerification: false, OnMatch: "link",
	}, owner)

	result, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", false), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("LinkOrCreate: %v", err)
	}
	if result.User.ID != owner.ID {
		t.Errorf("signed in as %q", result.User.ID)
	}
}

func TestTheDefaultsRequireVerification(t *testing.T) {
	// A service built without a linking block gets the safe reading, since
	// leaving it out is not a decision to allow this.
	service := NewAccountLinkingService(nil, NewMemoryLinkedAccountStore(), NewMemoryUserStore())
	if !service.config.RequireVerification {
		t.Error("the default links an identity on an address nobody proved")
	}
	if service.config.MatchBy != "email" {
		t.Errorf("default match = %q", service.config.MatchBy)
	}
}

func TestAnAccountCanBeToldToRefuseRatherThanLink(t *testing.T) {
	owner := &User{ID: "u-1", Email: "someone@example.com", PasswordHash: "hash"}
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "email", RequireVerification: true, OnMatch: "reject",
	}, owner)

	_, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", true), &OAuth2Token{AccessToken: "at"})
	if !errors.Is(err, ErrAccountAlreadyLinked) {
		t.Errorf("error = %v, want the match refused", err)
	}
}

func TestAMatchCanBeLeftToTheAccountOwner(t *testing.T) {
	// The middle reading: the addresses agree, but joining two identities is
	// the owner's decision rather than the service's.
	owner := &User{ID: "u-1", Email: "someone@example.com", PasswordHash: "hash"}
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "email", RequireVerification: true, OnMatch: "prompt",
	}, owner)

	result, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", true), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("LinkOrCreate: %v", err)
	}
	if !result.NeedsConfirmation {
		t.Error("the identity was joined without asking")
	}
	if result.Action != "prompt" {
		t.Errorf("action = %q", result.Action)
	}
}

func TestMatchingTurnedOffMakesANewAccount(t *testing.T) {
	// Then an address that agrees means nothing, which is the strictest
	// reading and a legitimate one.
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "none", RequireVerification: true, OnMatch: "link",
	})

	result, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", true), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("LinkOrCreate: %v", err)
	}
	if result.Action != "created" {
		t.Errorf("action = %q, want a new account", result.Action)
	}
}

func TestMatchingOffAgainstAnAddressAlreadyRegisteredSaysWhy(t *testing.T) {
	// Two settings that cannot both be honoured: matching is off, so the
	// existing account is not joined, and the user store holds one account per
	// address, so another cannot be made. Whoever set match_by = "none" needs
	// to be told that, not "user already exists" from three layers down.
	owner := &User{ID: "u-1", Email: "someone@example.com", PasswordHash: "hash"}
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "none", RequireVerification: true, OnMatch: "link",
	}, owner)

	_, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", true), &OAuth2Token{AccessToken: "at"})
	if err == nil {
		t.Fatal("a second account was made for an address that already has one")
	}
	for _, want := range []string{"someone@example.com", "match_by"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

func TestAnIdentityWithNoAddressIsNotMatchedOnEmptiness(t *testing.T) {
	// A GitHub account with every address private. It must not be joined to
	// whatever account happens to have an empty address.
	other := &User{ID: "u-1", Email: "someone@example.com", PasswordHash: "hash"}
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "email", RequireVerification: true, OnMatch: "link",
	}, other)

	result, err := service.LinkOrCreate(context.Background(),
		socialIdentity("", false), &OAuth2Token{AccessToken: "at"})
	if err != nil {
		t.Fatalf("LinkOrCreate: %v", err)
	}
	if result.User.ID == other.ID {
		t.Error("an identity with no address was joined to an existing account")
	}
	if result.Action != "created" {
		t.Errorf("action = %q, want a new account", result.Action)
	}
}

func TestSigningInAgainReturnsTheSameAccount(t *testing.T) {
	// The second login must find the identity by what the provider calls it,
	// not make another account — which is what a mis-rendered identifier
	// would cause.
	service, _ := linkingService(t, &AccountLinkingConfig{
		Enabled: true, MatchBy: "email", RequireVerification: true, OnMatch: "link",
	})
	identity := socialIdentity("someone@example.com", true)

	first, err := service.LinkOrCreate(context.Background(), identity, &OAuth2Token{AccessToken: "at-1"})
	if err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	if first.Action != "created" {
		t.Errorf("first action = %q, want a new account", first.Action)
	}

	second, err := service.LinkOrCreate(context.Background(), identity, &OAuth2Token{AccessToken: "at-2"})
	if err != nil {
		t.Fatalf("second sign-in: %v", err)
	}
	if second.Action != "existing" {
		t.Errorf("second action = %q, want the account found rather than made again", second.Action)
	}
	if second.User.ID != first.User.ID {
		t.Errorf("signed in as %q then %q — the same person got two accounts",
			first.User.ID, second.User.ID)
	}

	// And the fresh token replaced the one that is on its way out.
	if second.LinkedAccount.AccessToken != "at-2" {
		t.Errorf("stored token = %q, want the one from this sign-in", second.LinkedAccount.AccessToken)
	}
}

func TestLinkingCanBeOffAltogether(t *testing.T) {
	service, _ := linkingService(t, &AccountLinkingConfig{Enabled: false})
	_, err := service.LinkOrCreate(context.Background(),
		socialIdentity("someone@example.com", true), &OAuth2Token{})
	if !errors.Is(err, ErrLinkingDisabled) {
		t.Errorf("error = %v, want it to say linking is off", err)
	}
}
