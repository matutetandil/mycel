package auth

import (
	"context"
	"strings"
	"testing"
)

// Somebody returning to a password they have used before.
//
// Two stores for this have existed since the auth system was written, one for
// PostgreSQL and one for MySQL, each with its own tests, and nothing ever
// built one: the manager had no field to hold it. So `password { history = 3 }`
// let an account alternate between two passwords for ever, which is exactly
// what a person required to change theirs every ninety days will do.

func managerWithHistory(t *testing.T, depth int) (*Manager, *User) {
	t.Helper()

	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		// Short and unfussy, so the test is about reuse and not complexity.
		Password: &PasswordConfig{MinLength: 8, History: depth},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	hash, err := manager.passwordHasher.Hash("first-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	user := &User{ID: "user-1", Email: "someone@example.test", PasswordHash: hash}
	if err := manager.userStore.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return manager, user
}

func TestAPasswordCannotBeTheOneAlreadyInUse(t *testing.T) {
	// history = 1 is the smallest thing the setting can mean.
	manager, _ := managerWithHistory(t, 1)
	ctx := context.Background()

	err := manager.ChangePassword(ctx, "user-1", "first-password", "first-password")
	if err == nil {
		t.Fatal("changing a password to itself was allowed")
	}
	// The message has to say what is wrong, or somebody retries the same thing.
	if !strings.Contains(err.Error(), "already in use") {
		t.Errorf("the refusal does not explain itself: %v", err)
	}

	// And a different one still goes through.
	if err := manager.ChangePassword(ctx, "user-1", "first-password", "second-password"); err != nil {
		t.Fatalf("a password never used was refused: %v", err)
	}
}

func TestAnAccountCannotCycleBetweenTwoPasswords(t *testing.T) {
	// The failure this setting exists to stop, and the one that happened.
	manager, _ := managerWithHistory(t, 3)
	ctx := context.Background()

	if err := manager.ChangePassword(ctx, "user-1", "first-password", "second-password"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := manager.ChangePassword(ctx, "user-1", "second-password", "first-password"); err == nil {
		t.Fatal("an account went straight back to the password it had a moment ago")
	}

	// Two changes on, the first is still remembered: three deep means three.
	if err := manager.ChangePassword(ctx, "user-1", "second-password", "third-password"); err != nil {
		t.Fatalf("third: %v", err)
	}
	if err := manager.ChangePassword(ctx, "user-1", "third-password", "first-password"); err == nil {
		t.Fatal("a password two changes back was accepted with history = 3")
	}
}

func TestOldEnoughPasswordsAreForgotten(t *testing.T) {
	// A history that never forgets is a different policy, and eventually an
	// account with nothing left to choose.
	manager, _ := managerWithHistory(t, 2)
	ctx := context.Background()

	for _, change := range [][2]string{
		{"first-password", "second-password"},
		{"second-password", "third-password"},
		{"third-password", "fourth-password"},
	} {
		if err := manager.ChangePassword(ctx, "user-1", change[0], change[1]); err != nil {
			t.Fatalf("%s -> %s: %v", change[0], change[1], err)
		}
	}

	// Depth 2 looks at the one in use and the one before it, so the first is
	// out of range by now.
	if err := manager.ChangePassword(ctx, "user-1", "fourth-password", "first-password"); err != nil {
		t.Errorf("a password well outside the configured depth was refused: %v", err)
	}
}

func TestNoHistoryConfiguredKeepsNoHistory(t *testing.T) {
	// Every deployment that has not asked for this. Nothing may be recorded
	// about their passwords beyond the one in use.
	manager, _ := managerWithHistory(t, 0)
	ctx := context.Background()

	if err := manager.ChangePassword(ctx, "user-1", "first-password", "second-password"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if err := manager.ChangePassword(ctx, "user-1", "second-password", "first-password"); err != nil {
		t.Errorf("a service with no history policy refused a reuse: %v", err)
	}

	held, err := manager.passwordHistory.GetRecentHashes(ctx, "user-1", 10)
	if err != nil {
		t.Fatalf("GetRecentHashes: %v", err)
	}
	if len(held) != 0 {
		t.Errorf("hashes were kept for a service that asked for no history: %d", len(held))
	}
}

func TestTheRecordIsTrimmedToWhatThePolicyLooksAt(t *testing.T) {
	// Otherwise the row count per account grows for the life of the account,
	// and the hashes of every password a person has ever used sit there.
	manager, _ := managerWithHistory(t, 2)
	ctx := context.Background()

	previous := "first-password"
	for _, next := range []string{"second-password", "third-password", "fourth-password", "fifth-password"} {
		if err := manager.ChangePassword(ctx, "user-1", previous, next); err != nil {
			t.Fatalf("%s: %v", next, err)
		}
		previous = next
	}

	held, err := manager.passwordHistory.GetRecentHashes(ctx, "user-1", 100)
	if err != nil {
		t.Fatalf("GetRecentHashes: %v", err)
	}
	if len(held) > 2 {
		t.Errorf("the history holds %d hashes for a policy that looks at 2", len(held))
	}
}

func TestHashesAreComparedWithTheHasher(t *testing.T) {
	// The reason this cannot be a string comparison: every hash carries its
	// own salt, so the same password hashed twice is not the same text, and
	// comparing the text would let every reuse through while looking right.
	manager, _ := managerWithHistory(t, 2)

	first, err := manager.passwordHasher.Hash("the-same-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	second, err := manager.passwordHasher.Hash("the-same-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of one password are identical, so this hasher is not salting")
	}

	user := &User{ID: "user-2", PasswordHash: first}
	if err := manager.refusePasswordReuse(context.Background(), user, "the-same-password"); err == nil {
		t.Error("a reuse got past a hash that was stored under a different salt")
	}
}

func TestAHistoryThatCannotBeReadDoesNotLockAnybodyOut(t *testing.T) {
	// A store that is down must not become either a way past the policy for
	// somebody who knows about it, or a reason nobody can change a password.
	// It cannot be both, and being unable to change one is the worse of the
	// two: a person acting on a leak needs the change to go through.
	manager, user := managerWithHistory(t, 3)
	manager.passwordHistory = brokenHistory{}

	if err := manager.refusePasswordReuse(context.Background(), user, "something-new"); err != nil {
		t.Errorf("a broken history stopped a password change: %v", err)
	}
	// What it still catches without reading anything: the one in use.
	if err := manager.refusePasswordReuse(context.Background(), user, "first-password"); err == nil {
		t.Error("the password in use was accepted while the history was down")
	}
}

type brokenHistory struct{}

func (brokenHistory) AddPasswordHash(ctx context.Context, userID, hash string) error {
	return context.DeadlineExceeded
}

func (brokenHistory) GetRecentHashes(ctx context.Context, userID string, count int) ([]string, error) {
	return nil, context.DeadlineExceeded
}

func (brokenHistory) CleanOldHashes(ctx context.Context, userID string, keepCount int) error {
	return context.DeadlineExceeded
}
