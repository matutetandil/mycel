package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A connector made of profiles is one thing at runtime and another thing in
// another environment: the same name resolving to a queue on one broker or
// another, or to an API in production and a file on a laptop. It is what the
// documentation recommends for per-environment configuration, so what a profile
// can hold decides whether that recommendation is usable.

func parsed(t *testing.T, config string) (*Configuration, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.mycel"), []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return NewHCLParser().Parse(context.Background(), dir)
}

func mustParseProfiles(t *testing.T, config string) *Configuration {
	t.Helper()
	cfg, err := parsed(t, config)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	return cfg
}

func TestAProfileCanHoldWhatItsConnectorHolds(t *testing.T) {
	// The profile block used to carry its own list of forty attributes against
	// the connector's hundred and fifty-nine, so a profile could not name a
	// queue, a vhost, a path or a command — and the settings it could not name
	// are exactly the ones that differ between environments.
	for name, body := range map[string]string{
		"a queue on a broker": `
      type   = "mq"
      driver = "rabbitmq"
      url    = "amqp://localhost:5672"
      vhost  = "/orders"`,
		"a command to run": `
      type        = "exec"
      command     = "/usr/bin/report"
      working_dir = "/srv"`,
		"a directory of files": `
      type      = "file"
      base_path = "/srv/incoming"
      format    = "csv"`,
		"a topic on kafka": `
      type    = "mq"
      driver  = "kafka"
      brokers = ["localhost:9092"]
      topic   = "orders"`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := parsed(t, `
connector "source" {
  select  = "dev"
  default = "dev"

  profile "dev" {`+body+`
  }
}
`)
			if err != nil {
				t.Fatalf("a profile could not hold what its connector holds: %v", err)
			}
			if len(cfg.Connectors) != 1 {
				t.Fatalf("connectors = %d", len(cfg.Connectors))
			}
		})
	}
}

func TestAProfileCanHoldTheBlocksItsConnectorHolds(t *testing.T) {
	cfg, err := parsed(t, `
connector "source" {
  select  = "dev"
  default = "dev"

  profile "dev" {
    type   = "mq"
    driver = "rabbitmq"
    url    = "amqp://localhost:5672"

    consumer {
      queue     = "orders"
      prefetch  = 10
      auto_ack  = false
    }

    retry {
      attempts = 3
      delay    = "1s"
    }
  }
}
`)
	if err != nil {
		t.Fatalf("a profile could not hold the blocks its connector holds: %v", err)
	}

	profiles, ok := cfg.Connectors[0].Properties["_profiles"]
	if !ok {
		t.Fatalf("the connector has no profiles: %v", cfg.Connectors[0].Properties)
	}
	_ = profiles
}

func TestEachProfileDeclaresWhatItIs(t *testing.T) {
	// Profiles are heterogeneous — one example resolves to an HTTP API or a
	// SQLite database depending on the selection — so the type belongs to the
	// profile and a parent type does not stand in for it.
	cfg := mustParseProfiles(t, `
connector "backend" {
  select  = "env(\"BACKEND\")"
  default = "local"

  profile "local" {
    type     = "database"
    driver   = "sqlite"
    database = ":memory:"
  }

  profile "remote" {
    type     = "http"
    base_url = "https://api.example.com"
  }
}
`)
	if cfg.Connectors[0].Type != "profiled" {
		t.Errorf("the connector is %q, want it marked as made of profiles", cfg.Connectors[0].Type)
	}
}

func TestAProfileWithNoTypeIsRefused(t *testing.T) {
	// It would parse and then fail to build at startup, naming neither the
	// connector nor the profile.
	_, err := parsed(t, `
connector "backend" {
  default = "local"

  profile "local" {
    database = ":memory:"
  }
}
`)
	if err == nil {
		t.Fatal("a profile with nothing to build was accepted")
	}
	if !strings.Contains(err.Error(), "local") || !strings.Contains(err.Error(), "type") {
		t.Errorf("error = %q, want it to name the profile and what is missing", err)
	}
}

func TestAConnectorOfProfilesNeedsAWayToChooseOne(t *testing.T) {
	_, err := parsed(t, `
connector "backend" {
  profile "local" {
    type     = "database"
    driver   = "sqlite"
    database = ":memory:"
  }
}
`)
	if err == nil {
		t.Fatal("a connector was accepted with profiles and no way to pick one")
	}
	if !strings.Contains(err.Error(), "select") && !strings.Contains(err.Error(), "default") {
		t.Errorf("error = %q, want it to say what is missing", err)
	}
}

func TestAProfileCannotContainAnotherProfile(t *testing.T) {
	_, err := parsed(t, `
connector "backend" {
  default = "local"

  profile "local" {
    type = "http"
    base_url = "https://api.example.com"

    profile "nested" {
      type = "http"
      base_url = "https://other.example.com"
    }
  }
}
`)
	if err == nil {
		t.Error("a profile inside a profile was accepted")
	}
}

func TestAProfileCanReshapeWhatItSends(t *testing.T) {
	// The transform is what lets one profile stand in for another whose
	// payload is shaped differently, and it is the one thing a profile has
	// that a connector does not.
	cfg := mustParseProfiles(t, `
connector "backend" {
  default = "legacy"

  profile "legacy" {
    type     = "http"
    base_url = "https://legacy.example.com"

    transform {
      customer_id = "input.customerId"
      total       = "input.amount"
    }
  }
}
`)
	if len(cfg.Connectors) != 1 {
		t.Fatalf("connectors = %d", len(cfg.Connectors))
	}
}

func TestASettingNoConnectorAcceptsIsStillRefusedInsideAProfile(t *testing.T) {
	// Widening what a profile takes must not turn it into a block that
	// swallows anything: a typo has to be reported here, not ignored.
	_, err := parsed(t, `
connector "backend" {
  default = "local"

  profile "local" {
    type       = "http"
    base_url   = "https://api.example.com"
    base_urll  = "https://typo.example.com"
  }
}
`)
	if err == nil {
		t.Error("a setting nobody supports was accepted inside a profile")
	}
}
