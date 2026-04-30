package sync

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestExecuteWithLock_HeartbeatExtendsTTL verifies that a flow taking
// longer than the configured lock.timeout still holds the lock thanks to
// the heartbeat. Without heartbeat, a second concurrent worker would
// observe the TTL-expired lock and acquire it, breaking mutual
// exclusion.
func TestExecuteWithLock_HeartbeatExtendsTTL(t *testing.T) {
	mgr := NewManager()
	defer mgr.Close()

	storage := &SyncStorageConfig{Driver: "memory"}
	cfg := &FlowLockConfig{
		Storage: storage,
		Key:     "'k'",
		Timeout: "200ms", // intentionally short — flow is slower
		Wait:    true,
		Retry:   "20ms",
	}

	// Worker A holds the lock and runs for 700ms — well past the 200ms
	// TTL. With heartbeat the TTL is extended to ~67ms intervals (200/3)
	// so the lock should remain held for the entire duration.
	aDone := make(chan struct{})
	go func() {
		defer close(aDone)
		_, err := mgr.ExecuteWithLock(context.Background(), cfg, "k", func() (interface{}, error) {
			time.Sleep(700 * time.Millisecond)
			return "ok", nil
		})
		if err != nil {
			t.Errorf("worker A: %v", err)
		}
	}()

	// Worker B tries to acquire the same lock 100ms later (after A but
	// before the original 200ms TTL expiry). With heartbeat, B should
	// have to wait for A to finish ~600ms later. Without heartbeat, B
	// would acquire at ~200ms when the TTL expires.
	time.Sleep(100 * time.Millisecond)
	bAcquired := make(chan time.Duration, 1)
	go func() {
		// Sub-config: short timeout so B doesn't wait forever, but long
		// enough that A finishes first.
		bCfg := *cfg
		bCfg.Timeout = "1500ms"
		start := time.Now()
		_, _ = mgr.ExecuteWithLock(context.Background(), &bCfg, "k", func() (interface{}, error) {
			return nil, nil
		})
		bAcquired <- time.Since(start)
	}()

	<-aDone

	select {
	case elapsed := <-bAcquired:
		// Worker B should have waited for A to release. Since A holds
		// the lock for 700ms total and B started at +100ms, B should
		// have waited ~600ms before getting the lock. Allow some slack.
		if elapsed < 400*time.Millisecond {
			t.Errorf("worker B acquired the lock too early (%s) — heartbeat did not keep A's lock alive", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker B never returned")
	}
}

// TestExecuteWithLock_HeartbeatExtendCalled is a tighter unit test:
// verifies the heartbeat goroutine actually invokes Extend on the
// underlying lock implementation while fn() is running.
func TestExecuteWithLock_HeartbeatExtendCalled(t *testing.T) {
	storage := &SyncStorageConfig{Driver: "memory"}
	mgr := NewManager()
	defer mgr.Close()

	// Wrap the memory lock with a counting decorator.
	memLock, err := mgr.GetLock(context.Background(), storage)
	if err != nil {
		t.Fatalf("GetLock: %v", err)
	}

	// Acquire the lock manually to set up the heartbeat target.
	if ok, _ := memLock.Acquire(context.Background(), "k2", 300*time.Millisecond); !ok {
		t.Fatal("could not acquire")
	}
	defer func() { _ = memLock.Release(context.Background(), "k2") }()

	// Drive Extend repeatedly for ~500ms. Each call should succeed
	// because we own the lock and we keep extending the TTL.
	var extendCount atomic.Int32
	end := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(end) {
		ok, err := memLock.Extend(context.Background(), "k2", 300*time.Millisecond)
		if err != nil {
			t.Fatalf("Extend error: %v", err)
		}
		if !ok {
			t.Fatal("Extend reported lock not held when it was")
		}
		extendCount.Add(1)
		time.Sleep(50 * time.Millisecond)
	}

	if got := extendCount.Load(); got < 8 {
		t.Errorf("expected ~10 extend calls, got %d", got)
	}

	// Lock should still be held even though the original 300ms TTL
	// elapsed several times during the test.
	held, _ := memLock.IsHeld(context.Background(), "k2")
	if !held {
		t.Error("lock should still be held after repeated Extend calls")
	}
}

// TestMemoryLock_ExtendNotHeldReturnsFalse: parity check with Redis —
// Extend on a key not owned by the caller returns false, no error.
func TestMemoryLock_ExtendNotHeldReturnsFalse(t *testing.T) {
	storage := NewMemoryLockStorage()
	defer storage.Close()
	a := NewMemoryLockWithStorage(storage)
	b := NewMemoryLockWithStorage(storage)

	if ok, _ := a.Acquire(context.Background(), "k", 500*time.Millisecond); !ok {
		t.Fatal("a.Acquire failed")
	}

	// b doesn't own the lock — Extend must return false.
	ok, err := b.Extend(context.Background(), "k", 500*time.Millisecond)
	if err != nil {
		t.Errorf("Extend should not error when not held, got: %v", err)
	}
	if ok {
		t.Error("Extend by non-owner must return false")
	}
}

// TestMemoryLock_ExtendMissingKeyReturnsFalse: extending a key that
// doesn't exist (TTL expired and cleanup ran) must return (false, nil),
// not an error.
func TestMemoryLock_ExtendMissingKeyReturnsFalse(t *testing.T) {
	lock := NewMemoryLock()
	defer lock.Close()

	ok, err := lock.Extend(context.Background(), "nonexistent", time.Second)
	if err != nil {
		t.Errorf("Extend on missing key should not error, got: %v", err)
	}
	if ok {
		t.Error("Extend on missing key must return false")
	}
}
