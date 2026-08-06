package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/flow"
)

func dropLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level}))
}

func dropHandler(buf *bytes.Buffer, showPayload bool) *FlowHandler {
	return &FlowHandler{
		Config: &flow.Config{
			Name: "sync_orders",
			From: &flow.FromConfig{Connector: "rabbit"},
		},
		Logger:          dropLogger(buf, slog.LevelDebug),
		ShowPayload:     showPayload,
		PayloadMaxBytes: 4096,
	}
}

var sampleInput = map[string]interface{}{"body": map[string]interface{}{"sku": "ABC-123"}}

// A drop must name the gate, the block behind it, and what that block was
// evaluating — the label alone does not tell you where to go and fix it.
func TestLogDroppedMessage_NamesGateAndDecidingConfig(t *testing.T) {
	var buf bytes.Buffer
	h := dropHandler(&buf, false)

	h.logDroppedMessage(context.Background(), sampleInput, &flow.FilteredResultWithPolicy{
		Filtered: true,
		Reason:   "filter",
		Detail:   "input.body.total > 100",
		Policy:   "ack",
	})

	out := buf.String()
	for _, want := range []string{
		"message dropped by policy",
		"sync_orders",
		"reason=filter",
		"from { filter }",
		"input.body.total > 100",
		"disposition=ack",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}

// A dropped message is still customer data, so the payload rides on the same
// explicit opt-in as the incoming-payload log rather than appearing by default.
func TestLogDroppedMessage_PayloadRequiresOptIn(t *testing.T) {
	drop := &flow.FilteredResultWithPolicy{Filtered: true, Reason: "accept", Detail: "x"}

	var off bytes.Buffer
	dropHandler(&off, false).logDroppedMessage(context.Background(), sampleInput, drop)
	if strings.Contains(off.String(), "ABC-123") {
		t.Errorf("payload logged without MYCEL_PAYLOAD_SHOW:\n%s", off.String())
	}
	if !strings.Contains(off.String(), "reason=accept") {
		t.Errorf("the reason must be logged regardless of the payload opt-in:\n%s", off.String())
	}

	var on bytes.Buffer
	dropHandler(&on, true).logDroppedMessage(context.Background(), sampleInput, drop)
	if !strings.Contains(on.String(), "ABC-123") {
		t.Errorf("payload missing with MYCEL_PAYLOAD_SHOW on:\n%s", on.String())
	}
}

// Every gate maps to the HCL block a user would go and edit.
func TestDropDecidedBy_CoversEveryReason(t *testing.T) {
	cases := map[string]string{
		"filter":             "from { filter }",
		"accept":             "accept { }",
		"dedupe_match":       "dedupe { }",
		"sequence_older":     "sequence_guard { }",
		"coordinate_timeout": "coordinate { on_timeout }",
	}
	for reason, want := range cases {
		if got := dropDecidedBy(reason); got != want {
			t.Errorf("dropDecidedBy(%q) = %q, want %q", reason, got, want)
		}
	}

	// An unknown reason falls back to itself rather than to an empty label.
	if got := dropDecidedBy("something_new"); got != "something_new" {
		t.Errorf("unknown reason should pass through, got %q", got)
	}
}

// A result that is not a drop, and a nil result, must log nothing.
func TestLogDroppedMessage_IgnoresNonDrops(t *testing.T) {
	var buf bytes.Buffer
	h := dropHandler(&buf, true)

	h.logDroppedMessage(context.Background(), sampleInput, nil)
	h.logDroppedMessage(context.Background(), sampleInput, &flow.FilteredResultWithPolicy{Filtered: false})

	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("expected no output for non-drops:\n%s", buf.String())
	}
}

// Below debug the line is skipped entirely, so the payload is never marshalled
// on a hot path.
func TestLogDroppedMessage_SkippedAboveDebug(t *testing.T) {
	var buf bytes.Buffer
	h := dropHandler(&buf, true)
	h.Logger = dropLogger(&buf, slog.LevelInfo)

	h.logDroppedMessage(context.Background(), sampleInput, &flow.FilteredResultWithPolicy{
		Filtered: true, Reason: "filter",
	})

	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("expected nothing at info level:\n%s", buf.String())
	}
}
