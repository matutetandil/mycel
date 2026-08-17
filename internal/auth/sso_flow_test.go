package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Signing in with Google, GitHub or a company's own identity provider, all the
// way through: the redirect out, the state coming back, the code exchanged, and
// the identity attached to an account. The exchange itself was covered; what
// happens around it — which account the identity lands on, and what is refused —
// was not.

// fakeProvider stands in for Google or GitHub without the network.
type fakeProvider struct {
	name      string
	user      *OAuth2UserInfo
	exchange  error
	userInfo  error
	exchanges int
}

func (p *fakeProvider) Name() string { return p.name }

func (p *fakeProvider) GetAuthURL(state string) string {
	return "https://provider.example.com/authorize?state=" + state
}

func (p *fakeProvider) ExchangeCode(context.Context, string) (*OAuth2Token, error) {
	p.exchanges++
	if p.exchange != nil {
		return nil, p.exchange
	}
	return &OAuth2Token{AccessToken: "provider-token", RefreshToken: "provider-refresh"}, nil
}

func (p *fakeProvider) GetUserInfo(context.Context, *OAuth2Token) (*OAuth2UserInfo, error) {
	if p.userInfo != nil {
		return nil, p.userInfo
	}
	return p.user, nil
}

// ssoWith builds a service holding one provider under the given name.
func ssoWith(t *testing.T, provider *fakeProvider) (*SSOService, UserStore) {
	t.Helper()
	users := NewMemoryUserStore()
	svc := NewSSOService(&Config{Preset: "development"}, NewMemoryLinkedAccountStore(), users, nil)
	svc.socialProviders[provider.name] = provider
	return svc, users
}

func TestSigningInWithAProviderCreatesTheAccount(t *testing.T) {
	svc, users := ssoWith(t, &fakeProvider{
		name: "google",
		user: &OAuth2UserInfo{
			Provider: "google", ID: "google-1", Email: "ada@example.com",
			Name: "Ada", EmailVerified: true,
		},
	})

	authURL, state, err := svc.BeginAuth(context.Background(), "google")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	if !strings.Contains(authURL, state) {
		t.Error("the address the browser is sent to does not carry the state, so the callback cannot be matched")
	}

	result, err := svc.HandleCallback(context.Background(), state, "the-code")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if result.User == nil || result.User.Email != "ada@example.com" {
		t.Fatalf("user = %+v", result.User)
	}
	if result.Action != "created" {
		t.Errorf("action = %q, want the account to have been created", result.Action)
	}

	// And it is an account, not only a result: signing in again has to find it.
	if _, err := users.FindByEmail(context.Background(), "ada@example.com"); err != nil {
		t.Errorf("the account was not stored: %v", err)
	}
}

func TestSigningInTwiceUsesTheSameAccount(t *testing.T) {
	// Otherwise every sign-in creates another account and somebody's history
	// scatters across all of them.
	provider := &fakeProvider{
		name: "google",
		user: &OAuth2UserInfo{
			Provider: "google", ID: "google-1", Email: "ada@example.com", EmailVerified: true,
		},
	}
	svc, _ := ssoWith(t, provider)

	var ids []string
	for i := 0; i < 2; i++ {
		_, state, err := svc.BeginAuth(context.Background(), "google")
		if err != nil {
			t.Fatalf("BeginAuth: %v", err)
		}
		result, err := svc.HandleCallback(context.Background(), state, "the-code")
		if err != nil {
			t.Fatalf("HandleCallback: %v", err)
		}
		ids = append(ids, result.User.ID)
	}

	if ids[0] != ids[1] {
		t.Errorf("two sign-ins produced two accounts: %s and %s", ids[0], ids[1])
	}
}

func TestAStateNobodyIssuedIsRefused(t *testing.T) {
	// The state is what ties the callback to the redirect that started it.
	// Accepting one nobody issued is what a cross-site request forgery against
	// the sign-in looks like.
	svc, _ := ssoWith(t, &fakeProvider{name: "google", user: &OAuth2UserInfo{ID: "1"}})

	_, err := svc.HandleCallback(context.Background(), "a-state-nobody-issued", "code")
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("error = %v, want the state to be refused", err)
	}
}

func TestAStateIsSpentOnce(t *testing.T) {
	// A callback replayed with the same state must not sign anybody in a second
	// time.
	svc, _ := ssoWith(t, &fakeProvider{
		name: "google",
		user: &OAuth2UserInfo{Provider: "google", ID: "google-1", Email: "ada@example.com"},
	})

	_, state, err := svc.BeginAuth(context.Background(), "google")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	if _, err := svc.HandleCallback(context.Background(), state, "code"); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if _, err := svc.HandleCallback(context.Background(), state, "code"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("error = %v, want the replayed callback to be refused", err)
	}
}

func TestAStateThatHasBeenSittingTooLongIsRefused(t *testing.T) {
	// Somebody who opens the provider's page and comes back an hour later
	// starts again rather than completing a flow nothing remembers the context
	// of.
	svc, _ := ssoWith(t, &fakeProvider{
		name: "google",
		user: &OAuth2UserInfo{Provider: "google", ID: "google-1", Email: "ada@example.com"},
	})

	_, state, err := svc.BeginAuth(context.Background(), "google")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}

	svc.mu.Lock()
	svc.states[state].ExpiresAt = time.Now().Add(-time.Minute)
	svc.mu.Unlock()

	if _, err := svc.HandleCallback(context.Background(), state, "code"); !errors.Is(err, ErrStateExpired) {
		t.Errorf("error = %v, want the expired state to be refused", err)
	}
}

func TestAProviderNobodyConfiguredIsRefused(t *testing.T) {
	svc, _ := ssoWith(t, &fakeProvider{name: "google", user: &OAuth2UserInfo{ID: "1"}})

	if _, _, err := svc.BeginAuth(context.Background(), "facebook"); !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("error = %v, want the provider to be refused", err)
	}
	// And nothing is left behind for a callback to find.
	svc.mu.RLock()
	states := len(svc.states)
	svc.mu.RUnlock()
	if states != 0 {
		t.Errorf("%d states left behind by a flow that never started", states)
	}
}

func TestAProviderThatFailsMidFlowIsReported(t *testing.T) {
	// Two different failures, and neither may end with somebody signed in.
	for name, provider := range map[string]*fakeProvider{
		"refusing the code": {
			name: "google", exchange: errors.New("invalid_grant"),
			user: &OAuth2UserInfo{ID: "1"},
		},
		"refusing to say who it is": {
			name: "google", userInfo: errors.New("insufficient scope"),
			user: &OAuth2UserInfo{ID: "1"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, _ := ssoWith(t, provider)
			_, state, err := svc.BeginAuth(context.Background(), "google")
			if err != nil {
				t.Fatalf("BeginAuth: %v", err)
			}
			result, err := svc.HandleCallback(context.Background(), state, "code")
			if err == nil {
				t.Fatalf("the sign-in completed anyway: %+v", result)
			}
		})
	}
}

func TestTheProvidersAServiceOffersAreTheOnesConfigured(t *testing.T) {
	// This list is what a sign-in page draws its buttons from.
	svc, _ := ssoWith(t, &fakeProvider{name: "google", user: &OAuth2UserInfo{ID: "1"}})

	providers := svc.GetAvailableProviders()
	if len(providers) != 1 || providers[0] != "google" {
		t.Errorf("providers = %v", providers)
	}
}

func TestAnIdentityCanBeDetachedFromAnAccount(t *testing.T) {
	// Somebody who signed up with Google and later set a password should be
	// able to disconnect Google — and the identity must actually go, or the
	// next sign-in reattaches it.
	svc, users := ssoWith(t, &fakeProvider{
		name: "google",
		user: &OAuth2UserInfo{
			Provider: "google", ID: "google-1", Email: "ada@example.com", EmailVerified: true,
		},
	})

	_, state, err := svc.BeginAuth(context.Background(), "google")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	result, err := svc.HandleCallback(context.Background(), state, "code")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	userID := result.User.ID

	// Detaching the only way into an account is refused, which is right: it
	// would lock somebody out of their own account with one click.
	if err := svc.UnlinkAccount(context.Background(), userID, "google"); err == nil {
		t.Error("the only way into the account was detached")
	}

	// Once there is a password, the identity can go.
	stored, err := users.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	stored.PasswordHash = "$argon2id$something"
	if err := users.Update(context.Background(), stored); err != nil {
		t.Fatalf("Update: %v", err)
	}

	accounts, err := svc.GetLinkedAccounts(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetLinkedAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("%d identities attached, want the one that was just used", len(accounts))
	}

	if err := svc.UnlinkAccount(context.Background(), userID, "google"); err != nil {
		t.Fatalf("UnlinkAccount: %v", err)
	}

	accounts, err = svc.GetLinkedAccounts(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetLinkedAccounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("%d identities still attached after unlinking", len(accounts))
	}
}

func TestStatesFromFlowsNobodyFinishedAreCleanedUp(t *testing.T) {
	// A sign-in page that is opened and abandoned leaves a state behind, and
	// a busy service accumulates them for as long as it runs.
	svc, _ := ssoWith(t, &fakeProvider{name: "google", user: &OAuth2UserInfo{ID: "1"}})

	for i := 0; i < 3; i++ {
		if _, _, err := svc.BeginAuth(context.Background(), "google"); err != nil {
			t.Fatalf("BeginAuth: %v", err)
		}
	}
	// One of them is still live; the other two were abandoned.
	svc.mu.Lock()
	aged := 0
	for _, state := range svc.states {
		if aged == 2 {
			break
		}
		state.ExpiresAt = time.Now().Add(-time.Minute)
		aged++
	}
	svc.mu.Unlock()

	svc.CleanExpiredStates()

	svc.mu.RLock()
	remaining := len(svc.states)
	svc.mu.RUnlock()
	if remaining != 1 {
		t.Errorf("%d states held, want only the one that is still live", remaining)
	}

	// And the sweep runs on its own: cancelling the context stops it rather
	// than leaving a ticker running for the life of the process.
	ctx, cancel := context.WithCancel(context.Background())
	svc.StartStateCleanup(ctx)
	cancel()
}

func TestARefreshNeedsSomethingToRefreshWith(t *testing.T) {
	// A provider that issued no refresh token cannot be asked for a new access
	// token, and asking anyway is a request that fails with the provider's own
	// wording rather than this one.
	svc, _ := ssoWith(t, &fakeProvider{name: "google", user: &OAuth2UserInfo{ID: "1"}})

	if _, err := svc.RefreshProviderToken(context.Background(), &LinkedAccount{
		Provider: "google", UserID: "u-1",
	}); err == nil {
		t.Error("a refresh was attempted with no refresh token")
	}

	// And one for a provider nobody configured.
	if _, err := svc.RefreshProviderToken(context.Background(), &LinkedAccount{
		Provider: "facebook", UserID: "u-1", RefreshToken: "r",
	}); err == nil {
		t.Error("a refresh was attempted against a provider nobody configured")
	}
}
