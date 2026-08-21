package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// When a filter rejects a message and the policy is requeue, the message goes
// back on the queue — and something has to count how many times, or a message
// nobody wants circles forever. id_field names the expression that identifies
// it, so what that expression produces is the key the counter is kept under.

func idHandler(t *testing.T, idField string) (*FlowHandler, *bytes.Buffer) {
	t.Helper()
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	var logged bytes.Buffer
	return &FlowHandler{
		Config: &flow.Config{
			Name: "process",
			From: &flow.FromConfig{
				Connector:    "queue",
				FilterConfig: &flow.FilterConfig{IDField: idField},
			},
		},
		Transformer: transformer,
		Logger:      slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}, &logged
}

func TestAMessageIdentifierIsWhatTheExpressionSays(t *testing.T) {
	handler, _ := idHandler(t, "input.order_id")

	got, err := handler.evaluateIDField(context.Background(),
		map[string]interface{}{"order_id": "order-42"})
	if err != nil {
		t.Fatalf("evaluateIDField: %v", err)
	}
	if got != "order-42" {
		t.Errorf("id = %q", got)
	}
}

func TestANumericIdentifierIsTheNumberSomebodyWouldSearchFor(t *testing.T) {
	// JSON carries every number as a float, and the default formatting turns a
	// large one into 1.2345678e+07 — which is what went into the requeue
	// counter and into every log line about the message, so an operator
	// searching for order 12345678 found nothing.
	handler, _ := idHandler(t, "input.order_id")

	got, err := handler.evaluateIDField(context.Background(),
		map[string]interface{}{"order_id": float64(12345678)})
	if err != nil {
		t.Fatalf("evaluateIDField: %v", err)
	}
	if got != "12345678" {
		t.Errorf("id = %q, want the number as it was written", got)
	}
}

func TestAnIdentifierBuiltFromSeveralFields(t *testing.T) {
	// A message with no natural key is identified by what makes it unique.
	handler, _ := idHandler(t, `input.tenant + ":" + input.reference`)

	got, err := handler.evaluateIDField(context.Background(),
		map[string]interface{}{"tenant": "acme", "reference": "r-1"})
	if err != nil {
		t.Fatalf("evaluateIDField: %v", err)
	}
	if got != "acme:r-1" {
		t.Errorf("id = %q", got)
	}
}

func TestAnExpressionThatCannotBeEvaluatedIsReported(t *testing.T) {
	// Naming a field the message does not carry leaves the requeue counter
	// with nothing to count under, and the consumer falls back to
	// acknowledging: the message is dropped rather than retried. That is a
	// configuration mistake and it has to be visible as one.
	handler, _ := idHandler(t, "input.absent.nested")

	if _, err := handler.evaluateIDField(context.Background(), map[string]interface{}{}); err == nil {
		t.Error("an expression that cannot be evaluated was reported as an identifier")
	}
}

func TestNoIdFieldIsNoIdentifier(t *testing.T) {
	handler, _ := idHandler(t, "")
	got, err := handler.evaluateIDField(context.Background(), map[string]interface{}{"id": "x"})
	if got != "" || err != nil {
		t.Errorf("id = %q err = %v, want nothing at all", got, err)
	}
}

func TestTheReasonAnIdentifierIsMissingIsSaidOutLoud(t *testing.T) {
	// The consumer's warning says "no message ID available", which reads as a
	// message that arrived without one. When the real reason is an expression
	// that failed, the flow has to say so or the mistake is invisible.
	handler, logged := idHandler(t, "input.absent.nested")
	handler.Config.From.FilterConfig.OnReject = "requeue"
	handler.Config.From.FilterConfig.Condition = "false"

	result := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue"}
	handler.attachMessageID(context.Background(), map[string]interface{}{}, result)

	if result.MessageID != "" {
		t.Errorf("message id = %q, want none", result.MessageID)
	}
	if !strings.Contains(logged.String(), "id_field") {
		t.Errorf("nothing named the expression that failed:\n%s", logged.String())
	}
}
