// Package tracing provides optional OpenTelemetry distributed tracing for the
// Mycel runtime. It is opt-in and a strict no-op unless configured: when no
// exporter is set up, the global tracer is OTel's built-in no-op and every
// instrumentation call costs essentially nothing.
//
// This is distinct from internal/trace, which is the development/debug tracer
// powering verbose flow logging and the Studio (DAP) debugger. The two are
// orthogonal and can be active at the same time.
//
// Tracing activates when MYCEL_TRACING is truthy OR an OTLP endpoint is
// configured via the standard OTEL_EXPORTER_OTLP_ENDPOINT /
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT environment variables. The OTLP exporter
// reads the rest of its configuration (headers, TLS/insecure, timeout) from the
// standard OTEL_* variables, so deployments configure it the same way as any
// other OpenTelemetry service.
package tracing

import (
	"context"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies Mycel's instrumentation scope in exported spans.
const tracerName = "github.com/matutetandil/mycel"

// Enabled reports whether distributed tracing should be set up. Opt-in: true
// when MYCEL_TRACING is truthy, or when an OTLP traces endpoint is configured.
func Enabled() bool {
	if truthy(os.Getenv("MYCEL_TRACING")) {
		return true
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Setup installs a global OTLP TracerProvider and the W3C trace-context +
// baggage propagator when tracing is enabled, and returns a shutdown function
// that flushes and stops the exporter. When tracing is disabled it does nothing
// and returns a no-op shutdown, leaving the global tracer as OTel's no-op.
//
// The returned shutdown must be called on runtime shutdown so buffered spans
// are flushed.
func Setup(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !Enabled() {
		return noop, nil
	}

	// The OTLP/gRPC exporter reads OTEL_EXPORTER_OTLP_* from the environment
	// when no explicit option overrides it.
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return noop, err
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(serviceVersion),
	))
	if err != nil {
		// A schema-URL conflict is not fatal; fall back to the default resource.
		res = resource.Default()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// StartFlowSpan starts the root span for a flow execution. It first extracts any
// inbound trace context from the source headers (W3C traceparent) so the flow
// joins an existing distributed trace, then opens a server-kind span. Returns
// the derived context (carrying the span) and the span itself. No-op-safe: when
// tracing is disabled this returns a non-recording span at near-zero cost.
func StartFlowSpan(ctx context.Context, flowName, source, operation string, headers map[string]interface{}) (context.Context, trace.Span) {
	if len(headers) > 0 {
		ctx = otel.GetTextMapPropagator().Extract(ctx, mapCarrier(headers))
	}
	ctx, span := otel.Tracer(tracerName).Start(ctx, "flow "+flowName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("mycel.flow", flowName),
			attribute.String("mycel.source", source),
		),
	)
	if operation != "" {
		span.SetAttributes(attribute.String("mycel.operation", operation))
	}
	return ctx, span
}

// StartConnectorSpan starts a client-kind child span around a connector
// write/step so the trace shows the flow's downstream calls.
func StartConnectorSpan(ctx context.Context, connectorName, operation, target string) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "write "+connectorName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("mycel.connector", connectorName)),
	)
	if operation != "" {
		span.SetAttributes(attribute.String("mycel.connector.operation", operation))
	}
	if target != "" {
		span.SetAttributes(attribute.String("mycel.connector.target", target))
	}
	return ctx, span
}

// End finalizes a span, recording err and marking the span as errored when err
// is non-nil.
func End(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// InjectHTTP writes the active trace context into outgoing HTTP headers so the
// downstream service continues the same distributed trace. No-op when tracing
// is disabled.
func InjectHTTP(ctx context.Context, header http.Header) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

// InjectInto merges the active trace context (W3C traceparent etc.) into a
// string header map for message-queue publishers whose protocol carries message
// headers — AMQP (RabbitMQ) and Kafka record headers. It creates the map if nil
// and returns it. When there is no active span (tracing disabled, or no flow
// span in ctx) it returns the map untouched without allocating, so it is a true
// no-op on the publish hot path.
//
// Redis Pub/Sub and MQTT v3 have no message-header mechanism, so trace context
// cannot be carried across those hops without mangling the payload — they are
// intentionally not propagated.
func InjectInto(ctx context.Context, headers map[string]string) map[string]string {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return headers
	}
	if headers == nil {
		headers = make(map[string]string)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
	return headers
}

// mapCarrier adapts a map[string]interface{} (Mycel's input["headers"]) to the
// OTel TextMapCarrier interface for inbound context extraction. Get is
// case-insensitive because header casing varies across sources (HTTP canonical
// "Traceparent" vs. an AMQP header set verbatim by the publisher) while the W3C
// propagator looks up the lowercase "traceparent".
type mapCarrier map[string]interface{}

func (c mapCarrier) Get(key string) string {
	for k, v := range c {
		if strings.EqualFold(k, key) {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func (c mapCarrier) Set(key, value string) { c[key] = value }

func (c mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
