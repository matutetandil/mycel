package graphql

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A subscription is a conversation over a socket, and the conversation has
// rules: a client says hello and waits to be acknowledged before it sends
// anything, every result carries the identifier of the subscription it belongs
// to, and a stream ends with a message saying so. A server that gets any of
// that wrong leaves a client waiting for something that will never arrive —
// there is no status code to read and no request to retry.
//
// So this speaks the protocol over a real socket rather than calling the
// handler's parts.

type conversation struct {
	*websocket.Conn
	t *testing.T
}

func (c *conversation) say(message map[string]interface{}) {
	c.t.Helper()
	if err := c.WriteJSON(message); err != nil {
		c.t.Fatalf("writing %v: %v", message["type"], err)
	}
}

func (c *conversation) hear() map[string]interface{} {
	c.t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		c.t.Fatalf("SetReadDeadline: %v", err)
	}
	var message map[string]interface{}
	if err := c.ReadJSON(&message); err != nil {
		c.t.Fatalf("reading: %v", err)
	}
	return message
}

// subscribable stands up a schema with one subscription field and returns the
// socket address plus the pubsub that feeds it.
func subscribable(t *testing.T) (string, *PubSub, *SubscriptionManager) {
	t.Helper()

	builder := NewSchemaBuilder()
	err := builder.RegisterHandler("Subscription.orderPlaced",
		func(_ context.Context, input map[string]interface{}) (interface{}, error) {
			return input, nil
		})
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	// A schema needs a query type even when only subscriptions are used.
	err = builder.RegisterHandler("Query.ping",
		func(context.Context, map[string]interface{}) (interface{}, error) { return "pong", nil })
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}

	schema, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	manager := NewSubscriptionManager(schema, slog.New(slog.NewTextHandler(io.Discard, nil)))
	manager.pubsub = builder.pubsub

	server := httptest.NewServer(manager.Handler())
	t.Cleanup(server.Close)

	return "ws" + strings.TrimPrefix(server.URL, "http"), builder.pubsub, manager
}

func connect(t *testing.T, address string) *conversation {
	t.Helper()
	dialer := websocket.Dialer{
		Subprotocols:     []string{"graphql-transport-ws"},
		HandshakeTimeout: 3 * time.Second,
	}
	conn, _, err := dialer.Dial(address, http.Header{})
	if err != nil {
		t.Fatalf("dialling: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &conversation{Conn: conn, t: t}
}

func TestTheServerAcknowledgesBeforeAnythingElse(t *testing.T) {
	// A client is required to wait for this, so a server that never sends it
	// leaves every client hanging at the first step.
	address, _, _ := subscribable(t)
	client := connect(t, address)

	client.say(map[string]interface{}{"type": "connection_init"})
	if got := client.hear()["type"]; got != "connection_ack" {
		t.Errorf("answered %v, want connection_ack", got)
	}
}

func TestAKeepaliveIsAnswered(t *testing.T) {
	// A socket through a proxy is closed when it goes quiet, so the exchange
	// that keeps it open has to work or long subscriptions die overnight.
	address, _, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	client.say(map[string]interface{}{"type": "ping"})
	if got := client.hear()["type"]; got != "pong" {
		t.Errorf("answered %v, want pong", got)
	}
}

func TestAPublishedEventReachesTheSubscriber(t *testing.T) {
	address, pubsub, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	client.say(map[string]interface{}{
		"type": "subscribe",
		"id":   "sub-1",
		"payload": map[string]interface{}{
			"query": `subscription { orderPlaced }`,
		},
	})

	// Give the subscription a moment to attach before publishing, or the event
	// is sent to nobody.
	time.Sleep(150 * time.Millisecond)
	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-1"})

	message := client.hear()
	if message["type"] != "next" {
		t.Fatalf("received %v, want next", message)
	}
	// Every result carries the identifier of the subscription it belongs to;
	// one connection can hold several at once.
	if message["id"] != "sub-1" {
		t.Errorf("id = %v, want the subscription's", message["id"])
	}

	encoded, _ := json.Marshal(message["payload"])
	if !strings.Contains(string(encoded), "order-1") {
		t.Errorf("payload = %s, want what was published", encoded)
	}
}

func TestTwoSubscriptionsOnOneConnectionStayApart(t *testing.T) {
	address, pubsub, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	for _, id := range []string{"sub-1", "sub-2"} {
		client.say(map[string]interface{}{
			"type": "subscribe", "id": id,
			"payload": map[string]interface{}{"query": `subscription { orderPlaced }`},
		})
	}
	time.Sleep(150 * time.Millisecond)
	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-1"})

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		message := client.hear()
		if message["type"] != "next" {
			t.Fatalf("received %v", message)
		}
		seen[message["id"].(string)] = true
	}
	if !seen["sub-1"] || !seen["sub-2"] {
		t.Errorf("delivered to %v, want both subscriptions", seen)
	}
}

func TestTheSameIdentifierTwiceIsRefused(t *testing.T) {
	// The identifier is how a client tells its own subscriptions apart. Two
	// under one name would have it unable to stop either.
	address, _, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	for i := 0; i < 2; i++ {
		client.say(map[string]interface{}{
			"type": "subscribe", "id": "sub-1",
			"payload": map[string]interface{}{"query": `subscription { orderPlaced }`},
		})
	}

	message := client.hear()
	if message["type"] != "error" {
		t.Fatalf("received %v, want the second refused", message)
	}
	if message["id"] != "sub-1" {
		t.Errorf("id = %v", message["id"])
	}
}

func TestASubscriptionWithNoIdentifierIsRefused(t *testing.T) {
	address, _, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	client.say(map[string]interface{}{
		"type":    "subscribe",
		"payload": map[string]interface{}{"query": `subscription { orderPlaced }`},
	})
	if got := client.hear()["type"]; got != "error" {
		t.Errorf("received %v, want it refused", got)
	}
}

func TestAQueryThatIsNotOneIsReportedOnItsSubscription(t *testing.T) {
	// The error has to carry the identifier, or a client with several open
	// cannot tell which one failed.
	address, _, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	client.say(map[string]interface{}{
		"type": "subscribe", "id": "sub-9",
		"payload": map[string]interface{}{"query": `subscription { thisFieldDoesNotExist }`},
	})

	message := client.hear()
	if message["type"] != "error" {
		t.Fatalf("received %v, want an error", message)
	}
	if message["id"] != "sub-9" {
		t.Errorf("id = %v, want the subscription that failed", message["id"])
	}
}

func TestStoppingASubscriptionStopsTheEvents(t *testing.T) {
	address, pubsub, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	client.say(map[string]interface{}{
		"type": "subscribe", "id": "sub-1",
		"payload": map[string]interface{}{"query": `subscription { orderPlaced }`},
	})
	time.Sleep(150 * time.Millisecond)
	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-1"})
	if got := client.hear()["type"]; got != "next" {
		t.Fatalf("received %v, want the first event", got)
	}

	client.say(map[string]interface{}{"type": "complete", "id": "sub-1"})
	time.Sleep(150 * time.Millisecond)
	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-2"})

	// No further events. The server also reports the stream as complete, which
	// is not an event and is what a client uses to forget the subscription; a
	// short deadline is the only way to assert an absence over a socket.
	if err := client.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	for {
		var message map[string]interface{}
		if err := client.ReadJSON(&message); err != nil {
			break // nothing more is coming, which is the point
		}
		if message["type"] == "next" {
			t.Fatalf("received an event after the subscription was stopped: %v", message)
		}
	}
}

// A subscription filter decides which subscribers an event is for. It was
// carried from the flow through three layers, stored, and read by nobody, so
// every subscriber received every event — including the ones a filter was
// written to keep apart.

func filteredSubscription(t *testing.T, filter string) (string, *PubSub) {
	t.Helper()

	builder := NewSchemaBuilder()
	if err := builder.RegisterHandler("Subscription.orderPlaced",
		func(_ context.Context, input map[string]interface{}) (interface{}, error) {
			return input, nil
		}); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if err := builder.RegisterHandler("Query.ping",
		func(context.Context, map[string]interface{}) (interface{}, error) { return "pong", nil }); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	builder.SetSubscriptionFilter("orderPlaced", filter)

	schema, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	manager := NewSubscriptionManager(schema, slog.New(slog.NewTextHandler(io.Discard, nil)))
	manager.pubsub = builder.pubsub

	server := httptest.NewServer(manager.Handler())
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), builder.pubsub
}

// subscribeAs connects, presents the given connection parameters, and
// subscribes.
func subscribeAs(t *testing.T, address string, params map[string]interface{}) *conversation {
	t.Helper()
	client := connect(t, address)
	init := map[string]interface{}{"type": "connection_init"}
	if params != nil {
		init["payload"] = params
	}
	client.say(init)
	client.hear()
	client.say(map[string]interface{}{
		"type": "subscribe", "id": "sub-1",
		"payload": map[string]interface{}{"query": `subscription { orderPlaced }`},
	})
	return client
}

func TestAFilterKeepsAnEventFromTheSubscribersItIsNotFor(t *testing.T) {
	// Two customers on one topic. Without the filter applied, each sees the
	// other's orders.
	address, pubsub := filteredSubscription(t, `input.customer_id == auth.customer_id`)

	mine := subscribeAs(t, address, map[string]interface{}{"customer_id": "c-1"})
	theirs := subscribeAs(t, address, map[string]interface{}{"customer_id": "c-2"})
	time.Sleep(200 * time.Millisecond)

	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-1", "customer_id": "c-1"})

	message := mine.hear()
	if message["type"] != "next" {
		t.Fatalf("the subscriber it was for received %v", message)
	}
	encoded, _ := json.Marshal(message["payload"])
	if !strings.Contains(string(encoded), "order-1") {
		t.Errorf("payload = %s", encoded)
	}

	// And the other must receive nothing at all.
	if err := theirs.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var unwanted map[string]interface{}
	if err := theirs.ReadJSON(&unwanted); err == nil {
		t.Errorf("another customer's subscriber received %v", unwanted)
	}
}

func TestASubscriberWithNoCredentialsIsFilteredOut(t *testing.T) {
	// A socket that presented nothing matches nothing, rather than everything.
	address, pubsub := filteredSubscription(t, `input.customer_id == auth.customer_id`)
	anonymous := subscribeAs(t, address, nil)
	time.Sleep(200 * time.Millisecond)

	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-1", "customer_id": "c-1"})

	if err := anonymous.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var unwanted map[string]interface{}
	if err := anonymous.ReadJSON(&unwanted); err == nil {
		t.Errorf("a subscriber that presented nothing received %v", unwanted)
	}
}

func TestNoFilterMeansEveryEvent(t *testing.T) {
	// The default has to stay what it was: a topic without a filter is a
	// broadcast.
	address, pubsub := filteredSubscription(t, "")
	client := subscribeAs(t, address, nil)
	time.Sleep(200 * time.Millisecond)

	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-1"})
	if got := client.hear()["type"]; got != "next" {
		t.Errorf("received %v, want the event", got)
	}
}

func TestAFilterThatCannotBeEvaluatedDeliversNothing(t *testing.T) {
	// The other reading — deliver when in doubt — turns every mistake in a
	// filter into everybody seeing everything, which is the failure the filter
	// exists to prevent.
	address, pubsub := filteredSubscription(t, `input.customer_id == auth.absent.nested`)
	client := subscribeAs(t, address, map[string]interface{}{"customer_id": "c-1"})
	time.Sleep(200 * time.Millisecond)

	pubsub.Publish("orderPlaced", map[string]interface{}{"id": "order-1", "customer_id": "c-1"})

	if err := client.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var unwanted map[string]interface{}
	if err := client.ReadJSON(&unwanted); err == nil {
		t.Errorf("an event nobody could judge was delivered: %v", unwanted)
	}
}

func TestAMessageThatIsNotJSONDoesNotEndTheConversation(t *testing.T) {
	// A client that sends something malformed should get on with its life,
	// and every other subscription on that socket should survive it.
	address, _, _ := subscribable(t)
	client := connect(t, address)
	client.say(map[string]interface{}{"type": "connection_init"})
	client.hear()

	if err := client.WriteMessage(websocket.TextMessage, []byte("this is not json")); err != nil {
		t.Fatalf("writing: %v", err)
	}

	client.say(map[string]interface{}{"type": "ping"})
	if got := client.hear()["type"]; got != "pong" {
		t.Errorf("the connection stopped answering after a malformed message: %v", got)
	}
}
