package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// Rate limiting across more than one process.
//
// A limit held in memory is a limit per replica: three replicas behind a load
// balancer with "100 per second" configured allow 300. The Redis-backed store
// exists so the limit is the limit, and none of it had a test — including what
// happens when Redis is the thing that goes down, which is the case that
// decides whether a rate limiter can take a service offline.

// atStartOfWindow waits until a fresh second begins.
//
// The distributed limiter counts inside a one-second window keyed off the
// clock, so a burst that straddles a boundary is counted as two — the third
// request of three lands in a new window and is allowed. Locally the calls
// take microseconds and it never happens; on a loaded machine it does, which
// is a flaky test rather than a finding.
func atStartOfWindow() {
	now := time.Now()
	time.Sleep(time.Until(now.Truncate(time.Second).Add(time.Second)) + 5*time.Millisecond)
}

func redisStore(t *testing.T, prefix string) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStore(client, prefix), server
}

func TestALimitSharedBetweenProcesses(t *testing.T) {
	store, _ := redisStore(t, "")

	// Two limiters, as two replicas of the same service are.
	first := New(&Config{Enabled: true, RequestsPerSecond: 3, Burst: 3, KeyExtractor: "ip"})
	second := New(&Config{Enabled: true, RequestsPerSecond: 3, Burst: 3, KeyExtractor: "ip"})
	defer first.Close()
	defer second.Close()
	first.SetRedisStore(store)
	second.SetRedisStore(store)

	atStartOfWindow()
	allowed := 0
	for i := 0; i < 6; i++ {
		limiter := first
		if i%2 == 1 {
			limiter = second
		}
		if limiter.Allow("203.0.113.10") {
			allowed++
		}
	}

	// Three, not six: the count is one count, wherever the request landed.
	if allowed != 3 {
		t.Errorf("%d of 6 requests were allowed across two replicas, want 3", allowed)
	}

	// A different caller is not affected by the first one's spending.
	if !first.Allow("203.0.113.99") {
		t.Error("a caller who had sent nothing was rate limited")
	}
}

func TestWhatIsLeftIsReported(t *testing.T) {
	store, _ := redisStore(t, "")
	ctx := context.Background()

	atStartOfWindow()
	for i, want := range []int{2, 1, 0, 0} {
		allowed, remaining, err := store.Allow(ctx, "caller", 3, time.Second)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if remaining != want {
			t.Errorf("request %d left %d, want %d", i+1, remaining, want)
		}
		// The fourth is over the limit of three.
		if allowed != (i < 3) {
			t.Errorf("request %d allowed = %v", i+1, allowed)
		}
	}
}

func TestTheWindowMovesOn(t *testing.T) {
	store, server := redisStore(t, "")
	ctx := context.Background()

	atStartOfWindow()
	for i := 0; i < 2; i++ {
		if allowed, _, _ := store.Allow(ctx, "caller", 2, time.Second); !allowed {
			t.Fatalf("request %d was refused inside the limit", i+1)
		}
	}
	if allowed, _, _ := store.Allow(ctx, "caller", 2, time.Second); allowed {
		t.Fatal("a third request was allowed against a limit of two")
	}

	// A window is a slice of the clock, so moving past it starts a new count —
	// this is what makes a caller who waits able to carry on.
	server.FastForward(2 * time.Second)
	if allowed, _, _ := store.Allow(ctx, "caller", 2, time.Second); !allowed {
		t.Error("a caller was still refused after the window had passed")
	}
}

func TestCountsExpireOnTheirOwn(t *testing.T) {
	// Every distinct caller writes a key. Without an expiry, a service facing
	// the internet fills Redis with one key per address for ever.
	store, server := redisStore(t, "mycel:test")
	ctx := context.Background()

	if _, _, err := store.Allow(ctx, "203.0.113.10", 10, time.Second); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	keys := server.Keys()
	if len(keys) != 1 {
		t.Fatalf("keys = %v, want one", keys)
	}
	if ttl := server.TTL(keys[0]); ttl <= 0 {
		t.Errorf("the counter for %s has no expiry", keys[0])
	}
}

func TestKeysAreNamespaced(t *testing.T) {
	// One Redis is shared by more than one service, and by more than one thing
	// per service; a prefix is what stops two of them counting each other.
	store, server := redisStore(t, "orders:api")
	if _, _, err := store.Allow(context.Background(), "caller", 5, time.Second); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	keys := server.Keys()
	if len(keys) != 1 || len(keys[0]) < len("orders:api") || keys[0][:len("orders:api")] != "orders:api" {
		t.Errorf("keys = %v, want them under the configured prefix", keys)
	}

	// And a store told no prefix still writes somewhere identifiable rather
	// than at the top level of somebody else's database.
	plain, plainServer := redisStore(t, "")
	if _, _, err := plain.Allow(context.Background(), "caller", 5, time.Second); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if keys := plainServer.Keys(); len(keys) != 1 || keys[0][:6] != "mycel:" {
		t.Errorf("keys = %v, want a default namespace", keys)
	}
}

func TestRedisGoingDownDoesNotTakeTheServiceWithIt(t *testing.T) {
	// The decision that matters most here. A rate limiter that fails closed
	// when its store is unreachable refuses every request in the service —
	// turning a Redis outage into a total outage. It falls back to counting in
	// memory instead: the limit stops being shared, which is the mild failure.
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: server.Addr(),
		// Fail fast rather than retrying for seconds: what is being measured
		// is the decision taken when Redis does not answer, not the wait.
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisStore(client, "")

	limiter := New(&Config{Enabled: true, RequestsPerSecond: 100, Burst: 100, KeyExtractor: "ip"})
	defer limiter.Close()
	limiter.SetRedisStore(store)

	if !limiter.Allow("203.0.113.10") {
		t.Fatal("a request was refused while Redis was up")
	}

	server.Close()

	if !limiter.Allow("203.0.113.10") {
		t.Error("a request was refused because Redis was unreachable")
	}

	// And the local limit is still a limit, not an open door. The rate is slow
	// enough that nothing refills while the failed calls to Redis time out.
	strict := New(&Config{Enabled: true, RequestsPerSecond: 0.01, Burst: 1, KeyExtractor: "ip"})
	defer strict.Close()
	strict.SetRedisStore(store)
	if !strict.Allow("203.0.113.20") {
		t.Fatal("the first request was refused with a burst of one")
	}
	if strict.Allow("203.0.113.20") {
		t.Error("with Redis down the limit stopped applying altogether")
	}
}

func TestTheLimitUsedWhenOnlyARateIsConfigured(t *testing.T) {
	// Burst is what the distributed path counts against; a configuration that
	// sets only a rate would otherwise be a limit of zero — every request
	// refused, which is exactly the outage the fallback above avoids.
	store, _ := redisStore(t, "")
	limiter := New(&Config{Enabled: true, RequestsPerSecond: 2, KeyExtractor: "ip"})
	defer limiter.Close()
	limiter.SetRedisStore(store)

	atStartOfWindow()
	if !limiter.Allow("203.0.113.10") {
		t.Fatal("the first request was refused with a rate of two per second")
	}
	if !limiter.Allow("203.0.113.10") {
		t.Error("the second request was refused with a rate of two per second")
	}
	if limiter.Allow("203.0.113.10") {
		t.Error("a third request was allowed against a rate of two per second")
	}
}

func TestARateLimiterThatIsOffIsOff(t *testing.T) {
	// Every entry point has to agree, or turning the limiter off in
	// configuration leaves one of them still refusing.
	store, _ := redisStore(t, "")
	limiter := New(&Config{Enabled: false, RequestsPerSecond: 1, Burst: 1})
	defer limiter.Close()
	limiter.SetRedisStore(store)

	for i := 0; i < 5; i++ {
		if !limiter.Allow("caller") {
			t.Fatal("Allow refused a request with rate limiting disabled")
		}
		if !limiter.AllowN("caller", 10) {
			t.Fatal("AllowN refused a request with rate limiting disabled")
		}
		if !limiter.AllowKey("caller") {
			t.Fatal("AllowKey refused a request with rate limiting disabled")
		}
		if !limiter.AllowKeyN("caller", 10) {
			t.Fatal("AllowKeyN refused a request with rate limiting disabled")
		}
		if err := limiter.Wait(context.Background(), "caller"); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
}

func TestAskingForSeveralAtOnce(t *testing.T) {
	limiter := New(&Config{Enabled: true, RequestsPerSecond: 1, Burst: 5})
	defer limiter.Close()

	// A batch that fits.
	if !limiter.AllowN("caller", 3) {
		t.Error("a batch of three was refused against a burst of five")
	}
	// One that does not: what is left is two.
	if limiter.AllowN("caller", 3) {
		t.Error("a batch of three was allowed with two left")
	}
	if !limiter.AllowKeyN("caller", 2) {
		t.Error("a batch of two was refused with two left")
	}
	if limiter.AllowKey("caller") {
		t.Error("a request was allowed with nothing left")
	}
}

func TestWaitingUntilThereIsRoom(t *testing.T) {
	limiter := New(&Config{Enabled: true, RequestsPerSecond: 50, Burst: 1})
	defer limiter.Close()

	if err := limiter.Wait(context.Background(), "caller"); err != nil {
		t.Fatalf("the first wait returned %v", err)
	}
	// The second has to wait for the rate to refill rather than be refused.
	started := time.Now()
	if err := limiter.Wait(context.Background(), "caller"); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if time.Since(started) < 5*time.Millisecond {
		t.Error("Wait returned immediately with no tokens left")
	}

	// A caller who gives up must not be left waiting.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := New(&Config{Enabled: true, RequestsPerSecond: 0.001, Burst: 1})
	defer slow.Close()
	slow.Allow("caller")
	if err := slow.Wait(ctx, "caller"); err == nil {
		t.Error("Wait ignored a cancelled request")
	}
}

func TestWhoIsBeingCounted(t *testing.T) {
	// Which caller a request is attributed to. Getting this wrong is either
	// everybody sharing one bucket — one busy client locking out the rest — or
	// a limit that never applies because every request looks like a new caller.
	for name, tc := range map[string]struct {
		config  *Config
		prepare func(*http.Request)
		want    string
	}{
		"the address it came from": {
			&Config{KeyExtractor: "ip"},
			func(r *http.Request) { r.RemoteAddr = "203.0.113.10:54321" },
			"203.0.113.10",
		},
		"the client behind a proxy": {
			&Config{KeyExtractor: "ip"},
			func(r *http.Request) {
				r.RemoteAddr = "10.0.0.1:54321"
				r.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")
			},
			"203.0.113.10",
		},
		"the address a proxy names directly": {
			&Config{KeyExtractor: "ip"},
			func(r *http.Request) {
				r.RemoteAddr = "10.0.0.1:54321"
				r.Header.Set("X-Real-IP", "203.0.113.11")
			},
			"203.0.113.11",
		},
		"an api key": {
			&Config{KeyExtractor: "header:X-API-Key"},
			func(r *http.Request) { r.Header.Set("X-API-Key", "key-1") },
			"key-1",
		},
		"an api key that was not sent falls back to the address": {
			&Config{KeyExtractor: "header:X-API-Key"},
			func(r *http.Request) { r.RemoteAddr = "203.0.113.10:54321" },
			"203.0.113.10",
		},
		"something in the query string": {
			&Config{KeyExtractor: "query:tenant"},
			func(r *http.Request) { r.URL.RawQuery = "tenant=acme" },
			"acme",
		},
		"a query parameter that is absent falls back to the address": {
			&Config{KeyExtractor: "query:tenant"},
			func(r *http.Request) { r.RemoteAddr = "203.0.113.10:54321" },
			"203.0.113.10",
		},
		"address and client together": {
			&Config{KeyExtractor: "combined"},
			func(r *http.Request) {
				r.RemoteAddr = "203.0.113.10:54321"
				r.Header.Set("User-Agent", "curl/8")
			},
			"203.0.113.10:curl/8",
		},
		"anything unrecognised falls back to the address": {
			&Config{KeyExtractor: "whatever"},
			func(r *http.Request) { r.RemoteAddr = "203.0.113.10:54321" },
			"203.0.113.10",
		},
	} {
		t.Run(name, func(t *testing.T) {
			limiter := New(tc.config)
			defer limiter.Close()

			r := httptest.NewRequest(http.MethodGet, "/orders", nil)
			r.Header.Del("User-Agent")
			tc.prepare(r)

			if got := limiter.extractKey(r); got != tc.want {
				t.Errorf("counted as %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWhatACallerIsToldAboutTheLimit(t *testing.T) {
	limiter := New(&Config{
		Enabled: true, RequestsPerSecond: 2, Burst: 2,
		KeyExtractor: "ip", EnableHeaders: true,
	})
	defer limiter.Close()

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/orders", nil)
		r.RemoteAddr = "203.0.113.10:54321"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	first := request()
	if first.Header().Get("X-RateLimit-Limit") != "2" {
		t.Errorf("limit header = %q", first.Header().Get("X-RateLimit-Limit"))
	}
	// What is left has to actually go down, or a client pacing itself by the
	// header learns nothing from it.
	remaining, err := strconv.Atoi(first.Header().Get("X-RateLimit-Remaining"))
	if err != nil || remaining > 2 {
		t.Errorf("remaining = %q", first.Header().Get("X-RateLimit-Remaining"))
	}
	if first.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("nothing says when the limit resets")
	}

	request()
	refused := request()
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("third request = %d, want 429", refused.Code)
	}
	// Retry-After is what a well-behaved client waits for; without it, it
	// retries immediately and is refused again.
	if refused.Header().Get("Retry-After") == "" {
		t.Error("a refused request was not told when to come back")
	}
	if got := refused.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q", got)
	}
	if body := refused.Body.String(); !strings.Contains(body, "rate limit exceeded") {
		t.Errorf("body = %q", body)
	}
}

func TestStaleCallersAreForgotten(t *testing.T) {
	// One limiter is kept per caller. A service facing the internet would
	// otherwise hold one for every address that ever called it.
	limiter := New(&Config{Enabled: true, RequestsPerSecond: 10, Burst: 10})
	defer limiter.Close()

	for i := 0; i < 5; i++ {
		limiter.Allow(fmt.Sprintf("203.0.113.%d", i))
	}
	if len(limiter.limiters) != 5 {
		t.Fatalf("holding %d callers, want 5", len(limiter.limiters))
	}

	// Age four of them past the threshold and run the sweep.
	limiter.mu.Lock()
	for key, cl := range limiter.limiters {
		if key != "203.0.113.0" {
			cl.lastSeen = time.Now().Add(-10 * time.Minute)
		}
	}
	limiter.mu.Unlock()

	limiter.cleanup.Reset(time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		limiter.mu.RLock()
		held := len(limiter.limiters)
		limiter.mu.RUnlock()
		if held == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("callers that had not been seen for ten minutes were still held")
}
