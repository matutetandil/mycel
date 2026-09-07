package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A cache key derived from a list or a map.
//
// A key is a `${...}` template of scalars. When what identifies a request is
// a list — a faceted listing's `filter: [{code, value}, ...]` — there was
// nothing to write: `${input.filter}` rendered Go's own syntax into the key,
// and the canonical form the key needs (sorted, joined, hashed) could not be
// expressed anywhere before the lookup ran. `keys_from` already takes CEL for
// the same reason on the invalidation side; `key_from` is its read-side twin.

const galleryKey = `'gallery:' + (input.store ?? 'default') + ':' + ` +
	`hash_sha256(join(as_list(input.filter).map(f, f.attribute_code + '=' + f.value), '|'))`

func keyFromHandler(t *testing.T, cache *flow.CacheConfig, named map[string]*flow.NamedCacheConfig) *FlowHandler {
	t.Helper()
	h := cacheHandler(t, &flow.Config{Name: "gallery", Cache: cache}, named,
		map[string]connector.Connector{"cache": memoryCache(t, "cache")})
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatal(err)
	}
	h.Transformer = tr
	return h
}

func filters(pairs ...string) []interface{} {
	out := make([]interface{}, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, map[string]interface{}{"attribute_code": pairs[i], "value": pairs[i+1]})
	}
	return out
}

func TestACacheKeyCanBeDerivedFromAList(t *testing.T) {
	h := keyFromHandler(t, &flow.CacheConfig{Storage: "cache", TTL: "5m", KeyFrom: galleryKey}, nil)
	ctx := context.Background()

	bath := map[string]interface{}{"store": "au", "filter": filters("room", "Bath", "color", "White")}
	key, err := h.cacheKey(ctx, bath)
	if err != nil {
		t.Fatalf("cacheKey: %v", err)
	}
	if !strings.HasPrefix(key, "gallery:au:") || strings.Contains(key, "map[") || len(key) < len("gallery:au:")+64 {
		t.Errorf("key = %q, want the store and a sha256 of the pairs", key)
	}

	// The same request is the same key; a different set is a different key.
	again, _ := h.cacheKey(ctx, map[string]interface{}{"store": "au", "filter": filters("room", "Bath", "color", "White")})
	if again != key {
		t.Errorf("the same filter set produced two keys:\n  %s\n  %s", key, again)
	}
	other, _ := h.cacheKey(ctx, map[string]interface{}{"store": "au", "filter": filters("room", "Kitchen")})
	if other == key {
		t.Errorf("two different filter sets share the key %q", key)
	}

	// And the key is what the lookup uses.
	if err := h.storeInCache(ctx, key, map[string]interface{}{"items": 3}); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := h.checkCache(ctx, key); !hit {
		t.Error("what was stored under the derived key was not found under it")
	}
}

func TestAKeyFromThatDoesNotYieldAStringFailsTheRequest(t *testing.T) {
	for name, expr := range map[string]string{
		"a list":        "as_list(input.filter)",
		"a number":      "size(as_list(input.filter))",
		"empty text":    "''",
		"a bad express": "input.filter.nope(",
	} {
		t.Run(name, func(t *testing.T) {
			h := keyFromHandler(t, &flow.CacheConfig{Storage: "cache", TTL: "5m", KeyFrom: expr}, nil)
			key, err := h.cacheKey(context.Background(), map[string]interface{}{"filter": filters("room", "Bath")})
			if err == nil {
				t.Fatalf("key_from %q was accepted and produced %q", expr, key)
			}
			for _, want := range []string{"gallery", "key_from"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q: %v", want, err)
				}
			}
		})
	}
}

func TestANamedCachesPrefixGoesOnADerivedKeyToo(t *testing.T) {
	h := keyFromHandler(t,
		&flow.CacheConfig{Use: "catalog", KeyFrom: "'gallery:' + input.store"},
		map[string]*flow.NamedCacheConfig{"catalog": {Name: "catalog", Storage: "cache", Prefix: "shop"}})
	key, err := h.cacheKey(context.Background(), map[string]interface{}{"store": "au"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "shop:gallery:au" {
		t.Errorf("key = %q, want the named cache's prefix in front", key)
	}
}

func TestATemplateKeyStillWorksBesideKeyFrom(t *testing.T) {
	// The template form is untouched: a flow that has one keeps it.
	h := keyFromHandler(t, &flow.CacheConfig{Storage: "cache", Key: "product:${input.id}"}, nil)
	key, err := h.cacheKey(context.Background(), map[string]interface{}{"id": 42})
	if err != nil || key != "product:42" {
		t.Errorf("key = %q, err = %v", key, err)
	}
}

func TestValidateRefusesAKeyFromWrittenAsATemplate(t *testing.T) {
	cfg := &parser.Configuration{Flows: []*flow.Config{{
		Name:  "gallery",
		Cache: &flow.CacheConfig{Storage: "cache", KeyFrom: "gallery:${input.store}"},
	}}}
	errs := ValidateCacheKeys(cfg)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one for the key_from", errs)
	}
	for _, want := range []string{"gallery", "key_from", "${", "CEL"} {
		if !strings.Contains(errs[0].Error(), want) {
			t.Errorf("error does not mention %q: %v", want, errs[0])
		}
	}

	// And both at once is refused, whichever way the configuration was built.
	both := &parser.Configuration{Flows: []*flow.Config{{
		Name:  "gallery",
		Cache: &flow.CacheConfig{Storage: "cache", Key: "g:${input.store}", KeyFrom: "'g:' + input.store"},
	}}}
	if errs := ValidateCacheKeys(both); len(errs) != 1 || !strings.Contains(errs[0].Error(), "key_from") {
		t.Errorf("key and key_from together: errors = %v", errs)
	}
}
