package rabbitmq

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
)

// What a consumer does with a message once the flow has had it.
//
// Every one of these is a decision the broker acts on: an ack drops the
// message, a nack without requeue sends it to the dead-letter exchange, and a
// nack with requeue hands it straight back. Getting one wrong is either a
// message lost or a queue that redelivers the same payload for ever — and both
// look like the service working until somebody counts.

// broker records what the consumer told it to do with each delivery.
type broker struct {
	mu       sync.Mutex
	acked    []uint64
	nacked   []uint64
	requeued []uint64
	rejected []uint64
}

func (b *broker) Ack(tag uint64, multiple bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.acked = append(b.acked, tag)
	return nil
}

func (b *broker) Nack(tag uint64, multiple, requeue bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nacked = append(b.nacked, tag)
	if requeue {
		b.requeued = append(b.requeued, tag)
	}
	return nil
}

func (b *broker) Reject(tag uint64, requeue bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rejected = append(b.rejected, tag)
	return nil
}

// what describes the outcome in the words the broker understands.
func (b *broker) what() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case len(b.requeued) > 0:
		return "requeued"
	case len(b.rejected) > 0:
		return "rejected"
	case len(b.nacked) > 0:
		return "dead-lettered"
	case len(b.acked) > 0:
		return "acked"
	default:
		return "nothing"
	}
}

func consumer(t *testing.T, config *Config, handler HandlerFunc) *Connector {
	t.Helper()
	c := &Connector{
		name:     "orders_rabbit",
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		config:   config,
		handlers: map[string]HandlerFunc{},
	}
	c.requeueTracker = flow.NewRequeueTracker(10 * time.Minute)
	if handler != nil {
		c.handlers["orders"] = handler
	}
	return c
}

func consuming() *Config {
	return &Config{
		URL:      "amqp://localhost:5672/",
		Queue:    &QueueConfig{Name: "orders"},
		Consumer: &ConsumerConfig{AutoAck: false},
	}
}

func message(b *broker) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: b,
		DeliveryTag:  1,
		RoutingKey:   "orders",
		Exchange:     "orders_exchange",
		MessageId:    "order-1",
		Body:         []byte(`{"sku":"WIDGET-1"}`),
		Timestamp:    time.Unix(1700000000, 0),
		Headers:      amqp.Table{"traceparent": "00-abc-def-01"},
	}
}

func TestAMessageArrivesWrappedInItsEnvelope(t *testing.T) {
	// A queue message is an envelope: what was published is under body, and
	// everything about how it arrived sits beside it. Production flows read
	// input.body.* — flattening this would break every one of them.
	var seen map[string]interface{}
	c := consumer(t, consuming(), func(_ context.Context, input map[string]interface{}) (interface{}, error) {
		seen = input
		return nil, nil
	})

	b := &broker{}
	if err := c.handleDelivery(context.Background(), message(b)); err != nil {
		t.Fatalf("handleDelivery: %v", err)
	}

	body, ok := seen["body"].(map[string]interface{})
	if !ok || body["sku"] != "WIDGET-1" {
		t.Fatalf("body = %v, want what was published, one level down", seen["body"])
	}
	if seen["routing_key"] != "orders" || seen["exchange"] != "orders_exchange" {
		t.Errorf("routing key = %v, exchange = %v", seen["routing_key"], seen["exchange"])
	}

	headers, ok := seen["headers"].(map[string]interface{})
	if !ok || headers["traceparent"] != "00-abc-def-01" {
		t.Errorf("headers = %v, want the ones the message carried", seen["headers"])
	}

	// The properties are how a flow correlates a reply or dedupes on the
	// broker's own message id.
	properties, ok := seen["properties"].(map[string]interface{})
	if !ok || properties["message_id"] != "order-1" {
		t.Fatalf("properties = %v", seen["properties"])
	}
	if properties["delivery_tag"] != uint64(1) {
		t.Errorf("delivery tag = %v", properties["delivery_tag"])
	}

	if b.what() != "acked" {
		t.Errorf("the message was %s, want acked after a flow that succeeded", b.what())
	}
}

func TestAMessageThatIsNotJSONStillReachesTheFlow(t *testing.T) {
	// A queue carrying plain text, or a publisher that does not send JSON.
	var seen map[string]interface{}
	c := consumer(t, consuming(), func(_ context.Context, input map[string]interface{}) (interface{}, error) {
		seen = input
		return nil, nil
	})

	delivery := message(&broker{})
	delivery.Body = []byte("not json at all")
	if err := c.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery: %v", err)
	}
	if seen["body"] != "not json at all" {
		t.Errorf("body = %v, want the raw message", seen["body"])
	}
}

func TestAMessageNoFlowIsWaitingForIsNotRequeued(t *testing.T) {
	// Requeueing it would hand the same message back for ever, since no flow
	// will ever want it. It is dead-lettered instead, where somebody can look.
	c := consumer(t, consuming(), nil)

	b := &broker{}
	delivery := message(b)
	delivery.RoutingKey = "a-key-nobody-reads"
	if err := c.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handleDelivery: %v", err)
	}

	if b.what() != "dead-lettered" {
		t.Errorf("the message was %s, want dead-lettered", b.what())
	}
	if len(b.requeued) != 0 {
		t.Error("a message nobody reads was handed back to the queue")
	}
}

func TestWhatHappensWhenTheFlowFails(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		// A 4xx replayed produces the same 4xx. Without this the broker
		// redelivers for ever, because nothing tells it the payload is the
		// problem.
		"a failure that will never succeed": {permanentFailure{}, "acked"},

		// Transient, and no dead-letter configured: back to the queue for
		// another worker to try.
		"a failure worth retrying": {errors.New("connection refused"), "requeued"},
	} {
		t.Run(name, func(t *testing.T) {
			c := consumer(t, consuming(), func(context.Context, map[string]interface{}) (interface{}, error) {
				return nil, tc.err
			})

			b := &broker{}
			if err := c.handleDelivery(context.Background(), message(b)); err != nil {
				t.Fatalf("handleDelivery: %v", err)
			}
			if b.what() != tc.want {
				t.Errorf("the message was %s, want %s", b.what(), tc.want)
			}
		})
	}
}

func TestTheFlowsOwnDispositionWins(t *testing.T) {
	// error_handling in the flow says what to do per class of failure, and it
	// takes precedence over anything inferred from the error itself.
	for name, tc := range map[string]struct {
		disposition connector.Disposition
		want        string
	}{
		"ack drops it":                       {connector.DispositionAck, "acked"},
		"reject sends it to the dead-letter": {connector.DispositionReject, "dead-lettered"},
		"requeue hands it back":              {connector.DispositionRequeue, "requeued"},
	} {
		t.Run(name, func(t *testing.T) {
			c := consumer(t, consuming(), func(context.Context, map[string]interface{}) (interface{}, error) {
				return nil, connector.NewDispositionError(errors.New("downstream timed out"), tc.disposition)
			})

			b := &broker{}
			if err := c.handleDelivery(context.Background(), message(b)); err != nil {
				t.Fatalf("handleDelivery: %v", err)
			}
			if b.what() != tc.want {
				t.Errorf("the message was %s, want %s", b.what(), tc.want)
			}
		})
	}
}

func TestADispositionBeatsWhatTheErrorLooksLike(t *testing.T) {
	// A permanent failure would be acked on its own; a flow that says requeue
	// gets requeue. This is the precedence that makes error_handling worth
	// writing.
	c := consumer(t, consuming(), func(context.Context, map[string]interface{}) (interface{}, error) {
		return nil, connector.NewDispositionError(permanentFailure{}, connector.DispositionRequeue)
	})

	b := &broker{}
	if err := c.handleDelivery(context.Background(), message(b)); err != nil {
		t.Fatalf("handleDelivery: %v", err)
	}
	if b.what() != "requeued" {
		t.Errorf("the message was %s, want the disposition the flow chose", b.what())
	}
}

// permanentFailure is what a 4xx from a downstream service looks like by the
// time it reaches the consumer.
type permanentFailure struct{}

func (permanentFailure) Error() string     { return "422 unprocessable" }
func (permanentFailure) IsPermanent() bool { return true }
