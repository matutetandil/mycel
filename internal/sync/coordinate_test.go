package sync

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryCoordinator_SignalWait(t *testing.T) {
	coord := NewMemoryCoordinator(time.Second)
	defer coord.Close()

	ctx := context.Background()
	signal := "test-signal"

	// Signal first
	if err := coord.Signal(ctx, signal, 5*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait should return immediately
	ok, err := coord.Wait(ctx, signal, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected signal to be received")
	}
}

func TestMemoryCoordinator_WaitThenSignal(t *testing.T) {
	coord := NewMemoryCoordinator(time.Second)
	defer coord.Close()

	ctx := context.Background()
	signal := "wait-then-signal"

	var wg sync.WaitGroup
	var waitResult bool
	var waitErr error

	// Start waiting in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		waitResult, waitErr = coord.Wait(ctx, signal, 5*time.Second)
	}()

	// Give waiter time to register
	time.Sleep(50 * time.Millisecond)

	// Now signal
	if err := coord.Signal(ctx, signal, 5*time.Second); err != nil {
		t.Fatalf("unexpected error signaling: %v", err)
	}

	wg.Wait()

	if waitErr != nil {
		t.Fatalf("unexpected wait error: %v", waitErr)
	}
	if !waitResult {
		t.Fatal("expected wait to succeed after signal")
	}
}

func TestMemoryCoordinator_WaitTimeout(t *testing.T) {
	coord := NewMemoryCoordinator(time.Second)
	defer coord.Close()

	ctx := context.Background()
	signal := "timeout-signal"

	start := time.Now()
	ok, err := coord.Wait(ctx, signal, 200*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected timeout, not success")
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("expected to wait at least 150ms, got %v", elapsed)
	}
}

func TestMemoryCoordinator_Exists(t *testing.T) {
	coord := NewMemoryCoordinator(time.Second)
	defer coord.Close()

	ctx := context.Background()
	signal := "exists-signal"

	// Should not exist initially
	exists, err := coord.Exists(ctx, signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected signal to not exist")
	}

	// Signal
	if err := coord.Signal(ctx, signal, 5*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should exist now
	exists, err = coord.Exists(ctx, signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected signal to exist")
	}
}

func TestMemoryCoordinator_SignalExpiration(t *testing.T) {
	coord := NewMemoryCoordinator(50 * time.Millisecond)
	defer coord.Close()

	ctx := context.Background()
	signal := "expiring-signal"

	// Signal with short TTL
	if err := coord.Signal(ctx, signal, 100*time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should exist
	exists, err := coord.Exists(ctx, signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected signal to exist")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should not exist
	exists, err = coord.Exists(ctx, signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected signal to have expired")
	}
}

func TestMemoryCoordinator_MultipleWaiters(t *testing.T) {
	coord := NewMemoryCoordinator(time.Second)
	defer coord.Close()

	ctx := context.Background()
	signal := "multi-waiter-signal"

	var wg sync.WaitGroup
	results := make([]bool, 5)

	// Start multiple waiters
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ok, _ := coord.Wait(ctx, signal, 5*time.Second)
			results[idx] = ok
		}(i)
	}

	// Give waiters time to register
	time.Sleep(50 * time.Millisecond)

	// Signal once
	if err := coord.Signal(ctx, signal, 5*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wg.Wait()

	// All waiters should have received the signal
	for i, ok := range results {
		if !ok {
			t.Fatalf("waiter %d did not receive signal", i)
		}
	}
}

func TestMemoryCoordinator_ContextCancellation(t *testing.T) {
	coord := NewMemoryCoordinator(time.Second)
	defer coord.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	signal := "context-cancel-signal"

	start := time.Now()
	ok, err := coord.Wait(ctx, signal, 5*time.Second)
	elapsed := time.Since(start)

	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
	if ok {
		t.Fatal("expected failure due to context cancellation")
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("expected to cancel quickly, took %v", elapsed)
	}
}

func TestMemoryCoordinator_Stats(t *testing.T) {
	coord := NewMemoryCoordinator(time.Second)
	defer coord.Close()

	ctx := context.Background()

	// Signal a few times
	coord.Signal(ctx, "signal1", 5*time.Second)
	coord.Signal(ctx, "signal2", 5*time.Second)

	stats := coord.Stats()
	if stats["active_signals"].(int) != 2 {
		t.Fatalf("expected 2 active signals, got %v", stats["active_signals"])
	}
}

func TestOnTimeoutAction(t *testing.T) {
	tests := []struct {
		input    string
		expected OnTimeoutAction
	}{
		{"fail", OnTimeoutFail},
		{"retry", OnTimeoutRetry},
		{"skip", OnTimeoutSkip},
		{"pass", OnTimeoutPass},
		{"unknown", OnTimeoutFail},
		{"", OnTimeoutFail},
	}

	for _, tc := range tests {
		result := ParseOnTimeoutAction(tc.input)
		if result != tc.expected {
			t.Errorf("ParseOnTimeoutAction(%q) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}

func TestMemoryCoordinator_ConcurrentSignalWait(t *testing.T) {
	coord := NewMemoryCoordinator(100 * time.Millisecond)
	defer coord.Close()

	ctx := context.Background()
	signal := "concurrent-test"

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Start waiters and signalers concurrently
	for i := 0; i < 10; i++ {
		wg.Add(2)

		// Waiter
		go func() {
			defer wg.Done()
			ok, err := coord.Wait(ctx, signal, 500*time.Millisecond)
			if err == nil && ok {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()

		// Signaler (with small delay)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			coord.Signal(ctx, signal, 1*time.Second)
		}()
	}

	wg.Wait()

	// All waiters should have received the signal
	if successCount != 10 {
		t.Fatalf("expected 10 successful waits, got %d", successCount)
	}
}
