package connector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestChainRequestResponse_SingleHandler(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return "result1", nil
	})
	// No chaining needed for single handler
	result, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "result1" {
		t.Fatalf("expected result1, got %v", result)
	}
}

func TestChainRequestResponse_TwoHandlers(t *testing.T) {
	var secondCalled atomic.Bool

	primary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return "primary-result", nil
	})
	secondary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		secondCalled.Store(true)
		return "secondary-result", nil
	})

	chained := ChainRequestResponse(primary, secondary, nil)
	result, err := chained(context.Background(), map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "primary-result" {
		t.Fatalf("expected primary-result, got %v", result)
	}

	// Wait for fire-and-forget goroutine
	time.Sleep(50 * time.Millisecond)
	if !secondCalled.Load() {
		t.Fatal("secondary handler was not called")
	}
}

func TestChainRequestResponse_ThreeHandlers(t *testing.T) {
	var callCount atomic.Int32

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return "h1-result", nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return "h2-result", nil
	})
	h3 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return "h3-result", nil
	})

	// Chain: h1 + h2, then (h1+h2) + h3
	chained := ChainRequestResponse(h1, h2, nil)
	chained = ChainRequestResponse(chained, h3, nil)

	result, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if result != "h1-result" {
		t.Fatalf("expected h1-result (first registered), got %v", result)
	}

	time.Sleep(100 * time.Millisecond)
	if callCount.Load() != 3 {
		t.Fatalf("expected all 3 handlers called, got %d", callCount.Load())
	}
}

func TestChainRequestResponse_SecondaryError(t *testing.T) {
	primary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return "ok", nil
	})
	secondary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("secondary failed")
	})

	// Secondary errors should not affect primary result
	chained := ChainRequestResponse(primary, secondary, nil)
	result, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("primary should succeed even if secondary fails: %v", err)
	}
	if result != "ok" {
		t.Fatalf("expected ok, got %v", result)
	}
}

func TestChainRequestResponse_InputIsolation(t *testing.T) {
	var secondaryInput map[string]interface{}
	var mu sync.Mutex

	primary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		// Mutate input
		input["mutated"] = true
		return "ok", nil
	})
	secondary := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		secondaryInput = input
		mu.Unlock()
		return nil, nil
	})

	chained := ChainRequestResponse(primary, secondary, nil)
	chained(context.Background(), map[string]interface{}{"key": "value"})

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if secondaryInput == nil {
		t.Fatal("secondary was not called")
	}
	// Secondary should get a copy, not see mutations from primary
	if _, ok := secondaryInput["mutated"]; ok {
		t.Fatal("secondary should not see mutations from primary's input")
	}
}

func TestChainEventDriven_TwoHandlers(t *testing.T) {
	var callOrder []string
	var mu sync.Mutex

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		callOrder = append(callOrder, "h1")
		mu.Unlock()
		return "h1-result", nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		callOrder = append(callOrder, "h2")
		mu.Unlock()
		return "h2-result", nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	result, err := chained(context.Background(), map[string]interface{}{"msg": "test"})
	if err != nil {
		t.Fatal(err)
	}
	// Result comes from the existing (first) handler
	if result != "h1-result" {
		t.Fatalf("expected h1-result, got %v", result)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(callOrder) != 2 {
		t.Fatalf("expected 2 handlers called, got %d", len(callOrder))
	}
}

func TestChainEventDriven_WaitsForAll(t *testing.T) {
	var completed atomic.Int32

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		completed.Add(1)
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		completed.Add(1)
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	_, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}

	// Both should be complete since ChainEventDriven waits
	if completed.Load() != 2 {
		t.Fatalf("expected 2 completed, got %d", completed.Load())
	}
}

func TestChainEventDriven_FirstError(t *testing.T) {
	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("h1 failed")
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	_, err := chained(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error from h1")
	}
	if err.Error() != "h1 failed" {
		t.Fatalf("expected 'h1 failed', got '%v'", err)
	}
}

func TestChainEventDriven_SecondError(t *testing.T) {
	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("h2 failed")
	})

	chained := ChainEventDriven(h1, h2, nil)
	_, err := chained(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error from h2")
	}
	if err.Error() != "h2 failed" {
		t.Fatalf("expected 'h2 failed', got '%v'", err)
	}
}

func TestChainEventDriven_ThreeHandlers(t *testing.T) {
	var callCount atomic.Int32

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return nil, nil
	})
	h3 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		callCount.Add(1)
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	chained = ChainEventDriven(chained, h3, nil)

	_, err := chained(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if callCount.Load() != 3 {
		t.Fatalf("expected 3 handlers called, got %d", callCount.Load())
	}
}

func TestChainEventDriven_InputIsolation(t *testing.T) {
	var h2Input map[string]interface{}
	var mu sync.Mutex

	h1 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		input["mutated"] = true
		return nil, nil
	})
	h2 := HandlerFunc(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		h2Input = input
		mu.Unlock()
		return nil, nil
	})

	chained := ChainEventDriven(h1, h2, nil)
	chained(context.Background(), map[string]interface{}{"key": "value"})

	mu.Lock()
	defer mu.Unlock()
	if h2Input == nil {
		t.Fatal("h2 was not called")
	}
	// h2 gets a copy, shouldn't see h1's mutations
	if _, ok := h2Input["mutated"]; ok {
		t.Fatal("h2 should not see mutations from h1's input")
	}
}

func TestCopyInput(t *testing.T) {
	original := map[string]interface{}{
		"name": "test",
		"age":  30,
	}
	copied := CopyInput(original)

	if copied["name"] != "test" || copied["age"] != 30 {
		t.Fatal("copy should have same values")
	}

	copied["name"] = "modified"
	if original["name"] != "test" {
		t.Fatal("modifying copy should not affect original")
	}
}

func TestCopyInput_Nil(t *testing.T) {
	if CopyInput(nil) != nil {
		t.Fatal("copy of nil should be nil")
	}
}
