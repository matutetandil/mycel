package rabbitmq

import (
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

// What happens to a message the flow could not process.
//
// This is the machinery that came out of a real incident: a write that timed
// out, was retried, and raced the first attempt. Every branch here decides
// whether a message is tried again, given up on, or handed back to the queue
// for ever, and the difference between them is invisible from outside until
// somebody counts messages.

func withDLQ(dlq *DLQConfig) *Config {
	config := consuming()
	config.Consumer.DLQ = dlq
	return config
}

func TestAMessageIsTriedAgainUntilItIsNot(t *testing.T) {
	// The retry count rides on the message. Read wrongly, a message is either
	// retried for ever or given up on at the first failure — and the header a
	// broker writes is an int32, not an int.
	c := consumer(t, withDLQ(&DLQConfig{Enabled: true, MaxRetries: 3}), nil)

	for name, count := range map[string]interface{}{
		"as the broker writes it": int32(3),
		"as a wider number":       int64(3),
		"as a plain number":       3,
	} {
		t.Run("given up on when the count is there "+name, func(t *testing.T) {
			b := &broker{}
			delivery := message(b)
			delivery.Headers = amqp.Table{"x-retry-count": count}

			if err := c.handleRetry(delivery, errors.New("the supplier's API is down")); err != nil {
				t.Fatalf("handleRetry: %v", err)
			}
			// Rejected without requeue is what sends it to the dead-letter
			// exchange: the broker routes it, not us.
			if b.what() != "rejected" {
				t.Errorf("the message was %s, want it dead-lettered after the last attempt", b.what())
			}
		})
	}

	// Below the limit it goes round again. With no channel to republish
	// through — which is the state a dropped connection leaves, and the state
	// a handler is most likely to have failed in — the message goes back to
	// the queue rather than taking the process down with a nil dereference.
	b := &broker{}
	delivery := message(b)
	delivery.Headers = amqp.Table{"x-retry-count": int32(1)}
	if err := c.handleRetry(delivery, errors.New("the supplier's API is down")); err != nil {
		t.Fatalf("handleRetry: %v", err)
	}
	if b.what() != "requeued" {
		t.Errorf("a message with attempts left was %s, want it back on the queue", b.what())
	}
}

func TestAMessageWithNoCountYet(t *testing.T) {
	// The first failure: nothing has written a count, and the message must
	// not be treated as having exhausted its attempts.
	c := consumer(t, withDLQ(&DLQConfig{Enabled: true, MaxRetries: 3}), nil)

	b := &broker{}
	if err := c.handleRetry(message(b), errors.New("the supplier's API is down")); err != nil {
		t.Fatalf("handleRetry: %v", err)
	}
	if b.what() == "rejected" {
		t.Error("the first failure was treated as the last")
	}
}

func TestHowManyAttemptsWhenNobodySaid(t *testing.T) {
	// A dead-letter block with no limit written still has one, or a message
	// that always fails goes round for ever and the queue never drains.
	c := consumer(t, withDLQ(&DLQConfig{Enabled: true}), nil)

	b := &broker{}
	delivery := message(b)
	delivery.Headers = amqp.Table{"x-retry-count": int32(99)}
	if err := c.handleRetry(delivery, errors.New("the supplier's API is down")); err != nil {
		t.Fatalf("handleRetry: %v", err)
	}
	if b.what() != "rejected" {
		t.Errorf("a message on its hundredth attempt was %s", b.what())
	}
}

func TestTheCountCanBeKeptUnderAnotherName(t *testing.T) {
	// A queue shared with another system that already uses x-retry-count for
	// its own purposes.
	c := consumer(t, withDLQ(&DLQConfig{
		Enabled: true, MaxRetries: 2, RetryHeader: "x-mycel-attempts",
	}), nil)

	b := &broker{}
	delivery := message(b)
	// The default name is present and is not the one configured: it must be
	// ignored, or a message is given up on because of somebody else's header.
	delivery.Headers = amqp.Table{"x-retry-count": int32(9), "x-mycel-attempts": int32(0)}

	if err := c.handleRetry(delivery, errors.New("the supplier's API is down")); err != nil {
		t.Fatalf("handleRetry: %v", err)
	}
	if b.what() == "rejected" {
		t.Error("a message was dead-lettered on another system's retry count")
	}
}

func TestWithNoDeadLetterQueueAMessageGoesBack(t *testing.T) {
	// Without somewhere to put a message that keeps failing, the only honest
	// thing is to hand it back: dropping it loses an order, and the queue
	// depth is what tells somebody it is happening.
	for name, config := range map[string]*Config{
		"no dead-letter block": consuming(),
		"one switched off":     withDLQ(&DLQConfig{Enabled: false, MaxRetries: 3}),
	} {
		t.Run(name, func(t *testing.T) {
			c := consumer(t, config, nil)
			b := &broker{}

			if err := c.handleRetry(message(b), errors.New("the supplier's API is down")); err != nil {
				t.Fatalf("handleRetry: %v", err)
			}
			if b.what() != "requeued" {
				t.Errorf("the message was %s, want it handed back", b.what())
			}
		})
	}
}

func TestAcknowledgingAfterTheChannelIsGone(t *testing.T) {
	// A flow that acknowledges for itself, on a connection that dropped while
	// the message was being processed — which is the case the reconnect work
	// exists for. Saying so beats a nil dereference, and beats reporting an
	// acknowledgement the broker never received.
	c := consumer(t, consuming(), nil)

	if err := c.Ack(1, false); err == nil {
		t.Error("a message was acknowledged through a channel that is not there")
	}
	if err := c.Nack(1, false, true); err == nil {
		t.Error("a message was rejected through a channel that is not there")
	}
	if err := c.Reject(1, false); err == nil {
		t.Error("a message was refused through a channel that is not there")
	}
}
