package connector

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebugGate_DisabledByDefault(t *testing.T) {
	var g DebugGate

	// Should not block when disabled
	done := make(chan struct{})
	go func() {
		g.Acquire()
		g.Release()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Acquire/Release blocked on disabled gate")
	}
}

func TestDebugGate_EnabledBlocksUntilAllow(t *testing.T) {
	var g DebugGate
	g.SetEnabled(true)

	// Gate should block without Allow()
	blocked := make(chan struct{})
	go func() {
		g.Acquire()
		close(blocked)
	}()

	select {
	case <-blocked:
		t.Fatal("Acquire should block when gate is enabled without Allow()")
	case <-time.After(50 * time.Millisecond):
		// OK — still blocked
	}

	// Allow() should unblock exactly one Acquire
	g.Allow()

	select {
	case <-blocked:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Acquire should unblock after Allow()")
	}
}

func TestDebugGate_AllowOneAtATime(t *testing.T) {
	var g DebugGate
	g.SetEnabled(true)

	var processed atomic.Int32
	var wg sync.WaitGroup

	// Start 3 workers waiting on the gate
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Acquire()
			processed.Add(1)
			g.Release() // no-op in studio mode
		}()
	}

	// Allow 1 — only 1 should pass
	g.Allow()
	time.Sleep(50 * time.Millisecond)
	if got := processed.Load(); got != 1 {
		t.Errorf("after 1 Allow(), expected 1 processed, got %d", got)
	}

	// Allow 1 more
	g.Allow()
	time.Sleep(50 * time.Millisecond)
	if got := processed.Load(); got != 2 {
		t.Errorf("after 2 Allow(), expected 2 processed, got %d", got)
	}

	// Allow last one
	g.Allow()
	time.Sleep(50 * time.Millisecond)
	if got := processed.Load(); got != 3 {
		t.Errorf("after 3 Allow(), expected 3 processed, got %d", got)
	}

	wg.Wait()
}

func TestDebugGate_ReleaseIsNoOpWhenEnabled(t *testing.T) {
	var g DebugGate
	g.SetEnabled(true)

	// Allow one message through
	g.Allow()
	g.Acquire()
	g.Release() // should be no-op

	// Second Acquire should block (Release didn't put token back)
	blocked := make(chan struct{})
	go func() {
		g.Acquire()
		close(blocked)
	}()

	select {
	case <-blocked:
		t.Fatal("Release should be no-op in studio mode — second Acquire should block")
	case <-time.After(50 * time.Millisecond):
		// OK — still blocked
	}

	// Clean up
	g.SetEnabled(false)
}

func TestDebugGate_DisableUnblocks(t *testing.T) {
	var g DebugGate
	g.SetEnabled(true)

	// Disable while no token — future Acquires should pass through
	g.SetEnabled(false)

	done := make(chan struct{})
	go func() {
		g.Acquire() // Should not block since gate is now nil
		g.Release()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Acquire blocked after gate was disabled")
	}
}

func TestDebugGate_ReEnable(t *testing.T) {
	var g DebugGate

	// Enable, disable, re-enable
	g.SetEnabled(true)
	g.SetEnabled(false)
	g.SetEnabled(true)

	// Should block until Allow
	g.Allow()
	g.Acquire()
	g.Release()
}
