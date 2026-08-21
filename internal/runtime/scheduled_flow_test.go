package runtime

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// runtimeForConfig builds a runtime over one configuration file, with its
// connectors registered — the state registerFlows is called in.
func runtimeForConfig(t *testing.T, config string) *Runtime {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.mycel"), []byte(strings.TrimSpace(config)), 0o644); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}

	r, err := New(Options{
		ConfigDir: dir,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if err := r.initConnectors(context.Background()); err != nil {
		t.Fatalf("registering connectors: %v", err)
	}
	return r
}

// A scheduled flow has no source: the clock triggers it. Reading through the
// from block that is not there crashed the service at startup — in three
// separate places, each found only after fixing the one before — so the example
// demonstrating scheduled jobs could not be started at all, and `mycel validate`
// said the configuration was fine.

func TestAFlowWithNoSourceDoesNotCrashTheAccessors(t *testing.T) {
	// The accessors answer for a flow that has no from block rather than
	// dereferencing it. Called on a nil receiver, which is what the runtime
	// holds for a scheduled flow.
	var from *flow.FromConfig

	if got := from.GetOperation(); got != "" {
		t.Errorf("GetOperation on a flow with no source = %q", got)
	}
	if got := from.GetFormat(); got != "" {
		t.Errorf("GetFormat on a flow with no source = %q", got)
	}
	if got := from.GetConnector(); got != "" {
		t.Errorf("GetConnector on a flow with no source = %q", got)
	}
	if got := from.FilterCondition(); got != "" {
		t.Errorf("FilterCondition on a flow with no source = %q", got)
	}
}

func TestAScheduledFlowRegisters(t *testing.T) {
	config := `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "` + t.TempDir() + `/jobs.db"
}

flow "nightly_cleanup" {
  when = "0 3 * * *"

  to {
    connector = "db"
    target    = "logs"
    operation = "DELETE"
  }
}`

	r := runtimeForConfig(t, config)
	if err := r.registerFlows(); err != nil {
		t.Fatalf("a scheduled flow could not be registered: %v", err)
	}

	handler, found := r.flows.Get("nightly_cleanup")
	if !found {
		t.Fatal("the scheduled flow was not registered")
	}
	if handler.Source != nil {
		t.Error("a scheduled flow was given a source connector")
	}
}

func TestAFlowWithNeitherSourceNorScheduleIsRefused(t *testing.T) {
	// Nothing would ever run it, and saying so beats starting a service that
	// silently does nothing.
	config := `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "` + t.TempDir() + `/jobs.db"
}

flow "unreachable" {
  to {
    connector = "db"
    target    = "logs"
  }
}`

	r := runtimeForConfig(t, config)
	err := r.registerFlows()
	if err == nil {
		t.Fatal("a flow with no source and no schedule was registered; nothing can trigger it")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("the error does not name the flow: %v", err)
	}
}
