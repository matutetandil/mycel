package cdc

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// flakyListener simulates a driver whose connection drops: the first failN
// calls to Start return an error immediately, and every call after that blocks
// until the context is cancelled (a healthy, long-lived stream). It records how
// many times Start was invoked so a test can assert the supervisor reconnected.
type flakyListener struct {
	mu     sync.Mutex
	calls  int
	failN  int
	notify chan int // receives the call count on each Start invocation
}

func (f *flakyListener) Start(ctx context.Context, _ chan<- *Event) error {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()

	if f.notify != nil {
		f.notify <- n
	}

	if n <= f.failN {
		return fmt.Errorf("simulated connection drop #%d", n)
	}
	// Healthy run: stream until shutdown.
	<-ctx.Done()
	return ctx.Err()
}

func (f *flakyListener) Close() error { return nil }

func (f *flakyListener) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestSuperviseListenerReconnects is the regression test for the CDC zombie
// bug: when the replication connection dropped, listener.Start returned and the
// listener goroutine exited without ever being restarted, so the connector
// stopped streaming until a process restart.
//
// The fake listener fails its first two Start calls and then streams healthily.
// With supervision, Start must be invoked at least three times (two failed
// reconnects + one that sticks). The Listener interface is injectable, so this
// runs without a real PostgreSQL.
func TestSuperviseListenerReconnects(t *testing.T) {
	notify := make(chan int, 16)
	fake := &flakyListener{failN: 2, notify: notify}

	c := New("cdc_reconnect", &Config{Driver: "postgres"}, fake, nil)
	// Tiny backoff so the two reconnect waits don't slow the test.
	c.minBackoff = time.Millisecond
	c.maxBackoff = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Expect 3 invocations: drop, drop, then the healthy stream.
	for i := 1; i <= 3; i++ {
		select {
		case <-notify:
		case <-time.After(2 * time.Second):
			t.Fatalf("listener.Start was invoked only %d time(s); supervisor did not reconnect", i-1)
		}
	}

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestSuperviseListenerStopsOnShutdown verifies the supervisor does not restart
// the listener after the connector is closed (no reconnect storm on shutdown).
func TestSuperviseListenerStopsOnShutdown(t *testing.T) {
	notify := make(chan int, 16)
	fake := &flakyListener{failN: 0, notify: notify} // first call is the healthy stream

	c := New("cdc_shutdown", &Config{Driver: "postgres"}, fake, nil)
	c.minBackoff = time.Millisecond
	c.maxBackoff = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for the healthy stream to be running.
	select {
	case <-notify:
	case <-time.After(2 * time.Second):
		t.Fatal("listener.Start was never invoked")
	}

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After shutdown there must be no further Start invocations.
	time.Sleep(50 * time.Millisecond)
	if got := fake.callCount(); got != 1 {
		t.Fatalf("listener.Start invoked %d times after shutdown; expected exactly 1 (no reconnect storm)", got)
	}
}
