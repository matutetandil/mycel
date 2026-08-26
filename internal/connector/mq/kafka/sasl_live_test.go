package kafka

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector/mq/types"
)

// The sasl block, against a broker that asks who you are.
//
// Nothing in the test stack authenticated, so the block could be built into a
// mechanism and never presented to anything — the tests covered the mechanism
// it constructs, not whether a broker accepts it. The stack's Kafka now has a
// second listener speaking SASL_PLAINTEXT, on the same broker and the same
// topics: only the door is different.

func saslBrokers(t *testing.T) []string {
	t.Helper()

	brokers := os.Getenv("MYCEL_TEST_KAFKA_SASL_BROKERS")
	if brokers == "" {
		t.Skip("set MYCEL_TEST_KAFKA_SASL_BROKERS to run this against a broker that authenticates")
	}
	conn, err := net.DialTimeout("tcp", brokers, 2*time.Second)
	if err != nil {
		t.Skipf("nothing answers at %s: %v", brokers, err)
	}
	_ = conn.Close()
	return []string{brokers}
}

func saslProducer(t *testing.T, topic string, sasl *SASLConfig) *Connector {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Brokers = saslBrokers(t)
	cfg.Consumer = nil
	cfg.SASL = sasl
	cfg.Producer = &ProducerConfig{Topic: topic, Acks: "all", Retries: 1, LingerMs: 1}

	conn, err := NewConnector("secured", cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// The credentials the broker knows get a message onto the topic.
func TestAProducerWithTheRightCredentialsIsLetIn(t *testing.T) {
	conn := saslProducer(t, "orders", &SASLConfig{
		Mechanism: "PLAIN",
		Username:  "mycel",
		Password:  "mycel-secret",
	})

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	message := &types.Message{Body: map[string]interface{}{"id": fmt.Sprintf("sasl-%d", time.Now().UnixNano())}}
	if err := conn.Publish(ctx, message); err != nil {
		t.Errorf("a producer with the credentials the broker knows was refused: %v", err)
	}
}

// The wrong password does not. Without this the test above proves nothing: a
// broker that let everybody in would pass it just as well.
func TestAProducerWithTheWrongPasswordIsNot(t *testing.T) {
	conn := saslProducer(t, "orders", &SASLConfig{
		Mechanism: "PLAIN",
		Username:  "mycel",
		Password:  "not-the-password",
	})

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := conn.Publish(ctx, &types.Message{Body: map[string]interface{}{"id": "nope"}}); err == nil {
		t.Error("a producer with the wrong password published anyway")
	}
}

// And neither does presenting nothing at all.
func TestAProducerWithNoCredentialsIsNot(t *testing.T) {
	conn := saslProducer(t, "orders", nil)

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := conn.Publish(ctx, &types.Message{Body: map[string]interface{}{"id": "nope"}}); err == nil {
		t.Error("a producer that authenticated with nothing published anyway")
	}
}

// A consumer presents its credentials too.
//
// SASL used to be attached to the reader's dialer only inside the TLS branch,
// so a consumer configured with credentials and no TLS never presented them.
// Against a SASL_PLAINTEXT listener — which is how an internal broker is
// usually reached — the group coordinator lookup answered EOF and the consumer
// read nothing, for as long as the service ran, while the producer beside it
// published happily with the same configuration.
func TestAConsumerPresentsItsCredentialsToo(t *testing.T) {
	brokers := saslBrokers(t)

	topic := "secure_orders"
	group := fmt.Sprintf("mycel-sasl-test-%d", time.Now().UnixNano())

	credentials := &SASLConfig{Mechanism: "PLAIN", Username: "mycel", Password: "mycel-secret"}

	// Something to find once the consumer is in.
	producer := saslProducer(t, topic, credentials)
	if err := producer.Connect(context.Background()); err != nil {
		t.Fatalf("connect the producer: %v", err)
	}
	marker := fmt.Sprintf("sasl-consumer-%d", time.Now().UnixNano())
	publishCtx, cancelPublish := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelPublish()
	if err := producer.Publish(publishCtx, &types.Message{Body: map[string]interface{}{"id": marker}}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Brokers = brokers
	cfg.Producer = nil
	cfg.SASL = credentials
	cfg.Consumer = &ConsumerConfig{
		GroupID:         group,
		Topics:          []string{topic},
		AutoOffsetReset: "earliest",
		AutoCommit:      true,
	}

	consumer, err := NewConnector("secured-consumer", cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	defer func() { _ = consumer.Close(context.Background()) }()

	arrived := make(chan string, 8)
	consumer.RegisterHandler(topic, func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		// A message off a queue arrives wrapped, payload under body.
		if body, ok := input["body"].(map[string]interface{}); ok {
			if id, ok := body["id"].(string); ok {
				select {
				case arrived <- id:
				default:
				}
			}
		}
		return nil, nil
	})

	if err := consumer.Connect(context.Background()); err != nil {
		t.Fatalf("connect the consumer: %v", err)
	}
	// Connect prepares the reader; Start is what joins the group and begins
	// reading, which is what the runtime calls for an event-driven source.
	if err := consumer.Start(context.Background()); err != nil {
		t.Fatalf("start the consumer: %v", err)
	}

	deadline := time.After(30 * time.Second)
	for {
		select {
		case id := <-arrived:
			if id == marker {
				return
			}
		case <-deadline:
			t.Fatal("the consumer never read the message it was published — it did not get into its group")
		}
	}
}
