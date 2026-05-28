package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestRateLimitRetry_WebhookSucceedsAfter429: webhook returns 429 with
// Retry-After: 1, then 200. The connector retries once after the delay and
// surfaces success to the caller.
func TestRateLimitRetry_WebhookSucceedsAfter429(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate_limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	conn := NewConnector("t", &Config{
		WebhookURL: server.URL,
		Batch:      &BatchConfig{Enabled: false}, // bypass batcher to test the HTTP path directly
	})

	start := time.Now()
	res, err := conn.Send(context.Background(), &Message{Text: "hi"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success on retry, got %#v", res)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 server calls (initial + retry), got %d", got)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected at least ~1s of Retry-After backoff, got %v", elapsed)
	}
}

// TestRateLimitRetry_WebhookGivesUpAfterSecond429: two consecutive 429s must
// not loop forever — the connector retries exactly once, then surfaces the
// error.
func TestRateLimitRetry_WebhookGivesUpAfterSecond429(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate_limited"))
	}))
	defer server.Close()

	conn := NewConnector("t", &Config{
		WebhookURL: server.URL,
		Batch:      &BatchConfig{Enabled: false},
	})

	_, err := conn.Send(context.Background(), &Message{Text: "hi"})
	if err == nil {
		t.Fatal("expected error after persistent 429")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected exactly 2 calls (no infinite retry), got %d", got)
	}
}

// TestRateLimitRetry_ContextCancellation: a cancelled context aborts the wait
// instead of sleeping the full Retry-After.
func TestRateLimitRetry_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5") // long enough that we should NOT wait it out
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	conn := NewConnector("t", &Config{
		WebhookURL: server.URL,
		Batch:      &BatchConfig{Enabled: false},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := conn.Send(ctx, &Message{Text: "hi"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if elapsed > 1*time.Second {
		t.Errorf("cancellation should abort the wait quickly, took %v", elapsed)
	}
}

// TestParseRetryAfter_Seconds: an integer header is interpreted as seconds.
func TestParseRetryAfter_Seconds(t *testing.T) {
	if got := parseRetryAfter("3", time.Now()); got != 3*time.Second {
		t.Errorf("parseRetryAfter(\"3\") = %v, want 3s", got)
	}
}

// TestParseRetryAfter_HTTPDate: an HTTP-date in the future is interpreted as
// the wait until that date.
func TestParseRetryAfter_HTTPDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	target := now.Add(4 * time.Second)
	got := parseRetryAfter(target.Format(http.TimeFormat), now)
	if got < 4*time.Second || got > 5*time.Second {
		t.Errorf("parseRetryAfter(HTTP-date now+4s) = %v, want ~4s", got)
	}
}

// TestParseRetryAfter_ClampedToMax: a server asking for 5 minutes is capped at
// maxRetryAfter so the connector cannot be stalled indefinitely.
func TestParseRetryAfter_ClampedToMax(t *testing.T) {
	if got := parseRetryAfter("300", time.Now()); got != maxRetryAfter {
		t.Errorf("parseRetryAfter(\"300\") = %v, want clamped to %v", got, maxRetryAfter)
	}
}

// TestParseRetryAfter_FallbackOnGarbage: an unparseable header falls back to a
// safe default rather than retrying immediately.
func TestParseRetryAfter_FallbackOnGarbage(t *testing.T) {
	if got := parseRetryAfter("not-a-thing", time.Now()); got != defaultRetryAfter {
		t.Errorf("parseRetryAfter(garbage) = %v, want default %v", got, defaultRetryAfter)
	}
}
