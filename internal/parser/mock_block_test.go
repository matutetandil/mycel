package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Answering from recorded data instead of the real service. What makes it worth
// having over a test double is the second half — latency and a failure rate —
// which is how somebody finds out what their flow does when a dependency is
// slow or refusing, without arranging for one to be.

func parseMocks(t *testing.T, body string) *Configuration {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.mycel"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	config, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return config
}

func TestMocksAreOffUntilTheyAreTurnedOn(t *testing.T) {
	// A service that ships with a mocks block and forgets to turn it off would
	// answer production traffic from a directory of recorded files.
	config := parseMocks(t, `
service {
  name    = "orders"
  version = "1.0.0"
}

mocks {
  path = "./mocks"
}
`)

	if config.MockConfig == nil {
		t.Fatal("the block was not read at all")
	}
	if config.MockConfig.Enabled {
		t.Error("mocks are on for a service that did not turn them on")
	}
	if config.MockConfig.Path != "./mocks" {
		t.Errorf("path = %q", config.MockConfig.Path)
	}
}

func TestAConnectorCanBeMadeSlowOrUnreliable(t *testing.T) {
	// The reason to mock rather than point at a test double: this is how a
	// flow's retry and timeout behaviour is exercised at all.
	config := parseMocks(t, `
service {
  name    = "orders"
  version = "1.0.0"
}

mocks {
  enabled = true
  path    = "./mocks"

  connectors {
    db = {
      latency = "50ms"
    }

    payments = {
      latency   = "1s"
      fail_rate = 30
    }

    search = {
      enabled = false
    }
  }
}
`)

	if config.MockConfig == nil || !config.MockConfig.Enabled {
		t.Fatal("mocks are not on")
	}

	db := config.MockConfig.Connectors["db"]
	if db == nil || db.Latency != 50*time.Millisecond {
		t.Errorf("db = %+v, want the latency it was given", db)
	}

	payments := config.MockConfig.Connectors["payments"]
	if payments == nil {
		t.Fatal("the payments connector has no mock settings")
	}
	if payments.Latency != time.Second {
		t.Errorf("latency = %v", payments.Latency)
	}
	// Without this a flow's error handling is never exercised: every mocked
	// call succeeds.
	if payments.FailRate != 30 {
		t.Errorf("fail rate = %d, want the one configured", payments.FailRate)
	}

	// One connector can be left talking to the real thing while the rest are
	// mocked, which is how somebody isolates the part they are working on.
	search := config.MockConfig.Connectors["search"]
	if search == nil || search.Enabled == nil || *search.Enabled {
		t.Errorf("search = %+v, want it left unmocked", search)
	}
}

func TestSettingsThatAreNotSettingsAreIgnoredRatherThanFatal(t *testing.T) {
	// The per-connector block is keyed by connector name, so the parser cannot
	// tell a mistyped setting from a connector it has not seen. Refusing the
	// document would make a typo stop a service; ignoring it is the choice
	// that was made, and it should stay deliberate.
	config := parseMocks(t, `
service {
  name    = "orders"
  version = "1.0.0"
}

mocks {
  enabled = true

  connectors {
    db = {
      latency          = "not a duration"
      something_random = true
    }
  }
}
`)

	db := config.MockConfig.Connectors["db"]
	if db == nil {
		t.Fatal("the connector was dropped over a setting nothing reads")
	}
	if db.Latency != 0 {
		t.Errorf("latency = %v, want none: what was written is not a duration", db.Latency)
	}
}

func TestNoMocksBlockMeansNoMocks(t *testing.T) {
	config := parseMocks(t, `
service {
  name    = "orders"
  version = "1.0.0"
}
`)
	if config.MockConfig != nil && config.MockConfig.Enabled {
		t.Error("a service with no mocks block has mocks on")
	}
}
