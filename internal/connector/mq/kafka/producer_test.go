package kafka

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/mq/types"
)

// The producer, which is where durability is decided. acks is the setting that
// says whether a publish means "the leader has it and so do its replicas" or
// "it left this process" — and a service that meant the first and got the
// second loses messages on a broker restart, silently, until somebody counts.

func producer(t *testing.T, config *Config) *Connector {
	t.Helper()
	c, err := NewConnector("orders_kafka", config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestWhatAPublishWaitsFor(t *testing.T) {
	// Each of these is a different promise to the caller, and the wrong one is
	// invisible until a broker goes down mid-publish.
	for name, tc := range map[string]struct {
		acks string
		want kafkago.RequiredAcks
	}{
		"every replica":      {"all", kafkago.RequireAll},
		"only the leader":    {"one", kafkago.RequireOne},
		"nobody at all":      {"none", kafkago.RequireNone},
		"nothing configured": {"", kafkago.RequireAll},
	} {
		t.Run(name, func(t *testing.T) {
			c := producer(t, &Config{
				Brokers:  []string{"kafka:9092"},
				Producer: &ProducerConfig{Topic: "orders", Acks: tc.acks},
			})
			if c.writer.RequiredAcks != tc.want {
				t.Errorf("acks = %v, want %v", c.writer.RequiredAcks, tc.want)
			}
		})
	}

	// Nothing said at all is the safe end: a service that never mentions acks
	// waits for the replicas.
	c := producer(t, &Config{Brokers: []string{"kafka:9092"}, Producer: &ProducerConfig{Topic: "orders"}})
	if c.writer.RequiredAcks != kafkago.RequireAll {
		t.Errorf("acks = %v, want every replica by default", c.writer.RequiredAcks)
	}
}

func TestTheCompressionAskedForIsTheOneUsed(t *testing.T) {
	// A managed broker charges by the byte and caps message size, so this is
	// the difference between a payload that goes through and one that is
	// refused.
	for name, tc := range map[string]struct {
		compression string
		want        kafkago.Compression
	}{
		"gzip":         {"gzip", kafkago.Gzip},
		"snappy":       {"snappy", kafkago.Snappy},
		"lz4":          {"lz4", kafkago.Lz4},
		"zstd":         {"zstd", kafkago.Zstd},
		"none":         {"none", 0},
		"nothing said": {"", 0},
	} {
		t.Run(name, func(t *testing.T) {
			c := producer(t, &Config{
				Brokers:  []string{"kafka:9092"},
				Producer: &ProducerConfig{Topic: "orders", Compression: tc.compression},
			})
			if c.writer.Compression != tc.want {
				t.Errorf("compression = %v, want %v", c.writer.Compression, tc.want)
			}
		})
	}
}

func TestAProducerCarriesItsBrokersAndItsTopic(t *testing.T) {
	c := producer(t, &Config{
		Brokers: []string{"kafka-1:9092", "kafka-2:9092"},
		Producer: &ProducerConfig{
			Topic: "orders", Retries: 5, BatchSize: 200, Acks: "all",
		},
	})

	// The topic is deliberately not on the writer. kafka-go refuses a message
	// that carries one when the writer has one too, and this connector always
	// puts the topic on the message so a flow can name its own — so a writer
	// carrying it made every publish fail with a complaint about the
	// library's rules, on the setting the documentation lists as required.
	if c.writer.Topic != "" {
		t.Errorf("the writer carries topic %q, which makes every publish fail", c.writer.Topic)
	}
	// It reaches the message instead, which is what gets published.
	message, err := c.buildKafkaMessage(types.NewMessage(map[string]interface{}{"order_id": "order-1"}))
	if err != nil {
		t.Fatalf("buildKafkaMessage: %v", err)
	}
	if message.Topic != "" {
		t.Errorf("a message with no routing key already carries topic %q", message.Topic)
	}
	if !strings.Contains(c.writer.Addr.String(), "kafka-1:9092") {
		t.Errorf("addr = %v, want both brokers", c.writer.Addr)
	}
	// Retries and batching are what a publish does under load, and a setting
	// that does not reach the writer is one nobody honours.
	if c.writer.MaxAttempts != 5 {
		t.Errorf("attempts = %d", c.writer.MaxAttempts)
	}
	if c.writer.BatchSize != 200 {
		t.Errorf("batch size = %d", c.writer.BatchSize)
	}
	if c.writer.Balancer == nil {
		t.Error("no balancer, so every message would land on one partition")
	}
}

func TestABrokerAskingForCredentialsGetsThem(t *testing.T) {
	// Confluent and Aiven both want SASL over TLS; without a transport the
	// connection is refused by the broker and nothing here says why.
	dir := t.TempDir()
	caFile := writeCertificate(t, dir)

	c := producer(t, &Config{
		Brokers:  []string{"kafka:9092"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
		TLS:      &TLSConfig{Enabled: true, CAFile: caFile},
		SASL:     &SASLConfig{Mechanism: "SCRAM-SHA-512", Username: "svc", Password: "s3cret"},
	})

	transport, ok := c.writer.Transport.(*kafkago.Transport)
	if !ok {
		t.Fatal("the producer has no transport, so it connects with neither TLS nor credentials")
	}
	if transport.TLS == nil {
		t.Error("no TLS on a producer configured for it")
	}
	if transport.SASL == nil {
		t.Error("no credentials on a producer configured for them")
	}
}

func TestCredentialsThatCannotBeBuiltStopTheConnection(t *testing.T) {
	// At startup, rather than as a connection that never establishes.
	c, err := NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
		SASL:     &SASLConfig{Mechanism: "GSSAPI", Username: "svc", Password: "s3cret"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	if err := c.Connect(context.Background()); err == nil {
		t.Error("a producer connected with a mechanism it cannot speak")
	}

	// And a TLS authority that is not there.
	c, err = NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
		TLS:      &TLSConfig{Enabled: true, CAFile: filepath.Join(t.TempDir(), "absent.pem")},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if err := c.Connect(context.Background()); err == nil {
		t.Error("a producer connected with an authority that is not there")
	}
}

// --- Publishing --------------------------------------------------------------

func TestAMessageWithNowhereToGoIsRefused(t *testing.T) {
	// Rather than published to the empty topic, which the broker answers with
	// something that names neither the flow nor the message.
	c := producer(t, &Config{
		Brokers:  []string{"127.0.0.1:1"},
		Producer: &ProducerConfig{Acks: "all"}, // no default topic
	})

	err := c.Publish(context.Background(), &types.Message{Body: map[string]interface{}{"sku": "W-1"}})
	if err == nil {
		t.Fatal("a message with no topic was published")
	}
	if !strings.Contains(err.Error(), "topic") {
		t.Errorf("error = %q, want it to name what is missing", err)
	}
}

func TestAMessageWithNoTopicOfItsOwnUsesTheProducersDefault(t *testing.T) {
	// The ordinary case: the connector names a topic and a flow publishes to
	// it without repeating it.
	c := producer(t, &Config{
		Brokers:  []string{"127.0.0.1:1"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := c.Publish(ctx, &types.Message{Body: map[string]interface{}{"sku": "W-1"}})
	// The broker is not there, so this fails at the write rather than before
	// it — which is what says the topic was resolved.
	if err == nil {
		t.Fatal("a publish to a broker that is not running reported success")
	}
	if strings.Contains(err.Error(), "topic is required") {
		t.Errorf("the default topic was not used: %v", err)
	}
}

func TestPublishingWithoutAProducerIsReported(t *testing.T) {
	// A flow writing to a connector configured only to consume. Saying so
	// beats a nil dereference at the first message.
	c, err := NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Consumer: &ConsumerConfig{GroupID: "orders", Topics: []string{"orders"}},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close(context.Background())

	message := &types.Message{RoutingKey: "orders", Body: map[string]interface{}{}}

	if err := c.Publish(context.Background(), message); err == nil {
		t.Error("a publish succeeded on a connector with no producer")
	}
	if err := c.PublishBatch(context.Background(), []*types.Message{message}); err == nil {
		t.Error("a batch succeeded on a connector with no producer")
	}
	if _, err := c.Write(context.Background(), &connector.Data{Target: "orders"}); err == nil {
		t.Error("a write succeeded on a connector with no producer")
	}
}

func TestABatchIsCheckedBeforeAnyOfItIsSent(t *testing.T) {
	// One message with nowhere to go must not leave the rest half-published:
	// the whole batch is built first.
	c := producer(t, &Config{
		Brokers:  []string{"127.0.0.1:1"},
		Producer: &ProducerConfig{Acks: "all"}, // no default topic
	})

	err := c.PublishBatch(context.Background(), []*types.Message{
		{RoutingKey: "orders", ID: "1", Body: map[string]interface{}{}},
		{ID: "2", Body: map[string]interface{}{}}, // nowhere to go
	})
	if err == nil {
		t.Fatal("a batch with a message that has no topic was sent")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("error = %q, want it to name the message", err)
	}
}

func TestAKeyKeepsAnEntitysMessagesInOrder(t *testing.T) {
	// Kafka orders within a partition, and the key is what picks one — two
	// messages about the same order with different keys can be processed out
	// of order.
	c := producer(t, &Config{
		Brokers:  []string{"127.0.0.1:1"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// The key is put on the record rather than on the message, so what is
	// observable here is that it got as far as the write.
	err := c.PublishWithKey(ctx, "order-1", &types.Message{
		RoutingKey: "orders", Body: map[string]interface{}{"sku": "W-1"},
	})
	if err == nil {
		t.Fatal("a publish to a broker that is not running reported success")
	}
	if strings.Contains(err.Error(), "not initialized") {
		t.Errorf("the producer was not used: %v", err)
	}

	// And naming the topic per message, which is how one connector serves
	// several.
	other := &types.Message{Body: map[string]interface{}{}}
	_ = c.PublishToTopic(ctx, "returns", other)
	if other.RoutingKey != "returns" {
		t.Errorf("the topic did not reach the message: %+v", other)
	}
}

func TestClosingTwiceIsHarmless(t *testing.T) {
	// Shutdown runs after a failed start often enough that this has to hold.
	c := producer(t, &Config{
		Brokers:  []string{"kafka:9092"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
	})

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("the second close failed: %v", err)
	}
}

// writeCertificate writes a self-signed certificate, the way a broker's CA
// bundle arrives on disk.
func writeCertificate(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "kafka.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}

	path := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	return path
}

func TestARepublishWaitsForTheReplicasToo(t *testing.T) {
	// The writer a consumer builds to republish — to the dead-letter topic, or
	// back onto its own for a bounded retry. It had no acks setting at all,
	// which is fire and forget: the offset for the original message commits,
	// so a republish the broker drops leaves the message in neither place.
	c, err := NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Consumer: &ConsumerConfig{GroupID: "orders", Topics: []string{"orders"}},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	writer, err := c.ensureWriter()
	if err != nil {
		t.Fatalf("ensureWriter: %v", err)
	}
	if writer.RequiredAcks != kafkago.RequireAll {
		t.Errorf("acks = %v, want the replicas: a dead-letter that can be dropped loses the message",
			writer.RequiredAcks)
	}

	// And it is built once: a writer per rejected message would open a
	// connection per message.
	again, err := c.ensureWriter()
	if err != nil {
		t.Fatalf("ensureWriter: %v", err)
	}
	if again != writer {
		t.Error("a second writer was built")
	}
}

func TestARepublishCarriesTheBrokersCredentials(t *testing.T) {
	// A consumer against a managed broker republishes to the same one, so it
	// needs the same TLS and credentials — without them the dead-letter write
	// is refused and the message is lost.
	dir := t.TempDir()

	c, err := NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Consumer: &ConsumerConfig{GroupID: "orders", Topics: []string{"orders"}},
		TLS:      &TLSConfig{Enabled: true, CAFile: writeCertificate(t, dir)},
		SASL:     &SASLConfig{Mechanism: "SCRAM-SHA-256", Username: "svc", Password: "s3cret"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	writer, err := c.ensureWriter()
	if err != nil {
		t.Fatalf("ensureWriter: %v", err)
	}
	transport, ok := writer.Transport.(*kafkago.Transport)
	if !ok || transport.TLS == nil || transport.SASL == nil {
		t.Error("the republish writer connects with neither TLS nor credentials")
	}
}

func TestARepublishWithCredentialsThatCannotBeBuiltIsReported(t *testing.T) {
	c, err := NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Consumer: &ConsumerConfig{GroupID: "orders", Topics: []string{"orders"}},
		SASL:     &SASLConfig{Mechanism: "GSSAPI", Username: "svc", Password: "s3cret"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	if _, err := c.ensureWriter(); err == nil {
		t.Error("a republish writer was built with a mechanism this connector cannot speak")
	}
}

func TestAFlowNeedNotNameATopicItAlreadyConfigured(t *testing.T) {
	// A consumer names its topics in the connector block, so the flow does not
	// repeat them — it gets the catch-all. Requiring the operation here would
	// mean writing the topic twice and having them disagree.
	c, err := NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Consumer: &ConsumerConfig{GroupID: "orders", Topics: []string{"orders"}},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	params := map[string]interface{}{}
	if err := c.ValidateSourceParams(params); err != nil {
		t.Fatalf("a flow naming no topic was refused: %v", err)
	}
	if params["operation"] != "*" {
		t.Errorf("operation = %v, want the catch-all", params["operation"])
	}

	// One that does name a topic keeps it.
	named := map[string]interface{}{"operation": "returns"}
	if err := c.ValidateSourceParams(named); err != nil {
		t.Fatalf("ValidateSourceParams: %v", err)
	}
	if named["operation"] != "returns" {
		t.Errorf("operation = %v, want the one the flow named", named["operation"])
	}
}

func TestCommittingWithoutAConsumerIsReported(t *testing.T) {
	c, err := NewConnector("orders_kafka", &Config{
		Brokers:  []string{"kafka:9092"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	if err := c.CommitMessages(context.Background()); err == nil {
		t.Error("offsets were committed by a connector with no consumer")
	}
}

func TestASchemaRegistryIsBuiltWhenItIsConfigured(t *testing.T) {
	// Reachable from the connector, because encoding a record needs the id the
	// registry issued.
	c := producer(t, &Config{
		Brokers:        []string{"kafka:9092"},
		Producer:       &ProducerConfig{Topic: "orders", Acks: "all"},
		SchemaRegistry: &SchemaRegistryConfig{URL: "http://registry:8081"},
	})

	if c.GetSchemaRegistry() == nil {
		t.Error("a connector configured with a schema registry has none")
	}

	plain := producer(t, &Config{
		Brokers:  []string{"kafka:9092"},
		Producer: &ProducerConfig{Topic: "orders", Acks: "all"},
	})
	if plain.GetSchemaRegistry() != nil {
		t.Error("a connector with no schema registry configured has one")
	}
}
