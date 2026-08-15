package runtime

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Editing a file and having the service follow it is the whole promise of hot
// reload, and nothing exercised the part that notices: the watcher, the signal
// handler, and the hooks the reloader calls around a switch. What was covered
// was the switch itself, which is only reached once something triggers it.

func TestEditingAFileReachesTheRunningService(t *testing.T) {
	r, path := reloadableRuntime(t, workingConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.initHotReload(ctx); err != nil {
		t.Fatalf("initHotReload: %v", err)
	}
	t.Cleanup(func() {
		if r.hotWatcher != nil {
			_ = r.hotWatcher.Stop()
		}
	})

	rewrite(t, path, strings.Replace(workingConfig,
		`operation = "GET /items"`, `operation = "GET /things"`, 1))

	// The watcher debounces, so this waits rather than sleeping a fixed time.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if handler, ok := r.flows.Get("list_items"); ok &&
			handler.Config.From.GetOperation() == "GET /things" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	handler, _ := r.flows.Get("list_items")
	t.Errorf("the edit never reached the service; the flow still answers %q",
		handler.Config.From.GetOperation())
}

func TestAnEditThatDoesNotParseLeavesTheServiceServing(t *testing.T) {
	// The failure that matters is not the one reported — it is what the
	// service is left serving. Somebody saving a file mid-edit must not take
	// the flows down.
	r, path := reloadableRuntime(t, workingConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.initHotReload(ctx); err != nil {
		t.Fatalf("initHotReload: %v", err)
	}
	t.Cleanup(func() {
		if r.hotWatcher != nil {
			_ = r.hotWatcher.Stop()
		}
	})

	rewrite(t, path, "flow \"list_items\" { from {")

	// Give the watcher time to notice, debounce and refuse it.
	time.Sleep(2 * time.Second)

	if _, ok := r.flows.Get("list_items"); !ok {
		t.Error("a file saved mid-edit took the running flow down")
	}
	if len(r.connectors.List()) == 0 {
		t.Error("a file saved mid-edit took the connectors down")
	}
}

func TestTheHooksAroundASwitch(t *testing.T) {
	// The reloader calls these in order around the switch. Each is a place a
	// reload can be turned away before anything running is touched.
	r, path := reloadableRuntime(t, workingConfig)
	ctx := context.Background()

	if err := r.hotReloadLoad(ctx, r.configDir); err != nil {
		t.Fatalf("loading a configuration that parses failed: %v", err)
	}
	if err := r.hotReloadValidate(ctx); err != nil {
		t.Fatalf("validating a configuration that starts failed: %v", err)
	}
	if err := r.hotReloadPrepare(ctx); err != nil {
		t.Fatalf("preparing failed: %v", err)
	}
	r.hotReloadComplete(ctx)
	// Rolling back with nothing to roll back to is what happens when the
	// switch fails before it replaced anything: it must not panic, and it
	// must leave the running configuration alone.
	r.hotReloadRollback(ctx, context.DeadlineExceeded)

	if _, ok := r.flows.Get("list_items"); !ok {
		t.Error("rolling back took the running flow down")
	}

	// And a configuration that does not parse is turned away by the load hook,
	// before the dry run and long before the switch.
	rewrite(t, path, "connector \"db\" {")
	if err := r.hotReloadLoad(ctx, r.configDir); err == nil {
		t.Error("a configuration that does not parse was loaded")
	}
	if err := r.hotReloadValidate(ctx); err == nil {
		t.Error("a configuration that does not parse passed the dry run")
	}
}

func TestAWatcherOnADirectoryThatIsNotThereIsReported(t *testing.T) {
	// At startup, where the path can be fixed — rather than as a service that
	// runs with hot reload quietly doing nothing.
	r, _ := reloadableRuntime(t, workingConfig)
	r.configDir = "/a/directory/nobody/has"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.initHotReload(ctx); err == nil {
		if r.hotWatcher != nil {
			_ = r.hotWatcher.Stop()
		}
		t.Error("a watcher was started on a directory that does not exist")
	}
}
