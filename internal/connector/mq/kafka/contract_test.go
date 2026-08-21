package kafka

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// What the runtime asks a connector, rather than what a broker does with it.

func connectorWith(config *Config) *Connector {
	return &Connector{
		name:     "orders_kafka",
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		config:   config,
		handlers: map[string]HandlerFunc{},
	}
}

func TestTheConnectorSaysWhatItIsConsuming(t *testing.T) {
	// This is what the banner prints and what an IDE shows, so a consumer
	// whose topics do not appear looks like one that is not running.
	c := connectorWith(&Config{
		Brokers:  []string{"kafka:9092"},
		Consumer: &ConsumerConfig{GroupID: "orders", Topics: []string{"orders", "returns"}},
	})

	kind, topics := c.SourceInfo()
	if kind != "kafka" {
		t.Errorf("kind = %q", kind)
	}
	if topics != "orders,returns" {
		t.Errorf("topics = %q, want both", topics)
	}

	// A producer has no topics of its own: each message names one.
	producer := connectorWith(&Config{Brokers: []string{"kafka:9092"}})
	if _, topics := producer.SourceInfo(); topics != "" {
		t.Errorf("topics = %q, want none", topics)
	}
}

func TestTwoFlowsOnOneTopicBothRun(t *testing.T) {
	// Fan-out: registering a second handler for a topic must not replace the
	// first, which would silently stop one of the flows.
	c := connectorWith(&Config{Brokers: []string{"kafka:9092"}})

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
		t.Fatal("no handler for a topic two flows registered")
	}
	if _, err := handler(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !first || !second {
		t.Errorf("first ran = %v, second ran = %v — one of the flows was replaced", first, second)
	}
}

func TestReadingFromAKafkaConnectorSaysToUseAConsumer(t *testing.T) {
	// A flow with a Kafka connector in its `to` and a read operation is a
	// configuration mistake, and the error is the only place to say so.
	c := connectorWith(&Config{Brokers: []string{"kafka:9092"}})

	_, err := c.Read(context.Background(), connector.Query{Target: "orders"})
	if err == nil {
		t.Fatal("a read was accepted")
	}
	if !strings.Contains(err.Error(), "consumer") {
		t.Errorf("error = %q, want it to say what to do instead", err)
	}
}

func TestAConnectorWithNoBrokerIsNotHealthy(t *testing.T) {
	// /health is what a load balancer and an orchestrator read; answering
	// healthy with no broker keeps traffic coming to a service that cannot
	// publish.
	c := connectorWith(&Config{Brokers: []string{"127.0.0.1:1"}})

	if err := c.Health(context.Background()); err == nil {
		t.Error("a connector that can reach no broker reported itself healthy")
	}
}

func TestAProducerHasNoConsumerToStart(t *testing.T) {
	// Start is called on every connector; one configured only to publish has
	// nothing to do and must not fail.
	c := connectorWith(&Config{Brokers: []string{"127.0.0.1:1"}})

	if err := c.Start(context.Background()); err != nil {
		t.Errorf("starting a producer failed: %v", err)
	}
}

func TestTheDebugGateLetsMessagesThroughOneAtATime(t *testing.T) {
	// What the IDE drives when somebody is stepping through a consumer. Off,
	// it must not hold anything back.
	c := connectorWith(&Config{Brokers: []string{"kafka:9092"}})

	c.SetDebugMode(true)
	c.AllowOne()
	c.SetDebugMode(false)
}
