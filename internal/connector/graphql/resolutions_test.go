package graphql

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
)

// Fields in one query are resolved one after another, so a query for three
// things backed by three flows used to cost the sum of them: three fields over
// a backend answering in 100ms took 321ms. The work starts when a field
// registers and the value is waited for later, which is a window graphql-go
// gives — it completes a level before asking for the values it was handed.

func params(field string) graphql.ResolveParams {
	return graphql.ResolveParams{
		Info: graphql.ResolveInfo{FieldName: field},
	}
}

func TestIndependentFieldsOverlap(t *testing.T) {
	ctx := WithResolutions(context.Background())

	slow := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		time.Sleep(60 * time.Millisecond)
		return "done", nil
	}

	start := time.Now()
	thunks := []func() (interface{}, error){
		startResolution(ctx, params("a"), map[string]interface{}{}, slow),
		startResolution(ctx, params("b"), map[string]interface{}{}, slow),
		startResolution(ctx, params("c"), map[string]interface{}{}, slow),
	}
	for _, thunk := range thunks {
		if _, err := thunk(); err != nil {
			t.Fatalf("thunk: %v", err)
		}
	}
	elapsed := time.Since(start)

	// Three of sixty milliseconds, in series, is a hundred and eighty.
	if elapsed > 140*time.Millisecond {
		t.Errorf("three fields took %v, so they ran one after another", elapsed)
	}
}

func TestIdenticalResolutionsRunOnce(t *testing.T) {
	ctx := WithResolutions(context.Background())

	var calls int64
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		atomic.AddInt64(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return "value", nil
	}

	input := map[string]interface{}{"id": 7}
	first := startResolution(ctx, params("item"), input, handler)
	second := startResolution(ctx, params("item"), map[string]interface{}{"id": 7}, handler)

	a, err := first()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := second()
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if a != b {
		t.Errorf("the two resolutions differ: %v and %v", a, b)
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("the handler ran %d times for one question asked twice", got)
	}
}

func TestDifferentArgumentsAreDifferentResolutions(t *testing.T) {
	// Sharing is by what was asked, so two different questions must not
	// collapse into one answer.
	ctx := WithResolutions(context.Background())

	var calls int64
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		atomic.AddInt64(&calls, 1)
		return input["id"], nil
	}

	one := startResolution(ctx, params("item"), map[string]interface{}{"id": 1}, handler)
	two := startResolution(ctx, params("item"), map[string]interface{}{"id": 2}, handler)

	a, _ := one()
	b, _ := two()
	if a == b {
		t.Errorf("two different arguments produced the same answer: %v", a)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Errorf("the handler ran %d times, want one per distinct question", got)
	}
}

func TestTheSameFieldNameOnDifferentTypesIsNotShared(t *testing.T) {
	ctx := WithResolutions(context.Background())

	var calls int64
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		atomic.AddInt64(&calls, 1)
		return "v", nil
	}

	withParent := func(parent, field string) graphql.ResolveParams {
		return graphql.ResolveParams{Info: graphql.ResolveInfo{
			FieldName:  field,
			ParentType: graphql.NewObject(graphql.ObjectConfig{Name: parent, Fields: graphql.Fields{}}),
		}}
	}

	a := startResolution(ctx, withParent("Order", "items"), map[string]interface{}{}, handler)
	b := startResolution(ctx, withParent("Cart", "items"), map[string]interface{}{}, handler)
	a()
	b()

	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Errorf("the handler ran %d times, want one per field", got)
	}
}

func TestAnErrorReachesEveryWaiter(t *testing.T) {
	ctx := WithResolutions(context.Background())

	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, context.DeadlineExceeded
	}

	first := startResolution(ctx, params("item"), map[string]interface{}{}, handler)
	second := startResolution(ctx, params("item"), map[string]interface{}{}, handler)

	if _, err := first(); err == nil {
		t.Error("the error did not reach the first waiter")
	}
	if _, err := second(); err == nil {
		t.Error("the error did not reach the second waiter, which shared the run")
	}
}

func TestWithoutAResolutionSetTheHandlerStillRuns(t *testing.T) {
	// A subscription, or a schema built directly, has no request around it.
	var called bool
	thunk := startResolution(context.Background(), params("item"), map[string]interface{}{},
		func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			called = true
			return "v", nil
		})

	if _, err := thunk(); err != nil {
		t.Fatalf("thunk: %v", err)
	}
	if !called {
		t.Error("the handler never ran")
	}
}
