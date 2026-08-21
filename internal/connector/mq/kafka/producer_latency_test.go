package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// How long a publish takes.
//
// A producer that batches waits for the batch to fill before sending, and how
// long it will wait is linger_ms. It was read from the configuration into a
// field nothing used, so the library's own default applied — a full second —
// and a flow publishing one message per request waited that long for each one,
// whatever the file said.

func kafkaTestBrokers(t *testing.T) []string {
	t.Helper()

	brokers := os.Getenv("MYCEL_TEST_KAFKA_BROKERS")
	if brokers == "" {
		brokers = "localhost:9092"
	}
	conn, err := net.DialTimeout("tcp", brokers, 2*time.Second)
	if err != nil {
		t.Skipf("no Kafka broker reachable at %s (set MYCEL_TEST_KAFKA_BROKERS to enable): %v", brokers, err)
	}
	_ = conn.Close()
	return []string{brokers}
}

func lingerProducer(t *testing.T, topic string, lingerMs int) *Connector {
	t.Helper()

	cfg := DefaultConfig()
	cfg.Brokers = kafkaTestBrokers(t)
	cfg.Consumer = nil
	cfg.Producer = &ProducerConfig{
		Topic:     topic,
		Acks:      "all",
		BatchSize: 16384,
		LingerMs:  lingerMs,
		Retries:   3,
	}

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

func TestASinglePublishDoesNotWaitForABatchThatWillNeverFill(t *testing.T) {
	// The one that matters for a flow answering a request: one message, and
	// nothing else coming to fill the batch with it.
	topic := fmt.Sprintf("mycel_linger_%d", time.Now().UnixNano())
	createTopic(t, topic)
	c := lingerProducer(t, topic, 5)

	// The first publish also creates the topic, so it is not the one timed.
	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "warmup"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	started := time.Now()
	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "order-1"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	took := time.Since(started)

	// With linger_ms unwired the library waits a second before sending. Half
	// of that is comfortably above anything a 5ms linger plus a round trip to
	// a local broker can account for, and comfortably below the second that
	// says the setting is being ignored.
	if took > 500*time.Millisecond {
		t.Errorf("one publish took %v with linger_ms = 5, so the setting is not reaching the writer", took)
	}
	t.Logf("a single publish with linger_ms = 5 took %v", took)
}

func TestALongerLingerIsHonouredToo(t *testing.T) {
	// The other direction: asking to wait actually waits, so the setting is
	// doing something rather than being replaced by a small constant.
	topic := fmt.Sprintf("mycel_linger_long_%d", time.Now().UnixNano())
	createTopic(t, topic)
	c := lingerProducer(t, topic, 400)

	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "warmup"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	started := time.Now()
	if _, err := c.Write(context.Background(), &connector.Data{
		Payload: map[string]interface{}{"order_id": "order-1"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	took := time.Since(started)

	if took < 200*time.Millisecond {
		t.Errorf("one publish took %v with linger_ms = 400, so the wait is not the configured one", took)
	}
	t.Logf("a single publish with linger_ms = 400 took %v", took)
}
