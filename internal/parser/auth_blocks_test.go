package parser

import (
	"strings"

	"github.com/zclconf/go-cty/cty"
	"testing"
)

// The auth block is the largest in the language — three attributes and fourteen
// nested blocks — and most of its parsers had no coverage at all: webauthn,
// sms, push and brute_force were at zero, and social, sso, security and
// endpoints under a quarter. A block that parses without error but drops the
// value is the dangerous shape here, because the result is authentication
// running with defaults nobody chose: no lockout, verification not required,
// an endpoint still enabled after being switched off.

func TestParseFullAuthBlock(t *testing.T) {
	cfg := parseOne(t, `
auth {
  preset = "standard"

  storage {
    driver = "memory"
  }

  jwt {
    secret           = "a-secret-long-enough-for-hs256"
    access_lifetime  = "15m"
    refresh_lifetime = "7d"
  }

  password {
    min_length = 12
  }

  mfa {
    enabled  = true
    required = "optional"
    methods  = ["totp", "webauthn"]

    webauthn {
      rp_name                  = "Mycel"
      rp_id                    = "example.com"
      origins                  = ["https://example.com"]
      user_verification        = "required"
      authenticator_attachment = "platform"
      resident_key             = "preferred"
      max_credentials          = 5
      attestation              = "none"
    }

    sms {
      provider    = "twilio"
      code_length = 6
      expiry      = "5m"
    }

    push {
      provider = "firebase"
      expiry   = "2m"
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
  }

  social {
    google {
      client_id     = "google-client"
      client_secret = "google-secret"
      scopes        = ["email", "profile"]
    }
    github {
      client_id     = "github-client"
      client_secret = "github-secret"
    }
  }

  endpoints {
    prefix = "/api/auth"

    login {
      path    = "/signin"
      enabled = true
    }

    register {
      enabled = false
    }
  }

  audit {
    enabled   = true
    connector = "audit_db"
    table     = "auth_events"
    events    = ["login", "logout"]
  }
}
`)

	if cfg.Auth == nil {
		t.Fatal("the auth block did not survive parsing")
	}
	a := cfg.Auth

	t.Run("top level", func(t *testing.T) {
		if a.Preset != "standard" {
			t.Errorf("preset = %q, want standard", a.Preset)
		}
	})

	t.Run("mfa", func(t *testing.T) {
		if a.MFA == nil {
			t.Fatal("the mfa block was dropped")
		}
		if !a.MFA.Enabled {
			t.Error("mfa.enabled did not apply — MFA would never start")
		}
		if a.MFA.Required != "optional" {
			t.Errorf("mfa.required = %q", a.MFA.Required)
		}
		if len(a.MFA.Methods) != 2 {
			t.Errorf("mfa.methods = %#v, want two", a.MFA.Methods)
		}
	})

	t.Run("webauthn", func(t *testing.T) {
		w := a.MFA.WebAuthn
		if w == nil {
			t.Fatal("the webauthn block was dropped")
		}
		if w.RPName != "Mycel" || w.RPID != "example.com" {
			t.Errorf("rp_name/rp_id = %q / %q", w.RPName, w.RPID)
		}
		// The relying-party origins are a security control: a dropped list
		// means the browser has nothing to check the assertion against.
		if len(w.Origins) != 1 || w.Origins[0] != "https://example.com" {
			t.Errorf("origins = %#v", w.Origins)
		}
		if w.UserVerification != "required" {
			t.Errorf("user_verification = %q, want required", w.UserVerification)
		}
		if w.MaxCredentials != 5 {
			t.Errorf("max_credentials = %d, want 5", w.MaxCredentials)
		}
	})

	t.Run("sms and push", func(t *testing.T) {
		if a.MFA.SMS == nil {
			t.Fatal("the sms block was dropped")
		}
		if a.MFA.SMS.Provider != "twilio" || a.MFA.SMS.CodeLength != 6 {
			t.Errorf("sms = %+v", a.MFA.SMS)
		}
		if a.MFA.Push == nil {
			t.Fatal("the push block was dropped")
		}
		if a.MFA.Push.Provider != "firebase" {
			t.Errorf("push.provider = %q", a.MFA.Push.Provider)
		}
	})

	t.Run("brute force", func(t *testing.T) {
		if a.Security == nil || a.Security.BruteForce == nil {
			t.Fatal("the brute_force block was dropped, so there would be no lockout at all")
		}
		bf := a.Security.BruteForce
		if !bf.Enabled {
			t.Error("brute_force.enabled did not apply")
		}
		if bf.MaxAttempts != 5 {
			t.Errorf("max_attempts = %d, want 5", bf.MaxAttempts)
		}
		if bf.Window != "15m" || bf.LockoutTime != "30m" {
			t.Errorf("window/lockout = %q / %q", bf.Window, bf.LockoutTime)
		}
		if bf.TrackBy != "ip+user" {
			t.Errorf("track_by = %q", bf.TrackBy)
		}
	})

	t.Run("social providers", func(t *testing.T) {
		if a.Social == nil {
			t.Fatal("the social block was dropped")
		}
		if a.Social.Google == nil || a.Social.Google.ClientID != "google-client" {
			t.Errorf("google = %+v", a.Social.Google)
		}
		if len(a.Social.Google.Scopes) != 2 {
			t.Errorf("google.scopes = %#v, want two", a.Social.Google.Scopes)
		}
		if a.Social.GitHub == nil || a.Social.GitHub.ClientSecret != "github-secret" {
			t.Errorf("github = %+v", a.Social.GitHub)
		}
		if a.Social.Apple != nil {
			t.Error("an apple block appeared without being configured")
		}
	})

	t.Run("endpoints", func(t *testing.T) {
		e := a.Endpoints
		if e == nil {
			t.Fatal("the endpoints block was dropped")
		}
		if e.Prefix != "/api/auth" {
			t.Errorf("prefix = %q", e.Prefix)
		}
		if e.Login == nil || e.Login.Path != "/signin" {
			t.Errorf("login = %+v", e.Login)
		}
		// Switching an endpoint off is the assertion that matters: a dropped
		// `enabled = false` leaves registration open to the world.
		if e.Register == nil {
			t.Fatal("the register endpoint block was dropped")
		}
		if e.Register.Enabled {
			t.Error("register.enabled = false did not apply, so registration stays open")
		}
	})

	t.Run("storage and audit", func(t *testing.T) {
		if a.Storage == nil || a.Storage.Driver != "memory" {
			t.Errorf("storage = %+v", a.Storage)
		}
		if a.Audit == nil || !a.Audit.Enabled {
			t.Fatalf("audit = %+v", a.Audit)
		}
		if a.Audit.Connector != "audit_db" || a.Audit.Table != "auth_events" {
			t.Errorf("audit connector/table = %q / %q", a.Audit.Connector, a.Audit.Table)
		}
		if len(a.Audit.Events) != 2 {
			t.Errorf("audit.events = %#v, want two", a.Audit.Events)
		}
	})
}

func TestAuthSSOBlocks(t *testing.T) {
	cfg := parseOne(t, `
auth {
  storage {
    driver = "memory"
  }

  sso {
    oidc "okta" {
      issuer        = "https://example.okta.com"
      client_id     = "okta-client"
      client_secret = "okta-secret"
    }
  }
}
`)
	if cfg.Auth == nil || cfg.Auth.SSO == nil {
		t.Fatal("the sso block was dropped")
	}
	if len(cfg.Auth.SSO.OIDC) != 1 {
		t.Fatalf("got %d oidc providers, want 1", len(cfg.Auth.SSO.OIDC))
	}
	// The label is the provider name flows and callbacks refer to; losing it
	// makes the provider unaddressable.
	if got := cfg.Auth.SSO.OIDC[0].Name; got != "okta" {
		t.Errorf("oidc name = %q, want okta", got)
	}
}

func TestAuthProviderBlock(t *testing.T) {
	// The provider block validates a credential against an external HTTP
	// endpoint. It was a silent no-op until 2.7.0, so the mapping arriving is
	// worth pinning.
	cfg := parseOne(t, `
auth {
  storage {
    driver = "memory"
  }

  provider "upstream" {
    type     = "http"
    validate = "https://identity.example.com/introspect"

    request = {
      Authorization = "Bearer {token}"
    }

    response {
      success = "status == 200"
      user_id = "body.sub"
      email   = "body.email"
    }
  }
}
`)
	if cfg.Auth == nil {
		t.Fatal("the auth block was dropped")
	}
	if len(cfg.Auth.Providers) != 1 {
		t.Fatalf("got %d providers, want 1", len(cfg.Auth.Providers))
	}
	p := cfg.Auth.Providers[0]
	if p.Name != "upstream" {
		t.Errorf("provider name = %q, want upstream", p.Name)
	}
	if p.Type != "http" {
		t.Errorf("type = %q, want http", p.Type)
	}
	if p.Validate != "https://identity.example.com/introspect" {
		t.Errorf("validate = %q", p.Validate)
	}
	// The response mapping is what turns an upstream answer into a session;
	// dropping it would accept every response as a successful login.
	if p.Response == nil {
		t.Fatal("the response mapping block was dropped")
	}
	if p.Response.Success != "status == 200" {
		t.Errorf("response.success = %q", p.Response.Success)
	}
	if p.Response.UserID != "body.sub" {
		t.Errorf("response.user_id = %q", p.Response.UserID)
	}
}

func TestAuthBlockRejectsAnUnknownAttribute(t *testing.T) {
	// auth was declared Open in the schema until 2.16.0 while the parser has
	// always been closed. This pins the parser side.
	dir := t.TempDir()
	if err := writeConfigAndParse(t, dir, `
auth {
  preset                          = "standard"
  definitely_not_a_real_attribute = 1

  storage {
    driver = "memory"
  }
}
`); err == nil {
		t.Error("an unknown auth attribute was accepted")
	}
}

// A boolean where a string-backed attribute goes used to be "panic: not a
// string" out of cty, during validation — the whole binary, over one word in a
// configuration file. Thirty attributes in the auth block were read that way.

func TestABooleanWhereAStringIsExpectedDoesNotPanic(t *testing.T) {
	cfg, err := tryParse(t, `
auth {
  mfa {
    enabled  = true
    required = false
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Auth == nil || cfg.Auth.MFA == nil {
		t.Fatal("no MFA configuration came back")
	}
	// It is stored as text because it also accepts a role name, so a written
	// false has to arrive as the string the runtime compares against.
	if cfg.Auth.MFA.Required != "false" {
		t.Errorf("required = %q, want %q", cfg.Auth.MFA.Required, "false")
	}
}

func TestStringValueAcceptsWhatPeopleWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  cty.Value
		want string
	}{
		{"a string", cty.StringVal("admin"), "admin"},
		{"true", cty.True, "true"},
		{"false", cty.False, "false"},
		{"a whole number", cty.NumberIntVal(30), "30"},
		{"a fraction", cty.NumberFloatVal(1.5), "1.5"},
		{"null", cty.NullVal(cty.String), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stringValue("required", tc.val)
			if err != nil {
				t.Fatalf("stringValue: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStringValueNamesTheAttributeItCannotRead(t *testing.T) {
	_, err := stringValue("required", cty.ListVal([]cty.Value{cty.StringVal("a")}))
	if err == nil {
		t.Fatal("a list was accepted where a string belongs")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want it to name the attribute", err)
	}
}

// The linking block decides what happens when someone signs in through a
// provider with an address an account already uses. It appeared in the auth
// guide, the parser rejected it, and the service that takes it was always
// built with nil — so whatever anyone wrote, the defaults applied.
func TestTheLinkingBlockReachesTheConfig(t *testing.T) {
	cfg, err := tryParse(t, `
auth {
  sso {
    linking {
      enabled              = true
      match_by             = "email"
      require_verification = true
      on_match             = "prompt"
    }

    oidc "corp" {
      issuer        = "https://id.example.com"
      client_id     = "c"
      client_secret = "s"
    }
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Auth == nil || cfg.Auth.SSO == nil {
		t.Fatal("no sso configuration came back")
	}
	linking := cfg.Auth.SSO.Linking
	if linking == nil {
		t.Fatal("the linking block was dropped, so the defaults would apply instead")
	}
	if linking.MatchBy != "email" || linking.OnMatch != "prompt" || !linking.RequireVerification {
		t.Errorf("linking = %+v", linking)
	}
	// And the provider beside it still arrives.
	if len(cfg.Auth.SSO.OIDC) != 1 {
		t.Errorf("got %d OIDC providers", len(cfg.Auth.SSO.OIDC))
	}
}
