package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// capturedLogs swaps the process logger for one writing into a buffer, so a
// test can assert on what an operator would actually read.
func capturedLogs(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf, func() { slog.SetDefault(prev) }
}

// logged reports whether any line at the given level contains the message.
func logged(t *testing.T, buf *bytes.Buffer, level, msg string) bool {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]interface{}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec["level"] == level && strings.Contains(rec["msg"].(string), msg) {
			return true
		}
	}
	return false
}

// A flow behaving exactly as written must not log as though it were
// misconfigured. Deciding not to signal — because the message was dropped, or
// because signal.when said no — is reported once, at the level that suits it,
// by the code that knows the reason. The emitter used to warn on top of that
// about an "empty key", which reads as a fault in an expression that is doing
// its job.
func TestCoordinateSignal_DropDoesNotWarnAboutAnEmptyKey(t *testing.T) {
	buf, restore := capturedLogs(t)
	defer restore()

	h, _, done := newSignallingHandler(t, true, false)
	defer done()

	ctx := context.Background()
	first := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "1"}}
	if _, err := h.HandleRequest(ctx, first); err != nil {
		t.Fatalf("first HandleRequest: %v", err)
	}

	buf.Reset()

	second := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "2"}}
	result, err := h.HandleRequest(ctx, second)
	if err != nil {
		t.Fatalf("second HandleRequest: %v", err)
	}
	if drop, ok := result.(*flow.FilteredResultWithPolicy); !ok || !drop.Filtered {
		t.Fatalf("expected a drop, got %T", result)
	}

	if logged(t, buf, "WARN", "empty key") {
		t.Errorf("a dropped message warned about an empty signal key; it reads as a config error over correct behaviour:\n%s", buf.String())
	}
	if !logged(t, buf, "INFO", "coordinate signal skipped") {
		t.Errorf("the reason for not signalling must still be reported:\n%s", buf.String())
	}
}

// The same, for the other deliberate no-emit.
func TestCoordinateSignal_WhenFalseDoesNotWarnAboutAnEmptyKey(t *testing.T) {
	buf, restore := capturedLogs(t)
	defer restore()

	h, _, done := newSignallingHandler(t, false, false)
	defer done()
	h.Config.Coordinate.Signal.When = "false"

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "1"}}
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if logged(t, buf, "WARN", "empty key") {
		t.Errorf("signal.when = false warned about an empty key:\n%s", buf.String())
	}
	if !logged(t, buf, "INFO", "coordinate signal skipped") {
		t.Errorf("the reason must still be reported:\n%s", buf.String())
	}
}

// The genuine fault still speaks. An emit expression that evaluates cleanly to
// nothing is a misconfiguration, and quieting the two cases above must not
// quiet this one — the warning moves to the code that knows which expression
// it was, and now names it.
func TestCoordinateSignal_EmptyKeyStillWarns(t *testing.T) {
	buf, restore := capturedLogs(t)
	defer restore()

	h, _, done := newSignallingHandler(t, false, false)
	defer done()
	// Evaluates cleanly, to nothing.
	h.Config.Coordinate.Signal.Emit = "'' + input.body.missing_prefix"

	input := map[string]interface{}{"body": map[string]interface{}{"sku": "X1", "n": "1", "missing_prefix": ""}}
	if _, err := h.HandleRequest(context.Background(), input); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	if !logged(t, buf, "WARN", "empty key") {
		t.Errorf("an emit expression evaluating to nothing must still warn:\n%s", buf.String())
	}
}

var _ = io.Discard
