package kafka

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// Two decisions a consumer makes before anything is written anywhere: which
// flow a message on this topic belongs to, and what to do with one a filter
// turned away. Kafka has no requeue — an offset moves forward and does not come
// back — so a message to be retried is republished, and the count of how many
// times is what stops that being a loop.

func consumerWith(handlers map[string]HandlerFunc) *Connector {
	c := &Connector{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// A broker nothing is listening on: a republish reaches the write and
		// fails there, which is what tells the two paths apart below.
		config:   &Config{Brokers: []string{"127.0.0.1:1"}},
		handlers: map[string]HandlerFunc{},
	}
	for topic, handler := range handlers {
		c.handlers[topic] = handler
	}
	return c
}

func noop(context.Context, map[string]interface{}) (interface{}, error) { return nil, nil }

func TestAMessageGoesToTheFlowForItsTopic(t *testing.T) {
	c := consumerWith(map[string]HandlerFunc{"orders": noop})

	if c.findHandler("orders") == nil {
		t.Error("a message on a topic a flow reads found no flow")
	}
	if c.findHandler("invoices") != nil {
		t.Error("a message on a topic nobody reads found one anyway")
	}
}

func TestAFlowCanTakeEverythingOnTheBroker(t *testing.T) {
	// The catch-all, which is what a flow with no operation gets.
	c := consumerWith(map[string]HandlerFunc{"*": noop})

	if c.findHandler("anything-at-all") == nil {
		t.Error("the catch-all did not take a message")
	}
}

func TestATopicWithItsOwnFlowDoesNotGoToTheCatchAll(t *testing.T) {
	// Otherwise a flow written for one topic is shadowed by a general one, and
	// which of them runs would depend on the order they were registered.
	var reachedSpecific bool
	c := consumerWith(map[string]HandlerFunc{
		"*": noop,
		"orders": func(context.Context, map[string]interface{}) (interface{}, error) {
			reachedSpecific = true
			return nil, nil
		},
	})

	handler := c.findHandler("orders")
	if handler == nil {
		t.Fatal("no flow took the message")
	}
	_, _ = handler(context.Background(), nil)
	if !reachedSpecific {
		t.Error("the catch-all took a message the topic's own flow should have")
	}
}

func TestAMessageNobodyWantsIsLetGo(t *testing.T) {
	// ack, and anything unrecognised: the offset moves on and the message is
	// not republished anywhere.
	c := consumerWith(nil)

	for _, policy := range []string{"ack", "", "something-nobody-implements"} {
		err := c.handleFilterReject(context.Background(),
			kafkago.Message{Topic: "orders", Key: []byte("k-1")},
			&flow.FilteredResultWithPolicy{Filtered: true, Policy: policy})
		if err != nil {
			t.Errorf("policy %q: %v", policy, err)
		}
	}
}

func TestAMessageIsOnlyRepublishedSoManyTimes(t *testing.T) {
	// The bound is what stops a requeue being a loop. Kafka cannot put a
	// message back, so a retry is a republish — and without a count the same
	// message would be republished for ever, each one a new message.
	c := consumerWith(nil)
	c.requeueTracker = flow.NewRequeueTracker(10 * time.Minute)

	// No writer is configured, so a republish fails — which is exactly how
	// this tells the two paths apart: the attempts that try to republish
	// report that failure, and the one past the bound does not.
	rejected := &flow.FilteredResultWithPolicy{
		Filtered: true, Policy: "requeue", MessageID: "order-1", MaxRequeue: 2,
	}
	message := kafkago.Message{Topic: "orders", Key: []byte("order-1")}

	stopped, cancel := context.WithCancel(context.Background())
	cancel()

	attempts := 0
	for i := 0; i < 5; i++ {
		if err := c.handleFilterReject(stopped, message, rejected); err != nil {
			attempts++
		}
	}

	// max_requeue counts deliveries, not republishes: the count is incremented
	// before the decision, so the message is put back one time fewer than the
	// bound and the last delivery is the one that gives up.
	if attempts != 1 {
		t.Errorf("%d republish attempts, want the one a bound of two allows", attempts)
	}
}

func TestAMessageWithNothingToCountUnderIsLetGo(t *testing.T) {
	// Without an identifier there is nothing to count republishes against, so
	// republishing would be an unbounded loop. Letting it go loses one
	// message; the loop loses the consumer.
	c := consumerWith(nil)
	c.requeueTracker = flow.NewRequeueTracker(10 * time.Minute)

	err := c.handleFilterReject(context.Background(),
		kafkago.Message{Topic: "orders"}, // no key, and no message id
		&flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue", MaxRequeue: 3})
	if err != nil {
		t.Errorf("a message with no identifier was republished anyway: %v", err)
	}
}

func TestTheMessageKeyStandsInForAnIdentifier(t *testing.T) {
	// A Kafka message usually carries the entity's key, which is exactly what
	// a requeue count should be kept under — this is the path that finds one
	// when the flow's id_field produced nothing.
	c := consumerWith(nil)
	c.requeueTracker = flow.NewRequeueTracker(10 * time.Minute)

	rejected := &flow.FilteredResultWithPolicy{Filtered: true, Policy: "requeue", MaxRequeue: 1}
	message := kafkago.Message{Topic: "orders", Key: []byte("order-7")}

	stopped, cancel := context.WithCancel(context.Background())
	cancel()

	attempts := 0
	for i := 0; i < 3; i++ {
		if err := c.handleFilterReject(stopped, message, rejected); err != nil {
			attempts++
		}
	}
	// A bound of one means the first delivery is also the last.
	if attempts != 0 {
		t.Errorf("%d republish attempts, want none: a bound of one gives up immediately", attempts)
	}
}
