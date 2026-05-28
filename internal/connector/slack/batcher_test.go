package slack

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder captures the messages a batcher dispatches, replacing the real
// webhook/API call so tests stay deterministic and offline. Optional failure
// injection covers the "send error" path.
type recorder struct {
	mu   sync.Mutex
	sent []*Message
	fail error
}

func (r *recorder) send(ctx context.Context, msg *Message) (*SendResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return &SendResult{Success: false, Error: r.fail.Error()}, r.fail
	}
	r.sent = append(r.sent, msg)
	return &SendResult{Success: true}, nil
}

func (r *recorder) snapshot() []*Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Message, len(r.sent))
	copy(out, r.sent)
	return out
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitFor polls until cond returns true or timeout elapses; returns false on
// timeout. Used instead of a fixed sleep so timer-driven flushes don't race
// the assertion.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestBatcherWindowFlushAndDefaultSummary covers the headline case: several
// messages submitted into the same window collapse into one bullet-list
// summary after the window elapses.
func TestBatcherWindowFlushAndDefaultSummary(t *testing.T) {
	rec := &recorder{}
	b, err := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  50 * time.Millisecond,
		MaxSize: 100,
		GroupBy: "channel",
	}, rec.send, quietLogger())
	if err != nil {
		t.Fatalf("newBatcher: %v", err)
	}

	for _, text := range []string{"first", "second", "third"} {
		if err := b.Submit(context.Background(), &Message{Channel: "#alerts", Text: text}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	if !waitFor(time.Second, func() bool { return len(rec.snapshot()) == 1 }) {
		t.Fatalf("expected 1 collapsed message after window, got %d", len(rec.snapshot()))
	}

	out := rec.snapshot()[0]
	if out.Channel != "#alerts" {
		t.Errorf("channel = %q, want #alerts", out.Channel)
	}
	if !strings.Contains(out.Text, "3 events") {
		t.Errorf("summary missing count header: %q", out.Text)
	}
	for _, want := range []string{"• first", "• second", "• third"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("summary missing %q, got: %q", want, out.Text)
		}
	}
	if !out.Mrkdwn {
		t.Error("collapsed summary must set Mrkdwn so bullets render")
	}
}

// TestBatcherSingleMessageBypass: a lone message in the window goes through
// unchanged — low-rate traffic looks identical to pre-batching behavior.
func TestBatcherSingleMessageBypass(t *testing.T) {
	rec := &recorder{}
	b, _ := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  30 * time.Millisecond,
		MaxSize: 100,
		GroupBy: "channel",
	}, rec.send, quietLogger())

	msg := &Message{Channel: "#alerts", Text: "lone event"}
	if err := b.Submit(context.Background(), msg); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if !waitFor(time.Second, func() bool { return len(rec.snapshot()) == 1 }) {
		t.Fatal("expected 1 dispatch")
	}
	got := rec.snapshot()[0]
	if got.Text != "lone event" {
		t.Errorf("single-message bypass should send text as-is, got %q", got.Text)
	}
	if strings.Contains(got.Text, "•") || strings.Contains(got.Text, "events:") {
		t.Errorf("single message must not be wrapped in summary: %q", got.Text)
	}
}

// TestBatcherMaxSizeForcesFlush: hitting MaxSize flushes synchronously,
// without waiting for the timer.
func TestBatcherMaxSizeForcesFlush(t *testing.T) {
	rec := &recorder{}
	b, _ := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  5 * time.Second, // long window — should NOT be needed
		MaxSize: 3,
		GroupBy: "channel",
	}, rec.send, quietLogger())

	for i := 0; i < 3; i++ {
		if err := b.Submit(context.Background(), &Message{Channel: "#alerts", Text: fmt.Sprintf("msg %d", i)}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	if !waitFor(200*time.Millisecond, func() bool { return len(rec.snapshot()) == 1 }) {
		t.Fatalf("MaxSize=3 should force a synchronous flush, got %d dispatches", len(rec.snapshot()))
	}
	out := rec.snapshot()[0]
	if !strings.Contains(out.Text, "3 events") {
		t.Errorf("expected 3-event summary, got %q", out.Text)
	}
}

// TestBatcherBlocksBypass: messages with rich content cannot be safely merged,
// so they short-circuit straight to send.
func TestBatcherBlocksBypass(t *testing.T) {
	rec := &recorder{}
	b, _ := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  5 * time.Second,
		MaxSize: 100,
		GroupBy: "channel",
	}, rec.send, quietLogger())

	msg := &Message{
		Channel: "#alerts",
		Text:    "rich",
		Blocks:  []Block{{Type: "section", Text: &TextObject{Type: "mrkdwn", Text: "hi"}}},
	}
	if err := b.Submit(context.Background(), msg); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if got := rec.snapshot(); len(got) != 1 || got[0] != msg {
		t.Fatalf("blocks message should bypass batcher and reach send immediately, got %#v", got)
	}
}

// TestBatcherPerChannelBucket: two channels do not share a buffer; each flushes
// on its own.
func TestBatcherPerChannelBucket(t *testing.T) {
	rec := &recorder{}
	b, _ := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  40 * time.Millisecond,
		MaxSize: 100,
		GroupBy: "channel",
	}, rec.send, quietLogger())

	_ = b.Submit(context.Background(), &Message{Channel: "#a", Text: "a1"})
	_ = b.Submit(context.Background(), &Message{Channel: "#a", Text: "a2"})
	_ = b.Submit(context.Background(), &Message{Channel: "#b", Text: "b1"})

	if !waitFor(time.Second, func() bool { return len(rec.snapshot()) == 2 }) {
		t.Fatalf("expected 2 dispatches (one per channel), got %d", len(rec.snapshot()))
	}

	byChannel := map[string]*Message{}
	for _, m := range rec.snapshot() {
		byChannel[m.Channel] = m
	}
	if a, ok := byChannel["#a"]; !ok || !strings.Contains(a.Text, "2 events") {
		t.Errorf("#a should have a 2-event summary, got %#v", a)
	}
	if bm, ok := byChannel["#b"]; !ok || bm.Text != "b1" {
		t.Errorf("#b should have a single passthrough, got %#v", bm)
	}
}

// TestBatcherGroupByGlobal: GroupBy="global" merges traffic across channels
// into one bucket — useful when the connector always posts to one channel.
func TestBatcherGroupByGlobal(t *testing.T) {
	rec := &recorder{}
	b, _ := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  40 * time.Millisecond,
		MaxSize: 100,
		GroupBy: "global",
	}, rec.send, quietLogger())

	_ = b.Submit(context.Background(), &Message{Channel: "#a", Text: "x"})
	_ = b.Submit(context.Background(), &Message{Channel: "#b", Text: "y"})

	if !waitFor(time.Second, func() bool { return len(rec.snapshot()) == 1 }) {
		t.Fatalf("global group should produce 1 dispatch, got %d", len(rec.snapshot()))
	}
	out := rec.snapshot()[0]
	if !strings.Contains(out.Text, "2 events") {
		t.Errorf("expected 2-event summary, got %q", out.Text)
	}
}

// TestBatcherCustomSummaryCEL: a configured Summary expression produces the
// collapsed text, with access to messages/count/channel/window.
func TestBatcherCustomSummaryCEL(t *testing.T) {
	rec := &recorder{}
	b, err := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  30 * time.Millisecond,
		MaxSize: 100,
		GroupBy: "channel",
		Summary: `"got " + string(count) + " in " + channel + ": " + messages.map(m, m.text).join(",")`,
	}, rec.send, quietLogger())
	if err != nil {
		t.Fatalf("newBatcher: %v", err)
	}

	_ = b.Submit(context.Background(), &Message{Channel: "#alerts", Text: "a"})
	_ = b.Submit(context.Background(), &Message{Channel: "#alerts", Text: "b"})

	if !waitFor(time.Second, func() bool { return len(rec.snapshot()) == 1 }) {
		t.Fatal("expected 1 dispatch")
	}
	got := rec.snapshot()[0].Text
	want := "got 2 in #alerts: a,b"
	if got != want {
		t.Errorf("custom summary = %q, want %q", got, want)
	}
}

// TestBatcherDrainOnClose: pending buckets must flush when Drain is called,
// so a graceful shutdown does not lose notifications.
func TestBatcherDrainOnClose(t *testing.T) {
	rec := &recorder{}
	b, _ := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  5 * time.Second, // window will NOT fire
		MaxSize: 100,
		GroupBy: "channel",
	}, rec.send, quietLogger())

	_ = b.Submit(context.Background(), &Message{Channel: "#a", Text: "x"})
	_ = b.Submit(context.Background(), &Message{Channel: "#a", Text: "y"})

	if err := b.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("Drain should flush pending bucket, got %d dispatches", len(got))
	}
	if !strings.Contains(got[0].Text, "2 events") {
		t.Errorf("drained summary = %q, want 2 events", got[0].Text)
	}

	// Second Drain is a no-op.
	if err := b.Drain(context.Background()); err != nil {
		t.Errorf("second Drain should be a no-op, got error: %v", err)
	}
}

// TestBatcherTruncatesAboveSlackLimit: a summary that would exceed the
// 38000-byte cap is truncated with a marker, never silently dropped.
func TestBatcherTruncatesAboveSlackLimit(t *testing.T) {
	rec := &recorder{}
	b, _ := newBatcher(&BatchConfig{
		Enabled: true,
		Window:  20 * time.Millisecond,
		MaxSize: 1000,
		GroupBy: "channel",
	}, rec.send, quietLogger())

	// 1000 messages of 100 chars → ~100000 chars of bullets → must truncate.
	long := strings.Repeat("x", 100)
	for i := 0; i < 1000; i++ {
		_ = b.Submit(context.Background(), &Message{Channel: "#a", Text: long})
	}

	if !waitFor(2*time.Second, func() bool { return len(rec.snapshot()) == 1 }) {
		t.Fatal("expected 1 dispatch")
	}
	out := rec.snapshot()[0]
	if len(out.Text) > slackMaxMessageBytes+64 {
		t.Errorf("summary length = %d, must be <= %d (with marker)", len(out.Text), slackMaxMessageBytes+64)
	}
	if !strings.Contains(out.Text, "truncated") {
		t.Errorf("truncated summary should carry a marker, got tail: %q", out.Text[len(out.Text)-50:])
	}
}

// TestParseBatchConfig_Defaults: omitting the batch block yields default-on.
func TestParseBatchConfig_Defaults(t *testing.T) {
	cfg, err := parseBatchConfig(nil)
	if err != nil {
		t.Fatalf("parseBatchConfig(nil): %v", err)
	}
	if !cfg.Enabled || cfg.Window != 3*time.Second || cfg.MaxSize != 50 || cfg.GroupBy != "channel" {
		t.Errorf("defaults = %#v, want enabled=true window=3s max_size=50 group_by=channel", cfg)
	}
}

// TestParseBatchConfig_OptOut: batch { enabled = false } restores immediate
// send.
func TestParseBatchConfig_OptOut(t *testing.T) {
	cfg, err := parseBatchConfig(map[string]interface{}{"enabled": false})
	if err != nil {
		t.Fatalf("parseBatchConfig: %v", err)
	}
	if cfg.Enabled {
		t.Error("enabled=false must disable batching")
	}
}

// TestParseBatchConfig_CustomKnobs: window/max_size/group_by/summary override
// the defaults.
func TestParseBatchConfig_CustomKnobs(t *testing.T) {
	cfg, err := parseBatchConfig(map[string]interface{}{
		"window":   "10s",
		"max_size": int64(200),
		"group_by": "global",
		"summary":  `"x"`,
	})
	if err != nil {
		t.Fatalf("parseBatchConfig: %v", err)
	}
	if cfg.Window != 10*time.Second {
		t.Errorf("Window = %v, want 10s", cfg.Window)
	}
	if cfg.MaxSize != 200 {
		t.Errorf("MaxSize = %d, want 200", cfg.MaxSize)
	}
	if cfg.GroupBy != "global" {
		t.Errorf("GroupBy = %q, want global", cfg.GroupBy)
	}
	if cfg.Summary != `"x"` {
		t.Errorf("Summary = %q, want \"x\"", cfg.Summary)
	}
}

// TestParseBatchConfig_RejectsInvalid: bad group_by, non-bool enabled, etc.
// surface as parse errors.
func TestParseBatchConfig_RejectsInvalid(t *testing.T) {
	cases := []map[string]interface{}{
		{"group_by": "weird"},
		{"window": "not-a-duration"},
		{"max_size": -1},
		{"enabled": "yes"},
	}
	for i, in := range cases {
		if _, err := parseBatchConfig(in); err == nil {
			t.Errorf("case %d: expected error for %#v", i, in)
		}
	}
}
