package graphql

import (
	"context"
	"log/slog"

	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A subscription filter decides which subscribers an event is for.
//
// It was written into a flow, carried through three layers — the flow's `to`
// block, RegisterSubscriptionWithFilter, SetSubscriptionFilter — stored on the
// schema builder, and read by nobody. Every subscriber received every event
// published to the topic, including the ones written specifically so they would
// not: an order event meant for the customer it belongs to went to everybody
// holding a socket. Nothing failed, since a delivered event looks the same
// either way.
//
// The filter is evaluated once per subscriber per event, against the published
// data and the parameters that subscriber sent when it connected — which is
// where a token lives on a websocket, there being no headers on the messages.

// subscriberFilter returns the filter to apply to one subscriber's stream, or
// nil when the topic has none.
//
// The expression is compiled once here rather than per event, so a filter that
// cannot compile is reported at subscription time instead of silently dropping
// every event afterwards.
func (b *SchemaBuilder) subscriberFilter(topic string, ctx context.Context) func(interface{}) bool {
	b.mu.RLock()
	expression := b.subscriptionFilters[topic]
	b.mu.RUnlock()

	if expression == "" {
		return nil
	}

	transformer, err := transform.NewCELTransformer()
	if err != nil {
		slog.Error("subscription filter cannot be evaluated, so every event will be delivered",
			"subscription", topic, "error", err)
		return nil
	}
	if _, err := transformer.Compile(expression); err != nil {
		slog.Error("subscription filter is not a usable expression, so every event will be delivered",
			"subscription", topic, "filter", expression, "error", err)
		return nil
	}

	// What the subscriber presented on connection_init. Read once: it cannot
	// change for the life of the socket.
	params := ConnectionParamsFromContext(ctx)

	return func(data interface{}) bool {
		event, ok := data.(map[string]interface{})
		if !ok {
			// Something that is not a record has no fields to filter on;
			// delivering it is the same as having no filter.
			event = map[string]interface{}{}
		}

		activation := map[string]interface{}{
			"input": event,
			// The subscriber's own identity, under the name the rest of the
			// runtime binds a caller's identity to.
			"auth": params,
		}

		allowed, err := transformer.EvaluateCondition(context.Background(), activation, expression)
		if err != nil {
			// An event that cannot be judged is not delivered. The opposite
			// reading would turn every mistake in a filter into everybody
			// seeing everything, which is the failure this exists to prevent.
			slog.Warn("subscription filter could not be evaluated for an event; it was not delivered",
				"subscription", topic, "error", err)
			return false
		}
		return allowed
	}
}
