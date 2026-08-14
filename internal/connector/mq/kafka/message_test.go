package kafka

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/connector/mq/types"
	"github.com/matutetandil/mycel/v2/internal/flow"
)

// What a message becomes on the way in, and what happens to it when the flow
// that took it failed. Kafka has no per-message nack — an offset moves forward
// and does not come back — so every one of these decisions is about whether the
// offset commits, and getting it wrong either loses the message or stops the
// partition.

// permanentFailure is what a 4xx from a downstream service looks like by the
// time it reaches here: replaying it would fail the same way for ever.
type permanentFailure struct{}

func (permanentFailure) Error() string     { return "422 unprocessable" }
func (permanentFailure) IsPermanent() bool { return true }

func consumerFor(handler HandlerFunc) *Connector {
	c := &Connector{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		config:   &Config{Brokers: []string{"127.0.0.1:1"}},
		handlers: map[string]HandlerFunc{},
	}
	if handler != nil {
		c.handlers["orders"] = handler
	}
	return c
}

func orderMessage(body string) kafkago.Message {
	return kafkago.Message{
		Topic:     "orders",
		Partition: 3,
		Offset:    42,
		Key:       []byte("order-1"),
		Value:     []byte(body),
		Time:      time.Unix(1700000000, 0),
		Headers: []kafkago.Header{
			{Key: "traceparent", Value: []byte("00-abc-def-01")},
			{Key: "content-type", Value: []byte("application/json")},
		},
	}
}

func TestAMessageArrivesWithEverythingAFlowCanRead(t *testing.T) {
	// The envelope is the contract: a flow reads input.body for what was
	// published and the rest for where it came from. A field missing here is a
	// flow that cannot be written.
	var seen map[string]interface{}
	c := consumerFor(func(_ context.Context, input map[string]interface{}) (interface{}, error) {
		seen = input
		return nil, nil
	})

	if err := c.handleMessage(context.Background(), orderMessage(`{"sku":"WIDGET-1"}`)); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}

	body, ok := seen["body"].(map[string]interface{})
	if !ok || body["sku"] != "WIDGET-1" {
		t.Fatalf("body = %v, want what was published", seen["body"])
	}
	if seen["topic"] != "orders" || seen["key"] != "order-1" {
		t.Errorf("topic = %v, key = %v", seen["topic"], seen["key"])
	}
	if seen["partition"] != 3 || seen["offset"] != int64(42) {
		t.Errorf("partition = %v, offset = %v", seen["partition"], seen["offset"])
	}
	if seen["timestamp"] != int64(1700000000) {
		t.Errorf("timestamp = %v", seen["timestamp"])
	}
	headers, ok := seen["headers"].(map[string]interface{})
	if !ok || headers["traceparent"] != "00-abc-def-01" {
		t.Errorf("headers = %v, want the ones the message carried", seen["headers"])
	}
}

func TestAMessageThatIsNotJSONStillReachesTheFlow(t *testing.T) {
	// A topic carrying plain text, or something published by a system that
	// does not send JSON. Refusing it here would stop the partition over a
	// message nobody can fix.
	var seen map[string]interface{}
	c := consumerFor(func(_ context.Context, input map[string]interface{}) (interface{}, error) {
		seen = input
		return nil, nil
	})

	if err := c.handleMessage(context.Background(), orderMessage("not json at all")); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	if seen["body"] != "not json at all" {
		t.Errorf("body = %v, want the raw message", seen["body"])
	}
}

func TestAMessageOnATopicNobodyReadsIsNotAnError(t *testing.T) {
	// Returning an error would stop the partition on a message no flow will
	// ever take. The offset commits and the loss is reported instead.
	c := consumerFor(nil)

	if err := c.handleMessage(context.Background(), orderMessage(`{}`)); err != nil {
		t.Errorf("a message nobody reads stopped the consumer: %v", err)
	}
}

func TestWhatHappensToAMessageTheFlowCouldNotHandle(t *testing.T) {
	// Each disposition maps onto offset semantics, and the difference between
	// them is whether the message is seen again.
	for name, tc := range map[string]struct {
		err         error
		wantRetried bool
	}{
		"a failure worth retrying": {
			err:         errors.New("connection refused"),
			wantRetried: true,
		},
		"one that will never succeed": {
			// A 4xx cannot be fixed by replaying it, and without this the
			// partition stops on it for ever.
			err:         permanentFailure{},
			wantRetried: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := consumerFor(func(context.Context, map[string]interface{}) (interface{}, error) {
				return nil, tc.err
			})

			err := c.handleMessage(context.Background(), orderMessage(`{}`))
			if tc.wantRetried && err == nil {
				t.Error("the offset committed, so the message is gone")
			}
			if !tc.wantRetried && err != nil {
				t.Errorf("the offset did not commit, so the partition stops here: %v", err)
			}
		})
	}
}

func TestADispositionFromTheFlowDecidesTheOffset(t *testing.T) {
	// error_handling in the flow says what to do per class of failure, and it
	// takes precedence over anything inferred from the error.
	for name, tc := range map[string]struct {
		disposition connector.Disposition
		wantRetried bool
	}{
		"ack commits and moves on":       {connector.DispositionAck, false},
		"requeue leaves the offset open": {connector.DispositionRequeue, true},
	} {
		t.Run(name, func(t *testing.T) {
			c := consumerFor(func(context.Context, map[string]interface{}) (interface{}, error) {
				return nil, connector.NewDispositionError(errors.New("downstream timed out"), tc.disposition)
			})

			err := c.handleMessage(context.Background(), orderMessage(`{}`))
			if tc.wantRetried != (err != nil) {
				t.Errorf("err = %v, want retried = %v", err, tc.wantRetried)
			}
		})
	}
}

func TestAMessageAFilterTurnedAwayFollowsItsPolicy(t *testing.T) {
	// The flow took the message and decided it was not for it. ack is the
	// quiet case: nothing is republished and the offset commits.
	c := consumerFor(func(context.Context, map[string]interface{}) (interface{}, error) {
		return &flow.FilteredResultWithPolicy{Filtered: true, Policy: "ack"}, nil
	})

	if err := c.handleMessage(context.Background(), orderMessage(`{}`)); err != nil {
		t.Errorf("a message the filter turned away stopped the consumer: %v", err)
	}
}

// --- On the way out ---------------------------------------------------------

func TestAPublishedMessageCarriesWhatItWasGiven(t *testing.T) {
	c := &Connector{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	built, err := c.buildKafkaMessage(&types.Message{
		ID:         "order-1",
		RoutingKey: "orders",
		Body:       map[string]interface{}{"sku": "WIDGET-1"},
		Headers:    map[string]string{"traceparent": "00-abc-def-01"},
	})
	if err != nil {
		t.Fatalf("buildKafkaMessage: %v", err)
	}

	if built.Topic != "orders" {
		t.Errorf("topic = %q, want where the flow said to publish", built.Topic)
	}
	// The key is what Kafka partitions on, so two messages about the same
	// order have to land on the same partition and stay in order.
	if string(built.Key) != "order-1" {
		t.Errorf("key = %q", built.Key)
	}
	if string(built.Value) != `{"sku":"WIDGET-1"}` {
		t.Errorf("value = %s", built.Value)
	}

	headers := map[string]string{}
	for _, h := range built.Headers {
		headers[h.Key] = string(h.Value)
	}
	// Without this a trace stops at the queue and the consumer's work looks
	// like it came from nowhere.
	if headers["traceparent"] != "00-abc-def-01" {
		t.Errorf("headers = %v", headers)
	}
	if headers["message-id"] != "order-1" {
		t.Errorf("the message id was not carried: %v", headers)
	}
}

func TestAMessageWithNothingToIdentifyItIsStillPublished(t *testing.T) {
	// No id means Kafka assigns the partition round-robin, which is the right
	// behaviour for a message with no entity to keep in order.
	c := &Connector{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	built, err := c.buildKafkaMessage(&types.Message{
		RoutingKey: "events",
		Body:       map[string]interface{}{"kind": "heartbeat"},
	})
	if err != nil {
		t.Fatalf("buildKafkaMessage: %v", err)
	}
	if len(built.Key) != 0 {
		t.Errorf("key = %q, want none", built.Key)
	}
	for _, h := range built.Headers {
		if h.Key == "message-id" {
			t.Error("an empty message id was sent as a header")
		}
	}
}

func TestABodyThatCannotBeSerialisedIsReported(t *testing.T) {
	// Rather than publishing something the consumer cannot read.
	c := &Connector{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	_, err := c.buildKafkaMessage(&types.Message{
		RoutingKey: "orders",
		Body:       map[string]interface{}{"fn": func() {}},
	})
	if err == nil {
		t.Error("a message that cannot be serialised was published")
	}
}
