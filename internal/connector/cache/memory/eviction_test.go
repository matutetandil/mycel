package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector/cache/types"
)

// A cache that lives in the process.
//
// It is the default one, so it is what most services get. Two things decide
// whether it is a cache or a memory leak: how many entries it will hold, and
// whether entries that expired are actually let go. Neither was tested, and
// both fail quietly — a cache that never evicts looks like a fast service
// until the process is killed for using too much memory.

func cacheWith(t *testing.T, config *types.Config) *Connector {
	t.Helper()
	c := New("products", config)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestACacheDoesNotGrowForEver(t *testing.T) {
	// A limit is what makes this a cache rather than a map that only grows.
	// Reaching it evicts whatever was used longest ago.
	c := cacheWith(t, &types.Config{MaxItems: 3})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := c.Set(ctx, fmt.Sprintf("key-%d", i), []byte("value"), 0); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	if c.Len() != 3 {
		t.Fatalf("holding %d entries", c.Len())
	}

	// Touch the oldest so it is no longer the one to go.
	if _, _, err := c.Get(ctx, "key-0"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if err := c.Set(ctx, "key-3", []byte("value"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if c.Len() != 3 {
		t.Errorf("holding %d entries, want the limit", c.Len())
	}
	// The one that was used most recently survived; the one nobody touched
	// went. Evicting the wrong one turns a cache into a cache that misses.
	if _, found, _ := c.Get(ctx, "key-0"); !found {
		t.Error("the entry that had just been read was evicted")
	}
	if _, found, _ := c.Get(ctx, "key-1"); found {
		t.Error("nothing was evicted, so the limit does not hold")
	}
}

func TestACacheWithNoLimitConfiguredStillHasOne(t *testing.T) {
	// Nothing written means a default rather than no limit at all: a cache
	// with no ceiling is a leak with a nice name.
	c := cacheWith(t, &types.Config{})

	if c.cache == nil {
		t.Fatal("no cache was built")
	}
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
	if c.Len() != 0 {
		t.Errorf("a new cache holds %d entries", c.Len())
	}
}

func TestEntriesThatExpiredAreLetGo(t *testing.T) {
	// Expiry alone only stops a value being served: the memory is still held
	// until something removes the entry. On a cache keyed by order id, that
	// is every order the service has ever seen.
	c := cacheWith(t, &types.Config{MaxItems: 100})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := c.Set(ctx, fmt.Sprintf("short-%d", i), []byte("value"), 10*time.Millisecond); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	if err := c.Set(ctx, "long", []byte("value"), time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if c.Len() != 6 {
		t.Fatalf("holding %d entries", c.Len())
	}

	time.Sleep(50 * time.Millisecond)

	// Still held, because nothing has swept yet — this is the state a cache
	// sits in between sweeps, and why the sweep exists.
	if c.Len() != 6 {
		t.Errorf("holding %d entries before the sweep", c.Len())
	}

	c.removeExpired()

	if c.Len() != 1 {
		t.Errorf("holding %d entries after the sweep, want only the one that has not expired", c.Len())
	}
	if _, found, _ := c.Get(ctx, "long"); !found {
		t.Error("the sweep took an entry that had not expired")
	}
}

func TestHowLongIsLeftOnAnEntry(t *testing.T) {
	// What a debugger reads to tell "this was never cached" from "this
	// expired a second ago".
	c := cacheWith(t, &types.Config{MaxItems: 10})
	ctx := context.Background()

	if err := c.Set(ctx, "with-expiry", []byte("value"), time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	remaining, err := c.TTL(ctx, "with-expiry")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if remaining <= 0 || remaining > time.Hour {
		t.Errorf("remaining = %v", remaining)
	}

	// An entry with no expiry answers -1, and one that is not there answers
	// -2: the same two answers Redis gives, so a flow reading either backend
	// sees the same thing.
	if err := c.Set(ctx, "forever", []byte("value"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _ := c.TTL(ctx, "forever"); got != -1*time.Second {
		t.Errorf("an entry with no expiry answered %v, want -1s", got)
	}
	if got, _ := c.TTL(ctx, "never-set"); got != -2*time.Second {
		t.Errorf("an entry nobody set answered %v, want -2s", got)
	}

	// One that has expired answers the same as one that was never there:
	// from the caller's side there is no difference.
	if err := c.Set(ctx, "gone", []byte("value"), 10*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if got, _ := c.TTL(ctx, "gone"); got != -2*time.Second {
		t.Errorf("an expired entry answered %v, want -2s", got)
	}
}

func TestUsingACacheThatWasNeverConnected(t *testing.T) {
	// Reachable when a connector is used before start-up finished. Said
	// plainly rather than as a nil dereference.
	c := New("products", &types.Config{MaxItems: 10})
	ctx := context.Background()

	if err := c.Health(ctx); err == nil {
		t.Error("a cache that was never built reported itself healthy")
	}
	if _, err := c.TTL(ctx, "anything"); err == nil {
		t.Error("a cache that was never built answered a TTL")
	}
	if c.Len() != 0 {
		t.Errorf("a cache that was never built holds %d entries", c.Len())
	}
	// Closing it is what a service that failed to start does on its way out.
	if err := c.Close(ctx); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestClosingTwice(t *testing.T) {
	// Shutdown reaches a connector twice — a cancelled context and an
	// explicit close — and closing the stop channel again would panic the
	// process on the way out.
	c := cacheWith(t, &types.Config{MaxItems: 10})
	ctx := context.Background()

	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Errorf("closing twice: %v", err)
	}
}

func TestTheConnectorSaysWhatItIs(t *testing.T) {
	c := New("products", &types.Config{})
	if c.Name() != "products" || c.Type() != "cache" {
		t.Errorf("name/type = %s/%s", c.Name(), c.Type())
	}
}
