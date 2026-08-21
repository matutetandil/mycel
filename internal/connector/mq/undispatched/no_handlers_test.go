package undispatched

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// A consumer nobody reads from is worth saying out loud.
//
// It is the shape that costs the most to find in production: the broker routes
// messages to a queue this service consumes, nothing claims them, and every
// one is dropped. The queue looks alive — deliveries, no acks — and nothing
// else moves. The line has to name the connector, because the fix is a flow
// whose `from` points at it.
func TestAConsumerWithNoFlowsSaysSoAndNamesItself(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ReportNoHandlers(logger, "orders_queue", "rabbitmq", "orders")

	said := out.String()
	for _, want := range []string{"orders_queue", "rabbitmq", "orders", "dropped"} {
		if !strings.Contains(said, want) {
			t.Errorf("the notice does not mention %q:\n%s", want, said)
		}
	}
	// Loud: this is not a detail somebody finds by turning debug logging on.
	if !strings.Contains(said, "ERROR") {
		t.Errorf("the notice is not an error:\n%s", said)
	}
	// And it says what to write, since the fix is not obvious from the symptom.
	if !strings.Contains(said, "from") {
		t.Errorf("the notice does not say what to write:\n%s", said)
	}
}

// Called with no logger it still says it, rather than panicking on a nil.
func TestTheNoticeSurvivesHavingNoLogger(t *testing.T) {
	ReportNoHandlers(nil, "orders_queue", "kafka", "orders")
}
