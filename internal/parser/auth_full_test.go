package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// An auth block with everything in it, read back attribute by attribute.
//
// The failure this guards against has happened twice in this repository: the
// parser accepts an attribute, nothing assigns it, and a service runs with a
// setting somebody wrote and nothing honours. Covering the branches is the
// point — each `if attr, exists` here is a setting that either arrives or is
// silently dropped, and there is no way to tell from the outside.

const everythingAuth = `
service {
  name    = "orders"
  version = "1.0.0"
}

auth {
  preset   = "strict"
  base_url = "https://app.example.com"

  jwt {
    secret           = "a-secret-long-enough-for-the-tests"
    algorithm        = "HS256"
    access_lifetime  = "15m"
    refresh_lifetime = "7d"
    issuer           = "orders"
    audience         = ["orders-api"]
  }

  password {
    min_length      = 12
    require_upper   = true
    require_lower   = true
    require_number  = true
    require_special = true
    algorithm       = "argon2id"
  }

  sessions {
    max_active        = 3
    idle_timeout      = "15m"
    absolute_timeout  = "8h"
    on_max_reached    = "revoke_oldest"
  }

  mfa {
    enabled          = true
    required         = "always"
    methods          = ["totp", "webauthn"]
    require_multiple = true
    min_factors      = 2
    grace_period     = "24h"

    totp {
      issuer    = "Orders"
      digits    = 6
      period    = 30
      skew      = 1
      algorithm = "SHA1"
    }

    webauthn {
      rp_id             = "app.example.com"
      rp_name           = "Orders"
      origins           = ["https://app.example.com"]
      user_verification = "preferred"
      timeout           = 60000
    }

    sms {
      provider    = "twilio"
      code_length = 6
      expiry      = "5m"
      rate_limit  = "3/hour"

      twilio {
        account_sid = "AC-account"
        auth_token  = "the-token"
        from_number = "+6499999999"
      }
    }

    push {
      provider = "firebase"
      expiry   = "2m"

      firebase {
        credentials = "./firebase.json"
      }
    }

    recovery {
      code_count  = 10
      code_length = 8
    }
  }

  security {
    brute_force {
      enabled      = true
      max_attempts = 5
      window       = "15m"
      lockout_time = "30m"
      track_by     = "ip+user"
    }

    rate_limit {
      enabled = true

      login {
        rate   = 5
        window = "1m"
      }
    }
  }

  storage {
    driver    = "database"
    connector = "store"
  }

  users {
    connector = "store"
    table     = "auth_users"
  }

  audit {
    enabled   = true
    connector = "store"
    table     = "auth_audit"
    events    = ["login", "failed_login"]
  }

  social {
    google {
      client_id     = "google-client"
      client_secret = "google-secret"
      scopes        = ["openid", "email"]
    }

    github {
      client_id     = "github-client"
      client_secret = "github-secret"
    }

    apple {
      client_id   = "apple-client"
      team_id     = "team"
      key_id      = "key"
      private_key = "-----BEGIN PRIVATE KEY-----"
    }
  }

  sso {
    oidc "corp" {
      issuer        = "https://id.example.com"
      client_id     = "corp-client"
      client_secret = "corp-secret"
      scopes        = ["openid", "profile"]
    }
  }

  endpoints {
    prefix = "/identity"

    login {
      path    = "/sign-in"
      enabled = true
    }

    register {
      enabled = false
    }
  }
}

connector "store" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
`

func parseAuth(t *testing.T, body string) *Configuration {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auth.mycel"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	config, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if config.Auth == nil {
		t.Fatal("the auth block was not read at all")
	}
	return config
}

func TestEverySettingInAnAuthBlockArrives(t *testing.T) {
	a := parseAuth(t, everythingAuth).Auth

	if a.Preset != "strict" || a.BaseURL != "https://app.example.com" {
		t.Errorf("preset = %q, base url = %q", a.Preset, a.BaseURL)
	}

	// Tokens: the lifetimes decide how long a stolen one is worth having.
	if a.JWT == nil {
		t.Fatal("no jwt block")
	}
	if a.JWT.AccessLifetime != "15m" || a.JWT.RefreshLifetime != "7d" {
		t.Errorf("lifetimes = %q / %q", a.JWT.AccessLifetime, a.JWT.RefreshLifetime)
	}
	if a.JWT.Issuer != "orders" || len(a.JWT.Audience) != 1 {
		t.Errorf("issuer = %q, audience = %v", a.JWT.Issuer, a.JWT.Audience)
	}

	// Password rules: each one not arriving is a rule nobody enforces.
	if a.Password == nil {
		t.Fatal("no password block")
	}
	if a.Password.MinLength != 12 {
		t.Errorf("min length = %d", a.Password.MinLength)
	}
	for name, on := range map[string]bool{
		"upper": a.Password.RequireUpper, "lower": a.Password.RequireLower,
		"number": a.Password.RequireNumber, "special": a.Password.RequireSpecial,
	} {
		if !on {
			t.Errorf("require %s did not arrive", name)
		}
	}

	// Sessions.
	if a.Sessions == nil || a.Sessions.MaxActive != 3 {
		t.Fatalf("sessions = %+v", a.Sessions)
	}
	if a.Sessions.IdleTimeout != "15m" || a.Sessions.OnMaxReached != "revoke_oldest" {
		t.Errorf("sessions = %+v", a.Sessions)
	}
}

func TestEverySettingInTheMFABlockArrives(t *testing.T) {
	// The block where a dropped setting is worst: a second factor that is
	// configured and not enforced looks exactly like one that is.
	mfa := parseAuth(t, everythingAuth).Auth.MFA
	if mfa == nil {
		t.Fatal("no mfa block")
	}

	if !mfa.Enabled || mfa.Required != "always" {
		t.Errorf("enabled = %v, required = %q", mfa.Enabled, mfa.Required)
	}
	if len(mfa.Methods) != 2 {
		t.Errorf("methods = %v, want both", mfa.Methods)
	}
	if !mfa.RequireMultiple || mfa.MinFactors != 2 {
		t.Errorf("require multiple = %v, min factors = %d", mfa.RequireMultiple, mfa.MinFactors)
	}
	if mfa.GracePeriod != "24h" {
		t.Errorf("grace period = %q", mfa.GracePeriod)
	}

	if mfa.TOTP == nil || mfa.TOTP.Issuer != "Orders" || mfa.TOTP.Digits != 6 {
		t.Errorf("totp = %+v", mfa.TOTP)
	}
	if mfa.TOTP.Period != 30 || mfa.TOTP.Skew != 1 {
		t.Errorf("totp = %+v", mfa.TOTP)
	}

	if mfa.WebAuthn == nil || mfa.WebAuthn.RPID != "app.example.com" {
		t.Fatalf("webauthn = %+v", mfa.WebAuthn)
	}
	// Without the origins the service starts and refuses every passkey.
	if len(mfa.WebAuthn.Origins) != 1 {
		t.Errorf("origins = %v", mfa.WebAuthn.Origins)
	}

	if mfa.SMS == nil || mfa.SMS.Provider != "twilio" || mfa.SMS.CodeLength != 6 {
		t.Fatalf("sms = %+v", mfa.SMS)
	}
	if mfa.SMS.Expiry != "5m" || mfa.SMS.RateLimit != "3/hour" {
		t.Errorf("sms = %+v", mfa.SMS)
	}
	if mfa.SMS.Twilio == nil || mfa.SMS.Twilio.FromNumber != "+6499999999" {
		t.Errorf("twilio = %+v", mfa.SMS.Twilio)
	}

	if mfa.Push == nil || mfa.Push.Provider != "firebase" || mfa.Push.Expiry != "2m" {
		t.Fatalf("push = %+v", mfa.Push)
	}
	if mfa.Push.Firebase == nil || mfa.Push.Firebase.Credentials != "./firebase.json" {
		t.Errorf("firebase = %+v", mfa.Push.Firebase)
	}

	if mfa.Recovery == nil || mfa.Recovery.CodeCount != 10 {
		t.Errorf("recovery = %+v", mfa.Recovery)
	}
}

func TestEverySettingInTheSecurityBlockArrives(t *testing.T) {
	security := parseAuth(t, everythingAuth).Auth.Security
	if security == nil {
		t.Fatal("no security block")
	}

	if security.BruteForce == nil {
		t.Fatal("no brute force block")
	}
	if !security.BruteForce.Enabled || security.BruteForce.MaxAttempts != 5 {
		t.Errorf("brute force = %+v", security.BruteForce)
	}
	// Which of these is used decides whether an attacker can lock somebody
	// else out.
	if security.BruteForce.TrackBy != "ip+user" {
		t.Errorf("track by = %q", security.BruteForce.TrackBy)
	}
	if security.BruteForce.Window != "15m" || security.BruteForce.LockoutTime != "30m" {
		t.Errorf("brute force = %+v", security.BruteForce)
	}

	if security.RateLimit == nil || !security.RateLimit.Enabled {
		t.Fatalf("rate limit = %+v", security.RateLimit)
	}
	if security.RateLimit.Login == nil || security.RateLimit.Login.Rate != 5 {
		t.Errorf("login limit = %+v", security.RateLimit.Login)
	}
}

func TestEveryIdentityProviderArrives(t *testing.T) {
	a := parseAuth(t, everythingAuth).Auth

	if a.Social == nil {
		t.Fatal("no social block")
	}
	if a.Social.Google == nil || a.Social.Google.ClientID != "google-client" {
		t.Errorf("google = %+v", a.Social.Google)
	}
	if len(a.Social.Google.Scopes) != 2 {
		t.Errorf("scopes = %v, want both", a.Social.Google.Scopes)
	}
	if a.Social.GitHub == nil || a.Social.GitHub.ClientSecret != "github-secret" {
		t.Errorf("github = %+v", a.Social.GitHub)
	}
	// Apple is its own shape: a key rather than a secret.
	if a.Social.Apple == nil || a.Social.Apple.TeamID != "team" || a.Social.Apple.KeyID != "key" {
		t.Errorf("apple = %+v", a.Social.Apple)
	}

	if a.SSO == nil || len(a.SSO.OIDC) != 1 {
		t.Fatalf("sso = %+v", a.SSO)
	}
	provider := a.SSO.OIDC[0]
	if provider.Name != "corp" || provider.Issuer != "https://id.example.com" {
		t.Errorf("oidc = %+v", provider)
	}
	if provider.ClientID != "corp-client" || len(provider.Scopes) != 2 {
		t.Errorf("oidc = %+v", provider)
	}
}

func TestWhereTheEndpointsAreMountedArrives(t *testing.T) {
	// The prefix and the per-endpoint paths are what a browser calls, so one
	// that does not arrive is a 404 on a route the configuration promises.
	endpoints := parseAuth(t, everythingAuth).Auth.Endpoints
	if endpoints == nil {
		t.Fatal("no endpoints block")
	}

	if endpoints.Prefix != "/identity" {
		t.Errorf("prefix = %q", endpoints.Prefix)
	}
	if endpoints.Login == nil || endpoints.Login.Path != "/sign-in" {
		t.Errorf("login = %+v", endpoints.Login)
	}
	// Turning one off is how somebody closes registration, and it has to
	// arrive or the endpoint stays open.
	if endpoints.Register == nil || endpoints.Register.Enabled {
		t.Errorf("register = %+v, want it turned off", endpoints.Register)
	}
}

func TestWhereAccountsAndTheirTrailAreKeptArrives(t *testing.T) {
	a := parseAuth(t, everythingAuth).Auth

	if a.Storage == nil || a.Storage.Driver != "database" || a.Storage.Connector != "store" {
		t.Errorf("storage = %+v", a.Storage)
	}
	if a.Users == nil || a.Users.Table != "auth_users" {
		t.Errorf("users = %+v", a.Users)
	}
	if a.Audit == nil || !a.Audit.Enabled || a.Audit.Table != "auth_audit" {
		t.Errorf("audit = %+v", a.Audit)
	}
	if len(a.Audit.Events) != 2 {
		t.Errorf("audit events = %v", a.Audit.Events)
	}
}

func TestTheWebAuthnSettingsTheTypeCarriesCanAllBeWritten(t *testing.T) {
	// Three of these — rp_display_name, rp_origins and timeout — had an HCL
	// tag on the type, were used by the service, and were not in the parser's
	// list, so writing any of them failed the document outright with
	// "Unsupported argument". This is the shape of that bug, which this
	// repository has now met in the schema, the runtime and here.
	config := parseAuth(t, `
service {
  name    = "orders"
  version = "1.0.0"
}

auth {
  preset = "development"

  jwt {
    secret = "a-secret-long-enough-for-the-tests"
  }

  mfa {
    webauthn {
      rp_name                  = "Orders"
      rp_display_name          = "Orders, the shop"
      rp_id                    = "app.example.com"
      origins                  = ["https://app.example.com"]
      rp_origins               = ["https://app.example.com", "https://admin.example.com"]
      authenticator_attachment = "cross-platform"
      user_verification        = "required"
      resident_key             = "preferred"
      max_credentials          = 5
      attestation              = "direct"
      allowed_aaguids          = ["aaguid-1"]
      timeout                  = 90000
    }
  }
}
`)

	w := config.Auth.MFA.WebAuthn
	if w == nil {
		t.Fatal("no webauthn block")
	}

	if w.RPDisplayName != "Orders, the shop" {
		t.Errorf("display name = %q — this is what the browser prompt says", w.RPDisplayName)
	}
	if len(w.RPOrigins) != 2 {
		t.Errorf("rp origins = %v, want both: the service prefers these over origins", w.RPOrigins)
	}
	if w.Timeout != 90000 {
		t.Errorf("timeout = %d, want the one configured", w.Timeout)
	}

	// And the rest, so the whole block is pinned rather than the three that
	// were missing.
	if w.RPName != "Orders" || w.RPID != "app.example.com" {
		t.Errorf("webauthn = %+v", w)
	}
	if w.AuthenticatorAttachment != "cross-platform" || w.UserVerification != "required" {
		t.Errorf("webauthn = %+v", w)
	}
	if w.ResidentKey != "preferred" || w.MaxCredentials != 5 || w.Attestation != "direct" {
		t.Errorf("webauthn = %+v", w)
	}
	if len(w.AllowedAAGUIDs) != 1 {
		t.Errorf("allowed aaguids = %v", w.AllowedAAGUIDs)
	}
}

func TestEveryFieldTheAuthTypesCarryCanBeWritten(t *testing.T) {
	// The mechanical version of the test above, over every auth type at once:
	// a field with an hcl tag that the parser does not accept is a setting
	// nobody can write, and a document containing it fails outright.
	//
	// It walks the types rather than a list kept by hand, so a field added
	// tomorrow is covered by this the day it appears.
	for _, tc := range []struct {
		block  string
		fields map[string]string // hcl name -> a value to write
	}{
		{"jwt", map[string]string{
			"secret": `"a-secret-long-enough-for-the-tests"`, "algorithm": `"HS256"`,
			"access_lifetime": `"15m"`, "refresh_lifetime": `"7d"`,
			"issuer": `"orders"`, "audience": `["orders-api"]`,
		}},
		{"password", map[string]string{
			"min_length": "12", "require_upper": "true", "require_lower": "true",
			"require_number": "true", "require_special": "true", "algorithm": `"argon2id"`,
		}},
		{"sessions", map[string]string{
			"max_active": "3", "idle_timeout": `"15m"`, "absolute_timeout": `"8h"`,
			"on_max_reached": `"revoke_oldest"`, "allow_list": "true", "allow_revoke": "true",
			"extend_on_activity": "true",
		}},
	} {
		t.Run(tc.block, func(t *testing.T) {
			var body string
			for name, value := range tc.fields {
				body += "    " + name + " = " + value + "\n"
			}

			parseAuth(t, `
service {
  name    = "orders"
  version = "1.0.0"
}

auth {
  preset = "development"

  jwt {
    secret = "a-secret-long-enough-for-the-tests"
  }

  `+tc.block+` {
`+body+`  }
}
`)
		})
	}
}

func TestTheMFABlockTheDocumentationShowsParses(t *testing.T) {
	// Copied from docs/guides/auth.md. It did not parse: the WebAuthn example
	// used rp_origins, which the parser did not accept, and the recovery block
	// was written under a name — with attribute names — that exist nowhere in
	// the implementation. Anybody following the guide met "Unsupported
	// argument" on their first try.
	parseAuth(t, `
service {
  name    = "orders"
  version = "1.0.0"
}

auth {
  preset = "development"

  jwt {
    secret = "a-secret-long-enough-for-the-tests"
  }

  mfa {
    required = "optional"
    methods  = ["totp", "webauthn"]

    totp {
      issuer = "My App"
      digits = 6
      period = 30
    }

    webauthn {
      rp_id             = "myapp.com"
      rp_name           = "My Application"
      rp_display_name   = "My Application"
      rp_origins        = ["https://myapp.com"]
      attestation       = "none"
      user_verification = "preferred"
      timeout           = 60000
    }

    recovery {
      enabled     = true
      code_count  = 10
      code_length = 8
    }
  }
}
`)
}
