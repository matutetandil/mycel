package auth

import (
	"net/http"
	"strings"
	"testing"
)

// The endpoints block is a list of paths a service promises to serve, with
// defaults for the ones nobody writes. Every entry in it is something a browser
// or a client is expected to call.
//
// An entry with no route behind it is the worst shape a promise can take: the
// configuration names the path, the documentation describes it, and the request
// comes back 404 — so the feature reads as broken rather than absent, and the
// search starts at the client.

// recordingMux notes every path a handler is registered under.
type recordingMux struct{ paths []string }

func (m *recordingMux) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	m.paths = append(m.paths, pattern)
}

func (m *recordingMux) has(path string) bool {
	for _, registered := range m.paths {
		// Go's patterns may carry a method and a wildcard; the path is what
		// matters here.
		if strings.HasSuffix(registered, path) {
			return true
		}
	}
	return false
}

func mountedRoutes(t *testing.T, config *Config) *recordingMux {
	t.Helper()
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mux := &recordingMux{}
	NewHandler(manager).RegisterRoutes(mux)
	return mux
}

func TestEveryEndpointTheConfigurationDeclaresIsServed(t *testing.T) {
	// Written from the configuration rather than from the handlers, so an
	// endpoint that is declared and never mounted is caught here rather than
	// by somebody's 404.
	mux := mountedRoutes(t, &Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		MFA:    &MFAConfig{Enabled: true, Methods: []string{"totp"}},
	})

	for name, path := range map[string]string{
		"registering":         "/register",
		"signing in":          "/login",
		"signing out":         "/logout",
		"refreshing a token":  "/refresh",
		"the current account": "/me",
		"changing a password": "/change-password",
		"listing sessions":    "/sessions",
	} {
		t.Run(name, func(t *testing.T) {
			if !mux.has(path) {
				t.Errorf("%s is declared and has no route: %v", path, mux.paths)
			}
		})
	}
}

func TestTheSecondFactorCanBeSetUpOverHTTP(t *testing.T) {
	// A service with MFA on has a manager that can enrol and verify, and until
	// now nothing a browser could call to do it: the endpoints were declared
	// in the configuration with defaults and never mounted, so `mfa_setup`
	// named a path that answered 404.
	mux := mountedRoutes(t, &Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
		MFA:    &MFAConfig{Enabled: true, Methods: []string{"totp"}},
	})

	for name, path := range map[string]string{
		"enrolling":             "/mfa/setup",
		"confirming the code":   "/mfa/verify",
		"turning it off":        "/mfa/disable",
		"using a recovery code": "/mfa/recovery",
	} {
		t.Run(name, func(t *testing.T) {
			if !mux.has(path) {
				t.Errorf("%s has no route: %v", path, mux.paths)
			}
		})
	}
}

func TestWithNoSecondFactorThoseRoutesAreNotServed(t *testing.T) {
	// A service that does not use MFA should not answer on paths that cannot
	// do anything.
	mux := mountedRoutes(t, &Config{
		Preset: "development",
		JWT:    &JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	})
	for _, path := range []string{"/mfa/setup", "/mfa/verify"} {
		if mux.has(path) {
			t.Errorf("%s is served although MFA is off", path)
		}
	}
}
