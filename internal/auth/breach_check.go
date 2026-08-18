package auth

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Whether a password is one that has already been published.
//
// `breach_check` was read by nothing, so a service that asked for this accepted
// passwords sitting in every list anybody uses. It is the single most effective
// password rule there is — far more than demanding a capital letter, which
// mostly produces Password1 — and it was the one that did nothing.
//
// The check is k-anonymous: the password is hashed, the first five characters
// of that hash are sent, and the service answers with every suffix it has under
// that prefix. It learns five characters of a hash, which it cannot turn back
// into anything, and the comparison happens here.

// BreachChecker reports whether a password is known to have been published.
type BreachChecker interface {
	// TimesSeen returns how many times a password appears in the source. Zero
	// means it does not.
	TimesSeen(ctx context.Context, password string) (int, error)
}

// PwnedPasswords is the Have I Been Pwned range API.
type PwnedPasswords struct {
	baseURL string
	client  *http.Client
}

// NewPwnedPasswords creates a checker against the public service.
func NewPwnedPasswords() *PwnedPasswords {
	return NewPwnedPasswordsAt("https://api.pwnedpasswords.com/range/")
}

// NewPwnedPasswordsAt creates a checker against a service speaking the same
// protocol, which is what a deployment that mirrors the list internally has.
func NewPwnedPasswordsAt(baseURL string) *PwnedPasswords {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &PwnedPasswords{
		baseURL: baseURL,
		// Somebody is waiting on a registration form. Two seconds is long
		// enough for this to work and short enough that it failing is not felt.
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

func (p *PwnedPasswords) TimesSeen(ctx context.Context, password string) (int, error) {
	// SHA-1 because that is what the published list is keyed on, not because
	// anything here trusts it: the hash never leaves as more than five
	// characters, and what comes back is compared, not stored.
	sum := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := hash[:5], hash[5:]

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+prefix, nil)
	if err != nil {
		return 0, err
	}
	// Asking for padding means every answer is the same size, so somebody
	// watching the connection cannot tell a common prefix from a rare one.
	request.Header.Set("Add-Padding", "true")

	response, err := p.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("the breach list could not be reached: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("the breach list answered %d", response.StatusCode)
	}

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(line[:colon]), suffix) {
			continue
		}
		count, err := strconv.Atoi(strings.TrimSpace(line[colon+1:]))
		if err != nil {
			return 0, nil
		}
		return count, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("the breach list could not be read: %w", err)
	}
	return 0, nil
}

// WithBreachChecker sets where passwords are checked against published lists.
func WithBreachChecker(checker BreachChecker) ManagerOption {
	return func(m *Manager) {
		m.breaches = checker
	}
}

// breachCheckEnabled reports whether passwords are checked at all.
func (m *Manager) breachCheckEnabled() bool {
	return m.config.Password != nil && m.config.Password.BreachCheck && m.breaches != nil
}

// refuseBreachedPassword reports whether a password is one that has been
// published.
//
// A source that cannot be reached lets the password through. That is the
// uncomfortable choice and it is the right one: the alternative is a service
// where nobody can register or change a password because somebody else's
// website is down. It is logged, so that a check silently doing nothing for a
// week is visible.
func (m *Manager) refuseBreachedPassword(ctx context.Context, password string) error {
	if !m.breachCheckEnabled() {
		return nil
	}

	seen, err := m.breaches.TimesSeen(ctx, password)
	if err != nil {
		m.logger.Warn("a password could not be checked against the breach list", "error", err)
		return nil
	}
	if seen == 0 {
		return nil
	}

	m.logger.Info("a password was refused for appearing in a published list", "times_seen", seen)
	return &AuthError{
		Code: ErrBreachedPassword.Code,
		Message: fmt.Sprintf(
			"This password has appeared in a data breach %d times, so it is already being guessed. Choose another",
			seen),
	}
}
