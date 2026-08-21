package runtime

import (
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// A flow that says only which named cache to use.
//
// Writing that was refused until now — `storage` was a required argument on
// the cache block, so `cache { use = "cache.short" }` could not be written at
// all, which is why the cache example in this repository declares three named
// caches and references none of them. With the block writable, what has to
// hold is that the flow reads its settings from the named cache rather than
// running with none.
func TestAFlowTakesItsCacheSettingsFromTheNamedOne(t *testing.T) {
	h := &FlowHandler{
		Config: &flow.Config{
			Name:  "get_user",
			Cache: &flow.CacheConfig{Use: "short"},
		},
		NamedCaches: map[string]*flow.NamedCacheConfig{
			"short": {Name: "short", Storage: "memcache", TTL: "5m"},
		},
	}

	if got := h.getCacheTTL(); got != 5*time.Minute {
		t.Errorf("ttl = %v, want the named cache's", got)
	}
}

func TestAnOverrideBeatsTheNamedCache(t *testing.T) {
	h := &FlowHandler{
		Config: &flow.Config{
			Name:  "get_user",
			Cache: &flow.CacheConfig{Use: "short", TTL: "1h"},
		},
		NamedCaches: map[string]*flow.NamedCacheConfig{
			"short": {Name: "short", Storage: "memcache", TTL: "5m"},
		},
	}

	if got := h.getCacheTTL(); got != time.Hour {
		t.Errorf("ttl = %v, want the override", got)
	}
}

func TestAReferenceToACacheNobodyDeclaredHasNoTTL(t *testing.T) {
	// Rather than crashing on the lookup.
	h := &FlowHandler{
		Config: &flow.Config{
			Name:  "get_user",
			Cache: &flow.CacheConfig{Use: "nobody_declared_this"},
		},
		NamedCaches: map[string]*flow.NamedCacheConfig{},
	}

	if got := h.getCacheTTL(); got != 0 {
		t.Errorf("ttl = %v, want none", got)
	}
}

// The prefix on a named cache.
//
// It is the namespace everything in that cache shares, and it was applied only
// when the flow wrote no key of its own — so the moment two flows referenced
// one named cache and each named its own key, they shared its keyspace and the
// prefix did nothing. The field's comment, the documentation and the word
// itself all say "prepended to all cache keys".
func TestANamedCachesPrefixGoesOnEveryKey(t *testing.T) {
	named := map[string]*flow.NamedCacheConfig{
		"products": {Name: "products", Storage: "memcache", Prefix: "products"},
		"plain":    {Name: "plain", Storage: "memcache"},
	}

	for name, tc := range map[string]struct {
		cache *flow.CacheConfig
		input map[string]interface{}
		want  string
	}{
		"a key of the flow's own": {
			&flow.CacheConfig{Use: "products", Key: "item"},
			nil,
			"products:item",
		},
		"a key with something interpolated": {
			&flow.CacheConfig{Use: "products", Key: "item:${input.id}"},
			map[string]interface{}{"id": 7},
			"products:item:7",
		},
		"no key at all, so the flow name": {
			&flow.CacheConfig{Use: "products"},
			nil,
			"products:get_user",
		},
		"a named cache with no prefix": {
			&flow.CacheConfig{Use: "plain", Key: "item"},
			nil,
			"item",
		},
		"no named cache at all": {
			&flow.CacheConfig{Storage: "memcache", Key: "item"},
			nil,
			"item",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := &FlowHandler{
				Config:      &flow.Config{Name: "get_user", Cache: tc.cache},
				NamedCaches: named,
			}
			if got := h.buildCacheKey(tc.input); got != tc.want {
				t.Errorf("key = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTwoFlowsSharingANamedCacheDoNotShareItsKeys(t *testing.T) {
	// What the prefix is for. Without it these two differ only by the key each
	// happens to have chosen, and a name chosen twice is a flow reading
	// another flow's answer.
	named := map[string]*flow.NamedCacheConfig{
		"orders":   {Name: "orders", Storage: "memcache", Prefix: "orders"},
		"invoices": {Name: "invoices", Storage: "memcache", Prefix: "invoices"},
	}

	one := &FlowHandler{
		Config:      &flow.Config{Name: "list", Cache: &flow.CacheConfig{Use: "orders", Key: "all"}},
		NamedCaches: named,
	}
	two := &FlowHandler{
		Config:      &flow.Config{Name: "list", Cache: &flow.CacheConfig{Use: "invoices", Key: "all"}},
		NamedCaches: named,
	}

	if one.buildCacheKey(nil) == two.buildCacheKey(nil) {
		t.Errorf("both flows cache under %q, so one reads the other's answer", one.buildCacheKey(nil))
	}
}
