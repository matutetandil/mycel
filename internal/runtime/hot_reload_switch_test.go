package runtime

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A hot reload replaces everything a service is built from at once, on a
// running process, with traffic arriving. The failure that matters is not the
// one that is reported — it is what the service is left serving afterwards.

const workingConfig = `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

connector "api" {
  type = "rest"
  port = 18191
}

flow "list_items" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`

func reloadableRuntime(t *testing.T, config string) (*Runtime, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.mycel")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, err := New(Options{
		ConfigDir: dir,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.initConnectors(context.Background()); err != nil {
		t.Fatalf("initConnectors: %v", err)
	}
	if err := r.registerFlows(); err != nil {
		t.Fatalf("registerFlows: %v", err)
	}
	t.Cleanup(func() { _ = r.connectors.CloseAll(context.Background()) })
	return r, path
}

func rewrite(t *testing.T, path, config string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestAReloadInstallsTheNewConfiguration(t *testing.T) {
	r, path := reloadableRuntime(t, workingConfig)

	rewrite(t, path, strings.Replace(workingConfig, `flow "list_items"`, `flow "list_products"`, 1))
	if err := r.hotReloadSwitch(context.Background()); err != nil {
		t.Fatalf("hotReloadSwitch: %v", err)
	}

	if _, ok := r.GetFlow("list_products"); !ok {
		t.Error("the flow the new configuration declares is not registered")
	}
	if _, ok := r.GetFlow("list_items"); ok {
		t.Error("the flow the old configuration declared is still registered")
	}
}

// Each of these is a mistake someone makes while editing a live configuration,
// and each used to leave the process running and serving nothing: the old
// connectors were closed and the flow registry emptied before the replacement
// was known to work, and "rollback" restored the configuration pointer alone.

func TestAReloadThatFailsLeavesTheServiceServing(t *testing.T) {
	for name, broken := range map[string]string{
		"a driver name with a typo": `
connector "db" {
  type     = "database"
  driver   = "sqlite3"
  database = ":memory:"
}
flow "list_items" {
  from { connector = "db" }
  to   { connector = "db" }
}
`,
		"a flow whose source connector does not exist": `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
flow "list_items" {
  from {
    connector = "absent"
    operation = "GET /items"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`,
		"a source missing what its connector requires": `
connector "api" {
  type = "rest"
  port = 18191
}
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
flow "list_items" {
  from {
    connector = "api"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`,
		"a file that does not parse": `
connector "db" {
  type = "database"
`,
	} {
		t.Run(name, func(t *testing.T) {
			r, path := reloadableRuntime(t, workingConfig)
			rewrite(t, path, broken)

			err := r.hotReloadSwitch(context.Background())
			if err == nil {
				t.Fatal("a configuration that cannot work was installed")
			}

			// The point of the test: still serving what it was serving.
			if _, ok := r.GetFlow("list_items"); !ok {
				t.Error("the service stopped serving the flow it had, so a typo took it down")
			}
			if len(r.ListFlows()) != 1 {
				t.Errorf("%d flows registered, want the one that was working", len(r.ListFlows()))
			}

			// And the connectors it was serving through are still open, or the
			// flow is registered against something that is closed.
			if _, err := r.connectors.Get("db"); err != nil {
				t.Errorf("the connector it was using is gone: %v", err)
			}
			if len(r.config.Connectors) == 0 {
				t.Error("the configuration was left empty")
			}
		})
	}
}

func TestAFailedReloadCanBeFollowedByAWorkingOne(t *testing.T) {
	// The state a failed reload leaves has to be one a later reload can work
	// from, or the first typo makes every subsequent edit fail too.
	r, path := reloadableRuntime(t, workingConfig)

	rewrite(t, path, `connector "db" { type = "database" driver = "nope" }`)
	if err := r.hotReloadSwitch(context.Background()); err == nil {
		t.Fatal("a broken configuration was installed")
	}

	rewrite(t, path, strings.Replace(workingConfig, `flow "list_items"`, `flow "list_orders"`, 1))
	if err := r.hotReloadSwitch(context.Background()); err != nil {
		t.Fatalf("a working configuration was refused after a failed reload: %v", err)
	}
	if _, ok := r.GetFlow("list_orders"); !ok {
		t.Error("the corrected configuration was not installed")
	}
}

func TestReloadingTheSameConfigurationIsHarmless(t *testing.T) {
	// It happens on every editor save, and twice on some.
	r, _ := reloadableRuntime(t, workingConfig)

	for i := 0; i < 3; i++ {
		if err := r.hotReloadSwitch(context.Background()); err != nil {
			t.Fatalf("reload %d: %v", i+1, err)
		}
		if _, ok := r.GetFlow("list_items"); !ok {
			t.Fatalf("the flow was lost on reload %d", i+1)
		}
	}
}

func TestAReloadPicksUpANewConnector(t *testing.T) {
	r, path := reloadableRuntime(t, workingConfig)

	rewrite(t, path, workingConfig+`
connector "archive" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}
`)
	if err := r.hotReloadSwitch(context.Background()); err != nil {
		t.Fatalf("hotReloadSwitch: %v", err)
	}
	if _, err := r.connectors.Get("archive"); err != nil {
		t.Errorf("the connector the new configuration adds is not registered: %v", err)
	}
	if _, ok := r.GetFlow("list_items"); !ok {
		t.Error("the existing flow was lost while adding a connector")
	}
}

func TestAReloadIsRefusedForAConfigurationStartupWouldHaveRefused(t *testing.T) {
	// Startup checks each flow's source against its connector's schema. A
	// reload that skipped the check could install, at three in the morning, a
	// configuration that the same file would have been rejected for at boot.
	r, path := reloadableRuntime(t, workingConfig)
	rewrite(t, path, strings.Replace(workingConfig, `    operation = "GET /items"`+"\n", "", 1))

	err := r.hotReloadSwitch(context.Background())
	if err == nil {
		t.Fatal("a reload installed a configuration startup would have refused")
	}
	if !strings.Contains(err.Error(), "operation") {
		t.Errorf("error = %q, want it to name what is missing", err)
	}
}

func TestReloadIsRefusedWhenItIsNotEnabled(t *testing.T) {
	r, _ := reloadableRuntime(t, workingConfig)
	err := r.Reload(context.Background())
	if err == nil {
		t.Fatal("a reload was accepted although hot reload is off")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("error = %q", err)
	}

	stats := r.ReloadStats()
	if stats["enabled"] != false {
		t.Errorf("stats = %v, want it to report hot reload as off", stats)
	}
}
