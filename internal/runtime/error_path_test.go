package runtime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// What happens when a flow fails is the half nobody exercises until it does.
// A fallback is where a message goes when it cannot be processed — the record
// somebody replays tomorrow — and an error response is what the caller is told.
// Both are built from expressions, and both used to swallow a failure in
// building them.

func failingFlow(t *testing.T, handling *flow.ErrorHandlingConfig, writers map[string]*recordingWriter) (*FlowHandler, *bytes.Buffer) {
	t.Helper()
	registry := connector.NewRegistry()
	for name, writer := range writers {
		registry.Replace(name, writer)
	}
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	var logged bytes.Buffer
	return &FlowHandler{
		Config: &flow.Config{
			Name:          "process_order",
			ErrorHandling: handling,
		},
		Connectors:  registry,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}, &logged
}

func TestAMessageThatCouldNotBeProcessedReachesTheFallback(t *testing.T) {
	dlq := &recordingWriter{name: "dlq"}
	h, _ := failingFlow(t, &flow.ErrorHandlingConfig{
		Fallback: &flow.FallbackConfig{
			Connector: "dlq", Target: "orders.failed", IncludeError: true,
		},
	}, map[string]*recordingWriter{"dlq": dlq})

	input := map[string]interface{}{"id": "order-1", "total": 42}
	if err := h.sendToFallback(context.Background(), input, errors.New("the database is down")); err != nil {
		t.Fatalf("sendToFallback: %v", err)
	}

	writes := dlq.writes()
	if len(writes) != 1 {
		t.Fatalf("the fallback received %d messages", len(writes))
	}
	if writes[0].Target != "orders.failed" {
		t.Errorf("target = %q", writes[0].Target)
	}

	// The message itself, because a fallback nobody can replay is a log line
	// with extra steps.
	original, _ := writes[0].Payload["original_input"].(map[string]interface{})
	if original == nil || original["id"] != "order-1" {
		t.Errorf("the message was not carried: %v", writes[0].Payload)
	}

	// And why it is there.
	failure, _ := writes[0].Payload["error"].(map[string]interface{})
	if failure == nil || !strings.Contains(failure["message"].(string), "database is down") {
		t.Errorf("the reason was not carried: %v", writes[0].Payload)
	}
	if failure["flow_name"] != "process_order" {
		t.Errorf("the flow was not named: %v", failure)
	}
}

func TestTheReasonIsLeftOutWhenItWasNotAskedFor(t *testing.T) {
	// An error message can carry more about the inside of a service than
	// somebody wants in a queue other teams read.
	dlq := &recordingWriter{name: "dlq"}
	h, _ := failingFlow(t, &flow.ErrorHandlingConfig{
		Fallback: &flow.FallbackConfig{Connector: "dlq", Target: "orders.failed"},
	}, map[string]*recordingWriter{"dlq": dlq})

	if err := h.sendToFallback(context.Background(),
		map[string]interface{}{"id": "order-1"}, errors.New("connection string: postgres://user:hunter2@db")); err != nil {
		t.Fatalf("sendToFallback: %v", err)
	}

	if _, present := dlq.writes()[0].Payload["error"]; present {
		t.Error("the reason was included although the flow did not ask for it")
	}
}

func TestAFallbackCanBeShapedForWhoeverReadsIt(t *testing.T) {
	dlq := &recordingWriter{name: "dlq"}
	h, _ := failingFlow(t, &flow.ErrorHandlingConfig{
		Fallback: &flow.FallbackConfig{
			Connector: "dlq", Target: "orders.failed",
			Transform: map[string]string{
				"order_id": "input.id",
				"reason":   "error.message",
			},
			TransformOrder: []string{"order_id", "reason"},
		},
	}, map[string]*recordingWriter{"dlq": dlq})

	err := h.sendToFallback(context.Background(),
		map[string]interface{}{"id": "order-1"}, errors.New("the database is down"))
	if err != nil {
		t.Fatalf("sendToFallback: %v", err)
	}

	sent := dlq.writes()[0].Payload
	if sent["order_id"] != "order-1" {
		t.Errorf("the shaped message = %v", sent)
	}
	if reason, _ := sent["reason"].(string); !strings.Contains(reason, "database is down") {
		t.Errorf("reason = %v", sent["reason"])
	}
}

func TestAFallbackShapeThatFailsIsSaidOutLoud(t *testing.T) {
	// The message still goes — losing it entirely is worse — but in a shape
	// the flow did not ask for. A record that lands in a dead-letter queue
	// looking like something else is one nobody can replay, so it cannot pass
	// in silence.
	dlq := &recordingWriter{name: "dlq"}
	h, logged := failingFlow(t, &flow.ErrorHandlingConfig{
		Fallback: &flow.FallbackConfig{
			Connector: "dlq", Target: "orders.failed",
			Transform:      map[string]string{"order_id": "input.absent.nested"},
			TransformOrder: []string{"order_id"},
		},
	}, map[string]*recordingWriter{"dlq": dlq})

	if err := h.sendToFallback(context.Background(),
		map[string]interface{}{"id": "order-1"}, errors.New("the database is down")); err != nil {
		t.Fatalf("sendToFallback: %v", err)
	}

	if len(dlq.writes()) != 1 {
		t.Fatal("the message was lost because its shape could not be built")
	}
	if !strings.Contains(logged.String(), "fallback transform") {
		t.Errorf("nothing was said about the shape that could not be built:\n%s", logged.String())
	}
}

func TestAFallbackThatCannotBeReachedIsReported(t *testing.T) {
	// The caller has to know the message is nowhere, since that is the moment
	// a consumer decides between acknowledging and redelivering.
	for name, handling := range map[string]*flow.ErrorHandlingConfig{
		"a connector that does not exist": {
			Fallback: &flow.FallbackConfig{Connector: "absent", Target: "x"},
		},
		"a connector that cannot be written to": {
			Fallback: &flow.FallbackConfig{Connector: "reader", Target: "x"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry := connector.NewRegistry()
			registry.Replace("reader", &stepConnector{name: "reader"})
			tr, _ := transform.NewCELTransformer()
			h := &FlowHandler{
				Config:      &flow.Config{Name: "f", ErrorHandling: handling},
				Connectors:  registry,
				Transformer: tr,
			}

			err := h.sendToFallback(context.Background(), map[string]interface{}{}, errors.New("failed"))
			if err == nil {
				t.Fatal("a message that reached no fallback was reported as delivered")
			}
			if !strings.Contains(err.Error(), handling.Fallback.Connector) {
				t.Errorf("error = %q, want the connector named", err)
			}
		})
	}
}

// An error_response is what a caller is told when the flow could not do what it
// was asked. Without one they get whatever the failure happened to say.

func TestAFlowCanSayWhatAFailureLooksLike(t *testing.T) {
	h, _ := failingFlow(t, &flow.ErrorHandlingConfig{
		ErrorResponse: &flow.ErrorResponseConfig{
			Status:  503,
			Headers: map[string]string{"Retry-After": "30"},
			Body: map[string]string{
				"code":    `"order_service_unavailable"`,
				"message": `"We could not take your order just now"`,
				"order":   "input.id",
			},
			BodyOrder: []string{"code", "message", "order"},
		},
	}, nil)

	wrapped := h.wrapErrorResponse(context.Background(),
		map[string]interface{}{"id": "order-1"}, errors.New("the database is down"))

	var flowErr *flow.FlowError
	if !errors.As(wrapped, &flowErr) {
		t.Fatalf("the failure came back as %T, want one carrying the response", wrapped)
	}
	if flowErr.Status != 503 {
		t.Errorf("status = %d", flowErr.Status)
	}
	if flowErr.Headers["Retry-After"] != "30" {
		t.Errorf("headers = %v, want the ones that tell a client when to come back", flowErr.Headers)
	}

	body := flowErr.Body
	if body == nil || body["code"] != "order_service_unavailable" {
		t.Errorf("body = %v", flowErr.Body)
	}
	// The reason the database gave is not the caller's business, and this is
	// where that is decided.
	if strings.Contains(strings.ToLower(join(body)), "database") {
		t.Errorf("the body carries what went wrong inside: %v", body)
	}
	if body["order"] != "order-1" {
		t.Errorf("the body could not read the request: %v", body)
	}
}

func TestWithNoErrorResponseTheFailureIsPassedAlong(t *testing.T) {
	h, _ := failingFlow(t, nil, nil)
	original := errors.New("the database is down")
	if got := h.wrapErrorResponse(context.Background(), map[string]interface{}{}, original); got != original {
		t.Errorf("the failure came back as %v, want it untouched", got)
	}
}

func TestAnErrorBodyThatFailsToBuildStillAnswers(t *testing.T) {
	// A caller waiting on a request must get an answer even when the shape of
	// it could not be built — but the difference has to be findable, or it is
	// blamed on the client.
	h, logged := failingFlow(t, &flow.ErrorHandlingConfig{
		ErrorResponse: &flow.ErrorResponseConfig{
			Status:    503,
			Body:      map[string]string{"code": "input.absent.nested"},
			BodyOrder: []string{"code"},
		},
	}, nil)

	wrapped := h.wrapErrorResponse(context.Background(),
		map[string]interface{}{}, errors.New("the database is down"))

	var flowErr *flow.FlowError
	if !errors.As(wrapped, &flowErr) {
		t.Fatalf("no answer came back: %v", wrapped)
	}
	if flowErr.Status != 503 {
		t.Errorf("status = %d, want the one that was configured", flowErr.Status)
	}
	if !strings.Contains(logged.String(), "error_response") {
		t.Errorf("nothing was said about the body that could not be built:\n%s", logged.String())
	}
}

func join(m map[string]interface{}) string {
	var parts []string
	for _, v := range m {
		parts = append(parts, strings.TrimSpace(strings.ToLower(toString(v))))
	}
	return strings.Join(parts, " ")
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
