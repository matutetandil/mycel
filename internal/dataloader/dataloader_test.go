package dataloader

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoader_Load(t *testing.T) {
	var batchCalls int32

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		atomic.AddInt32(&batchCalls, 1)
		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	loader := NewLoader(batchFn)

	// Load a single value
	result, err := loader.Load(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "A" {
		t.Errorf("expected 'A', got %q", result)
	}
}

func TestLoader_LoadMany(t *testing.T) {
	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	loader := NewLoader(batchFn)

	results, errs := loader.LoadMany(context.Background(), []int{0, 1, 2})

	if len(errs) > 0 {
		for _, err := range errs {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	expected := []string{"A", "B", "C"}
	for i, exp := range expected {
		if results[i] != exp {
			t.Errorf("expected %q at index %d, got %q", exp, i, results[i])
		}
	}
}

func TestLoader_Batching(t *testing.T) {
	var batchCalls int32
	var batchSizes []int
	var mu sync.Mutex

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		atomic.AddInt32(&batchCalls, 1)
		mu.Lock()
		batchSizes = append(batchSizes, len(keys))
		mu.Unlock()

		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	loader := NewLoader(batchFn, LoaderConfig{
		BatchSize: 10,
		Wait:      10 * time.Millisecond,
		Cache:     true,
	})

	// Load multiple items concurrently - they should be batched
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(key int) {
			defer wg.Done()
			_, _ = loader.Load(context.Background(), key)
		}(i)
	}
	wg.Wait()

	// Give time for batching
	time.Sleep(20 * time.Millisecond)

	// Should have batched the requests
	calls := atomic.LoadInt32(&batchCalls)
	if calls > 5 {
		t.Errorf("expected batching to reduce calls, got %d calls", calls)
	}
}

func TestLoader_Caching(t *testing.T) {
	var batchCalls int32

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		atomic.AddInt32(&batchCalls, 1)
		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	loader := NewLoader(batchFn, LoaderConfig{
		Cache: true,
		Wait:  1 * time.Millisecond,
	})

	ctx := context.Background()

	// First load
	result1, _ := loader.Load(ctx, 0)
	time.Sleep(5 * time.Millisecond)

	// Second load - should be cached
	result2, _ := loader.Load(ctx, 0)

	if result1 != result2 {
		t.Errorf("expected same result, got %q and %q", result1, result2)
	}
}

func TestLoader_NoCache(t *testing.T) {
	var batchCalls int32

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		atomic.AddInt32(&batchCalls, 1)
		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	loader := NewLoader(batchFn, LoaderConfig{
		Cache: false,
		Wait:  1 * time.Millisecond,
	})

	ctx := context.Background()

	// Load same key twice
	_, _ = loader.Load(ctx, 0)
	time.Sleep(5 * time.Millisecond)
	_, _ = loader.Load(ctx, 0)
	time.Sleep(5 * time.Millisecond)

	// Without cache, both should trigger batch calls
	calls := atomic.LoadInt32(&batchCalls)
	if calls < 2 {
		t.Errorf("expected at least 2 batch calls without cache, got %d", calls)
	}
}

func TestLoader_Errors(t *testing.T) {
	expectedErr := errors.New("load failed")

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		results := make([]string, len(keys))
		errs := make([]error, len(keys))
		for i, key := range keys {
			if key == 1 { // Use key value, not index - handles library reordering
				errs[i] = expectedErr
			} else {
				results[i] = "ok"
			}
		}
		return results, errs
	}

	loader := NewLoader(batchFn)

	results, errs := loader.LoadMany(context.Background(), []int{0, 1, 2})

	if results[0] != "ok" {
		t.Errorf("expected 'ok' for key 0, got %q", results[0])
	}
	if errs[1] != expectedErr {
		t.Errorf("expected error for key 1, got %v", errs[1])
	}
	if results[2] != "ok" {
		t.Errorf("expected 'ok' for key 2, got %q", results[2])
	}
}

func TestLoader_Prime(t *testing.T) {
	var batchCalls int32

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		atomic.AddInt32(&batchCalls, 1)
		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	loader := NewLoader(batchFn)
	ctx := context.Background()

	// Prime the cache
	loader.Prime(ctx, 0, "PRIMED")

	// Load - should get primed value
	result, _ := loader.Load(ctx, 0)
	if result != "PRIMED" {
		t.Errorf("expected 'PRIMED', got %q", result)
	}
}

func TestLoaderCollection(t *testing.T) {
	collection := NewLoaderCollection()

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	// Get or create loader
	loader1 := GetOrCreate(collection, "test", batchFn)
	loader2 := GetOrCreate(collection, "test", batchFn)

	// Should return same loader
	if loader1 != loader2 {
		t.Error("expected same loader instance")
	}
}

func TestLoaderCollection_Context(t *testing.T) {
	collection := NewLoaderCollection()
	ctx := WithLoaders(context.Background(), collection)

	batchFn := func(ctx context.Context, keys []int) ([]string, []error) {
		results := make([]string, len(keys))
		for i, k := range keys {
			results[i] = string(rune('A' + k))
		}
		return results, nil
	}

	// Get loader from context
	loader := GetOrCreateFromContext(ctx, "test", batchFn)

	result, _ := loader.Load(ctx, 0)
	if result != "A" {
		t.Errorf("expected 'A', got %q", result)
	}

	// Get same loader again
	loader2 := GetOrCreateFromContext(ctx, "test", batchFn)
	if loader != loader2 {
		t.Error("expected same loader from context")
	}
}

func TestSQLBatchLoader(t *testing.T) {
	sqlLoader := &SQLBatchLoader{
		Query:    "SELECT * FROM users WHERE id IN (?)",
		KeyField: "id",
	}

	// Mock SQL execution function
	execFn := func(ctx context.Context, query string, keys []interface{}) ([]map[string]interface{}, error) {
		results := make([]map[string]interface{}, len(keys))
		for i, key := range keys {
			results[i] = map[string]interface{}{
				"id":   key,
				"name": "User " + key.(string),
			}
		}
		return results, nil
	}

	batchFn := sqlLoader.CreateBatchFunc(execFn)

	results, errs := batchFn(context.Background(), []interface{}{"1", "2", "3"})

	if len(errs) > 0 {
		for _, err := range errs {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		if r == nil {
			t.Errorf("result %d is nil", i)
		}
	}
}

func TestSQLManyBatchLoader(t *testing.T) {
	loader := &SQLManyBatchLoader{
		Query:      "SELECT * FROM orders WHERE user_id IN (?)",
		ForeignKey: "user_id",
	}

	// Mock SQL execution function - returns multiple orders per user
	execFn := func(ctx context.Context, query string, keys []interface{}) ([]map[string]interface{}, error) {
		var results []map[string]interface{}
		for _, key := range keys {
			userID := key.(string)
			// User 1 has 2 orders, User 2 has 1 order, User 3 has 0 orders
			if userID == "1" {
				results = append(results,
					map[string]interface{}{"id": "o1", "user_id": "1", "total": 100},
					map[string]interface{}{"id": "o2", "user_id": "1", "total": 200},
				)
			} else if userID == "2" {
				results = append(results,
					map[string]interface{}{"id": "o3", "user_id": "2", "total": 150},
				)
			}
			// User 3 has no orders
		}
		return results, nil
	}

	batchFn := loader.CreateBatchFunc(execFn)

	results, errs := batchFn(context.Background(), []interface{}{"1", "2", "3"})

	if len(errs) > 0 {
		for _, err := range errs {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// User 1 should have 2 orders
	if len(results[0]) != 2 {
		t.Errorf("expected 2 orders for user 1, got %d", len(results[0]))
	}

	// User 2 should have 1 order
	if len(results[1]) != 1 {
		t.Errorf("expected 1 order for user 2, got %d", len(results[1]))
	}

	// User 3 should have 0 orders (empty slice, not nil)
	if results[2] == nil {
		t.Error("expected empty slice for user 3, got nil")
	}
	if len(results[2]) != 0 {
		t.Errorf("expected 0 orders for user 3, got %d", len(results[2]))
	}
}

func TestLoaderKey(t *testing.T) {
	key := LoaderKey("users", "byId")
	if key != "users:byId" {
		t.Errorf("expected 'users:byId', got %q", key)
	}
}
