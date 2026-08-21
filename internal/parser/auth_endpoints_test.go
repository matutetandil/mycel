package parser

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/auth"
)

// Moving the prefix must not close the doors.
//
// The endpoints block was parsed into an otherwise empty configuration, and an
// endpoint left nil is one that is never routed. So the one thing the block is
// most often written for — putting the routes under a different prefix — took
// every auth endpoint off the service: login, register, refresh, me, and the
// password, session and MFA routes. The service started, said "auth system
// initialized", and answered 404 to all of them. The auth example wrote
// exactly this block.
func TestWritingTheEndpointsBlockKeepsTheEndpoints(t *testing.T) {
	config, err := parseString(t, `
connector "api" {
  type = "rest"
  port = 8080
}

auth {
  preset   = "development"
  base_url = "http://localhost:8080"

  endpoints {
    prefix = "/identity"
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if config.Auth == nil || config.Auth.Endpoints == nil {
		t.Fatal("the auth block did not parse")
	}
	endpoints := config.Auth.Endpoints

	if endpoints.Prefix != "/identity" {
		t.Errorf("prefix = %q, want the one written", endpoints.Prefix)
	}

	// The whole set, because the bug took the whole set with it.
	for _, e := range []struct {
		name     string
		endpoint *auth.EndpointConfig
	}{
		{"login", endpoints.Login},
		{"logout", endpoints.Logout},
		{"register", endpoints.Register},
		{"refresh", endpoints.Refresh},
		{"me", endpoints.Me},
		{"password_forgot", endpoints.PasswordForgot},
		{"password_reset", endpoints.PasswordReset},
		{"password_change", endpoints.PasswordChange},
		{"sessions_list", endpoints.SessionsList},
		{"sessions_revoke", endpoints.SessionsRevoke},
		{"mfa_setup", endpoints.MFASetup},
		{"mfa_verify", endpoints.MFAVerify},
		{"mfa_disable", endpoints.MFADisable},
		{"mfa_recovery", endpoints.MFARecovery},
		{"social_callback", endpoints.SocialCallback},
		{"sso_callback", endpoints.SSOCallback},
	} {
		if e.endpoint == nil {
			t.Errorf("%s was turned off by a block that only moved the prefix", e.name)
			continue
		}
		if !e.endpoint.Enabled {
			t.Errorf("%s parsed as disabled", e.name)
		}
	}
}

// And an endpoint named in the block is still the one that wins: the defaults
// are a starting point, not a floor.
func TestAnEndpointCanStillBeMovedAndTurnedOff(t *testing.T) {
	config, err := parseString(t, `
connector "api" {
  type = "rest"
  port = 8080
}

auth {
  preset   = "development"
  base_url = "http://localhost:8080"

  endpoints {
    login {
      path = "/sign-in"
    }
    register {
      enabled = false
    }
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	endpoints := config.Auth.Endpoints

	if endpoints.Login == nil || endpoints.Login.Path != "/sign-in" {
		t.Errorf("login = %#v, want the path written in the block", endpoints.Login)
	}
	if endpoints.Register == nil || endpoints.Register.Enabled {
		t.Error("register stayed enabled although the block turned it off")
	}
	// One untouched keeps its default rather than following register out.
	if endpoints.Me == nil || !endpoints.Me.Enabled {
		t.Error("me was disabled by a block that never mentioned it")
	}
}
