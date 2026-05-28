package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEndToEnd_BatchedSendCollapsesAtWebhook drives the full integrated path
// (NewConnector → Send → batcher → rawSend → sendViaWebhook) against an
// httptest server. It is the test that proves the batching default actually
// kicks in: 5 individual Send() calls produce exactly 1 POST to the
// "webhook", with all 5 texts collapsed into the bullet summary.
func TestEndToEnd_BatchedSendCollapsesAtWebhook(t *testing.T) {
	var (
		mu       sync.Mutex
		received []Message
		hits     int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		body, _ := io.ReadAll(r.Body)
		var m Message
		if err := json.Unmarshal(body, &m); err != nil {
			t.Errorf("server failed to parse Slack payload: %v", err)
		}
		mu.Lock()
		received = append(received, m)
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	// Small window keeps the test fast while still exercising the timer path.
	conn := NewConnector("t", &Config{
		WebhookURL: server.URL,
		Batch: &BatchConfig{
			Enabled: true,
			Window:  100 * time.Millisecond,
			MaxSize: 100,
			GroupBy: "channel",
		},
	})

	for _, text := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		res, err := conn.Send(context.Background(), &Message{Channel: "#alerts", Text: text})
		if err != nil {
			t.Fatalf("Send(%q): %v", text, err)
		}
		if !res.Success {
			t.Fatalf("Send(%q): batched submit reported failure: %#v", text, res)
		}
	}

	// Close drains the bucket synchronously and waits for the flush to finish
	// — no time.Sleep races and the test stays deterministic.
	if err := conn.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected the 5 Sends to collapse into 1 POST, got %d", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("server saw %d messages, want 1", len(received))
	}
	got := received[0]
	if got.Channel != "#alerts" {
		t.Errorf("channel = %q, want #alerts", got.Channel)
	}
	if !strings.Contains(got.Text, "5 events") {
		t.Errorf("summary missing count header, got: %q", got.Text)
	}
	for _, want := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("summary missing %q, got: %q", want, got.Text)
		}
	}
	if !got.Mrkdwn {
		t.Error("collapsed summary must set Mrkdwn so bullets render in Slack")
	}
}

// TestEndToEnd_SingleSendBypassReachesWebhookAsIs proves the low-rate
// path: a lone Send() in the window arrives at the webhook unchanged (no
// bullet wrapper) — the "default-on is safe for low-rate users" promise.
func TestEndToEnd_SingleSendBypassReachesWebhookAsIs(t *testing.T) {
	var (
		mu       sync.Mutex
		received []Message
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m Message
		_ = json.Unmarshal(body, &m)
		mu.Lock()
		received = append(received, m)
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	conn := NewConnector("t", &Config{
		WebhookURL: server.URL,
		Batch: &BatchConfig{
			Enabled: true,
			Window:  100 * time.Millisecond,
			MaxSize: 100,
			GroupBy: "channel",
		},
	})

	if _, err := conn.Send(context.Background(), &Message{Channel: "#alerts", Text: "single event"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := conn.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("server saw %d messages, want 1", len(received))
	}
	got := received[0]
	if got.Text != "single event" {
		t.Errorf("single Send must arrive as-is, got %q", got.Text)
	}
	if strings.Contains(got.Text, "•") || strings.Contains(got.Text, "events:") {
		t.Errorf("single Send must not be wrapped in summary: %q", got.Text)
	}
}

// TestEndToEnd_DefaultBatchOnFromFactory proves the "upgrade and it just works"
// promise: a Config produced by the factory with NO Batch set enables
// batching by default, so an existing config that hits Slack hard suddenly
// stops hitting the rate limit after the upgrade.
func TestEndToEnd_DefaultBatchOnFromFactory(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	// No Batch field set — DefaultBatchConfig() should kick in.
	conn := NewConnector("t", &Config{WebhookURL: server.URL})
	if conn.batcher == nil {
		t.Fatal("default batcher must be installed when Config.Batch is nil")
	}

	// With the default 3s window we'd block the test; force a small window by
	// rebuilding the connector with the same defaults except Window.
	conn = NewConnector("t", &Config{
		WebhookURL: server.URL,
		Batch:      &BatchConfig{Enabled: true, Window: 50 * time.Millisecond, MaxSize: 50, GroupBy: "channel"},
	})

	for i := 0; i < 4; i++ {
		_, _ = conn.Send(context.Background(), &Message{Channel: "#a", Text: "x"})
	}
	_ = conn.Close(context.Background())

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("4 Sends should collapse into 1 POST under default-on batching, got %d", got)
	}
}
