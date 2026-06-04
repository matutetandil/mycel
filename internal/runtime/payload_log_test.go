package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/flow"
)

// newPayloadTestHandler builds a minimal FlowHandler wired to a buffer logger
// at the given level, sufficient to exercise logIncomingPayload.
func newPayloadTestHandler(level slog.Level, show bool, maxBytes int) (*FlowHandler, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level}))
	h := &FlowHandler{
		Config: &flow.Config{
			Name: "ingest_orders",
			From: &flow.FromConfig{Connector: "orders_queue"},
		},
		Logger:          logger,
		ShowPayload:     show,
		PayloadMaxBytes: maxBytes,
	}
	return h, buf
}

func TestLogIncomingPayload_ShowsAtDebug(t *testing.T) {
	h, buf := newPayloadTestHandler(slog.LevelDebug, true, 4096)
	h.logIncomingPayload(context.Background(), map[string]interface{}{
		"order_id": "A-1",
		"email":    "Foo@Bar.com",
	})

	out := buf.String()
	if !strings.Contains(out, "incoming payload") {
		t.Fatalf("expected 'incoming payload' line, got: %s", out)
	}
	if !strings.Contains(out, "orders_queue") {
		t.Errorf("expected source connector in log, got: %s", out)
	}
	if !strings.Contains(out, "A-1") || !strings.Contains(out, "Foo@Bar.com") {
		t.Errorf("expected payload contents in log, got: %s", out)
	}
}

func TestLogIncomingPayload_DisabledWhenShowFalse(t *testing.T) {
	h, buf := newPayloadTestHandler(slog.LevelDebug, false, 4096)
	h.logIncomingPayload(context.Background(), map[string]interface{}{"k": "v"})
	if strings.Contains(buf.String(), "incoming payload") {
		t.Errorf("payload should not be logged when ShowPayload=false, got: %s", buf.String())
	}
}

func TestLogIncomingPayload_SilentBelowDebug(t *testing.T) {
	// MYCEL_PAYLOAD_SHOW=true but level is info: nothing should be emitted.
	h, buf := newPayloadTestHandler(slog.LevelInfo, true, 4096)
	h.logIncomingPayload(context.Background(), map[string]interface{}{"k": "v"})
	if strings.Contains(buf.String(), "incoming payload") {
		t.Errorf("payload should not be logged below debug level, got: %s", buf.String())
	}
}

func TestLogIncomingPayload_Truncates(t *testing.T) {
	big := strings.Repeat("x", 5000)
	h, buf := newPayloadTestHandler(slog.LevelDebug, true, 100)
	h.logIncomingPayload(context.Background(), map[string]interface{}{"blob": big})

	out := buf.String()
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation marker, got: %s", out)
	}
	// The full 5000-char blob must not be present verbatim.
	if strings.Contains(out, big) {
		t.Errorf("payload was not truncated")
	}
}

func TestFormatPayload_DefaultCapWhenNonPositive(t *testing.T) {
	got := formatPayload(map[string]interface{}{"a": 1}, 0)
	if got != `{"a":1}` {
		t.Errorf("formatPayload = %q, want %q", got, `{"a":1}`)
	}
}
