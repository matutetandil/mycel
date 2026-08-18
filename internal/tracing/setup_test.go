package tracing

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// Turning tracing on, and what it costs when it is off.
//
// Tracing is opt-in and the default is a service that pays nothing for it, so
// the switch itself is the part worth checking: read wrongly in one direction
// a service exports nothing while somebody waits for spans, and in the other
// every service starts an exporter to a collector that is not there.

func TestWhatTurnsTracingOn(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		want bool
	}{
		"nothing set at all":          {nil, false},
		"switched on":                 {map[string]string{"MYCEL_TRACING": "true"}, true},
		"switched on as a number":     {map[string]string{"MYCEL_TRACING": "1"}, true},
		"switched on in another word": {map[string]string{"MYCEL_TRACING": "yes"}, true},
		"switched on loudly":          {map[string]string{"MYCEL_TRACING": "ON"}, true},
		"switched off explicitly":     {map[string]string{"MYCEL_TRACING": "false"}, false},
		"a word nobody meant as yes":  {map[string]string{"MYCEL_TRACING": "maybe"}, false},
		// Naming a collector is enough: this is the standard variable, and a
		// service given one and not exporting is the confusing half.
		"a collector to send to":      {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4317"}, true},
		"a collector for traces only": {map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector:4317"}, true},
		"an empty collector address":  {map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": ""}, false},
	} {
		t.Run(name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAServiceThatIsNotTracing(t *testing.T) {
	// The default. Setting up has to be free and has to hand back something
	// safe to call on the way out — a shutdown that is nil would panic every
	// service that does not trace, which is most of them.
	shutdown, err := Setup(context.Background(), "orders", "2.19.0")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if shutdown == nil {
		t.Fatal("no shutdown was returned, so the caller has nothing to defer")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}

	// And a span started without tracing is a no-op that still returns a
	// usable context and span: every instrumentation point calls these
	// unconditionally.
	ctx, span := StartSpan(context.Background(), "process_order")
	if ctx == nil || span == nil {
		t.Fatal("starting a span with tracing off returned nothing")
	}
	End(span, nil)
	End(nil, nil)
}

func TestALogLineFromInsideATrace(t *testing.T) {
	// What links a log line to the trace it belongs to. With no active span
	// the record passes through untouched, which is what makes it safe to
	// install whether or not tracing is on.
	var written bytes.Buffer
	handler := NewLogHandler(slog.NewJSONHandler(&written, nil))
	logger := slog.New(handler)

	logger.InfoContext(context.Background(), "processing order", "order_id", "order-1")

	line := written.String()
	if !strings.Contains(line, "order-1") {
		t.Errorf("the log line lost its attributes: %s", line)
	}
	if strings.Contains(line, "trace_id") {
		t.Errorf("a line logged outside a trace was given a trace id: %s", line)
	}

	// The wrapper has to survive the two things slog does to a handler —
	// otherwise a logger built with .With() loses the trace ids entirely.
	grouped := handler.WithGroup("flow").WithAttrs([]slog.Attr{slog.String("name", "process_order")})
	if grouped == nil {
		t.Fatal("wrapping the handler returned nothing")
	}
	written.Reset()
	slog.New(grouped).InfoContext(context.Background(), "processing order")
	if !strings.Contains(written.String(), "process_order") {
		t.Errorf("attributes added to the handler were lost: %s", written.String())
	}
}

func TestCarryingATraceBetweenServices(t *testing.T) {
	// A trace crosses a service boundary in a header. What is being checked
	// here is the carrier over Mycel's own shape for headers — a map of
	// anything, because that is what a flow's input holds.
	carrier := mapCarrier{
		"traceparent":  "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"content-type": "application/json",
		"x-count":      42,
	}

	if got := carrier.Get("traceparent"); !strings.HasPrefix(got, "00-") {
		t.Errorf("the trace header read as %q", got)
	}
	// A header that is not text is not a trace header, and must not be
	// rendered into one.
	if got := carrier.Get("x-count"); got != "" {
		t.Errorf("a header that is not text read as %q", got)
	}
	if got := carrier.Get("absent"); got != "" {
		t.Errorf("a header nobody sent read as %q", got)
	}

	carrier.Set("tracestate", "vendor=1")
	if carrier["tracestate"] != "vendor=1" {
		t.Errorf("setting a header did not take: %v", carrier)
	}

	keys := carrier.Keys()
	if len(keys) != 4 {
		t.Errorf("keys = %v", keys)
	}
}

func TestSendingATraceOnward(t *testing.T) {
	// With tracing off there is nothing to inject, and the important part is
	// that it is not an error: these run on every outbound call.
	headers := map[string]string{"content-type": "application/json"}
	after := InjectInto(context.Background(), headers)

	if after == nil {
		t.Fatal("injecting into headers returned nothing")
	}
	if after["content-type"] != "application/json" {
		t.Errorf("the headers that were already there were lost: %v", after)
	}

	// A message with no headers keeps none: with tracing off there is nothing
	// to add, and allocating an empty map per message to hand back would be
	// waste on a path that runs for every message published.
	if got := InjectInto(context.Background(), nil); got != nil {
		t.Errorf("a message with no headers was given %v", got)
	}
}
