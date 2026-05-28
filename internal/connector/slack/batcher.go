package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/cel-go/cel"

	"github.com/matutetandil/mycel/internal/transform"
)

// sendFunc is what the batcher calls to actually deliver a message to Slack.
// In production it is wired to Connector.Send (which picks webhook vs API);
// tests inject a capture-only func to assert on the dispatched messages.
type sendFunc func(ctx context.Context, msg *Message) (*SendResult, error)

// batcher coalesces messages submitted within a short window into a single
// collapsed message, so high-rate writes do not trip Slack's "high volume of
// activity" suppression on chat.postMessage / incoming webhooks.
//
// Semantics:
//   - One bucket per channel by default (cfg.GroupBy == "channel"); the first
//     message in a bucket arms a Window-duration timer.
//   - Flush triggers, whichever comes first: timer expiry, MaxSize reached, or
//     Drain() (called from Connector.Close).
//   - Single-message bucket → bypasses the summary and sends the message
//     as-is, so low-rate traffic is visually identical to pre-v2.5.0.
//   - Messages with rich content (Blocks/Attachments) or a thread reply
//     (ThreadTS) skip the batcher entirely — collapsing them would either lose
//     structure or land the summary in the wrong thread.
type batcher struct {
	cfg    *BatchConfig
	send   sendFunc
	eval   *transform.CELTransformer // built only when cfg.Summary != ""
	logger *slog.Logger

	mu      sync.Mutex
	buffers map[string]*bucket
	closed  bool

	// wg tracks in-flight dispatch goroutines so Drain can wait for them.
	wg sync.WaitGroup
}

// bucket holds the messages queued for one bucket key (channel name or "*")
// and the timer scheduled to flush them.
type bucket struct {
	messages []*Message
	timer    *time.Timer
}

// newBatcher constructs a batcher for the given configuration. When cfg.Summary
// is non-empty the CEL environment for it is built eagerly so a bad expression
// fails fast at connector construction, not at flush time.
func newBatcher(cfg *BatchConfig, send sendFunc, logger *slog.Logger) (*batcher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	b := &batcher{
		cfg:     cfg,
		send:    send,
		logger:  logger,
		buffers: make(map[string]*bucket),
	}
	if cfg.Summary != "" {
		eval, err := transform.NewCELTransformerWithOptions(
			cel.Variable("messages", cel.ListType(cel.DynType)),
			cel.Variable("count", cel.IntType),
			cel.Variable("channel", cel.StringType),
			cel.Variable("window", cel.StringType),
		)
		if err != nil {
			return nil, fmt.Errorf("compile batch summary env: %w", err)
		}
		b.eval = eval
	}
	return b, nil
}

// Submit enqueues msg for batched delivery, or bypasses to immediate send when
// the message cannot be safely collapsed.
func (b *batcher) Submit(ctx context.Context, msg *Message) error {
	if shouldBypass(msg) {
		_, err := b.send(ctx, msg)
		return err
	}

	key := b.bucketKey(msg)

	b.mu.Lock()
	if b.closed {
		// Late submission after Drain — fall back to a direct send so the
		// caller does not silently drop the message.
		b.mu.Unlock()
		_, err := b.send(ctx, msg)
		return err
	}

	buf, ok := b.buffers[key]
	if !ok {
		buf = &bucket{}
		b.buffers[key] = buf
	}
	buf.messages = append(buf.messages, msg)

	// First message in the bucket arms the window timer.
	if len(buf.messages) == 1 {
		k := key
		buf.timer = time.AfterFunc(b.cfg.Window, func() { b.flush(k) })
	}

	// MaxSize reached: flush synchronously, stopping the timer.
	if len(buf.messages) >= b.cfg.MaxSize {
		if buf.timer != nil {
			buf.timer.Stop()
			buf.timer = nil
		}
		msgs := buf.messages
		delete(b.buffers, key)
		b.mu.Unlock()
		return b.dispatch(ctx, key, msgs)
	}

	b.mu.Unlock()
	return nil
}

// flush is the timer callback. It pops the bucket and dispatches it; the
// timer goroutine carries no caller context, so dispatch uses Background.
func (b *batcher) flush(key string) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	buf, ok := b.buffers[key]
	if !ok || len(buf.messages) == 0 {
		b.mu.Unlock()
		return
	}
	msgs := buf.messages
	delete(b.buffers, key)
	b.mu.Unlock()

	if err := b.dispatch(context.Background(), key, msgs); err != nil {
		b.logger.Warn("slack batch flush failed",
			"bucket", key,
			"count", len(msgs),
			"error", err)
	}
}

// dispatch is the single sender. One queued message goes through as-is; >1
// collapses through summarize() into one message tagged mrkdwn.
func (b *batcher) dispatch(ctx context.Context, key string, msgs []*Message) error {
	b.wg.Add(1)
	defer b.wg.Done()

	var out *Message
	if len(msgs) == 1 {
		out = msgs[0]
	} else {
		s, err := b.summarize(ctx, key, msgs)
		if err != nil {
			return err
		}
		out = s
	}
	_, err := b.send(ctx, out)
	return err
}

// summarize collapses N>=2 messages into one. With a custom Summary expression
// the CEL evaluator produces the text; otherwise the built-in bullet formatter
// is used.
func (b *batcher) summarize(ctx context.Context, key string, msgs []*Message) (*Message, error) {
	channel := ""
	if b.cfg.GroupBy == "channel" {
		channel = key
	}
	if channel == "" && len(msgs) > 0 {
		channel = msgs[0].Channel
	}

	var text string
	if b.cfg.Summary == "" {
		text = defaultSummary(msgs)
	} else {
		msgMaps := make([]interface{}, len(msgs))
		for i, m := range msgs {
			msgMaps[i] = messageToMap(m)
		}
		activation := map[string]interface{}{
			"messages": msgMaps,
			"count":    int64(len(msgs)),
			"channel":  channel,
			"window":   b.cfg.Window.String(),
		}
		v, err := b.eval.EvaluateWith(ctx, b.cfg.Summary, activation)
		if err != nil {
			return nil, fmt.Errorf("batch summary CEL: %w", err)
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("batch summary must evaluate to a string (got %T)", v)
		}
		text = s
	}

	return &Message{
		Channel: channel,
		Text:    truncateForSlack(text),
		Mrkdwn:  true,
	}, nil
}

// Drain stops accepting new submissions, flushes every non-empty bucket
// synchronously, and waits for any in-flight dispatches started before the
// call. The first dispatch error is returned; the rest are logged. Safe to
// call multiple times.
func (b *batcher) Drain(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true

	snapshot := make(map[string][]*Message, len(b.buffers))
	for k, buf := range b.buffers {
		if buf.timer != nil {
			buf.timer.Stop()
		}
		if len(buf.messages) > 0 {
			snapshot[k] = buf.messages
		}
		delete(b.buffers, k)
	}
	b.mu.Unlock()

	var firstErr error
	for k, msgs := range snapshot {
		if err := b.dispatch(ctx, k, msgs); err != nil {
			if firstErr == nil {
				firstErr = err
			} else {
				b.logger.Warn("slack drain dispatch failed", "bucket", k, "error", err)
			}
		}
	}
	b.wg.Wait()
	return firstErr
}

// bucketKey is the per-message bucket identifier under the current GroupBy.
func (b *batcher) bucketKey(msg *Message) string {
	if b.cfg.GroupBy == "global" {
		return "*"
	}
	return msg.Channel
}

// shouldBypass reports whether a message must skip the batcher because
// collapsing it would either lose structure or land in the wrong thread.
func shouldBypass(m *Message) bool {
	return len(m.Blocks) > 0 || len(m.Attachments) > 0 || m.ThreadTS != ""
}

// messageToMap exposes the user-visible fields of a Message to the CEL
// summary expression. Internal/transport-only fields (UnfurlLinks, Mrkdwn…)
// are intentionally hidden.
func messageToMap(m *Message) map[string]interface{} {
	return map[string]interface{}{
		"text":     m.Text,
		"channel":  m.Channel,
		"username": m.Username,
	}
}

// defaultSummary renders the built-in bullet list used when no Summary CEL is
// configured. Empty texts are skipped; embedded newlines are collapsed so each
// bullet stays on one line in the Slack UI.
func defaultSummary(msgs []*Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📨 *%d events:*\n", len(msgs))
	for _, m := range msgs {
		t := strings.TrimSpace(m.Text)
		if t == "" {
			continue
		}
		t = strings.ReplaceAll(t, "\n", " ")
		fmt.Fprintf(&b, "• %s\n", t)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Slack's per-message text limit is ~40,000 bytes. We leave headroom for the
// JSON envelope and any markdown the renderer adds.
const slackMaxMessageBytes = 38000

func truncateForSlack(s string) string {
	if len(s) <= slackMaxMessageBytes {
		return s
	}
	return s[:slackMaxMessageBytes] + "\n…(truncated)"
}
