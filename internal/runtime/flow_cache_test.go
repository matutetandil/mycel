package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
	cachepkg "github.com/matutetandil/mycel/v2/internal/connector/cache"
	"github.com/matutetandil/mycel/v2/internal/flow"
)

// A flow's own cache block is the one way a cache is reached today, and every
// part of it fails quietly: a storage name that resolves to nothing means every
// request does the work while the configuration says otherwise, and a lifetime
// that does not parse means entries that never expire or never survive.

func memoryCache(t *testing.T, name string) connector.Connector {
	t.Helper()
	conn, err := cachepkg.NewFactory().Create(context.Background(), &connector.Config{
		Name: name, Type: "cache", Driver: "memory",
	})
	if err != nil {
		t.Fatalf("building the cache: %v", err)
	}
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func cacheHandler(t *testing.T, cfg *flow.Config, named map[string]*flow.NamedCacheConfig, conns map[string]connector.Connector) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	for name, conn := range conns {
		registry.Replace(name, conn)
	}
	return &FlowHandler{
		Config:      cfg,
		Connectors:  registry,
		NamedCaches: named,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestSomethingStoredIsFoundAgain(t *testing.T) {
	h := cacheHandler(t,
		&flow.Config{Name: "get_product", Cache: &flow.CacheConfig{Storage: "cache", TTL: "5m"}},
		nil,
		map[string]connector.Connector{"cache": memoryCache(t, "cache")})
	ctx := context.Background()

	if _, found, err := h.checkCache(ctx, "product:1"); err != nil || found {
		t.Fatalf("an empty cache reported a hit: found=%v err=%v", found, err)
	}

	value := map[string]interface{}{"id": "1", "name": "a product", "price": 10.5}
	if err := h.storeInCache(ctx, "product:1", value); err != nil {
		t.Fatalf("storeInCache: %v", err)
	}

	got, found, err := h.checkCache(ctx, "product:1")
	if err != nil {
		t.Fatalf("checkCache: %v", err)
	}
	if !found {
		t.Fatal("what was just stored was not found")
	}
	stored, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("the entry came back as %T", got)
	}
	if stored["name"] != "a product" || stored["price"] != 10.5 {
		t.Errorf("the entry came back changed: %v", stored)
	}
}

func TestAFlowWithNoCacheBlockNeverHits(t *testing.T) {
	// Every request has to do the work, and storing has to be harmless rather
	// than an error, since the same code path runs either way.
	h := cacheHandler(t, &flow.Config{Name: "get_product"}, nil, nil)
	ctx := context.Background()

	if err := h.storeInCache(ctx, "k", map[string]interface{}{"a": 1}); err != nil {
		t.Errorf("storing with no cache configured: %v", err)
	}
	if _, found, err := h.checkCache(ctx, "k"); err != nil || found {
		t.Errorf("a flow with no cache reported found=%v err=%v", found, err)
	}
}

func TestACacheNamingStorageThatDoesNotExistIsNotACache(t *testing.T) {
	// It falls through and does the work rather than failing the request — but
	// it must not report a hit, which would answer with nothing.
	h := cacheHandler(t,
		&flow.Config{Name: "get_product", Cache: &flow.CacheConfig{Storage: "typo", TTL: "5m"}},
		nil, map[string]connector.Connector{"cache": memoryCache(t, "cache")})

	if got := h.getCacheConnector(); got != nil {
		t.Error("a storage name that resolves to nothing produced a cache")
	}
	if _, found, _ := h.checkCache(context.Background(), "k"); found {
		t.Error("a cache that does not exist reported a hit")
	}
}

func TestAConnectorThatIsNotACacheIsNotUsedAsOne(t *testing.T) {
	// Pointing storage at a database rather than a cache is an easy mistake and
	// must not end in a type assertion deciding the behaviour silently.
	registry := connector.NewRegistry()
	registry.Replace("not_a_cache", &stepConnector{name: "not_a_cache"})
	h := &FlowHandler{
		Config:     &flow.Config{Name: "f", Cache: &flow.CacheConfig{Storage: "not_a_cache"}},
		Connectors: registry,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if got := h.getCacheConnector(); got != nil {
		t.Error("a connector that is not a cache was used as one")
	}
}

func TestACacheCanBeDeclaredOnceAndReferredToByName(t *testing.T) {
	// The recommended shape: one declaration, many flows pointing at it.
	h := cacheHandler(t,
		&flow.Config{Name: "get_product", Cache: &flow.CacheConfig{Use: "products"}},
		map[string]*flow.NamedCacheConfig{
			"products": {Name: "products", Storage: "cache", TTL: "10m"},
		},
		map[string]connector.Connector{"cache": memoryCache(t, "cache")})

	if h.getCacheConnector() == nil {
		t.Fatal("the named cache did not resolve to its storage")
	}
	if got := h.getCacheTTL(); got != 10*time.Minute {
		t.Errorf("ttl = %v, want the named cache's 10m", got)
	}
	if got := h.cacheMetricName(); got != "cache" {
		t.Errorf("metric label = %q, want the storage name", got)
	}
}

func TestAFlowCanOverrideTheLifetimeItInherits(t *testing.T) {
	h := cacheHandler(t,
		&flow.Config{Name: "get_price", Cache: &flow.CacheConfig{Use: "products", TTL: "30s"}},
		map[string]*flow.NamedCacheConfig{
			"products": {Name: "products", Storage: "cache", TTL: "10m"},
		},
		map[string]connector.Connector{"cache": memoryCache(t, "cache")})

	if got := h.getCacheTTL(); got != 30*time.Second {
		t.Errorf("ttl = %v, want the flow's own 30s over the named 10m", got)
	}
}

func TestALifetimeThatIsNotOneLeavesItToTheConnector(t *testing.T) {
	// Rather than a nonsensical duration, which would either expire entries
	// immediately or keep them for ever depending on how it failed to parse.
	h := cacheHandler(t,
		&flow.Config{Name: "f", Cache: &flow.CacheConfig{Storage: "cache", TTL: "five minutes"}},
		nil, map[string]connector.Connector{"cache": memoryCache(t, "cache")})

	if got := h.getCacheTTL(); got != 0 {
		t.Errorf("ttl = %v, want it left to the connector's own default", got)
	}
}

func TestTheMetricLabelIsBoundedRatherThanPerKey(t *testing.T) {
	// The cache key is evaluated per message, so labelling metrics with it
	// would create a new time series per request — the classic way to bring
	// down a metrics backend.
	for name, h := range map[string]*FlowHandler{
		"a storage name": cacheHandler(t,
			&flow.Config{Name: "get_product", Cache: &flow.CacheConfig{Storage: "redis_cache"}}, nil, nil),
		"no cache at all": cacheHandler(t, &flow.Config{Name: "get_product"}, nil, nil),
		"a named cache that does not resolve": cacheHandler(t,
			&flow.Config{Name: "get_product", Cache: &flow.CacheConfig{Use: "absent"}}, nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			label := h.cacheMetricName()
			if label == "" {
				t.Error("the metric has no label")
			}
			if label != "redis_cache" && label != "get_product" {
				t.Errorf("label = %q, want the storage or the flow name", label)
			}
		})
	}
}

func TestAnEntryOutlivesNothingWhenItsTimeIsUp(t *testing.T) {
	conn := memoryCache(t, "cache")
	h := cacheHandler(t,
		&flow.Config{Name: "f", Cache: &flow.CacheConfig{Storage: "cache", TTL: "50ms"}},
		nil, map[string]connector.Connector{"cache": conn})
	ctx := context.Background()

	if err := h.storeInCache(ctx, "k", map[string]interface{}{"a": 1}); err != nil {
		t.Fatalf("storeInCache: %v", err)
	}
	if _, found, _ := h.checkCache(ctx, "k"); !found {
		t.Fatal("the entry was not there immediately after being stored")
	}

	time.Sleep(120 * time.Millisecond)
	if _, found, _ := h.checkCache(ctx, "k"); found {
		t.Error("the entry outlived the lifetime it was stored with")
	}
}

// Invalidation is what keeps a cache from being wrong: the write that changes a
// record has to clear the entries derived from it, in the same flow, or the
// next read serves what was true a moment ago.

func TestAWriteClearsTheEntriesItInvalidates(t *testing.T) {
	conn := memoryCache(t, "cache")
	h := cacheHandler(t, &flow.Config{
		Name:  "update_product",
		Cache: &flow.CacheConfig{Storage: "cache"},
		After: &flow.AfterConfig{Invalidate: &flow.InvalidateConfig{
			Keys: []string{"product:${input.id}", "product:${input.id}:price"},
		}},
	}, nil, map[string]connector.Connector{"cache": conn})
	ctx := context.Background()

	for _, key := range []string{"product:1", "product:1:price", "product:2"} {
		if err := h.storeInCache(ctx, key, map[string]interface{}{"k": key}); err != nil {
			t.Fatalf("storeInCache %s: %v", key, err)
		}
	}

	if err := h.executeInvalidation(ctx, map[string]interface{}{"id": "1"}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}

	for key, want := range map[string]bool{
		"product:1":       false,
		"product:1:price": false,
		"product:2":       true,
	} {
		_, found, _ := h.checkCache(ctx, key)
		if found != want {
			t.Errorf("%s present = %v, want %v", key, found, want)
		}
	}
}

func TestInvalidationCanClearAFamilyOfEntries(t *testing.T) {
	conn := memoryCache(t, "cache")
	h := cacheHandler(t, &flow.Config{
		Name:  "update_product",
		Cache: &flow.CacheConfig{Storage: "cache"},
		After: &flow.AfterConfig{Invalidate: &flow.InvalidateConfig{
			Patterns: []string{"product:${input.id}:*"},
		}},
	}, nil, map[string]connector.Connector{"cache": conn})
	ctx := context.Background()

	for _, key := range []string{"product:1:detail", "product:1:price", "product:2:detail"} {
		if err := h.storeInCache(ctx, key, map[string]interface{}{"k": key}); err != nil {
			t.Fatalf("storeInCache: %v", err)
		}
	}

	if err := h.executeInvalidation(ctx, map[string]interface{}{"id": "1"}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}

	for key, want := range map[string]bool{
		"product:1:detail": false,
		"product:1:price":  false,
		"product:2:detail": true,
	} {
		_, found, _ := h.checkCache(ctx, key)
		if found != want {
			t.Errorf("%s present = %v, want %v", key, found, want)
		}
	}
}

func TestInvalidationCanNameACacheOtherThanTheFlowsOwn(t *testing.T) {
	// A flow that writes need not be one that reads: the entries it invalidates
	// often belong to a different flow's cache.
	own := memoryCache(t, "own")
	other := memoryCache(t, "other")
	h := cacheHandler(t, &flow.Config{
		Name:  "update_product",
		Cache: &flow.CacheConfig{Storage: "own"},
		After: &flow.AfterConfig{Invalidate: &flow.InvalidateConfig{
			Storage: "other", Keys: []string{"product:1"},
		}},
	}, nil, map[string]connector.Connector{"own": own, "other": other})
	ctx := context.Background()

	if err := h.storeInCache(ctx, "product:1", map[string]interface{}{"a": 1}); err != nil {
		t.Fatalf("storeInCache: %v", err)
	}
	if err := cachepkg.GetCache(other).Set(ctx, "product:1", []byte(`{"a":1}`), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := h.executeInvalidation(ctx, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}

	if exists, _ := cachepkg.GetCache(other).Exists(ctx, "product:1"); exists {
		t.Error("the named cache's entry was not cleared")
	}
	if _, found, _ := h.checkCache(ctx, "product:1"); !found {
		t.Error("the flow's own cache was cleared although another was named")
	}
}

func TestInvalidationNamingACacheThatDoesNotExistIsReported(t *testing.T) {
	h := cacheHandler(t, &flow.Config{
		Name: "update_product",
		After: &flow.AfterConfig{Invalidate: &flow.InvalidateConfig{
			Storage: "typo", Keys: []string{"k"},
		}},
	}, nil, map[string]connector.Connector{"cache": memoryCache(t, "cache")})

	if err := h.executeInvalidation(context.Background(), map[string]interface{}{}, nil); err == nil {
		t.Error("invalidation against a cache that does not exist reported success")
	}
}

func TestAFlowWithNothingToInvalidateDoesNothing(t *testing.T) {
	h := cacheHandler(t, &flow.Config{Name: "f"}, nil, nil)
	if err := h.executeInvalidation(context.Background(), map[string]interface{}{}, nil); err != nil {
		t.Errorf("a flow with no invalidation rules: %v", err)
	}
}

// The cache connector's own configuration has to survive the trip through the
// flow, since a prefix is what keeps two services sharing one Redis apart.
func TestTheCachesOwnPrefixStillApplies(t *testing.T) {
	conn, err := cachepkg.NewFactory().Create(context.Background(), &connector.Config{
		Name: "cache", Type: "cache", Driver: "memory",
		Properties: map[string]interface{}{"prefix": "mercury"},
	})
	if err != nil {
		t.Fatalf("building the cache: %v", err)
	}
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	h := cacheHandler(t,
		&flow.Config{Name: "f", Cache: &flow.CacheConfig{Storage: "cache", TTL: "5m"}},
		nil, map[string]connector.Connector{"cache": conn})
	ctx := context.Background()

	if err := h.storeInCache(ctx, "product:1", map[string]interface{}{"a": 1}); err != nil {
		t.Fatalf("storeInCache: %v", err)
	}
	if _, found, _ := h.checkCache(ctx, "product:1"); !found {
		t.Error("a prefixed cache stored an entry it could not find again")
	}

	// The flow hands the connector an unprefixed key and the connector applies
	// its own prefix on both sides, so the flow never sees it — asking the
	// connector for the prefixed key would prefix it a second time.
	if _, found, _ := cachepkg.GetCache(conn).Get(ctx, "mercury:product:1"); found {
		t.Error("the prefix was applied by the flow rather than by the connector")
	}
}
