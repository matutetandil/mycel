package rabbitmq

import (
	"context"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/v3/internal/connector/mq/types"
)

// The dead letter queue, set up and used.
//
// setupDLQ and the naming behind it were at zero, and a DLQ is what a consumer
// leans on when a message cannot be handled: without one a rejected message is
// gone. This declares a queue with a DLQ, rejects a message, and looks for it
// where it should have landed.
func TestARejectedMessageLandsInTheDeadLetterQueue(t *testing.T) {
	target := rabbitMQTestURL(t)

	queue := uniqueName("mycel_dlq_main")
	c := newTestConnector(t, target, &QueueConfig{
		Name:            queue,
		Durable:         true,
		CreateIfMissing: true,
	}, nil)
	defer c.Close(context.Background())

	// The names the connector works out when the configuration does not say.
	dlq := c.getDLQQueueName(&DLQConfig{Enabled: true})
	if dlq != queue+".dlq" {
		t.Errorf("dead letter queue = %q, want the main queue's name with .dlq", dlq)
	}
	if named := c.getDLQQueueName(&DLQConfig{Enabled: true, Queue: "spelled_out"}); named != "spelled_out" {
		t.Errorf("a dead letter queue that was named came out as %q", named)
	}
	if dlx := c.getDLXExchangeName(&DLQConfig{Enabled: true}); dlx != "dlx" {
		t.Errorf("dead letter exchange with no exchange configured = %q", dlx)
	}

	defer purgeOrDelete(target, queue)
	defer purgeOrDelete(target, dlq)

	if err := c.setupDLQ(&DLQConfig{Enabled: true}); err != nil {
		t.Fatalf("setting up the dead letter queue: %v", err)
	}

	// The queue has to be declared with the dead letter exchange for a
	// rejection to go anywhere, which is what the consumer does at startup.
	conn, err := amqp.Dial(target)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	defer ch.Close()

	deadLettered := uniqueName("mycel_dlq_src")
	defer purgeOrDelete(target, deadLettered)
	if _, err := ch.QueueDeclare(deadLettered, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "dlx",
		"x-dead-letter-routing-key": dlq,
	}); err != nil {
		t.Fatalf("declaring the queue that dead-letters: %v", err)
	}
	if err := ch.QueueBind(dlq, dlq, "dlx", false, nil); err != nil {
		t.Fatalf("binding the dead letter queue: %v", err)
	}

	if err := ch.PublishWithContext(context.Background(), "", deadLettered, false, false,
		amqp.Publishing{ContentType: "application/json", Body: []byte(`{"id":1}`)}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Taken and refused, the way a consumer refuses a message it cannot
	// handle: nacked without requeue.
	delivery, ok, err := ch.Get(deadLettered, false)
	if err != nil || !ok {
		t.Fatalf("nothing to take from the queue: ok=%v err=%v", ok, err)
	}
	if err := delivery.Nack(false, false); err != nil {
		t.Fatalf("nack: %v", err)
	}

	// And it is in the dead letter queue.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, found, err := ch.Get(dlq, true); err == nil && found {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Error("the message was rejected and never arrived in the dead letter queue")
}

// A batch is published one at a time, and a failure names the message that
// failed rather than the batch.
func TestABatchIsPublished(t *testing.T) {
	target := rabbitMQTestURL(t)

	queue := uniqueName("mycel_batch")
	c := newTestConnector(t, target, &QueueConfig{
		Name:            queue,
		Durable:         true,
		CreateIfMissing: true,
	}, nil)
	defer c.Close(context.Background())
	defer purgeOrDelete(target, queue)

	// Declared here: connecting dials the broker, it does not declare — the
	// consumer does that when it starts, and this test does not start one.
	conn, err := amqp.Dial(target)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	defer ch.Close()
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatalf("declaring the queue: %v", err)
	}

	messages := []*types.Message{
		types.NewMessage(map[string]interface{}{"id": 1}),
		types.NewMessage(map[string]interface{}{"id": 2}),
		types.NewMessage(map[string]interface{}{"id": 3}),
	}
	for _, msg := range messages {
		msg.RoutingKey = queue
	}

	if err := c.PublishBatch(context.Background(), messages); err != nil {
		t.Fatalf("publishing a batch: %v", err)
	}

	found := 0
	deadline := time.Now().Add(10 * time.Second)
	for found < len(messages) && time.Now().Before(deadline) {
		if _, ok, err := ch.Get(queue, true); err == nil && ok {
			found++
			continue
		}
		time.Sleep(100 * time.Millisecond)
	}
	if found != len(messages) {
		t.Errorf("%d of %d messages arrived", found, len(messages))
	}
}

// What a connector says about itself, which the banner and the startup notice
// both read.
func TestAConnectorSaysWhichQueueItIsFor(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Queue = &QueueConfig{Name: "orders"}
	cfg.Exchange = &ExchangeConfig{Name: "orders_exchange"}

	c, err := NewConnector("rabbit", cfg, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if c.Name() != "rabbit" {
		t.Errorf("name = %q", c.Name())
	}
	if c.Type() != "mq" {
		t.Errorf("type = %q", c.Type())
	}
	if c.QueueName() != "orders" {
		t.Errorf("queue = %q", c.QueueName())
	}
	if c.ExchangeName() != "orders_exchange" {
		t.Errorf("exchange = %q", c.ExchangeName())
	}
	driver, queue := c.SourceInfo()
	if driver != "rabbitmq" || queue != "orders" {
		t.Errorf("source info = %q/%q", driver, queue)
	}

	// And with nothing configured, it says nothing rather than guessing.
	bare, err := NewConnector("rabbit", DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if bare.QueueName() != "" || bare.ExchangeName() != "" {
		t.Errorf("a connector with no queue says %q/%q", bare.QueueName(), bare.ExchangeName())
	}
}

// A flow reading from a queue gets a catch-all when it names no operation,
// which is what makes `operation` optional on a queue source.
func TestASourceWithoutAnOperationCatchesEverything(t *testing.T) {
	c, err := NewConnector("rabbit", DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	params := map[string]interface{}{}
	if err := c.ValidateSourceParams(params); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if params["operation"] != "*" {
		t.Errorf("operation = %#v, want the catch-all", params["operation"])
	}

	named := map[string]interface{}{"operation": "orders.created"}
	if err := c.ValidateSourceParams(named); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if named["operation"] != "orders.created" {
		t.Errorf("an operation that was named came out as %#v", named["operation"])
	}
}
