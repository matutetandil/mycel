package auth

import (
	"context"
	"strings"
	"testing"
)

// What a failed sign-in is counted against, and what the manager keeps its
// state in. Both are decisions somebody made in the configuration, and neither
// was exercised.

func TestWhatAFailedSignInIsCountedAgainst(t *testing.T) {
	// Counting by address alone lets one attacker lock out an account they do
	// not own; counting by address of origin alone lets a botnet through.
	// Which one a service picked has to be the one that is used.
	for name, tc := range map[string]struct {
		trackBy string
		want    string
	}{
		"the account":        {"user", "ada@example.com"},
		"where it came from": {"ip", "10.0.0.1"},
		"both together":      {"ip+user", "10.0.0.1:ada@example.com"},
		"nothing said":       {"", "10.0.0.1:ada@example.com"},
		"something unknown":  {"by-the-moon", "10.0.0.1:ada@example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := NewManager(&Config{
				Preset: "development",
				JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
				Security: &SecurityConfig{
					BruteForce: &BruteForceConfig{Enabled: true, TrackBy: tc.trackBy},
				},
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}

			if got := m.bruteForceKey("ada@example.com", "10.0.0.1"); got != tc.want {
				t.Errorf("counted against %q, want %q", got, tc.want)
			}
		})
	}

	// Every preset configures this, so the fallback below is for a manager
	// built without one: it counts against the account, which is the safe end
	// — an attacker cannot lock somebody else out by guessing from anywhere.
	bare := &Manager{config: &Config{}}
	if got := bare.bruteForceKey("ada@example.com", "10.0.0.1"); got != "ada@example.com" {
		t.Errorf("counted against %q", got)
	}
}

func TestGuessingAPasswordEnoughTimesLocksTheAccount(t *testing.T) {
	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
		Security: &SecurityConfig{
			BruteForce: &BruteForceConfig{
				Enabled: true, MaxAttempts: 3, Window: "15m", LockoutTime: "15m", TrackBy: "user", FailDelay: "0",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	user := registeredAccount(t, m)

	for i := 0; i < 3; i++ {
		if _, _, err := m.Login(context.Background(), &LoginRequest{
			Email: user.Email, Password: "not-the-password",
		}, "10.0.0.1", "test"); err == nil {
			t.Fatal("the wrong password signed in")
		}
	}

	// The right password now, which must not work: that is the whole point of
	// a lockout.
	_, _, err = m.Login(context.Background(), &LoginRequest{
		Email: user.Email, Password: "a-long-enough-password",
	}, "10.0.0.1", "test")
	if err == nil {
		t.Fatal("the account signed in after being locked")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "lock") {
		t.Errorf("error = %q, want it to say the account is locked", err)
	}
}

func TestStoresGivenToTheManagerAreTheOnesItUses(t *testing.T) {
	// The runtime hands these in from the storage block, and one that is
	// accepted and ignored means a service keeping its sessions somewhere
	// nobody configured.
	sessions := NewMemorySessionStore()
	tokens := NewMemoryTokenStore()
	bruteForce := NewMemoryBruteForceStore()
	mfa := NewMemoryMFAStore()
	users := NewMemoryUserStore()

	m, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-the-tests"},
	},
		WithUserStore(users),
		WithSessionStore(sessions),
		WithTokenStore(tokens),
		WithBruteForceStore(bruteForce),
		WithMFAStore(mfa),
	)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if m.sessionStore != sessions {
		t.Error("the session store handed in is not the one in use")
	}
	if m.tokenStore != tokens {
		t.Error("the token store handed in is not the one in use")
	}
	if m.bruteForceStore != bruteForce {
		t.Error("the brute force store handed in is not the one in use")
	}
	if m.mfaStore != mfa {
		t.Error("the MFA store handed in is not the one in use")
	}
	if m.userStore != users {
		t.Error("the user store handed in is not the one in use")
	}

	// And a session opened by the manager lands in the store that was given.
	user := registeredAccount(t, m)
	count, err := sessions.Count(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count == 0 {
		t.Error("the session went somewhere other than the store handed in")
	}
}

func TestAOneTimeCodeIsCheckedAgainstTheSecret(t *testing.T) {
	// The standalone check, which is what a service verifying a code outside
	// the manager reaches for.
	config := &TOTPConfig{Issuer: "Orders", Digits: 6, Period: 30}
	service := NewTOTPService(config)

	secret, err := service.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}

	code := service.GenerateCode(secret)

	if !ValidateTOTPCode(secret, code, config) {
		t.Error("the code this secret produces was refused")
	}
	if ValidateTOTPCode(secret, "000000", config) {
		t.Error("a code nobody generated was accepted")
	}
	// Another secret's code, which is what an attacker with their own
	// authenticator would send.
	other, err := service.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	otherCode := service.GenerateCode(other)
	if otherCode != code && ValidateTOTPCode(secret, otherCode, config) {
		t.Error("a code from another secret was accepted")
	}
}
