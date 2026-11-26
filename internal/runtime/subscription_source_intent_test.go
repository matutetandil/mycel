package runtime

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
)

// A flow fed by a remote GraphQL subscription consumes events, the way a queue
// consumer does, and what it does with each one is write it somewhere.
//
// The intent was read off the source operation, and "Subscription.priceChanged"
// carries no HTTP method, so it parsed as a GET. The GraphQL connector was not
// on the list of event-driven sources (it also serves queries), and the
// destination named no SQL verb, so the flow stayed a read: the published
// graphql-subscription-client example, which stores each price change in
// `price_history`, ran a SELECT with the event as its filter on every event
// and stored nothing.

func subscriptionSourcedHandler(t *testing.T, sourceOperation string, sink connector.Connector) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	registry.Replace("sink", sink)
	return &FlowHandler{
		Config: &flow.Config{
			Name: "track_price_changes",
			From: &flow.FromConfig{
				Connector:       "products_api",
				ConnectorParams: map[string]interface{}{"operation": sourceOperation},
			},
			To: &flow.ToConfig{
				Connector:       "sink",
				ConnectorParams: map[string]interface{}{"target": "price_history"},
			},
		},
		SourceType: "graphql",
		Connectors: registry,
		Dest:       sink,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestAFlowFedByASubscriptionWritesEachEvent(t *testing.T) {
	sink := &recordingWriter{name: "sink"}
	h := subscriptionSourcedHandler(t, "Subscription.priceChanged", sink)

	if _, err := h.HandleRequest(context.Background(), map[string]interface{}{"sku": "A-1", "newPrice": 9.5}); err != nil {
		t.Fatalf("handling an event failed: %v", err)
	}

	written := sink.writes()
	if len(written) != 1 {
		t.Fatalf("an event from a subscription was written %d times, want 1 — it was treated as a read", len(written))
	}
	if written[0].Operation != "INSERT" {
		t.Errorf("operation = %q, want INSERT", written[0].Operation)
	}
	if written[0].Target != "price_history" {
		t.Errorf("target = %q, want price_history", written[0].Target)
	}
}

// The same connector serving a query is still a read: the fix is about the
// subscription operation, not about GraphQL sources in general.
func TestAGraphQLQuerySourceIsStillARead(t *testing.T) {
	h := subscriptionSourcedHandler(t, "Query.products", &recordingWriter{name: "sink"})
	if op := h.resolvedOperation(); !op.IsRead() {
		t.Fatalf("Query.products resolved to %q, want a read", op.Method)
	}
}
