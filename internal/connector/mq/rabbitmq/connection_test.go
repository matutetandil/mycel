package rabbitmq

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/mq/types"
)

// Building a connector and what it answers before a broker is reached. Every
// guard here is between a configuration mistake and a nil dereference on the
// first message.

func built(t *testing.T, config *Config) *Connector {
	t.Helper()
	c, err := NewConnector("orders_rabbit", config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	return c
}

// reachable is the shape the factory hands the connector: a URL when one was
// written, and the parts filled in with defaults either way.
func reachable() *Config {
	c := consuming()
	c.Host = "localhost"
	c.Port = 5672
	c.Username = "guest"
	c.Password = "guest"
	c.Vhost = "/"
	return c
}

func TestAConnectorIsRefusedRatherThanBuiltWrong(t *testing.T) {
	// At startup, where somebody can fix it, rather than as a connection that
	// never establishes.
	_, err := NewConnector("orders_rabbit", &Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("a connector with nothing to connect to was built")
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Errorf("error = %q", err)
	}
}

func TestNothingWorksBeforeTheBrokerIsReached(t *testing.T) {
	// A flow writing to a connector whose connection is not up, or a health
	// probe arriving during startup. Each has to say so rather than crash.
	c := built(t, reachable())
	ctx := context.Background()

	if err := c.Health(ctx); err == nil {
		t.Error("a connector with no connection reported itself healthy")
	}

	if _, err := c.Read(ctx, connector.Query{Target: "orders"}); err == nil {
		t.Error("a read succeeded with no channel")
	}

	if _, err := c.Write(ctx, &connector.Data{
		Target: "orders", Payload: map[string]interface{}{"sku": "W-1"},
	}); err == nil {
		t.Error("a write succeeded with no channel")
	}

	if err := c.Publish(ctx, &types.Message{
		RoutingKey: "orders", Body: map[string]interface{}{"sku": "W-1"},
	}); err == nil {
		t.Error("a publish succeeded with no channel")
	}
}

func TestClosingWhatWasNeverOpenedIsHarmless(t *testing.T) {
	// Shutdown runs after a failed start often enough that this has to hold,
	// and twice, because a runtime shutting down may close every connector
	// after one of them has already failed.
	c := built(t, reachable())

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("the second close failed: %v", err)
	}
}

func TestTheAddressAConnectorDialsIsTheOneConfigured(t *testing.T) {
	// A URL is used as written; otherwise it is built from the parts, and the
	// virtual host is the one that separates two services sharing a broker.
	// Both are set, which is what the factory always produces: the parts carry
	// defaults and the URL is what somebody actually wrote. The URL has to win,
	// or a service configured against CloudAMQP quietly dials localhost.
	written := built(t, &Config{
		URL:      "amqp://someone:secret@rabbit.example.com:5672/orders",
		Host:     "localhost",
		Port:     5672,
		Username: "guest",
		Password: "guest",
		Queue:    &QueueConfig{Name: "orders"},
	})
	if got := written.config.AMQPURL(); got != "amqp://someone:secret@rabbit.example.com:5672/orders" {
		t.Errorf("url = %q, want the one written", got)
	}

	assembled := built(t, &Config{
		Host:     "rabbit.example.com",
		Port:     5672,
		Username: "someone",
		Password: "secret",
		Vhost:    "orders",
		Queue:    &QueueConfig{Name: "orders"},
	})
	url := assembled.config.AMQPURL()
	for _, want := range []string{"rabbit.example.com", "5672", "someone", "orders"} {
		if !strings.Contains(url, want) {
			t.Errorf("url = %q, want it to carry %q", url, want)
		}
	}
}

func TestTheDefaultsAConsumerGetsWhenItSaysNothing(t *testing.T) {
	// A dead-letter that is configured with nothing has to be usable: on, with
	// a bound, and a header to count in.
	dlq := DefaultDLQConfig()
	if !dlq.Enabled {
		t.Error("a dead-letter configured with nothing is off")
	}
	if dlq.MaxRetries < 1 {
		t.Errorf("max retries = %d, want a bound", dlq.MaxRetries)
	}
	if dlq.RetryHeader == "" {
		t.Error("no header to count attempts in, so every delivery starts from zero")
	}

	// And a publisher: persistent by default, because a broker restart losing
	// messages is not what anybody expects from a queue.
	publisher := DefaultPublisherConfig()
	if !publisher.Persistent {
		t.Error("messages are published as transient, so a broker restart loses them")
	}
	if publisher.ContentType == "" {
		t.Error("no content type, so a consumer cannot tell what it was sent")
	}
}

func TestTheDebugGateLetsMessagesThroughOneAtATime(t *testing.T) {
	// What an IDE drives when somebody steps through a consumer. Off, it must
	// hold nothing back.
	c := built(t, reachable())

	c.SetDebugMode(true)
	c.AllowOne()
	c.SetDebugMode(false)
}
