package cache

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// A cache is written as a connector like any other, and everything it needs is
// read out of loose properties: the address, the key prefix, the pool, and
// which of the three Redis topologies it is talking to. A property that is read
// wrongly here does not fail — it produces a cache that connects to the wrong
// place, or shares keys with a neighbour, or opens one connection where fifty
// were asked for.

func cacheConfig(driver string, props map[string]interface{}) *connector.Config {
	return &connector.Config{Name: "cache", Type: "cache", Driver: driver, Properties: props}
}

func TestTheFactoryClaimsOnlyCaches(t *testing.T) {
	f := NewFactory()
	if f.Type() != "cache" {
		t.Errorf("type = %q", f.Type())
	}
	for _, driver := range []string{"redis", "memory"} {
		if !f.Supports("cache", driver) {
			t.Errorf("a cache with driver %q is not claimed", driver)
		}
	}
	// A driver is required by the connector's schema, so the empty case is
	// refused before it reaches here rather than defaulting to one of them.
	if f.Supports("cache", "") {
		t.Error("a cache with no driver was claimed")
	}
	for _, connType := range []string{"database", "mq", "rest"} {
		if f.Supports(connType, "redis") {
			t.Errorf("%q was claimed by the cache factory", connType)
		}
	}
}

func TestAnAddressIsBuiltFromThePartsItIsGiven(t *testing.T) {
	// Someone writes host and port rather than a URL far more often than not,
	// and a wrong address here is a cache that never answers.
	for name, tc := range map[string]struct {
		props map[string]interface{}
		want  string
	}{
		"host alone takes the default port and database": {
			props: map[string]interface{}{"host": "redis.internal"},
			want:  "redis://redis.internal:6379/0",
		},
		"port and database as numbers": {
			props: map[string]interface{}{"host": "redis.internal", "port": 6380, "db": 3},
			want:  "redis://redis.internal:6380/3",
		},
		"port and database as strings, which is what env() returns": {
			props: map[string]interface{}{"host": "redis.internal", "port": "6380", "db": "3"},
			want:  "redis://redis.internal:6380/3",
		},
		"a password is carried in the address": {
			props: map[string]interface{}{"host": "redis.internal", "password": "s3cret"},
			want:  "redis://:s3cret@redis.internal:6379/0",
		},
		"nothing to build from": {
			props: map[string]interface{}{"port": 6379},
			want:  "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := buildRedisURL(tc.props); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAWrittenAddressWins(t *testing.T) {
	// Both forms may be present; the explicit one is the one that was meant.
	cfg := NewFactory().parseConfig(cacheConfig("redis", map[string]interface{}{
		"url":  "redis://elsewhere:6379/1",
		"host": "ignored",
	}))
	if cfg.URL != "redis://elsewhere:6379/1" {
		t.Errorf("url = %q, want the one that was written", cfg.URL)
	}
}

func TestEveryCacheSettingIsRead(t *testing.T) {
	cfg := NewFactory().parseConfig(cacheConfig("redis", map[string]interface{}{
		"mode":        "standalone",
		"url":         "redis://localhost:6379/0",
		"prefix":      "mercury",
		"max_items":   500,
		"eviction":    "lfu",
		"default_ttl": "10m",
		"pool": map[string]interface{}{
			"max_connections": 50,
			"min_idle":        5,
			"max_idle_time":   "5m",
			"connect_timeout": "2s",
		},
	}))

	if cfg.Prefix != "mercury" {
		t.Errorf("prefix = %q", cfg.Prefix)
	}
	if cfg.MaxItems != 500 {
		t.Errorf("max_items = %d", cfg.MaxItems)
	}
	if cfg.Eviction != "lfu" {
		t.Errorf("eviction = %q", cfg.Eviction)
	}
	if cfg.DefaultTTL != 10*time.Minute {
		t.Errorf("default_ttl = %v, want 10m", cfg.DefaultTTL)
	}
	if cfg.Pool.MaxConnections != 50 || cfg.Pool.MinIdle != 5 {
		t.Errorf("pool = %+v, want the connection counts that were written", cfg.Pool)
	}
	if cfg.Pool.MaxIdleTime != 5*time.Minute || cfg.Pool.ConnectTimeout != 2*time.Second {
		t.Errorf("pool timings = %+v", cfg.Pool)
	}
}

func TestADurationThatIsNotOneLeavesTheDefault(t *testing.T) {
	// Rather than starting with a nonsensical expiry — a cache whose entries
	// vanish immediately is worse than one with no expiry at all.
	cfg := NewFactory().parseConfig(cacheConfig("redis", map[string]interface{}{
		"url":         "redis://localhost:6379",
		"default_ttl": "ten minutes",
	}))
	if cfg.DefaultTTL != 0 {
		t.Errorf("default_ttl = %v, want it left unset", cfg.DefaultTTL)
	}
}

func TestTheClusterAndSentinelTopologiesAreRead(t *testing.T) {
	f := NewFactory()

	cluster := f.parseConfig(cacheConfig("redis", map[string]interface{}{
		"mode": "cluster",
		"cluster": map[string]interface{}{
			"nodes":         []interface{}{"node-1:6379", "node-2:6379"},
			"password":      "s3cret",
			"max_redirects": 5,
		},
	}))
	if cluster.Cluster == nil {
		t.Fatal("the cluster block was not read")
	}
	if len(cluster.Cluster.Nodes) != 2 {
		t.Errorf("nodes = %v, want both", cluster.Cluster.Nodes)
	}
	if cluster.Cluster.Password != "s3cret" {
		t.Errorf("password was not read")
	}

	sentinel := f.parseConfig(cacheConfig("redis", map[string]interface{}{
		"mode": "sentinel",
		"sentinel": map[string]interface{}{
			"master_name": "mymaster",
			"nodes":       []interface{}{"sentinel-1:26379"},
			"password":    "s3cret",
		},
	}))
	if sentinel.Sentinel == nil {
		t.Fatal("the sentinel block was not read")
	}
	if sentinel.Sentinel.MasterName != "mymaster" {
		t.Errorf("master_name = %q", sentinel.Sentinel.MasterName)
	}
	if len(sentinel.Sentinel.Nodes) != 1 {
		t.Errorf("nodes = %v", sentinel.Sentinel.Nodes)
	}
}

func TestATopologyMissingWhatItNeedsIsRefusedAtStartup(t *testing.T) {
	// Each of these would otherwise be discovered on the first cache read, in
	// the middle of serving a request, rather than when the service starts.
	f := NewFactory()
	ctx := context.Background()

	for name, tc := range map[string]struct {
		props map[string]interface{}
		want  string
	}{
		"standalone with no address": {
			props: map[string]interface{}{},
			want:  "url",
		},
		"cluster with no nodes": {
			props: map[string]interface{}{"mode": "cluster"},
			want:  "cluster.nodes",
		},
		"sentinel with no master": {
			props: map[string]interface{}{
				"mode":     "sentinel",
				"sentinel": map[string]interface{}{"nodes": []interface{}{"s:26379"}},
			},
			want: "master_name",
		},
		"sentinel with a master and no nodes": {
			props: map[string]interface{}{
				"mode":     "sentinel",
				"sentinel": map[string]interface{}{"master_name": "mymaster"},
			},
			want: "sentinel.nodes",
		},
		"a topology that does not exist": {
			props: map[string]interface{}{"mode": "replica"},
			want:  "standalone, cluster, sentinel",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.Create(ctx, cacheConfig("redis", tc.props))
			if err == nil {
				t.Fatal("an unusable cache configuration was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestAMemoryCacheGetsWorkingDefaults(t *testing.T) {
	// It is the driver someone reaches for first, usually with nothing beside
	// it, and a size of zero would make it a cache that never holds anything.
	conn, err := NewFactory().Create(context.Background(), cacheConfig("memory", nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if GetCache(conn) == nil {
		t.Fatal("the connector built for a cache is not one")
	}

	cfg := NewFactory().parseConfig(cacheConfig("memory", nil))
	if _, err := NewFactory().createMemory("cache", cfg); err != nil {
		t.Fatalf("createMemory: %v", err)
	}
	if cfg.MaxItems <= 0 {
		t.Errorf("max_items = %d, want a working default", cfg.MaxItems)
	}
	if cfg.Eviction == "" {
		t.Error("no eviction policy was chosen, so a full cache would have no rule for what to drop")
	}
}

func TestADriverThatIsNotACacheIsRefused(t *testing.T) {
	_, err := NewFactory().Create(context.Background(), cacheConfig("memcached", nil))
	if err == nil {
		t.Fatal("an unknown cache driver was accepted")
	}
	if !strings.Contains(err.Error(), "memcached") {
		t.Errorf("error = %q, want it to name the driver", err)
	}
}

func TestSomethingThatIsNotACacheIsNotOne(t *testing.T) {
	if GetCache(nil) != nil {
		t.Error("nil was reported as a cache")
	}
}
