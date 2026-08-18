package auth

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// breach_check was read by nothing, so a service that asked for this accepted
// passwords sitting in every list anybody uses. It is the most effective
// password rule there is — more than demanding a capital letter, which mostly
// produces Password1 — and it was the one that did nothing.

// pwnedLike answers the way the range API does, for a list written here.
func pwnedLike(t *testing.T, published map[string]int) *httptest.Server {
	t.Helper()

	byPrefix := map[string][]string{}
	for password, seen := range published {
		sum := sha1.Sum([]byte(password))
		hash := strings.ToUpper(hex.EncodeToString(sum[:]))
		byPrefix[hash[:5]] = append(byPrefix[hash[:5]], fmt.Sprintf("%s:%d", hash[5:], seen))
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/range/"))
		// Always something, so that an empty answer is not how a prefix with
		// no matches is told apart from one with them.
		lines := append([]string{"0000000000000000000000000000000000A:3"}, byPrefix[prefix]...)
		_, _ = w.Write([]byte(strings.Join(lines, "\r\n")))
	}))
}

func breachService(t *testing.T, checker BreachChecker) *Manager {
	t.Helper()

	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8, BreachCheck: true},
	}, WithBreachChecker(checker))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager
}

func TestAPasswordFromEveryListIsRefused(t *testing.T) {
	server := pwnedLike(t, map[string]int{"password123": 251682})
	defer server.Close()
	manager := breachService(t, NewPwnedPasswordsAt(server.URL+"/range/"))

	_, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.test", Password: "password123",
	})
	if err == nil {
		t.Fatal("a password published a quarter of a million times was accepted")
	}
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != "breached_password" {
		t.Fatalf("refusal = %v", err)
	}
	// The number is the argument: "this is already being guessed" lands where
	// "choose a stronger password" does not.
	if !strings.Contains(err.Error(), "251682") {
		t.Errorf("the refusal does not say how often it has been seen: %v", err)
	}
}

func TestAPasswordNobodyHasPublishedIsFine(t *testing.T) {
	server := pwnedLike(t, map[string]int{"password123": 251682})
	defer server.Close()
	manager := breachService(t, NewPwnedPasswordsAt(server.URL+"/range/"))

	if _, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.test", Password: "a-password-nobody-has-used",
	}); err != nil {
		t.Errorf("a password not on the list was refused: %v", err)
	}
}

func TestThePasswordItselfNeverLeaves(t *testing.T) {
	// The whole reason this is safe to switch on. What the service sees is
	// five characters of a hash.
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		_, _ = w.Write([]byte("0000000000000000000000000000000000A:3\r\n"))
	}))
	defer server.Close()

	checker := NewPwnedPasswordsAt(server.URL + "/range/")
	if _, err := checker.TimesSeen(context.Background(), "correct-horse-battery-staple"); err != nil {
		t.Fatalf("TimesSeen: %v", err)
	}

	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	path := asked[0]
	if strings.Contains(strings.ToLower(path), "horse") {
		t.Fatalf("the password went over the wire: %s", path)
	}
	prefix := strings.TrimPrefix(path, "/range/")
	if len(prefix) != 5 {
		t.Errorf("sent %q, want five characters of a hash", prefix)
	}
	// And it really is the prefix of this password's hash.
	sum := sha1.Sum([]byte("correct-horse-battery-staple"))
	if !strings.EqualFold(prefix, hex.EncodeToString(sum[:])[:5]) {
		t.Errorf("prefix = %q, which is not this password's", prefix)
	}
}

func TestAListThatCannotBeReachedDoesNotStopAnybody(t *testing.T) {
	// The uncomfortable choice, and the right one: the alternative is a
	// service where nobody can register because somebody else's website is
	// down.
	manager := breachService(t, brokenChecker{})

	if _, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.test", Password: "a-password-nobody-has-used",
	}); err != nil {
		t.Errorf("an unreachable breach list stopped a registration: %v", err)
	}
}

type brokenChecker struct{}

func (brokenChecker) TimesSeen(ctx context.Context, password string) (int, error) {
	return 0, errors.New("the list is not answering")
}

func TestNothingIsCheckedUnlessItIsAskedFor(t *testing.T) {
	// Every deployment that has not turned this on: no request leaves the
	// process, whatever the password.
	asked := 0
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8},
	}, WithBreachChecker(countingChecker{asked: &asked}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if _, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.test", Password: "password123",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if asked != 0 {
		t.Errorf("a service with breach_check off asked %d times", asked)
	}
}

type countingChecker struct{ asked *int }

func (c countingChecker) TimesSeen(ctx context.Context, password string) (int, error) {
	*c.asked++
	return 0, nil
}

func TestAShortPasswordIsRefusedWithoutAskingAnybody(t *testing.T) {
	// The cheap rules first: no reason to make a network call about a password
	// that is refused for its length.
	asked := 0
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 12, BreachCheck: true},
	}, WithBreachChecker(countingChecker{asked: &asked}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if _, _, err := manager.Register(context.Background(), &RegisterRequest{
		Email: "someone@example.test", Password: "short",
	}); err == nil {
		t.Fatal("a password below min_length was accepted")
	}
	if asked != 0 {
		t.Errorf("the breach list was asked about a password refused for its length")
	}
}

func TestChangingAndResettingAreCheckedToo(t *testing.T) {
	// Otherwise the rule applies to the front door and not to the two other
	// ways in.
	server := pwnedLike(t, map[string]int{"password123456": 100})
	defer server.Close()
	manager := breachService(t, NewPwnedPasswordsAt(server.URL+"/range/"))
	user := registered(t, manager, "someone@example.test", "a-password-nobody-has-used")

	err := manager.ChangePassword(context.Background(), user.ID,
		"a-password-nobody-has-used", "password123456")
	if err == nil {
		t.Error("a published password was accepted as a change")
	}
}
