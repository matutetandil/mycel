package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

var testError = errors.New("test error")

func TestBreaker_Basic(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	// Should start closed
	if cb.State() != StateClosed {
		t.Errorf("expected closed state, got %v", cb.State())
	}

	// Successful request
	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	if err != nil {
		t.Errorf("successful request should not error: %v", err)
	}
}

func TestBreaker_OpensAfterFailures(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	// Cause failures
	for i := 0; i < 3; i++ {
		cb.Execute(context.Background(), func() error {
			return testError
		})
	}

	// Should be open now
	if cb.State() != StateOpen {
		t.Errorf("expected open state after %d failures, got %v", 3, cb.State())
	}

	// Further requests should fail fast
	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
	})

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return testError
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected open state, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Next request should transition to half-open
	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	if err != nil {
		t.Errorf("request in half-open should succeed: %v", err)
	}

	// Should be closed after success (success threshold = 1)
	if cb.State() != StateClosed {
		t.Errorf("expected closed state after success in half-open, got %v", cb.State())
	}
}

func TestBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
	})

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return testError
		})
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Fail in half-open
	cb.Execute(context.Background(), func() error {
		return testError
	})

	// Should be open again
	if cb.State() != StateOpen {
		t.Errorf("expected open state after failure in half-open, got %v", cb.State())
	}
}

func TestBreaker_SuccessResetsFailures(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	// Two failures
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return testError
		})
	}

	if cb.Failures() != 2 {
		t.Errorf("expected 2 failures, got %d", cb.Failures())
	}

	// Success should reset
	cb.Execute(context.Background(), func() error {
		return nil
	})

	if cb.Failures() != 0 {
		t.Errorf("expected 0 failures after success, got %d", cb.Failures())
	}

	// Should still be closed
	if cb.State() != StateClosed {
		t.Errorf("expected closed state, got %v", cb.State())
	}
}

func TestBreaker_ExecuteWithResult(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	result, err := cb.ExecuteWithResult(context.Background(), func() (interface{}, error) {
		return "hello", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}

	// Test with error
	_, err = cb.ExecuteWithResult(context.Background(), func() (interface{}, error) {
		return nil, testError
	})

	if !errors.Is(err, testError) {
		t.Errorf("expected testError, got %v", err)
	}
}

func TestBreaker_Reset(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return testError
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected open state, got %v", cb.State())
	}

	// Reset
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected closed state after reset, got %v", cb.State())
	}

	if cb.Failures() != 0 {
		t.Errorf("expected 0 failures after reset, got %d", cb.Failures())
	}
}

func TestBreaker_Stats(t *testing.T) {
	cb := New(&Config{
		Name:             "test-cb",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	// Cause some activity
	cb.Execute(context.Background(), func() error {
		return testError
	})

	stats := cb.Stats()

	if stats["name"] != "test-cb" {
		t.Errorf("expected name 'test-cb', got %v", stats["name"])
	}

	if stats["state"] != "closed" {
		t.Errorf("expected state 'closed', got %v", stats["state"])
	}

	if stats["failures"].(int) != 1 {
		t.Errorf("expected 1 failure, got %v", stats["failures"])
	}
}

func TestBreaker_OnStateChange(t *testing.T) {
	changes := make(chan struct {
		from, to State
	}, 10)

	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(name string, from, to State) {
			changes <- struct {
				from, to State
			}{from, to}
		},
	})

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return testError
		})
	}

	// Wait for callback
	select {
	case change := <-changes:
		if change.from != StateClosed || change.to != StateOpen {
			t.Errorf("expected closed->open, got %v->%v", change.from, change.to)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected state change callback")
	}
}

func TestBreaker_Concurrent(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 100,
		SuccessThreshold: 10,
		Timeout:          100 * time.Millisecond,
	})

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// Launch 100 concurrent requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cb.Execute(context.Background(), func() error {
				return nil
			})
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successCount != 100 {
		t.Errorf("expected 100 successes, got %d", successCount)
	}
}

func TestBreaker_MaxConcurrent(t *testing.T) {
	cb := New(&Config{
		Name:             "test",
		FailureThreshold: 10,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		MaxConcurrent:    2,
	})

	// Start 2 long-running requests
	started := make(chan struct{})
	done := make(chan struct{})

	for i := 0; i < 2; i++ {
		go func() {
			cb.Execute(context.Background(), func() error {
				started <- struct{}{}
				<-done
				return nil
			})
		}()
	}

	// Wait for both to start
	<-started
	<-started

	// Third request should fail
	err := cb.Execute(context.Background(), func() error {
		return nil
	})

	if err == nil {
		t.Error("expected max concurrent error")
	}

	// Release the long-running requests
	close(done)
}

func TestManager_GetOrCreate(t *testing.T) {
	manager := NewManager(&Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	cb1 := manager.Get("service1")
	cb2 := manager.Get("service1")

	if cb1 != cb2 {
		t.Error("expected same circuit breaker for same service")
	}

	cb3 := manager.Get("service2")
	if cb1 == cb3 {
		t.Error("expected different circuit breaker for different service")
	}
}

func TestManager_Stats(t *testing.T) {
	manager := NewManager(DefaultConfig("default"))

	manager.Get("service1")
	manager.Get("service2")

	stats := manager.Stats()

	if len(stats) != 2 {
		t.Errorf("expected 2 circuit breakers, got %d", len(stats))
	}

	if _, ok := stats["service1"]; !ok {
		t.Error("expected stats for service1")
	}

	if _, ok := stats["service2"]; !ok {
		t.Error("expected stats for service2")
	}
}

func TestManager_Reset(t *testing.T) {
	manager := NewManager(&Config{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	})

	cb := manager.Get("service1")

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return testError
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected open state, got %v", cb.State())
	}

	// Reset all
	manager.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected closed state after reset, got %v", cb.State())
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}
