package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newCheckRuntime writes cfg to a temp dir and builds a runtime from it.
func newCheckRuntime(t *testing.T, cfg string) *Runtime {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.mycel"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rt, err := New(Options{ConfigDir: dir, Environment: "development"})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Shutdown() })
	return rt
}

// The regression this guards: check used to create the runtime and report
// success without opening a single connection, so a database on an
// unroutable address passed.
func TestCheckConnectivity_ReachableConnectorPasses(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "check.db")
	rt := newCheckRuntime(t, `
service { name = "t" }

connector "local_sqlite" {
  type   = "database"
  driver = "sqlite"
  path   = "`+dbPath+`"
}
`)

	results := rt.CheckConnectivity(context.Background(), 5*time.Second)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].OK() {
		t.Errorf("expected sqlite to be reachable, got: %v", results[0].Err)
	}
	if results[0].Name != "local_sqlite" {
		t.Errorf("Name=%q, want local_sqlite", results[0].Name)
	}
	if results[0].Type != "database" || results[0].Driver != "sqlite" {
		t.Errorf("kind=%s/%s, want database/sqlite", results[0].Type, results[0].Driver)
	}
}

// A port nothing listens on is refused outright — distinct from a timeout,
// and the distinction is what tells you whether to look at the port or the
// firewall.
func TestCheckConnectivity_RefusedConnectionFails(t *testing.T) {
	rt := newCheckRuntime(t, `
service { name = "t" }

connector "refused" {
  type     = "database"
  driver   = "postgres"
  host     = "127.0.0.1"
  port     = 59999
  database = "nope"
  user     = "nobody"
  password = "wrong"
}
`)

	results := rt.CheckConnectivity(context.Background(), 5*time.Second)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].OK() {
		t.Fatal("expected an unreachable postgres to fail the check")
	}
	if results[0].TimedOut {
		t.Errorf("a refused connection should not be reported as a timeout: %v", results[0].Err)
	}
}

// Building the connector is part of the check, and its failure must be
// reported per connector rather than aborting the sweep — with the missing
// env var named, same as at startup.
func TestCheckConnectivity_FactoryFailureNamesMissingEnv(t *testing.T) {
	const varName = "MYCEL_TEST_UNSET_BASE_URL"
	os.Unsetenv(varName)

	rt := newCheckRuntime(t, `
service { name = "t" }

connector "api" {
  type     = "http"
  base_url = env("`+varName+`")
}
`)

	results := rt.CheckConnectivity(context.Background(), 5*time.Second)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].OK() {
		t.Fatal("expected a connector with an empty base_url to fail")
	}
	if !strings.Contains(results[0].Err.Error(), varName) {
		t.Errorf("error should name the missing variable, got: %v", results[0].Err)
	}
}

// One broken connector must not hide the others: the command reports the whole
// picture, so results come back for every connector, sorted by name.
func TestCheckConnectivity_ReportsEveryConnectorSorted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "check.db")
	rt := newCheckRuntime(t, `
service { name = "t" }

connector "zeta_ok" {
  type   = "database"
  driver = "sqlite"
  path   = "`+dbPath+`"
}

connector "alpha_broken" {
  type     = "database"
  driver   = "postgres"
  host     = "127.0.0.1"
  port     = 59999
  database = "nope"
  user     = "nobody"
  password = "wrong"
}
`)

	results := rt.CheckConnectivity(context.Background(), 5*time.Second)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "alpha_broken" || results[1].Name != "zeta_ok" {
		t.Errorf("expected results sorted by name, got %q then %q", results[0].Name, results[1].Name)
	}
	if results[0].OK() {
		t.Error("alpha_broken should have failed")
	}
	if !results[1].OK() {
		t.Errorf("zeta_ok should have passed, got: %v", results[1].Err)
	}
}

func TestCheckConnectivity_NoConnectors(t *testing.T) {
	rt := newCheckRuntime(t, `service { name = "t" }`)

	if results := rt.CheckConnectivity(context.Background(), time.Second); len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}
