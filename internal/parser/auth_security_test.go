package parser

import "testing"

// The auth block is where this session found the most bugs, and every one of
// them had the same shape: a block that parses into a field nobody reads, or a
// name the documentation carried and the parser refused. These cover the
// reading itself — that what someone writes arrives, with the value they wrote.

func authConfig(t *testing.T, body string) *Configuration {
	t.Helper()
	cfg, err := tryParse(t, "auth {\n"+body+"\n}\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Auth == nil {
		t.Fatal("the auth block produced no configuration")
	}
	return cfg
}

func TestTheUsersBlockCarriesItsColumnNames(t *testing.T) {
	// The point of the block: an existing table, with the names it already has.
	cfg := authConfig(t, `
  users {
    connector = "db"
    table     = "accounts"

    fields {
      id            = "account_id"
      email         = "login"
      password_hash = "secret"
    }
  }`)

	users := cfg.Auth.Users
	if users == nil {
		t.Fatal("the users block was dropped")
	}
	if users.Connector != "db" || users.Table != "accounts" {
		t.Errorf("users = %+v", users)
	}
	if users.Fields == nil {
		t.Fatal("the fields block was dropped, so the default column names would be used against someone else's table")
	}
	if users.Fields.ID != "account_id" || users.Fields.Email != "login" || users.Fields.PasswordHash != "secret" {
		t.Errorf("fields = %+v", users.Fields)
	}
}

func TestTheSecurityBlockCarriesEachOfItsParts(t *testing.T) {
	// Six nested blocks, each read by something different. One dropped is a
	// protection that looks configured and is not — which is exactly what
	// happened to rate_limit.
	cfg := authConfig(t, `
  security {
    brute_force {
      enabled      = true
      max_attempts = 3
      window       = "15m"
      lockout_time = "1h"
      fail_delay   = "2s"
    }

    replay_protection {
      enabled = true
      window  = "5m"
    }

    ip_rules {
      allowlist = ["10.0.0.0/8"]
      blocklist = ["203.0.113.7"]
    }

    rate_limit {
      enabled = true
      key_by  = "ip"

      login {
        rate   = 5
        window = "1m"
      }
    }
  }`)

	security := cfg.Auth.Security
	if security == nil {
		t.Fatal("the security block was dropped")
	}

	if security.BruteForce == nil {
		t.Fatal("brute_force was dropped")
	}
	if security.BruteForce.MaxAttempts != 3 || security.BruteForce.FailDelay != "2s" {
		t.Errorf("brute_force = %+v", security.BruteForce)
	}
	if security.ReplayProtection == nil || !security.ReplayProtection.Enabled {
		t.Errorf("replay_protection = %+v", security.ReplayProtection)
	}
	if security.IPRules == nil || len(security.IPRules.Allowlist) != 1 {
		t.Errorf("ip_rules = %+v", security.IPRules)
	}
	if security.RateLimit == nil {
		t.Fatal("rate_limit was dropped")
	}
	if security.RateLimit.KeyBy != "ip" {
		t.Errorf("key_by = %q", security.RateLimit.KeyBy)
	}
	// The per-endpoint limit is the whole point of the block, and it is a
	// nested block inside a nested block — the shape most likely to be lost.
	if security.RateLimit.Login == nil || security.RateLimit.Login.Rate != 5 {
		t.Errorf("login limit = %+v", security.RateLimit.Login)
	}
}

func TestTheProgressiveDelayArrivesWithItsNumbers(t *testing.T) {
	cfg := authConfig(t, `
  security {
    brute_force {
      enabled = true

      progressive_delay {
        enabled    = true
        initial    = "1s"
        max        = "30s"
        multiplier = 2
      }
    }
  }`)

	pd := cfg.Auth.Security.BruteForce.ProgressiveDelay
	if pd == nil {
		t.Fatal("the progressive_delay block was dropped")
	}
	if !pd.Enabled || pd.Initial != "1s" || pd.Max != "30s" || pd.Multiplier != 2 {
		t.Errorf("progressive_delay = %+v", pd)
	}
}

func TestTheMFABlockAndItsMethods(t *testing.T) {
	cfg := authConfig(t, `
  mfa {
    enabled  = true
    required = "admin"
    methods  = ["totp", "webauthn"]

    totp {
      issuer = "Example"
      digits = 6
    }

    recovery {
      enabled     = true
      code_count  = 10
      code_length = 8
    }
  }`)

	mfa := cfg.Auth.MFA
	if mfa == nil {
		t.Fatal("the mfa block was dropped")
	}
	if !mfa.Enabled {
		t.Error("enabled was not read, which is the gate the manager checks")
	}
	// Required is text because it also takes a role name, which is why writing
	// a boolean there used to panic.
	if mfa.Required != "admin" {
		t.Errorf("required = %q", mfa.Required)
	}
	if len(mfa.Methods) != 2 || mfa.Methods[0] != "totp" {
		t.Errorf("methods = %v", mfa.Methods)
	}
	if mfa.TOTP == nil || mfa.TOTP.Issuer != "Example" {
		t.Errorf("totp = %+v", mfa.TOTP)
	}
	if mfa.Recovery == nil || mfa.Recovery.CodeCount != 10 {
		t.Errorf("recovery = %+v", mfa.Recovery)
	}
}

func TestWritingTheMFABlockTurnsItOn(t *testing.T) {
	// Presence is the opt-in: the block was unreadable before 2.7.0 and MFA
	// never started, so this is the behaviour that fixed it.
	cfg := authConfig(t, `
  mfa {
    methods = ["totp"]
  }`)
	if !cfg.Auth.MFA.Enabled {
		t.Error("an mfa block with no enabled attribute left MFA off")
	}

	off := authConfig(t, `
  mfa {
    enabled = false
    methods = ["totp"]
  }`)
	if off.Auth.MFA.Enabled {
		t.Error("enabled = false did not turn it off")
	}
}

func TestAnUnknownAttributeInTheAuthBlockIsRefused(t *testing.T) {
	// The alternative is a setting that looks applied and is not, which is the
	// failure this whole area kept producing.
	if _, err := tryParse(t, `
auth {
  security {
    brute_force {
      max_attemps = 3
    }
  }
}
`); err == nil {
		t.Error("a misspelt attribute was accepted")
	}
}
