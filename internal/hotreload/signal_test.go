package hotreload

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Reloading a running service without restarting it.
//
// Hot reload is what makes a configuration change something other than an
// outage, and this repository has twice found it dropping things on the way
// through — consumers left halted after a reload, and flows whose handlers
// went missing. The two entry points are a file changing and somebody sending
// SIGHUP, and the second had no test at all.

// keepSignalAlive stops SIGHUP from killing the test process.
//
// A signal nobody is listening for takes its default action, and SIGHUP's is
// to terminate. The handler under test registers and deregisters its own
// interest; this registration stays for the whole test so there is always
// somebody listening.
func keepSignalAlive(t *testing.T) {
	t.Helper()
	spare := make(chan os.Signal, 8)
	signal.Notify(spare, syscall.SIGHUP)
	t.Cleanup(func() { signal.Stop(spare) })
}

func watcherThatCounts(t *testing.T, reloads *int32) *Watcher {
	t.Helper()

	dir := t.TempDir()
	w, err := NewWatcher(&Config{
		Enabled:    true,
		Paths:      []string{dir},
		Extensions: []string{".mycel"},
		Debounce:   10 * time.Millisecond,
	}, nil, func(ctx context.Context) error {
		atomic.AddInt32(reloads, 1)
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	return w
}

func TestReloadingByHand(t *testing.T) {
	// kill -HUP is how somebody reloads a service they cannot restart, and
	// it is what an operator reaches for at three in the morning.
	keepSignalAlive(t)

	var reloads int32
	watcher := watcherThatCounts(t, &reloads)

	handler := NewSignalHandler(watcher, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.Start(ctx)
	defer handler.Stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("could not send the signal: %v", err)
	}

	waitFor(t, func() bool { return atomic.LoadInt32(&reloads) == 1 },
		"the service did not reload when it was sent SIGHUP")

	// A second one reloads again: this is not a one-shot.
	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	waitFor(t, func() bool { return atomic.LoadInt32(&reloads) == 2 },
		"the service reloaded once and then stopped listening")
}

func TestAReloadThatFailsLeavesTheServiceRunning(t *testing.T) {
	// The service is serving traffic while this happens. A configuration
	// somebody broke has to be refused, not applied and not fatal.
	keepSignalAlive(t)

	dir := t.TempDir()
	var attempts int32
	watcher, err := NewWatcher(&Config{
		Enabled: true, Paths: []string{dir},
		Extensions: []string{".mycel"}, Debounce: 10 * time.Millisecond,
	}, nil, func(ctx context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("connector \"orders_db\" refers to a driver nobody implements")
	}, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	handler := NewSignalHandler(watcher, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.Start(ctx)
	defer handler.Stop()

	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	waitFor(t, func() bool { return atomic.LoadInt32(&attempts) == 1 },
		"the reload was never attempted")

	// Still listening afterwards: a broken file must not cost the operator
	// the ability to fix it and reload again.
	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	waitFor(t, func() bool { return atomic.LoadInt32(&attempts) == 2 },
		"a failed reload stopped the service from listening for the next one")

	// And nothing was recorded as a successful reload.
	if !watcher.LastReload().IsZero() {
		t.Errorf("a failed reload was recorded as having succeeded at %v", watcher.LastReload())
	}
}

func TestNoLongerListeningAfterShutdown(t *testing.T) {
	keepSignalAlive(t)

	var reloads int32
	watcher := watcherThatCounts(t, &reloads)

	handler := NewSignalHandler(watcher, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler.Start(ctx)

	handler.Stop()

	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	time.Sleep(200 * time.Millisecond)

	if got := atomic.LoadInt32(&reloads); got != 0 {
		t.Errorf("a stopped handler reloaded %d times", got)
	}
}

func TestTwoReloadsAtOnce(t *testing.T) {
	// A file saved twice, or a save landing while somebody sends SIGHUP.
	// Running both at once would have two goroutines swapping the runtime's
	// connectors and flows underneath each other.
	dir := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	var concurrent, total int32

	watcher, err := NewWatcher(&Config{
		Enabled: true, Paths: []string{dir},
		Extensions: []string{".mycel"}, Debounce: 10 * time.Millisecond,
	}, nil, func(ctx context.Context) error {
		if atomic.AddInt32(&concurrent, 1) > 1 {
			t.Error("two reloads ran at the same time")
		}
		atomic.AddInt32(&total, 1)
		close(started)
		<-release
		atomic.AddInt32(&concurrent, -1)
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = watcher.TriggerReload(context.Background())
	}()

	<-started
	// While the first is still inside the reload function.
	if watcher.IsReloading() != true {
		t.Error("a reload in progress was not reported as in progress")
	}
	if err := watcher.TriggerReload(context.Background()); err != nil {
		t.Errorf("the second reload returned an error rather than standing down: %v", err)
	}

	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&total); got != 1 {
		t.Errorf("the reload function ran %d times for two overlapping requests", got)
	}
	if watcher.IsReloading() {
		t.Error("the watcher is still reporting a reload after it finished")
	}
	// A reload that worked is dated, which is what an operator checks to see
	// whether their change took.
	if watcher.LastReload().IsZero() {
		t.Error("a successful reload was not recorded")
	}
}

func TestWhatIsWatchedWithNothingConfigured(t *testing.T) {
	config := DefaultConfig("/etc/mycel")

	if !config.Enabled {
		t.Error("hot reload defaults to off")
	}
	if len(config.Paths) != 1 || config.Paths[0] != "/etc/mycel" {
		t.Errorf("paths = %v", config.Paths)
	}
	// The extension is the one the configuration files use; watching the old
	// one would mean a file saved and nothing happening.
	if len(config.Extensions) != 1 || config.Extensions[0] != ".mycel" {
		t.Errorf("extensions = %v", config.Extensions)
	}
	// Editors write a file in several steps, so without a pause a single
	// save reloads the service two or three times.
	if config.Debounce <= 0 {
		t.Error("no debounce, so one save reloads the service several times")
	}
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
