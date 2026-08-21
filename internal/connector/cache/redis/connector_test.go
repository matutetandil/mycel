package redis

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
	goredis "github.com/redis/go-redis/v9"
)

// Several of these point the client at somewhere nothing is listening, which is
// the case being tested; the driver's own retry logging would otherwise bury
// the test output in dial failures that are the expected result.
func TestMain(m *testing.M) {
	goredis.SetLogger(discardLogger{})
	os.Exit(m.Run())
}

type discardLogger struct{}

func (discardLogger) Printf(context.Context, string, ...interface{}) {}

// The cache connector is the one a service leans on hardest — it is read on
// nearly every request — and every one of its operations has a silent failure
// mode. A prefix that is applied on write and not on read is a cache that never
// hits. A delete that misses is stale data served indefinitely. None of that
// shows up as an error anywhere.

func testConnector(t *testing.T, config *types.Config) (*Connector, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)

	if config == nil {
		config = &types.Config{}
	}
	config.URL = "redis://" + server.Addr()

	c := New("cache", config)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c, server
}

func TestAValueComesBackAsItWasStored(t *testing.T) {
	c, _ := testConnector(t, nil)
	ctx := context.Background()

	if err := c.Set(ctx, "customer:1", []byte(`{"id":"1"}`), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	value, found, err := c.Get(ctx, "customer:1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("the value that was just written was not found")
	}
	if string(value) != `{"id":"1"}` {
		t.Errorf("value = %q", value)
	}
}

func TestAMissingKeyIsNotAnError(t *testing.T) {
	// This is the distinction the whole cache rests on: a miss is the ordinary
	// case and has to be told apart from a cache that is broken, or every miss
	// becomes a failed request.
	c, _ := testConnector(t, nil)

	value, found, err := c.Get(context.Background(), "never-written")
	if err != nil {
		t.Fatalf("a miss came back as an error: %v", err)
	}
	if found {
		t.Error("a key that was never written was found")
	}
	if value != nil {
		t.Errorf("value = %q, want nothing", value)
	}
}

func TestThePrefixIsAppliedOnBothSides(t *testing.T) {
	// A prefix is how two services share one Redis without colliding. Applied
	// on write but not on read, every lookup misses and the cache does nothing
	// at all — while appearing to work.
	c, server := testConnector(t, &types.Config{Prefix: "mercury"})
	ctx := context.Background()

	if err := c.Set(ctx, "customer:1", []byte("value"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, err := server.Get("mercury:customer:1"); err != nil {
		t.Errorf("the key was not written under the prefix: %v", err)
	}
	if _, err := server.Get("customer:1"); err == nil {
		t.Error("the key was written without the prefix as well")
	}

	if _, found, err := c.Get(ctx, "customer:1"); err != nil || !found {
		t.Errorf("the prefixed key was not found on read: found=%v err=%v", found, err)
	}

	// And every other operation has to agree with it.
	if exists, err := c.Exists(ctx, "customer:1"); err != nil || !exists {
		t.Errorf("Exists disagrees with the prefix: exists=%v err=%v", exists, err)
	}
	if err := c.Delete(ctx, "customer:1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if exists, _ := c.Exists(ctx, "customer:1"); exists {
		t.Error("Delete did not remove the prefixed key")
	}
}

func TestTwoServicesWithDifferentPrefixesDoNotSeeEachOther(t *testing.T) {
	server := miniredis.RunT(t)
	ctx := context.Background()

	one := New("one", &types.Config{URL: "redis://" + server.Addr(), Prefix: "svc-a"})
	two := New("two", &types.Config{URL: "redis://" + server.Addr(), Prefix: "svc-b"})
	for _, c := range []*Connector{one, two} {
		if err := c.Connect(ctx); err != nil {
			t.Fatalf("Connect: %v", err)
		}
		defer func(c *Connector) { _ = c.Close(ctx) }(c)
	}

	if err := one.Set(ctx, "shared", []byte("a"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := two.Set(ctx, "shared", []byte("b"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	valueOne, _, _ := one.Get(ctx, "shared")
	valueTwo, _, _ := two.Get(ctx, "shared")
	if string(valueOne) != "a" || string(valueTwo) != "b" {
		t.Errorf("one=%q two=%q, want each service to see only its own", valueOne, valueTwo)
	}
}

func TestAnEntryExpires(t *testing.T) {
	c, server := testConnector(t, nil)
	ctx := context.Background()

	if err := c.Set(ctx, "short", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	ttl, err := c.TTL(ctx, "short")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("ttl = %v, want the minute that was asked for", ttl)
	}

	server.FastForward(2 * time.Minute)

	if _, found, err := c.Get(ctx, "short"); err != nil || found {
		t.Errorf("an expired entry was still there: found=%v err=%v", found, err)
	}
}

func TestTheDefaultExpiryAppliesWhenNoneIsGiven(t *testing.T) {
	// Otherwise a default_ttl in the configuration does nothing, and entries
	// written by a flow that names no expiry stay for ever.
	c, server := testConnector(t, &types.Config{DefaultTTL: time.Minute})
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("value"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ttl, err := c.TTL(ctx, "k")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("ttl = %v, want the configured default to have been applied", ttl)
	}

	server.FastForward(2 * time.Minute)
	if _, found, _ := c.Get(ctx, "k"); found {
		t.Error("the entry outlived the default expiry")
	}
}

func TestAnExplicitExpiryOverridesTheDefault(t *testing.T) {
	c, server := testConnector(t, &types.Config{DefaultTTL: time.Hour})
	ctx := context.Background()

	if err := c.Set(ctx, "k", []byte("value"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	server.FastForward(2 * time.Minute)
	if _, found, _ := c.Get(ctx, "k"); found {
		t.Error("the entry kept the default expiry rather than the one it was given")
	}
}

func TestAKeyWithNoExpiryIsReportedAsSuch(t *testing.T) {
	c, _ := testConnector(t, nil)
	ctx := context.Background()

	if err := c.Set(ctx, "forever", []byte("value"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ttl, err := c.TTL(ctx, "forever")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl > 0 {
		t.Errorf("ttl = %v, want it reported as not expiring", ttl)
	}
}

func TestSeveralKeysGoAtOnce(t *testing.T) {
	c, _ := testConnector(t, &types.Config{Prefix: "p"})
	ctx := context.Background()

	for _, key := range []string{"a", "b", "c"} {
		if err := c.Set(ctx, key, []byte("value"), 0); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}

	if err := c.Delete(ctx, "a", "b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for key, want := range map[string]bool{"a": false, "b": false, "c": true} {
		exists, err := c.Exists(ctx, key)
		if err != nil {
			t.Fatalf("Exists %s: %v", key, err)
		}
		if exists != want {
			t.Errorf("%s exists = %v, want %v", key, exists, want)
		}
	}

	// Deleting nothing is not an error, since a flow may have nothing to drop.
	if err := c.Delete(ctx); err != nil {
		t.Errorf("deleting no keys: %v", err)
	}
}

func TestInvalidationByPatternTakesTheMatchesAndNothingElse(t *testing.T) {
	// This is how a cache is invalidated when one record changes and a family
	// of derived keys has to go with it. Too wide and the whole cache is
	// dropped on every write; too narrow and stale entries are served.
	c, _ := testConnector(t, &types.Config{Prefix: "mercury"})
	ctx := context.Background()

	keys := []string{
		"product:1:detail", "product:1:price",
		"product:2:detail",
		"customer:1:detail",
	}
	for _, key := range keys {
		if err := c.Set(ctx, key, []byte("value"), 0); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}

	if err := c.DeletePattern(ctx, "product:1:*"); err != nil {
		t.Fatalf("DeletePattern: %v", err)
	}

	for key, want := range map[string]bool{
		"product:1:detail":  false,
		"product:1:price":   false,
		"product:2:detail":  true,
		"customer:1:detail": true,
	} {
		exists, err := c.Exists(ctx, key)
		if err != nil {
			t.Fatalf("Exists %s: %v", key, err)
		}
		if exists != want {
			t.Errorf("%s exists = %v, want %v", key, exists, want)
		}
	}
}

func TestInvalidationByPatternStaysInsideThePrefix(t *testing.T) {
	// A pattern is prefixed like any other key, so one service cannot clear
	// another's entries out of a shared Redis by writing a wide enough pattern.
	server := miniredis.RunT(t)
	ctx := context.Background()

	mine := New("mine", &types.Config{URL: "redis://" + server.Addr(), Prefix: "svc-a"})
	if err := mine.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = mine.Close(ctx) }()

	if err := mine.Set(ctx, "k", []byte("value"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := server.Set("svc-b:k", "someone else's"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := mine.DeletePattern(ctx, "*"); err != nil {
		t.Fatalf("DeletePattern: %v", err)
	}

	if _, err := server.Get("svc-b:k"); err != nil {
		t.Error("another service's entries were cleared by a wide pattern")
	}
	if exists, _ := mine.Exists(ctx, "k"); exists {
		t.Error("the pattern did not clear this service's own entries")
	}
}

func TestUsingTheCacheBeforeItIsConnectedIsRefused(t *testing.T) {
	// A cache whose connection failed at startup must say so on use rather than
	// reporting every read as a miss, which would look like a cold cache and
	// silently send the whole load to the origin.
	c := New("cache", &types.Config{URL: "redis://127.0.0.1:1"})
	ctx := context.Background()

	if _, _, err := c.Get(ctx, "k"); err == nil {
		t.Error("a read on an unconnected cache was reported as a miss")
	}
	if err := c.Set(ctx, "k", []byte("v"), 0); err == nil {
		t.Error("a write on an unconnected cache was accepted")
	}
	if err := c.Delete(ctx, "k"); err == nil {
		t.Error("a delete on an unconnected cache was accepted")
	}
	if err := c.DeletePattern(ctx, "*"); err == nil {
		t.Error("an invalidation on an unconnected cache was accepted")
	}
	if _, err := c.Exists(ctx, "k"); err == nil {
		t.Error("an existence check on an unconnected cache was accepted")
	}
	if _, err := c.TTL(ctx, "k"); err == nil {
		t.Error("a ttl read on an unconnected cache was accepted")
	}
	if err := c.Health(ctx); err == nil {
		t.Error("an unconnected cache reported itself healthy")
	}
}

func TestAnAddressThatIsNotOneIsRefusedAtStartup(t *testing.T) {
	c := New("cache", &types.Config{URL: "not-a-redis-url"})
	err := c.Connect(context.Background())
	if err == nil {
		t.Fatal("an address that is not one was accepted")
	}
	if !strings.Contains(err.Error(), "redis URL") {
		t.Errorf("error = %q, want it to name the address", err)
	}
}

func TestAnUnreachableCacheIsReportedAtStartup(t *testing.T) {
	// Rather than starting and failing on the first request, which is what an
	// unchecked connection does.
	c := New("cache", &types.Config{URL: "redis://127.0.0.1:1"})
	if err := c.Connect(context.Background()); err == nil {
		t.Error("a cache nothing is listening on was accepted")
	}
}

func TestHealthFollowsTheServer(t *testing.T) {
	c, server := testConnector(t, nil)
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Errorf("a working cache reported unhealthy: %v", err)
	}

	server.Close()
	if err := c.Health(ctx); err == nil {
		t.Error("a cache whose server is gone reported healthy")
	}
}

func TestTheConnectorDescribesItself(t *testing.T) {
	c, _ := testConnector(t, nil)
	if c.Name() != "cache" {
		t.Errorf("name = %q", c.Name())
	}
	if c.Type() != "cache" {
		t.Errorf("type = %q", c.Type())
	}
	if c.Mode() != "standalone" {
		t.Errorf("mode = %q, want the default topology", c.Mode())
	}
	if c.Client() == nil {
		t.Error("no client is reachable, so nothing else can share the connection")
	}
	if c.ClusterClient() != nil {
		t.Error("a standalone cache offered a cluster client")
	}
	if c.UniversalRedisClient() == nil {
		t.Error("no client at all is reachable")
	}
}

func TestClosingACacheThatNeverConnectedIsHarmless(t *testing.T) {
	// Shutdown closes every registered connector, including one whose Connect
	// failed at startup. Reporting an error for that would turn a shutdown into
	// a failed one and bury whatever else went wrong.
	c := New("cache", &types.Config{URL: "redis://127.0.0.1:1"})
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("closing a cache that never connected: %v", err)
	}
}

func TestAPoolSettingReachesTheClient(t *testing.T) {
	// The pool is the difference between a cache that serves a burst and one
	// that queues behind a single connection.
	c, _ := testConnector(t, &types.Config{
		Pool: types.PoolConfig{MaxConnections: 42, MinIdle: 7, ConnectTimeout: 3 * time.Second},
	})

	opts := c.Client().Options()
	if opts.PoolSize != 42 {
		t.Errorf("pool size = %d, want the 42 that was configured", opts.PoolSize)
	}
	if opts.MinIdleConns != 7 {
		t.Errorf("min idle = %d, want 7", opts.MinIdleConns)
	}
	if opts.DialTimeout != 3*time.Second {
		t.Errorf("connect timeout = %v, want 3s", opts.DialTimeout)
	}
}
