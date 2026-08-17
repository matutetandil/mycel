package rabbitmq

import (
	"context"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// A message the flow turned away, and one that failed enough times. These are
// the two ways a message stops circulating, and the failure in both directions
// is quiet: too eager and a message is dropped, too reluctant and the queue
// redelivers the same payload for ever.

func withDLQ(maxRetries int) *Config {
	return &Config{
		URL:   "amqp://localhost:5672/",
		Queue: &QueueConfig{Name: "orders"},
		Consumer: &ConsumerConfig{
			AutoAck: false,
			DLQ: &DLQConfig{
				Enabled:     true,
				MaxRetries:  maxRetries,
				RetryHeader: "x-retry-count",
			},
		},
	}
}

func TestAMessageTheFilterTurnedAwayFollowsItsPolicy(t *testing.T) {
	// The flow took the message and decided it was not for it. Which of these
	// three happens is written in the flow, and each means something different
	// to the broker.
	for name, tc := range map[string]struct {
		policy string
		want   string
	}{
		"ack drops it":                       {"ack", "acked"},
		"reject sends it to the dead-letter": {"reject", "dead-lettered"},
		"anything else drops it":             {"something-nobody-implements", "acked"},
	} {
		t.Run(name, func(t *testing.T) {
			c := consumer(t, consuming(), nil)
			b := &broker{}

			err := c.handleFilterReject(message(b), &flow.FilteredResultWithPolicy{
				Filtered: true, Policy: tc.policy,
			})
			if err != nil {
				t.Fatalf("handleFilterReject: %v", err)
			}
			if b.what() != tc.want {
				t.Errorf("the message was %s, want %s", b.what(), tc.want)
			}
		})
	}
}

func TestARequeuedMessageIsHandedBackOnlySoManyTimes(t *testing.T) {
	// Without the bound this is a message that circles for ever: the filter
	// turns it away, the broker hands it back, the filter turns it away.
	c := consumer(t, consuming(), nil)
	rejected := &flow.FilteredResultWithPolicy{
		Filtered: true, Policy: "requeue", MessageID: "order-1", MaxRequeue: 3,
	}

	handedBack := 0
	for i := 0; i < 6; i++ {
		b := &broker{}
		if err := c.handleFilterReject(message(b), rejected); err != nil {
			t.Fatalf("handleFilterReject: %v", err)
		}
		switch b.what() {
		case "requeued":
			handedBack++
		case "acked":
			// Given up on, which is where this has to end.
		default:
			t.Fatalf("the message was %s", b.what())
		}
	}

	if handedBack == 0 {
		t.Error("a message the flow asked to requeue was never handed back")
	}
	if handedBack >= 6 {
		t.Error("the message was handed back every time: it would circle for ever")
	}
}

func TestAMessageWithNothingToCountUnderIsDropped(t *testing.T) {
	// Without an identifier there is nothing to count attempts against, so
	// requeueing would be unbounded. Dropping one message beats a queue that
	// never drains.
	c := consumer(t, consuming(), nil)

	b := &broker{}
	delivery := message(b)
	delivery.MessageId = ""

	err := c.handleFilterReject(delivery, &flow.FilteredResultWithPolicy{
		Filtered: true, Policy: "requeue", MaxRequeue: 3,
	})
	if err != nil {
		t.Fatalf("handleFilterReject: %v", err)
	}
	if b.what() != "acked" {
		t.Errorf("the message was %s, want dropped rather than circling", b.what())
	}
}

func TestTheBrokersOwnMessageIdStandsInForAnIdentifier(t *testing.T) {
	// A publisher that sets message_id gives the consumer something to count
	// against without the flow naming a field.
	c := consumer(t, consuming(), nil)

	b := &broker{}
	err := c.handleFilterReject(message(b), &flow.FilteredResultWithPolicy{
		Filtered: true, Policy: "requeue", MaxRequeue: 3,
	})
	if err != nil {
		t.Fatalf("handleFilterReject: %v", err)
	}
	if b.what() != "requeued" {
		t.Errorf("the message was %s, want handed back under the broker's own id", b.what())
	}
}

func TestAnAutoAckedConsumerHasNothingLeftToDecide(t *testing.T) {
	// The broker considered the message delivered the moment it was sent, so
	// nothing here can ack or nack it.
	config := consuming()
	config.Consumer.AutoAck = true
	c := consumer(t, config, nil)

	b := &broker{}
	if err := c.handleFilterReject(message(b), &flow.FilteredResultWithPolicy{
		Filtered: true, Policy: "reject",
	}); err != nil {
		t.Fatalf("handleFilterReject: %v", err)
	}
	if b.what() != "nothing" {
		t.Errorf("the consumer tried to %s a message the broker already considers delivered", b.what())
	}
}

// --- Failing enough times ----------------------------------------------------

func TestWithNoDeadLetterAFailedMessageGoesBackToTheQueue(t *testing.T) {
	// The default: another worker, or this one again, gets to try.
	c := consumer(t, consuming(), nil)

	b := &broker{}
	if err := c.handleRetry(message(b), errors.New("connection refused")); err != nil {
		t.Fatalf("handleRetry: %v", err)
	}
	if b.what() != "requeued" {
		t.Errorf("the message was %s, want handed back", b.what())
	}
}

func TestAMessageThatHasFailedEnoughTimesIsDeadLettered(t *testing.T) {
	// The count travels with the message in a header, so a consumer that
	// restarts does not start it again from zero.
	c := consumer(t, withDLQ(3), nil)

	b := &broker{}
	delivery := message(b)
	delivery.Headers = amqp.Table{"x-retry-count": int32(3)}

	if err := c.handleRetry(delivery, errors.New("connection refused")); err != nil {
		t.Fatalf("handleRetry: %v", err)
	}
	if b.what() != "rejected" {
		t.Errorf("the message was %s, want sent to the dead-letter exchange", b.what())
	}
}

func TestTheCountIsUnderstoodWhicheverWayTheBrokerSendsIt(t *testing.T) {
	// AMQP headers come back as whichever integer the broker chose. A type
	// this does not understand reads as zero, and a message that has already
	// failed its limit starts again from nothing — for ever.
	for name, value := range map[string]interface{}{
		"a 32-bit integer": int32(5),
		"a 64-bit integer": int64(5),
		"a plain integer":  int(5),
	} {
		t.Run(name, func(t *testing.T) {
			c := consumer(t, withDLQ(3), nil)
			b := &broker{}
			delivery := message(b)
			delivery.Headers = amqp.Table{"x-retry-count": value}

			if err := c.handleRetry(delivery, errors.New("connection refused")); err != nil {
				t.Fatalf("handleRetry: %v", err)
			}
			if b.what() != "rejected" {
				t.Errorf("a message past its limit was %s: the count was read as zero", b.what())
			}
		})
	}
}

func TestADeadLetterThatIsConfiguredButOffBehavesLikeNone(t *testing.T) {
	config := withDLQ(3)
	config.Consumer.DLQ.Enabled = false
	c := consumer(t, config, nil)

	b := &broker{}
	delivery := message(b)
	delivery.Headers = amqp.Table{"x-retry-count": int32(99)}

	if err := c.handleRetry(delivery, errors.New("connection refused")); err != nil {
		t.Fatalf("handleRetry: %v", err)
	}
	if b.what() != "requeued" {
		t.Errorf("the message was %s, want handed back: the dead-letter is turned off", b.what())
	}
}

func TestTheDeadLetterNamesFollowFromTheQueue(t *testing.T) {
	// A consumer that names nothing still gets a dead-letter exchange and
	// queue named after the one it reads, which is what makes the default
	// usable without writing three more names.
	c := consumer(t, withDLQ(3), nil)
	dlq := c.getDLQConfig()
	if dlq == nil {
		t.Fatal("a configured dead-letter is not there")
	}

	if name := c.getDLXExchangeName(dlq); name == "" {
		t.Error("the dead-letter exchange has no name")
	}
	if name := c.getDLQQueueName(dlq); name == "" {
		t.Error("the dead-letter queue has no name")
	}

	// And names that were written are the ones used.
	dlq.Exchange = "orders.dead"
	dlq.Queue = "orders.dead.queue"
	if got := c.getDLXExchangeName(dlq); got != "orders.dead" {
		t.Errorf("exchange = %q", got)
	}
	if got := c.getDLQQueueName(dlq); got != "orders.dead.queue" {
		t.Errorf("queue = %q", got)
	}

	// A consumer with no dead-letter configured has none.
	if consumer(t, consuming(), nil).getDLQConfig() != nil {
		t.Error("a consumer that configured no dead-letter has one")
	}
}

func TestAConsumerSaysWhatItIsReading(t *testing.T) {
	// What the banner prints and what an IDE steps through.
	c := consumer(t, consuming(), nil)

	kind, source := c.SourceInfo()
	if kind != "rabbitmq" {
		t.Errorf("kind = %q", kind)
	}
	if source != "orders" {
		t.Errorf("source = %q, want the queue it reads", source)
	}
	if c.QueueName() != "orders" {
		t.Errorf("queue = %q", c.QueueName())
	}

	if c.Name() != "orders_rabbit" || c.Type() != "mq" {
		t.Errorf("name = %q, type = %q", c.Name(), c.Type())
	}
}

func TestTwoFlowsOnOneQueueBothRun(t *testing.T) {
	// Fan-out: a second flow registering for the same queue must not replace
	// the first, which would silently stop one of them.
	c := consumer(t, consuming(), nil)

	var first, second bool
	c.RegisterRoute("orders", func(context.Context, map[string]interface{}) (interface{}, error) {
		first = true
		return nil, nil
	})
	c.RegisterRoute("orders", func(context.Context, map[string]interface{}) (interface{}, error) {
		second = true
		return nil, nil
	})

	handler := c.findHandler("orders")
	if handler == nil {
		t.Fatal("no handler for a queue two flows registered")
	}
	if _, err := handler(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !first || !second {
		t.Errorf("first ran = %v, second ran = %v — one of the flows was replaced", first, second)
	}
}
