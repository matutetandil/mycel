package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// The retry policy is what a caller reaches for when a dependency falls over.
// Its two failure modes are opposite and both bad: not retrying when it should,
// and retrying so fast that it becomes the reason the dependency stays down.

func TestNextDelayPerStrategy(t *testing.T) {
	p := RetryPolicy{Delay: 100 * time.Millisecond, MaxDelay: time.Second, Backoff: "exponential"}
	// 100 -> 200 -> 400 -> 800 -> capped at 1s
	got := []time.Duration{}
	d := p.Delay
	for i := 0; i < 5; i++ {
		got = append(got, d)
		d = p.nextDelay(d)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("exponential step %d = %v, want %v", i, got[i], want[i])
		}
	}

	// Linear adds the base delay each time rather than doubling.
	lin := RetryPolicy{Delay: 100 * time.Millisecond, MaxDelay: time.Second, Backoff: "linear"}
	d = lin.Delay
	for _, want := range []time.Duration{200 * time.Millisecond, 300 * time.Millisecond, 400 * time.Millisecond} {
		d = lin.nextDelay(d)
		if d != want {
			t.Errorf("linear = %v, want %v", d, want)
		}
	}

	// Constant does not grow at all.
	con := RetryPolicy{Delay: 250 * time.Millisecond, MaxDelay: time.Second, Backoff: "constant"}
	if got := con.nextDelay(con.Delay); got != 250*time.Millisecond {
		t.Errorf("constant = %v, want it unchanged", got)
	}

	// The cap is what keeps an exponential from waiting for minutes.
	capped := RetryPolicy{Delay: time.Second, MaxDelay: 1500 * time.Millisecond, Backoff: "exponential"}
	if got := capped.nextDelay(time.Second); got != 1500*time.Millisecond {
		t.Errorf("capped = %v, want the cap", got)
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	p := DefaultRetryPolicy(3)
	if p.Attempts != 3 {
		t.Errorf("attempts = %d", p.Attempts)
	}
	// The default first wait is what the hardcoded implementation used, so a
	// connector that says nothing about timing behaves as it did.
	if p.Delay != 100*time.Millisecond {
		t.Errorf("delay = %v, want 100ms", p.Delay)
	}
	if p.Backoff != "exponential" {
		t.Errorf("backoff = %q, want exponential", p.Backoff)
	}
	if p.MaxDelay != 30*time.Second {
		t.Errorf("max_delay = %v, want 30s", p.MaxDelay)
	}
	// A count below one still has to mean "try once".
	if got := DefaultRetryPolicy(0).Attempts; got != 1 {
		t.Errorf("DefaultRetryPolicy(0).Attempts = %d, want 1", got)
	}
}

func TestRetriesUntilSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 3).WithRetryPolicy(RetryPolicy{
		Attempts: 3, Delay: time.Millisecond, MaxDelay: time.Second, Backoff: "constant",
	})

	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "POST", Target: "/things", Payload: map[string]interface{}{"a": 1},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("the server saw %d calls, want 3", got)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	// A 400 will be a 400 however many times it is sent. Retrying it wastes
	// the budget and delays the error the caller needs to see.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 5).WithRetryPolicy(RetryPolicy{
		Attempts: 5, Delay: time.Millisecond, MaxDelay: time.Second, Backoff: "constant",
	})

	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "POST", Target: "/things", Payload: map[string]interface{}{"a": 1},
	}); err == nil {
		t.Fatal("a 400 was reported as success")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("a client error was retried: %d calls", got)
	}
}

func TestConfiguredDelayIsHonoured(t *testing.T) {
	// The point of the change: a caller can now ask for a gap longer than the
	// fifth of a second the hardcoded version allowed.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("api", srv.URL, 0, nil, nil, 2).WithRetryPolicy(RetryPolicy{
		Attempts: 2, Delay: 300 * time.Millisecond, MaxDelay: time.Second, Backoff: "constant",
	})

	start := time.Now()
	_, _ = c.Write(context.Background(), &connector.Data{
		Operation: "POST", Target: "/things", Payload: map[string]interface{}{"a": 1},
	})
	elapsed := time.Since(start)

	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("the server saw %d calls, want 2", calls)
	}
	// One gap of 300ms between two attempts. The old hardcoded path would have
	// waited 100ms and finished well under this bound.
	if elapsed < 250*time.Millisecond {
		t.Errorf("the two attempts took %v, so the configured delay was ignored", elapsed)
	}
}

func TestWithRetryPolicyFillsInMissingFields(t *testing.T) {
	// A policy built by hand with only an attempt count must not end up with a
	// zero delay, which would retry as fast as the network allows.
	c := New("api", "http://example.com", 0, nil, nil, 1).
		WithRetryPolicy(RetryPolicy{Attempts: 3})
	if c.retry.Delay <= 0 {
		t.Errorf("delay = %v, want a non-zero default", c.retry.Delay)
	}
	if c.retry.Backoff == "" {
		t.Error("backoff was left empty")
	}
	if c.retryCount != 3 {
		t.Errorf("retryCount = %d, want it kept in step with the policy", c.retryCount)
	}
}
