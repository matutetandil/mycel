package auth

import (
	"context"
	"strings"
	"testing"
)

// How many places one account can be signed in from, and what happens at the
// limit. Both answers are somebody's policy: one says "you are already signed
// in on three devices", the other quietly signs out the oldest.

func sessionManager(t *testing.T, sessions *SessionsConfig) *Manager {
	t.Helper()
	m, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
		Sessions: sessions,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestSigningInFromOneMoreDeviceThanAllowed(t *testing.T) {
	for name, tc := range map[string]struct {
		onMaxReached string
		refused      bool
	}{
		"the newest is refused": {"reject_new", true},
		"the oldest is ended":   {"revoke_oldest", false},
		"nothing said":          {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			m := sessionManager(t, &SessionsConfig{
				MaxActive: 2, OnMaxReached: tc.onMaxReached,
			})
			user := registeredAccount(t, m)

			// The registration opened one; open a second.
			if _, err := m.EstablishSession(context.Background(), user, "10.0.0.1", "phone"); err != nil {
				t.Fatalf("the second session was refused: %v", err)
			}

			_, err := m.EstablishSession(context.Background(), user, "10.0.0.2", "laptop")
			if tc.refused {
				if err == nil {
					t.Fatal("a session past the limit was opened")
				}
				if !strings.Contains(err.Error(), "sessions") {
					t.Errorf("error = %q, want it to say why", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("EstablishSession: %v", err)
			}
			// The limit holds: the oldest was ended to make room.
			count, err := m.sessionStore.Count(context.Background(), user.ID)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if count > 2 {
				t.Errorf("%d sessions open, want the limit to hold", count)
			}
		})
	}
}

func TestWithNoLimitEveryDeviceGetsASession(t *testing.T) {
	m := sessionManager(t, &SessionsConfig{})
	user := registeredAccount(t, m)

	for i := 0; i < 5; i++ {
		if _, err := m.EstablishSession(context.Background(), user, "10.0.0.1", "device"); err != nil {
			t.Fatalf("session %d was refused: %v", i, err)
		}
	}
}

func TestASessionOpenedThisWayCanBeUsed(t *testing.T) {
	// EstablishSession is the tail of Login, and signing in through an
	// identity provider ends the same way — so the tokens it issues have to
	// be the same kind of tokens.
	m := sessionManager(t, &SessionsConfig{})
	user := registeredAccount(t, m)

	tokens, err := m.EstablishSession(context.Background(), user, "10.0.0.1", "browser")
	if err != nil {
		t.Fatalf("EstablishSession: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("tokens = %+v", tokens)
	}

	seen, claims, err := m.ValidateToken(context.Background(), tokens.AccessToken)
	if err != nil {
		t.Fatalf("the token it issued does not validate: %v", err)
	}
	if seen.ID != user.ID {
		t.Errorf("the token belongs to %q, want the account it was issued for", seen.ID)
	}
	// A session id, because signing out has to have something to end.
	if claims.SessionID == "" {
		t.Error("the token belongs to no session, so signing out cannot end it")
	}
}

// --- Identities attached to an account --------------------------------------

func TestConfirmingALinkAttachesTheIdentity(t *testing.T) {
	// The branch for when a provider's address matches an existing account:
	// the service asks rather than attaching silently, because attaching on a
	// matching address alone is how somebody takes an account by registering
	// the address at a provider.
	users := NewMemoryUserStore()
	store := NewMemoryLinkedAccountStore()
	linking := NewAccountLinkingService(nil, store, users)

	m := sessionManager(t, &SessionsConfig{})
	user := registeredAccount(t, m)
	if err := users.Create(context.Background(), user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	account, err := linking.ConfirmLink(context.Background(), user.ID,
		&OAuth2UserInfo{Provider: "google", ID: "google-1", Email: user.Email, Name: "Ada"},
		&OAuth2Token{AccessToken: "t", ExpiresIn: 3600})
	if err != nil {
		t.Fatalf("ConfirmLink: %v", err)
	}
	if account.UserID != user.ID {
		t.Errorf("the identity landed on %q", account.UserID)
	}
	// The provider said when its token expires, and a refresh has to know.
	if account.ExpiresAt == nil {
		t.Error("the token's expiry was not kept, so nothing knows when to refresh")
	}

	listed, err := linking.GetLinkedAccounts(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetLinkedAccounts: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("%d identities attached, want the one confirmed", len(listed))
	}
}

func TestAnIdentityAlreadyOnAnotherAccountIsNotMoved(t *testing.T) {
	// Otherwise confirming a link takes somebody else's identity, which is
	// exactly the attack the confirmation exists to stop.
	users := NewMemoryUserStore()
	store := NewMemoryLinkedAccountStore()
	linking := NewAccountLinkingService(nil, store, users)

	for _, id := range []string{"u-1", "u-2"} {
		if err := users.Create(context.Background(), &User{ID: id, Email: id + "@example.com"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	identity := &OAuth2UserInfo{Provider: "google", ID: "google-1", Email: "ada@example.com"}
	if _, err := linking.ConfirmLink(context.Background(), "u-1", identity, &OAuth2Token{AccessToken: "t"}); err != nil {
		t.Fatalf("ConfirmLink: %v", err)
	}

	_, err := linking.ConfirmLink(context.Background(), "u-2", identity, &OAuth2Token{AccessToken: "t"})
	if err == nil {
		t.Error("an identity already on one account was attached to another")
	}
}

func TestConfirmingForAnAccountNobodyHasIsRefused(t *testing.T) {
	linking := NewAccountLinkingService(nil, NewMemoryLinkedAccountStore(), NewMemoryUserStore())

	_, err := linking.ConfirmLink(context.Background(), "u-nobody",
		&OAuth2UserInfo{Provider: "google", ID: "google-1"},
		&OAuth2Token{AccessToken: "t"})
	if err == nil {
		t.Error("an identity was attached to an account that does not exist")
	}
}

func TestClosingAnAccountTakesItsIdentitiesWithIt(t *testing.T) {
	// An identity left behind still points at an account id, and the next
	// account issued that id inherits somebody else's sign-in.
	store := NewMemoryLinkedAccountStore()
	ctx := context.Background()

	for _, provider := range []string{"google", "github"} {
		if err := store.Create(ctx, &LinkedAccount{
			ID: provider + "-1", UserID: "u-1", Provider: provider, ProviderID: "x",
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := store.Create(ctx, &LinkedAccount{
		ID: "other", UserID: "u-2", Provider: "google", ProviderID: "y",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.DeleteByUserID(ctx, "u-1"); err != nil {
		t.Fatalf("DeleteByUserID: %v", err)
	}

	gone, err := store.FindByUserID(ctx, "u-1")
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(gone) != 0 {
		t.Errorf("%d identities left behind", len(gone))
	}

	kept, err := store.FindByUserID(ctx, "u-2")
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(kept) != 1 {
		t.Error("another account's identity was taken with it")
	}
}
