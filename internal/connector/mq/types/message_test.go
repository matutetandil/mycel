package types

import (
	"encoding/json"
	"testing"
	"time"
)

// What a message looks like on the way out to a broker.
//
// Every queue connector builds one of these, so what is in it is what a
// consumer on the other side receives — including the consumers already in
// production, which read the body under `input.body`.

func TestAMessageCarriesEnoughToBeTracedBack(t *testing.T) {
	before := time.Now().Unix()
	msg := NewMessage(map[string]interface{}{"order_id": "order-1"})

	// The identifier is what a consumer keys idempotency on and what appears
	// in a log when somebody asks what happened to a message.
	if msg.ID == "" {
		t.Error("a message went out with no identifier")
	}
	if other := NewMessage(nil); other.ID == msg.ID {
		t.Error("two messages were given the same identifier")
	}
	if msg.Timestamp < before {
		t.Errorf("timestamp = %d, want when it was built", msg.Timestamp)
	}
	// Brokers route and consumers parse by this; without it a JSON body can
	// arrive as an opaque blob.
	if msg.ContentType != "application/json" {
		t.Errorf("content type = %q", msg.ContentType)
	}
	if msg.Body["order_id"] != "order-1" {
		t.Errorf("body = %v", msg.Body)
	}
}

func TestWhereAMessageIsSent(t *testing.T) {
	msg := NewMessageWithRouting(
		map[string]interface{}{"order_id": "order-1"},
		"orders", "order.created",
	)

	if msg.Exchange != "orders" || msg.RoutingKey != "order.created" {
		t.Errorf("routed to %s/%s", msg.Exchange, msg.RoutingKey)
	}
	// It is still a message: routing does not replace what the plain
	// constructor sets.
	if msg.ID == "" || msg.ContentType != "application/json" {
		t.Errorf("message = %+v", msg)
	}
}

func TestHeadersOnAMessage(t *testing.T) {
	// Headers are how tracing travels between services — a traceparent set
	// here is what joins a consumer's spans to the producer's.
	msg := NewMessage(nil)

	if got := msg.GetHeader("traceparent"); got != "" {
		t.Errorf("a message with no headers answered %q", got)
	}

	msg.SetHeader("traceparent", "00-trace-span-01")
	msg.SetHeader("x-tenant", "acme")

	if got := msg.GetHeader("traceparent"); got != "00-trace-span-01" {
		t.Errorf("traceparent = %q", got)
	}
	if got := msg.GetHeader("absent"); got != "" {
		t.Errorf("a header nobody set answered %q", got)
	}

	msg.SetHeader("x-tenant", "other")
	if got := msg.GetHeader("x-tenant"); got != "other" {
		t.Errorf("a header set twice = %q, want the second value", got)
	}
}

func TestWhatGoesOnTheWire(t *testing.T) {
	// The JSON shape is the contract with everything consuming these, so a
	// renamed field is a consumer that stops finding what it reads.
	msg := NewMessageWithRouting(map[string]interface{}{"order_id": "order-1"}, "orders", "order.created")
	msg.SetHeader("x-tenant", "acme")
	msg.DeliveryTag = 7
	msg.Redelivered = true

	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var sent map[string]interface{}
	if err := json.Unmarshal(encoded, &sent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range []string{"id", "body", "headers", "routing_key", "exchange", "timestamp", "content_type"} {
		if _, ok := sent[field]; !ok {
			t.Errorf("%s is missing from the encoded message", field)
		}
	}
	// Delivery details belong to one delivery from one broker and mean nothing
	// to whoever receives the message.
	if _, ok := sent["DeliveryTag"]; ok {
		t.Error("the delivery tag was sent to the consumer")
	}
	if _, ok := sent["Redelivered"]; ok {
		t.Error("the redelivery flag was sent to the consumer")
	}

	// Empty optional fields stay out rather than arriving as blanks.
	plain, _ := json.Marshal(NewMessage(nil))
	var minimal map[string]interface{}
	_ = json.Unmarshal(plain, &minimal)
	if _, ok := minimal["headers"]; ok {
		t.Error("a message with no headers sent an empty headers field")
	}
	if _, ok := minimal["routing_key"]; ok {
		t.Error("a message with no routing key sent an empty one")
	}
}

func TestHowAMessageIsAcknowledged(t *testing.T) {
	// This comes out of configuration as text. Anything unrecognised has to
	// land on automatic acknowledgement: the alternative, treating a typo as
	// manual, is a queue that fills because nothing ever acknowledges.
	for text, want := range map[string]AckMode{
		"auto":     AckModeAuto,
		"manual":   AckModeManual,
		"none":     AckModeNone,
		"":         AckModeAuto,
		"Manual":   AckModeAuto,
		"whatever": AckModeAuto,
	} {
		if got := ParseAckMode(text); got != want {
			t.Errorf("ParseAckMode(%q) = %v, want %v", text, got, want)
		}
	}

	// And back to text, which is what a log or an error message shows.
	for mode, want := range map[AckMode]string{
		AckModeAuto:   "auto",
		AckModeManual: "manual",
		AckModeNone:   "none",
		AckMode(99):   "unknown",
	} {
		if got := mode.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", mode, got, want)
		}
	}
}

func TestPersistenceIsTheAMQPNumbering(t *testing.T) {
	// These are sent to the broker as they are: AMQP defines 1 as transient
	// and 2 as persistent, so the values are not free to change.
	if DeliveryModeTransient != 1 {
		t.Errorf("transient = %d, want 1", DeliveryModeTransient)
	}
	if DeliveryModePersistent != 2 {
		t.Errorf("persistent = %d, want 2", DeliveryModePersistent)
	}
}
