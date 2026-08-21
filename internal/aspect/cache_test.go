package aspect

import (
	"context"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/memory"
	cachetypes "github.com/matutetandil/mycel/v3/internal/connector/cache/types"
)

// An aspect that caches has to actually cache.
//
// It asked the connector to be a Reader and a Writer. A cache connector is
// neither — it has Get, Set and Delete — so the type assertions found nothing
// and the block read nothing, stored nothing and invalidated nothing, quietly,
// while looking exactly like the flow-level cache that does work. An aspect
// declared to spare an expensive call made every one of them.
func executorWithCache(t *testing.T) (*Executor, *memory.Connector) {
	t.Helper()

	store := memory.New("cache", &cachetypes.Config{})
	if err := store.Connect(context.Background()); err != nil {
		t.Fatalf("cache connect: %v", err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })

	connectors := connector.NewRegistry()
	connectors.Replace("cache", store)

	e, err := NewExecutor(NewRegistry(), connectors)
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return e, store
}

func TestAnAspectThatCachesSparesTheSecondCall(t *testing.T) {
	e, _ := executorWithCache(t)
	ctx := context.Background()

	calls := 0
	flow := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		calls++
		return &connector.Result{
			Rows:     []map[string]interface{}{{"id": "p1", "name": "Widget"}},
			Affected: 1,
		}, nil
	}

	config := &CacheConfig{Storage: "cache", Key: "product:p1", TTL: "1m"}
	input := map[string]interface{}{"id": "p1"}

	first, err := e.executeCache(ctx, config, input, flow)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(first.Rows) != 1 || first.Rows[0]["name"] != "Widget" {
		t.Fatalf("first answer = %#v", first.Rows)
	}
	if calls != 1 {
		t.Fatalf("the flow ran %d times on the first call", calls)
	}

	second, err := e.executeCache(ctx, config, input, flow)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls != 1 {
		t.Errorf("the flow ran again: %d calls, want the cached answer", calls)
	}
	if len(second.Rows) != 1 || second.Rows[0]["name"] != "Widget" {
		t.Errorf("the cached answer came back as %#v", second.Rows)
	}
}

// And an aspect that invalidates puts the next call back on the flow.
func TestInvalidatingPutsTheNextCallBackOnTheFlow(t *testing.T) {
	e, _ := executorWithCache(t)
	ctx := context.Background()

	calls := 0
	flow := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		calls++
		return &connector.Result{Rows: []map[string]interface{}{{"id": "p1"}}, Affected: 1}, nil
	}

	config := &CacheConfig{Storage: "cache", Key: "product:p1", TTL: "1m"}
	input := map[string]interface{}{"id": "p1"}

	if _, err := e.executeCache(ctx, config, input, flow); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := e.executeCache(ctx, config, input, flow); err != nil {
		t.Fatalf("second: %v", err)
	}
	if calls != 1 {
		t.Fatalf("the cache did not hold: %d calls", calls)
	}

	if err := e.executeInvalidate(ctx, &InvalidateConfig{
		Storage: "cache",
		Keys:    []string{"product:p1"},
	}, input, nil); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	if _, err := e.executeCache(ctx, config, input, flow); err != nil {
		t.Fatalf("after invalidating: %v", err)
	}
	if calls != 2 {
		t.Errorf("the entry was invalidated and the flow still did not run: %d calls", calls)
	}
}

// A pattern clears everything under it, which is what invalidating a whole
// collection after a write means.
func TestInvalidatingAPatternClearsWhatIsUnderIt(t *testing.T) {
	e, _ := executorWithCache(t)
	ctx := context.Background()

	served := map[string]int{}
	for _, id := range []string{"p1", "p2"} {
		id := id
		flow := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
			served[id]++
			return &connector.Result{Rows: []map[string]interface{}{{"id": id}}, Affected: 1}, nil
		}
		config := &CacheConfig{Storage: "cache", Key: "product:" + id, TTL: "1m"}
		for i := 0; i < 2; i++ {
			if _, err := e.executeCache(ctx, config, map[string]interface{}{}, flow); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
		}
		if served[id] != 1 {
			t.Fatalf("%s was served %d times, so it was not cached", id, served[id])
		}
	}

	if err := e.executeInvalidate(ctx, &InvalidateConfig{
		Storage:  "cache",
		Patterns: []string{"product:*"},
	}, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("invalidate by pattern: %v", err)
	}

	for _, id := range []string{"p1", "p2"} {
		id := id
		flow := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
			served[id]++
			return &connector.Result{Rows: []map[string]interface{}{{"id": id}}, Affected: 1}, nil
		}
		config := &CacheConfig{Storage: "cache", Key: "product:" + id, TTL: "1m"}
		if _, err := e.executeCache(ctx, config, map[string]interface{}{}, flow); err != nil {
			t.Fatalf("%s after the pattern: %v", id, err)
		}
		if served[id] != 2 {
			t.Errorf("%s was not cleared by the pattern", id)
		}
	}
}

// A connector that is not a cache, and one that is not there at all: the flow
// still runs, because caching is an optimisation and losing it is not a reason
// to refuse a request.
func TestAStorageThatCannotCacheStillLetsTheFlowRun(t *testing.T) {
	e, _ := executorWithCache(t)
	ctx := context.Background()

	calls := 0
	flow := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		calls++
		return &connector.Result{Rows: []map[string]interface{}{{"id": "p1"}}}, nil
	}

	if _, err := e.executeCache(ctx, &CacheConfig{
		Storage: "nothing_registered", Key: "k", TTL: "1m",
	}, map[string]interface{}{}, flow); err != nil {
		t.Fatalf("with a missing connector: %v", err)
	}
	if calls != 1 {
		t.Errorf("the flow did not run when the cache was missing")
	}

	// Invalidating against one that is not there says so, since that is a
	// write path and silence would hide a stale entry forever.
	if err := e.executeInvalidate(ctx, &InvalidateConfig{
		Storage: "nothing_registered", Keys: []string{"k"},
	}, map[string]interface{}{}, nil); err == nil {
		t.Error("invalidating against a connector that is not there was reported as done")
	}
}
