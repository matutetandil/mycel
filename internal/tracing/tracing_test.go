package tracing

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// A fixed, valid W3C traceparent for join-existing-trace assertions.
const (
	parentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	parentSpanID  = "00f067aa0ba902b7"
	traceparent   = "00-" + parentTraceID + "-" + parentSpanID + "-01"
)

func hasAttr(span sdktrace.ReadOnlySpan, key, value string) bool {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key && kv.Value.AsString() == value {
			return true
		}
	}
	return false
}

func TestEnabled(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"off by default", map[string]string{}, false},
		{"MYCEL_TRACING=true", map[string]string{"MYCEL_TRACING": "true"}, true},
		{"MYCEL_TRACING=1", map[string]string{"MYCEL_TRACING": "1"}, true},
		{"MYCEL_TRACING=false", map[string]string{"MYCEL_TRACING": "false"}, false},
		{"OTLP endpoint set", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4317"}, true},
		{"OTLP traces endpoint set", map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector:4317"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear the relevant vars, then apply the case.
			t.Setenv("MYCEL_TRACING", "")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// withRecorder installs a recorder-backed provider + W3C propagator as the
// globals and restores the previous ones on cleanup.
func withRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	return sr
}

func TestStartFlowSpanNoopByDefault(t *testing.T) {
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	ctx, span := StartFlowSpan(context.Background(), "create_user", "api", "POST", nil)
	if span.IsRecording() {
		t.Error("expected a non-recording (no-op) span when tracing is disabled")
	}
	// Must not panic and must be safe to end.
	End(span, errors.New("boom"))
	if ctx == nil {
		t.Error("StartFlowSpan returned a nil context")
	}
}

func TestStartFlowSpanJoinsInboundTrace(t *testing.T) {
	sr := withRecorder(t)

	headers := map[string]interface{}{"traceparent": traceparent}
	_, span := StartFlowSpan(context.Background(), "item_update", "rabbit", "update", headers)

	// The flow span must inherit the inbound trace id (same distributed trace).
	if got := span.SpanContext().TraceID().String(); got != parentTraceID {
		t.Errorf("flow span trace id = %s, want inbound %s", got, parentTraceID)
	}
	End(span, nil)

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 recorded span, got %d", len(ended))
	}
	if name := ended[0].Name(); name != "flow item_update" {
		t.Errorf("span name = %q, want %q", name, "flow item_update")
	}
	if parent := ended[0].Parent().SpanID().String(); parent != parentSpanID {
		t.Errorf("span parent id = %s, want %s", parent, parentSpanID)
	}
	if !hasAttr(ended[0], "mycel.flow", "item_update") {
		t.Error("missing mycel.flow attribute")
	}
}

func TestStartFlowSpanHeaderCaseInsensitive(t *testing.T) {
	withRecorder(t)
	// AMQP/HTTP may carry the header with different casing than the lowercase
	// "traceparent" the W3C propagator looks up.
	headers := map[string]interface{}{"Traceparent": traceparent}
	_, span := StartFlowSpan(context.Background(), "f", "src", "", headers)
	if got := span.SpanContext().TraceID().String(); got != parentTraceID {
		t.Errorf("case-insensitive extraction failed: trace id = %s, want %s", got, parentTraceID)
	}
	End(span, nil)
}

func TestConnectorSpanErrorStatus(t *testing.T) {
	sr := withRecorder(t)
	_, span := StartConnectorSpan(context.Background(), "magento", "POST", "/rest/V1/x")
	End(span, errors.New("502 bad gateway"))

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 recorded span, got %d", len(ended))
	}
	if ended[0].Status().Code != codes.Error {
		t.Errorf("status code = %v, want codes.Error", ended[0].Status().Code)
	}
	if ended[0].Status().Description != "502 bad gateway" {
		t.Errorf("status description = %q, want error message", ended[0].Status().Description)
	}
	if len(ended[0].Events()) == 0 {
		t.Error("expected an exception event from RecordError")
	}
}

func TestInjectHTTPRoundtrip(t *testing.T) {
	withRecorder(t)

	// Start a span, inject its context into outgoing HTTP headers, and confirm
	// a traceparent is written that carries the same trace id.
	ctx, span := StartConnectorSpan(context.Background(), "http_client", "GET", "/users")
	defer span.End()

	h := http.Header{}
	InjectHTTP(ctx, h)

	tp := h.Get("traceparent")
	if tp == "" {
		t.Fatal("InjectHTTP wrote no traceparent header")
	}
	// Round-trip back through the map carrier and confirm the trace id matches.
	extracted := otel.GetTextMapPropagator().Extract(
		context.Background(),
		mapCarrier(map[string]interface{}{"traceparent": tp}),
	)
	if got, want := trace.SpanContextFromContext(extracted).TraceID(), span.SpanContext().TraceID(); got != want {
		t.Errorf("round-trip trace id = %s, want %s", got, want)
	}
}

func TestInjectIntoMessageHeaders(t *testing.T) {
	withRecorder(t)

	ctx, span := StartConnectorSpan(context.Background(), "rabbit", "publish", "q")
	defer span.End()

	headers := InjectInto(ctx, nil)
	tp, ok := headers["traceparent"]
	if !ok || tp == "" {
		t.Fatalf("InjectInto did not write a traceparent header; got %v", headers)
	}
	// The injected context round-trips back to the same trace.
	extracted := otel.GetTextMapPropagator().Extract(context.Background(), mapCarrier(toAny(headers)))
	if got, want := trace.SpanContextFromContext(extracted).TraceID(), span.SpanContext().TraceID(); got != want {
		t.Errorf("round-trip trace id = %s, want %s", got, want)
	}
}

func TestInjectIntoNoopWithoutSpan(t *testing.T) {
	withRecorder(t)
	// No active span in ctx → must not allocate or write, returns input as-is.
	if got := InjectInto(context.Background(), nil); got != nil {
		t.Errorf("InjectInto with no active span returned %v, want nil (no allocation)", got)
	}
	existing := map[string]string{"x": "y"}
	got := InjectInto(context.Background(), existing)
	if len(got) != 1 || got["x"] != "y" {
		t.Errorf("InjectInto with no active span mutated headers: %v", got)
	}
}

func TestLogHandlerAddsTraceID(t *testing.T) {
	withRecorder(t)

	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(NewLogHandler(base))

	// With an active span, the record gains trace_id / span_id.
	ctx, span := StartFlowSpan(context.Background(), "f", "src", "", nil)
	logger.InfoContext(ctx, "during flow")
	span.End()

	out := buf.String()
	wantTID := span.SpanContext().TraceID().String()
	if !strings.Contains(out, `"trace_id":"`+wantTID+`"`) {
		t.Errorf("log line missing trace_id %s: %s", wantTID, out)
	}
	if !strings.Contains(out, `"span_id":"`) {
		t.Errorf("log line missing span_id: %s", out)
	}

	// Without an active span, no trace fields are added.
	buf.Reset()
	logger.InfoContext(context.Background(), "no span")
	if strings.Contains(buf.String(), "trace_id") {
		t.Errorf("unexpected trace_id without an active span: %s", buf.String())
	}
}

func toAny(m map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
