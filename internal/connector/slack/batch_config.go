package slack

import (
	"fmt"
	"time"
)

// BatchConfig controls how the Slack connector coalesces high-rate writes into
// a single summary message to stay under Slack's per-channel rate limit
// (chat.postMessage and incoming webhooks ~1 msg/sec; above that Slack hides
// messages with "high volume of activity" instead of erroring).
//
// Batching is ENABLED BY DEFAULT. Authors who need every message delivered
// individually opt out with `batch { enabled = false }`.
type BatchConfig struct {
	// Enabled toggles the batcher. Default true (set false to keep the
	// pre-v2.5.0 behavior of sending every message immediately).
	Enabled bool

	// Window is the tumbling-window duration. The first message in an empty
	// bucket arms a timer; the bucket flushes when the timer fires (or when
	// MaxSize is reached, whichever comes first). Default 3s.
	Window time.Duration

	// MaxSize caps the number of messages held in one bucket before forcing a
	// flush, so a sustained flood cannot grow memory unbounded. Default 50.
	MaxSize int

	// GroupBy chooses the bucket key: "channel" (one buffer per Slack channel,
	// matching Slack's per-channel rate limit — default) or "global" (one
	// shared buffer; useful for connectors that always post to a single
	// channel).
	GroupBy string

	// Summary is an optional CEL expression that produces the text of the
	// collapsed message. Available variables: `messages` (the buffered
	// messages, each with text/channel/username), `count` (int), `channel`
	// (string), `window` (string). Must evaluate to a string. Empty means use
	// the built-in bullet-list formatter.
	Summary string
}

// DefaultBatchConfig is the default-on configuration applied when the user
// does not declare a `batch { }` block. Batching is enabled with conservative
// defaults that keep low-rate traffic indistinguishable from the pre-v2.5.0
// behavior (a lone message in a 3s window passes through unwrapped).
func DefaultBatchConfig() *BatchConfig {
	return &BatchConfig{
		Enabled: true,
		Window:  3 * time.Second,
		MaxSize: 50,
		GroupBy: "channel",
	}
}

// parseBatchConfig converts the parser-provided Properties["batch"] value into
// a BatchConfig. The parser stores a `batch { ... }` block as a
// map[string]interface{}; nil means the user did not declare the block, in
// which case the defaults apply.
func parseBatchConfig(raw interface{}) (*BatchConfig, error) {
	cfg := DefaultBatchConfig()
	if raw == nil {
		return cfg, nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("batch must be a block, e.g. batch { window = \"5s\" }")
	}

	if v, ok := m["enabled"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("batch.enabled must be a bool")
		}
		cfg.Enabled = b
	}

	if v, ok := m["window"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("batch.window must be a duration string (e.g. \"3s\")")
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("batch.window: %w", err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("batch.window must be positive")
		}
		cfg.Window = d
	}

	if v, ok := m["max_size"]; ok {
		n, ok := coerceInt(v)
		if !ok {
			return nil, fmt.Errorf("batch.max_size must be an integer")
		}
		if n <= 0 {
			return nil, fmt.Errorf("batch.max_size must be positive")
		}
		cfg.MaxSize = n
	}

	if v, ok := m["group_by"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("batch.group_by must be \"channel\" or \"global\"")
		}
		switch s {
		case "channel", "global":
			cfg.GroupBy = s
		default:
			return nil, fmt.Errorf("batch.group_by must be \"channel\" or \"global\" (got %q)", s)
		}
	}

	if v, ok := m["summary"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("batch.summary must be a CEL expression string")
		}
		cfg.Summary = s
	}

	return cfg, nil
}

// coerceInt accepts the variety of numeric types HCL/ctyValueToGo can produce
// (int, int64, float64) and yields an int.
func coerceInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
