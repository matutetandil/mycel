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

func TestDebugGate_EnabledSerializesAccess(t *testing.T) {
	var g DebugGate
	g.SetEnabled(true)

	var active atomic.Int32
	var maxActive atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Acquire()
			cur := active.Add(1)
			// Track maximum concurrent access
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond) // Simulate work
			active.Add(-1)
			g.Release()
		}()
	}

	wg.Wait()

	if maxActive.Load() > 1 {
		t.Errorf("expected max 1 concurrent access, got %d", maxActive.Load())
	}
}

func TestDebugGate_DisableUnblocks(t *testing.T) {
	var g DebugGate
	g.SetEnabled(true)

	// Acquire the token
	g.Acquire()

	// Disable while token is held — future Acquires should pass through
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

	// Should work normally
	g.Acquire()
	g.Release()
}
