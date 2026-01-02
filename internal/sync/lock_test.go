package sync

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryLock_AcquireRelease(t *testing.T) {
	lock := NewMemoryLock()
	defer lock.Close()

	ctx := context.Background()
	key := "test-lock"

	// Acquire lock
	acquired, err := lock.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Check if held
	held, err := lock.IsHeld(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !held {
		t.Fatal("expected lock to be held")
	}

	// Release lock
	if err := lock.Release(ctx, key); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check not held
	held, err = lock.IsHeld(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if held {
		t.Fatal("expected lock to not be held after release")
	}
}

func TestMemoryLock_ConcurrentAcquire(t *testing.T) {
	// Use shared storage for testing distributed locks
	storage := NewMemoryLockStorage()
	defer storage.Close()

	lock := NewMemoryLockWithStorage(storage)
	lock2 := NewMemoryLockWithStorage(storage)

	ctx := context.Background()
	key := "concurrent-lock"

	// Acquire lock
	acquired, err := lock.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Try to acquire with another instance
	acquired2, err := lock2.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired2 {
		t.Fatal("expected second acquire to fail")
	}

	// Release first lock
	if err := lock.Release(ctx, key); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now second instance should be able to acquire
	acquired2, err = lock2.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired2 {
		t.Fatal("expected second acquire to succeed after release")
	}
}

func TestMemoryLock_Expiration(t *testing.T) {
	storage := NewMemoryLockStorage()
	defer storage.Close()

	lock := NewMemoryLockWithStorage(storage)
	lock2 := NewMemoryLockWithStorage(storage)

	ctx := context.Background()
	key := "expiring-lock"

	// Acquire with short timeout
	acquired, err := lock.Acquire(ctx, key, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Another instance should be able to acquire
	acquired2, err := lock2.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired2 {
		t.Fatal("expected to acquire expired lock")
	}
}

func TestMemoryLock_ReentrantAcquire(t *testing.T) {
	lock := NewMemoryLock()
	defer lock.Close()

	ctx := context.Background()
	key := "reentrant-lock"

	// Acquire lock
	acquired, err := lock.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Same instance should be able to "re-acquire" (extend)
	acquired, err = lock.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected reentrant acquire to succeed")
	}
}

func TestMemoryLock_ReleaseNotHeld(t *testing.T) {
	lock := NewMemoryLock()
	defer lock.Close()

	ctx := context.Background()
	key := "not-held-lock"

	// Try to release without acquiring
	err := lock.Release(ctx, key)
	if err != ErrLockReleased {
		t.Fatalf("expected ErrLockReleased, got: %v", err)
	}
}

func TestMemoryLock_ReleaseDifferentOwner(t *testing.T) {
	storage := NewMemoryLockStorage()
	defer storage.Close()

	lock1 := NewMemoryLockWithStorage(storage)
	lock2 := NewMemoryLockWithStorage(storage)

	ctx := context.Background()
	key := "owner-test-lock"

	// Lock1 acquires
	acquired, err := lock1.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Lock2 tries to release - should fail
	err = lock2.Release(ctx, key)
	if err != ErrLockNotHeld {
		t.Fatalf("expected ErrLockNotHeld, got: %v", err)
	}
}

func TestAcquireWithRetry(t *testing.T) {
	storage := NewMemoryLockStorage()
	defer storage.Close()

	lock := NewMemoryLockWithStorage(storage)
	lock2 := NewMemoryLockWithStorage(storage)

	ctx := context.Background()
	key := "retry-lock"

	// Acquire lock
	acquired, err := lock.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	// Start a goroutine to release after 200ms
	go func() {
		time.Sleep(200 * time.Millisecond)
		lock.Release(ctx, key)
	}()

	cfg := &LockConfig{
		Timeout: 1 * time.Second,
		Wait:    true,
		Retry:   50 * time.Millisecond,
	}

	start := time.Now()
	acquired, err = AcquireWithRetry(ctx, lock2, key, cfg)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock with retry")
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("expected to wait for lock release, elapsed: %v", elapsed)
	}
}

func TestAcquireWithRetry_Timeout(t *testing.T) {
	storage := NewMemoryLockStorage()
	defer storage.Close()

	lock := NewMemoryLockWithStorage(storage)
	lock2 := NewMemoryLockWithStorage(storage)

	ctx := context.Background()
	key := "timeout-lock"

	// Acquire lock and don't release
	acquired, err := lock.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	cfg := &LockConfig{
		Timeout: 200 * time.Millisecond,
		Wait:    true,
		Retry:   50 * time.Millisecond,
	}

	acquired, err = AcquireWithRetry(ctx, lock2, key, cfg)
	if err != ErrLockTimeout {
		t.Fatalf("expected ErrLockTimeout, got: %v", err)
	}
	if acquired {
		t.Fatal("expected acquire to fail")
	}
}

func TestAcquireWithRetry_NoWait(t *testing.T) {
	storage := NewMemoryLockStorage()
	defer storage.Close()

	lock := NewMemoryLockWithStorage(storage)
	lock2 := NewMemoryLockWithStorage(storage)

	ctx := context.Background()
	key := "nowait-lock"

	// Acquire lock
	acquired, err := lock.Acquire(ctx, key, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	cfg := &LockConfig{
		Timeout: 5 * time.Second,
		Wait:    false,
	}

	acquired, err = AcquireWithRetry(ctx, lock2, key, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Fatal("expected acquire to fail immediately")
	}
}

func TestMemoryLock_Concurrent(t *testing.T) {
	lock := NewMemoryLock()
	defer lock.Close()

	ctx := context.Background()
	key := "concurrent-test"

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Try to acquire from 10 goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			acquired, err := lock.Acquire(ctx, key, 100*time.Millisecond)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if acquired {
				mu.Lock()
				successCount++
				mu.Unlock()
				// Hold for a bit then release
				time.Sleep(20 * time.Millisecond)
				lock.Release(ctx, key)
			}
		}()
	}

	wg.Wait()

	// Only one should have acquired initially
	// (but others might have acquired after release)
	if successCount == 0 {
		t.Fatal("expected at least one successful acquire")
	}
}
