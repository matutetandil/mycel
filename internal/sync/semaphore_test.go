package sync

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemorySemaphore_AcquireRelease(t *testing.T) {
	sem := NewMemorySemaphore(3)
	defer sem.Close()

	ctx := context.Background()
	key := "test-semaphore"

	// Acquire first permit
	permit1, err := sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if permit1 == "" {
		t.Fatal("expected non-empty permit ID")
	}

	// Check available
	available, err := sem.Available(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if available != 2 {
		t.Fatalf("expected 2 available permits, got %d", available)
	}

	// Release permit
	if err := sem.Release(ctx, key, permit1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check available again
	available, err = sem.Available(ctx, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if available != 3 {
		t.Fatalf("expected 3 available permits, got %d", available)
	}
}

func TestMemorySemaphore_MaxPermits(t *testing.T) {
	sem := NewMemorySemaphore(2)
	defer sem.Close()

	ctx := context.Background()
	key := "max-test"

	// Acquire all permits
	permit1, err := sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	permit2, err := sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Third acquire should fail
	_, err = sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != ErrSemaphoreFull {
		t.Fatalf("expected ErrSemaphoreFull, got: %v", err)
	}

	// Release one
	if err := sem.Release(ctx, key, permit1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now should be able to acquire
	permit3, err := sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cleanup
	sem.Release(ctx, key, permit2)
	sem.Release(ctx, key, permit3)
}

func TestMemorySemaphore_LeaseExpiration(t *testing.T) {
	sem := NewMemorySemaphore(1)
	defer sem.Close()

	ctx := context.Background()
	key := "lease-test"

	// Acquire with short lease
	_, err := sem.Acquire(ctx, key, 5*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Semaphore should be full
	_, err = sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != ErrSemaphoreFull {
		t.Fatalf("expected ErrSemaphoreFull, got: %v", err)
	}

	// Wait for lease to expire
	time.Sleep(150 * time.Millisecond)

	// Now should be able to acquire
	permit2, err := sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error after lease expiry: %v", err)
	}

	sem.Release(ctx, key, permit2)
}

func TestMemorySemaphore_ReleaseNotFound(t *testing.T) {
	sem := NewMemorySemaphore(2)
	defer sem.Close()

	ctx := context.Background()
	key := "release-test"

	// Try to release non-existent permit
	err := sem.Release(ctx, key, "non-existent-permit")
	if err != ErrPermitNotFound {
		t.Fatalf("expected ErrPermitNotFound, got: %v", err)
	}
}

func TestMemorySemaphore_Concurrent(t *testing.T) {
	sem := NewMemorySemaphore(5)
	defer sem.Close()

	ctx := context.Background()
	key := "concurrent-test"

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Try to acquire from 20 goroutines
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			permit, err := sem.Acquire(ctx, key, 5*time.Second, 1*time.Second)
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()

				// Simulate work
				time.Sleep(50 * time.Millisecond)

				sem.Release(ctx, key, permit)
			}
		}()
	}

	wg.Wait()

	// Should have had some successes
	if successCount == 0 {
		t.Fatal("expected some successful acquisitions")
	}
	t.Logf("successful acquisitions: %d", successCount)
}

func TestAcquireSemaphoreWithRetry(t *testing.T) {
	sem := NewMemorySemaphore(1)
	defer sem.Close()

	ctx := context.Background()
	key := "retry-test"

	// Acquire the only permit
	permit1, err := sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Start goroutine to release after 200ms
	go func() {
		time.Sleep(200 * time.Millisecond)
		sem.Release(ctx, key, permit1)
	}()

	// Try to acquire with retry
	cfg := &SemaphoreConfig{
		MaxPermits: 1,
		Timeout:    1 * time.Second,
		Lease:      30 * time.Second,
	}

	start := time.Now()
	permit2, err := AcquireSemaphoreWithRetry(ctx, sem, key, cfg)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if permit2 == "" {
		t.Fatal("expected non-empty permit ID")
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("expected to wait for permit release, elapsed: %v", elapsed)
	}

	sem.Release(ctx, key, permit2)
}

func TestAcquireSemaphoreWithRetry_Timeout(t *testing.T) {
	sem := NewMemorySemaphore(1)
	defer sem.Close()

	ctx := context.Background()
	key := "timeout-test"

	// Acquire and don't release
	_, err := sem.Acquire(ctx, key, 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to acquire with retry but times out
	cfg := &SemaphoreConfig{
		MaxPermits: 1,
		Timeout:    200 * time.Millisecond,
		Lease:      30 * time.Second,
	}

	_, err = AcquireSemaphoreWithRetry(ctx, sem, key, cfg)
	if err != ErrSemaphoreTimeout {
		t.Fatalf("expected ErrSemaphoreTimeout, got: %v", err)
	}
}

func TestMemorySemaphore_AvailableWithoutKey(t *testing.T) {
	sem := NewMemorySemaphore(5)
	defer sem.Close()

	ctx := context.Background()

	// Check available for non-existent key
	available, err := sem.Available(ctx, "non-existent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if available != 5 {
		t.Fatalf("expected 5 available permits for new key, got %d", available)
	}
}
