package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// What the sessions block is allowed to decide.
//
// Three settings in it were read by nothing while their neighbours worked,
// which is what made it invisible: max_active, idle_timeout and
// on_max_reached are honoured, so the block looked alive. Listing and
// revoking were governed elsewhere, and what is recorded about the person on
// the other end was not governed at all.

// mountedPaths records what a handler registered.
type mountedPaths struct{ paths []string }

func (m *mountedPaths) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	m.paths = append(m.paths, pattern)
}

func (m *mountedPaths) has(substring string) bool {
	for _, p := range m.paths {
		if strings.Contains(p, substring) {
			return true
		}
	}
	return false
}

func handlerWithSessions(t *testing.T, sessions *SessionsConfig) *mountedPaths {
	t.Helper()

	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Sessions: sessions,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	mux := &mountedPaths{}
	NewHandler(manager).RegisterRoutes(mux)
	return mux
}

func TestListingAndRevokingCanBeTurnedOff(t *testing.T) {
	// The failure: writing them off did nothing, so a deployment that decided
	// nobody may list or revoke sessions still served both endpoints and had
	// no way to know.
	off := handlerWithSessions(t, &SessionsConfig{AllowList: false, AllowRevoke: false})

	if off.has("/sessions") {
		t.Errorf("sessions endpoints are served with both settings off: %v", off.paths)
	}

	on := handlerWithSessions(t, &SessionsConfig{AllowList: true, AllowRevoke: true})
	if !on.has("/sessions") {
		t.Errorf("sessions endpoints are missing with both settings on: %v", on.paths)
	}
}

func TestOneCanBeOffWithoutTheOther(t *testing.T) {
	// Listing your own sessions and being able to end one are different
	// permissions: a service may well offer the first and not the second.
	listOnly := handlerWithSessions(t, &SessionsConfig{AllowList: true, AllowRevoke: false})

	var listed, revoked bool
	for _, p := range listOnly.paths {
		switch {
		case strings.HasSuffix(p, "/sessions"):
			listed = true
		case strings.HasSuffix(p, "/sessions/"):
			revoked = true
		}
	}
	if !listed {
		t.Errorf("listing was allowed and is not served: %v", listOnly.paths)
	}
	if revoked {
		t.Errorf("revoking was refused and is served anyway: %v", listOnly.paths)
	}
}

func TestAServiceWithNoSessionsBlockKeepsWhatItHad(t *testing.T) {
	// Most deployments. Nothing configured must not take away endpoints that
	// have always been there.
	none := handlerWithSessions(t, nil)
	if !none.has("/sessions") {
		t.Errorf("a service with no sessions block lost its endpoints: %v", none.paths)
	}
}

func TestWhatIsKeptAboutSomebodySigningIn(t *testing.T) {
	// An address is personal data in most of the places this runs, and the
	// list naming what to record was read by nothing: a deployment that
	// listed only the browser string was storing addresses anyway.
	ctx := context.Background()
	user := &User{ID: "user-1", Email: "someone@example.test"}

	for name, tc := range map[string]struct {
		track    []string
		wantIP   bool
		wantUser bool
	}{
		"nothing said records everything": {nil, true, true},
		"only the address":                {[]string{"ip"}, true, false},
		"only the browser":                {[]string{"user_agent"}, false, true},
		"both, named":                     {[]string{"ip", "user_agent"}, true, true},
		"neither":                         {[]string{"location"}, false, false},
		"written with odd spacing":        {[]string{" IP "}, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			manager, err := NewManager(&Config{
				Preset:   "development",
				JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
				Sessions: &SessionsConfig{Track: tc.track},
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}

			session, err := manager.createSession(ctx, user, "203.0.113.10", "Mozilla/5.0")
			if err != nil {
				t.Fatalf("createSession: %v", err)
			}

			if (session.IP != "") != tc.wantIP {
				t.Errorf("address recorded = %q, want recorded: %v", session.IP, tc.wantIP)
			}
			if (session.UserAgent != "") != tc.wantUser {
				t.Errorf("browser recorded = %q, want recorded: %v", session.UserAgent, tc.wantUser)
			}
			// Whatever is recorded, the session itself still works: this is
			// not a way to break sign-in.
			if session.ID == "" || session.UserID != "user-1" {
				t.Errorf("session = %+v", session)
			}
		})
	}
}

func TestWhatHappensAtTheSessionLimitHasToBeUnderstood(t *testing.T) {
	// A word nobody implemented meant the opposite of what it says: `deny`,
	// which the guide published, fell through to revoking the oldest session,
	// so a service that meant to turn the newest sign-in away silently signed
	// somebody out instead.
	for name, tc := range map[string]struct {
		configured string
		want       string
		refused    bool
	}{
		// Not written means whatever the preset says, which is a real value:
		// the check has to run after the preset is merged, not before.
		"nothing said":              {"", "revoke_oldest", false},
		"revoke the oldest":         {"revoke_oldest", "revoke_oldest", false},
		"refuse the newest":         {"reject_new", "reject_new", false},
		"refuse, spelled deny":      {"deny", "reject_new", false},
		"a word nobody implemented": {"revoke_all", "", true},
		"a typo":                    {"reject-new", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			sessions := &SessionsConfig{MaxActive: 2, OnMaxReached: tc.configured}
			manager, err := NewManager(&Config{
				Preset:   "development",
				JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
				Sessions: sessions,
			})

			if tc.refused {
				if err == nil {
					t.Fatalf("%q was accepted, and means revoke_oldest without saying so", tc.configured)
				}
				// The message has to name what is accepted, or the operator is
				// left guessing at a word.
				if !strings.Contains(err.Error(), "reject_new") {
					t.Errorf("error does not say what is accepted: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			if got := manager.Config().Sessions.OnMaxReached; got != tc.want {
				t.Errorf("on_max_reached = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestASessionThatIsNotExtendedByUseEndsOnSchedule(t *testing.T) {
	// The difference between a sliding session and a fixed one, which is what
	// extend_on_activity is for and what it decided nothing about: every
	// validated request refreshes the last-active time the idle sweep reads,
	// so a session in constant use never reached its idle timeout however the
	// deployment was configured.
	ctx := context.Background()
	user := &User{ID: "user-1", Email: "someone@example.test"}

	for name, tc := range map[string]struct {
		sessions  *SessionsConfig
		endsAfter time.Duration
	}{
		"extended by use, so the idle window is not the end": {
			&SessionsConfig{ExtendOnActivity: true, IdleTimeout: "30m", AbsoluteTimeout: "24h"},
			24 * time.Hour,
		},
		"fixed, so it ends an idle window after it began": {
			&SessionsConfig{ExtendOnActivity: false, IdleTimeout: "30m", AbsoluteTimeout: "24h"},
			30 * time.Minute,
		},
		"fixed with no idle window is just the absolute one": {
			&SessionsConfig{ExtendOnActivity: false, AbsoluteTimeout: "24h"},
			24 * time.Hour,
		},
		"a fixed window longer than the absolute one does not extend it": {
			&SessionsConfig{ExtendOnActivity: false, IdleTimeout: "48h", AbsoluteTimeout: "24h"},
			24 * time.Hour,
		},
	} {
		t.Run(name, func(t *testing.T) {
			manager, err := NewManager(&Config{
				Preset:   "development",
				JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
				Sessions: tc.sessions,
			})
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}

			before := time.Now()
			session, err := manager.createSession(ctx, user, "203.0.113.10", "Mozilla/5.0")
			if err != nil {
				t.Fatalf("createSession: %v", err)
			}

			lasts := session.ExpiresAt.Sub(before)
			if lasts < tc.endsAfter-time.Minute || lasts > tc.endsAfter+time.Minute {
				t.Errorf("the session lasts %v, want about %v", lasts.Round(time.Second), tc.endsAfter)
			}
			// However it ends, when it was last used stays truthful: a listing
			// showing creation time forever would be worse than the bug.
			if session.LastActiveAt.IsZero() {
				t.Error("the session does not record when it was last used")
			}
		})
	}
}

func TestUsingAFixedSessionDoesNotBuyMoreTime(t *testing.T) {
	// The behaviour the expiry stands in for: validating a token refreshes the
	// last-active time, and with a fixed window that must not move the end.
	ctx := context.Background()
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Sessions: &SessionsConfig{ExtendOnActivity: false, IdleTimeout: "30m", AbsoluteTimeout: "24h"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	user := &User{ID: "user-1", Email: "someone@example.test"}
	if err := manager.userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	tokens, err := manager.EstablishSession(ctx, user, "203.0.113.10", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("EstablishSession: %v", err)
	}

	claims, err := manager.tokenManager.ValidateAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatalf("the token this just issued does not validate: %v", err)
	}
	opened, err := manager.sessionStore.FindByID(ctx, claims.SessionID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	endsAt := opened.ExpiresAt

	if _, _, err := manager.ValidateToken(ctx, tokens.AccessToken); err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	used, err := manager.sessionStore.FindByID(ctx, claims.SessionID)
	if err != nil {
		t.Fatalf("FindByID after use: %v", err)
	}
	if !used.ExpiresAt.Equal(endsAt) {
		t.Errorf("using the session moved its end from %v to %v", endsAt, used.ExpiresAt)
	}
}
