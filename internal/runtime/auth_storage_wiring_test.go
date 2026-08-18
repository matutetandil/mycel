package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/auth"
)

// Which stores the auth manager ends up with.
//
// The failure mode this guards is quiet: a service configures storage, starts
// without complaint, and keeps its accounts in memory — so every account
// registered since the last restart disappears with the process. Every branch
// that could produce that has to fail loudly instead.

func runtimeWithConnectors(t *testing.T, extra string) *Runtime {
	t.Helper()
	return newCheckRuntime(t, `
service {
  name       = "auth-wiring"
  version    = "1.0.0"
  admin_port = 9398
}

connector "api" {
  type = "rest"
  port = 3398
}

connector "store" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
`+extra)
}

func TestNoStorageMeansNoStores(t *testing.T) {
	// The default: everything in memory, which is right for development and
	// says so in the banner.
	rt := runtimeWithConnectors(t, "")

	for name, cfg := range map[string]*auth.Config{
		"no auth at all":     nil,
		"auth with no block": {Preset: "development"},
		"memory by name":     {Preset: "development", Storage: &auth.StorageConfig{Driver: "memory"}},
		"nothing said":       {Preset: "development", Storage: &auth.StorageConfig{}},
	} {
		t.Run(name, func(t *testing.T) {
			opts, err := rt.buildAuthStores(cfg)
			if err != nil {
				t.Fatalf("buildAuthStores: %v", err)
			}
			if len(opts) != 0 {
				t.Errorf("%d stores were built for a service that asked for none", len(opts))
			}
		})
	}
}

func TestAStorageDriverNobodyImplementsIsRefused(t *testing.T) {
	// By name, at startup. Falling back to memory here is the quiet failure
	// this whole file is about.
	rt := runtimeWithConnectors(t, "")

	_, err := rt.buildAuthStores(&auth.Config{
		Preset:  "development",
		Storage: &auth.StorageConfig{Driver: "cassandra"},
	})
	if err == nil {
		t.Fatal("a storage driver nothing implements was accepted")
	}
	if !strings.Contains(err.Error(), "cassandra") {
		t.Errorf("error = %q, want it to name what was asked for", err)
	}
}

func TestDatabaseStorageNeedsAConnectorThatIsThere(t *testing.T) {
	rt := runtimeWithConnectors(t, "")
	if err := rt.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}

	for name, tc := range map[string]struct {
		connector string
		wantIn    string
	}{
		"none named":              {"", "no connector was named"},
		"one nobody declared":     {"a_connector_nobody_declared", "not found"},
		"one that is not a store": {"api", "database access"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := rt.buildAuthStores(&auth.Config{
				Preset:  "development",
				Storage: &auth.StorageConfig{Driver: "database", Connector: tc.connector},
			})
			if err == nil {
				t.Fatal("it was accepted, and the accounts would be held in memory")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error = %q, want it to say %q", err, tc.wantIn)
			}
		})
	}
}

func TestADatabaseWithNoStoresBehindItIsRefused(t *testing.T) {
	// The user stores are written for postgres and mysql. A service pointing
	// auth at sqlite has to hear that, rather than starting and keeping its
	// accounts in memory.
	rt := runtimeWithConnectors(t, "")
	if err := rt.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}

	_, err := rt.buildAuthStores(&auth.Config{
		Preset:  "development",
		Storage: &auth.StorageConfig{Driver: "database", Connector: "store"},
	})
	if err == nil {
		t.Fatal("a database with no user store behind it was accepted")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Errorf("error = %q, want it to say which databases do have one", err)
	}
}

func TestRedisStorageBuildsWhatItCanHold(t *testing.T) {
	// Redis holds sessions, tokens, the brute-force counters and outstanding
	// password reset tokens, but not accounts — which is why the warning about
	// them exists.
	rt := runtimeWithConnectors(t, "")

	opts, err := rt.buildAuthStores(&auth.Config{
		Preset:  "development",
		Storage: &auth.StorageConfig{Driver: "redis", Address: "localhost:6379"},
	})
	if err != nil {
		t.Fatalf("buildAuthStores: %v", err)
	}
	if len(opts) != 4 {
		t.Errorf("%d stores, want sessions, tokens, brute-force counters and reset tokens", len(opts))
	}
}

// --- The audit trail --------------------------------------------------------

func TestAnAuditBlockNamingNothingIsRefused(t *testing.T) {
	// Nothing read this block at all before 2.19.0, so a service configured to
	// record sign-ins wrote nothing — and that is discovered during an
	// investigation, which is the worst moment to find there is nothing to
	// investigate with.
	rt := runtimeWithConnectors(t, "")

	_, err := rt.buildAuditStore(&auth.Config{
		Preset: "development",
		Audit:  &auth.AuditConfig{Enabled: true},
	})
	if err == nil {
		t.Fatal("an audit block naming no connector was accepted")
	}
	if !strings.Contains(err.Error(), "connector") {
		t.Errorf("error = %q", err)
	}
}

func TestNoAuditBlockMeansNoAuditStore(t *testing.T) {
	rt := runtimeWithConnectors(t, "")

	for name, cfg := range map[string]*auth.Config{
		"no auth":  nil,
		"no audit": {Preset: "development"},
	} {
		t.Run(name, func(t *testing.T) {
			opt, err := rt.buildAuditStore(cfg)
			if err != nil || opt != nil {
				t.Errorf("opt = %v, err = %v, want nothing at all", opt, err)
			}
		})
	}
}

func TestAnAuditTrailOnADatabaseWithNoStoreIsRefused(t *testing.T) {
	rt := runtimeWithConnectors(t, "")
	if err := rt.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}

	_, err := rt.buildAuditStore(&auth.Config{
		Preset: "development",
		Audit:  &auth.AuditConfig{Enabled: true, Connector: "store", Table: "auth_audit"},
	})
	if err == nil {
		t.Fatal("an audit trail was configured against a database with no store for it")
	}
}
