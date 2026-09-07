package aspect

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// An aspect's cache block derives its key the same way a flow's does.
func TestAnAspectsCacheKeyCanBeDerivedFromAList(t *testing.T) {
	e, store := executorWithCache(t)
	ctx := context.Background()

	calls := 0
	flow := func(ctx context.Context, input map[string]interface{}) (*connector.Result, error) {
		calls++
		return &connector.Result{Rows: []map[string]interface{}{{"n": 2}}, Affected: 1}, nil
	}

	config := &CacheConfig{
		Storage: "cache", TTL: "1m",
		KeyFrom: "'ids:' + hash_sha256(join(as_list(input.ids).map(i, string(i)), ','))",
	}
	input := map[string]interface{}{"ids": []interface{}{3, 1, 2}}

	if _, err := e.executeCache(ctx, config, input, flow); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := e.executeCache(ctx, config, input, flow); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls != 1 {
		t.Errorf("the flow ran %d times, want the cached answer the second time", calls)
	}

	// The entry is under the derived key, not under Go's rendering of the list.
	keys := store.Keys()
	if len(keys) != 1 || !strings.HasPrefix(keys[0], "ids:") || strings.Contains(keys[0], "[") {
		t.Errorf("stored keys = %v", keys)
	}

	// A key_from that does not yield a string fails rather than caching under
	// a literal.
	bad := &CacheConfig{Storage: "cache", TTL: "1m", KeyFrom: "as_list(input.ids)"}
	if _, err := e.executeCache(ctx, bad, input, flow); err == nil || !strings.Contains(err.Error(), "key_from") {
		t.Errorf("a list-valued key_from: %v", err)
	}
}
