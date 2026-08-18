package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/connector/mq/types"
)

// Whether a publish means the broker has the message.
//
// Without confirms, publishing is a write to a socket: it returns as soon as
// the bytes are handed over, and a broker that dies a moment later takes the
// message with it. A flow that acknowledges its own source message after
// publishing downstream is relying on the stronger meaning — and
// `confirms = true` was parsed into a field nothing read, so it had the weaker
// one and said nothing.
//
// This needs a broker: whether the answer is a confirmation is the broker's to
// give, and a mock asserting we called a method proves nothing about it. The
// integration suite sets MYCEL_TEST_RABBITMQ_URL.

func confirmingConnector(t *testing.T, target string, publisher *PublisherConfig) *Connector {
	t.Helper()

	cfg := DefaultConfig()
	cfg.URL = target
	cfg.Publisher = publisher

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c, err := NewConnector(t.Name(), cfg, logger)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestAConfirmedPublishWaitsForTheBroker(t *testing.T) {
	target := rabbitMQTestURL(t)

	queue := fmt.Sprintf("mycel_confirms_%d", time.Now().UnixNano())
	c := confirmingConnector(t, target, &PublisherConfig{
		// The default exchange routes by queue name, so a message published
		// with the queue as its routing key lands in it.
		RoutingKey: queue,
		Confirms:   true,
	})

	// Declare the queue so the message has somewhere to go.
	if _, err := c.channel.QueueDeclare(queue, false, true, false, false, nil); err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}

	// The assertion that discriminates: a channel in confirm mode hands back
	// something to wait on, and one that is not hands back nothing. A local
	// broker answers in microseconds, so "the message is there straight
	// after publishing" would pass without confirms too.
	confirmation, err := c.channel.PublishWithDeferredConfirmWithContext(
		context.Background(), "", queue, false, false,
		amqp.Publishing{ContentType: "application/json", Body: []byte(`{"probe":true}`)},
	)
	if err != nil {
		t.Fatalf("publishing to check the mode: %v", err)
	}
	if confirmation == nil {
		t.Fatal("the channel is not in confirm mode, so confirms = true bought nothing")
	}
	if _, ok, _ := c.channel.Get(queue, true); !ok {
		t.Error("the probe message did not arrive")
	}

	if err := c.Publish(context.Background(), types.NewMessage(map[string]interface{}{
		"order_id": "order-1",
	})); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// The publish returned, so the broker has confirmed it: the message is
	// already there to be read, with no waiting.
	message, ok, err := c.channel.Get(queue, true)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("the publish returned and the broker did not have the message")
	}
	if len(message.Body) == 0 {
		t.Error("the message arrived empty")
	}
}

func TestAConfirmedPublishThatTheBrokerRefuses(t *testing.T) {
	// Mandatory says the message must be routable. Published to an exchange
	// that routes it nowhere, the broker returns it — and a flow relying on
	// confirms needs that to be a failure, not a silent success, because the
	// message is gone either way.
	target := rabbitMQTestURL(t)

	exchange := fmt.Sprintf("mycel_confirms_ex_%d", time.Now().UnixNano())
	c := confirmingConnector(t, target, &PublisherConfig{
		Exchange:   exchange,
		RoutingKey: "nothing.is.bound.to.this",
		Confirms:   true,
		Mandatory:  true,
	})

	if err := c.channel.ExchangeDeclare(exchange, "topic", false, true, false, false, nil); err != nil {
		t.Fatalf("ExchangeDeclare: %v", err)
	}

	// A publish to an exchange with no binding is confirmed by the broker —
	// it took responsibility, there was simply nowhere to put it — so this
	// asserts the publish completes rather than hangs. What must not happen
	// is waiting for ever for an answer that already came.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- c.Publish(ctx, types.NewMessage(map[string]interface{}{"order_id": "order-1"}))
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the publish never returned, so a flow would be stuck on it")
	}
}

func TestAnUnconfirmedPublishStillWorks(t *testing.T) {
	// The default, and the common case: no confirms configured, and the
	// publish returns without waiting.
	target := rabbitMQTestURL(t)

	queue := fmt.Sprintf("mycel_noconfirms_%d", time.Now().UnixNano())
	c := confirmingConnector(t, target, &PublisherConfig{RoutingKey: queue})

	if _, err := c.channel.QueueDeclare(queue, false, true, false, false, nil); err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}

	if err := c.Publish(context.Background(), types.NewMessage(map[string]interface{}{
		"order_id": "order-1",
	})); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// It arrives, it is simply not waited for.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok, err := c.channel.Get(queue, true); err == nil && ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("the message never arrived")
}

func TestAFlowPublishingWithConfirms(t *testing.T) {
	// Through Write, which is the path a flow takes.
	target := rabbitMQTestURL(t)

	queue := fmt.Sprintf("mycel_confirms_write_%d", time.Now().UnixNano())
	c := confirmingConnector(t, target, &PublisherConfig{Confirms: true})

	if _, err := c.channel.QueueDeclare(queue, false, true, false, false, nil); err != nil {
		t.Fatalf("QueueDeclare: %v", err)
	}

	result, err := c.Write(context.Background(), &connector.Data{
		Target:  queue,
		Payload: map[string]interface{}{"order_id": "order-1"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}

	if _, ok, err := c.channel.Get(queue, true); err != nil || !ok {
		t.Errorf("the flow was told the message was published and the broker does not have it: %v", err)
	}
}
