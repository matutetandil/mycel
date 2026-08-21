package mq

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Which configurations this factory answers to.
//
// The dispatch decides what a `type = "mq"` connector becomes, and an empty
// driver means RabbitMQ — the default that every queue example relies on
// without writing it.
func TestWhichQueueConfigurationsAreSupported(t *testing.T) {
	f := NewFactory(nil)

	for _, c := range []struct {
		connType string
		driver   string
		want     bool
	}{
		{"mq", "rabbitmq", true},
		{"mq", "kafka", true},
		{"mq", "redis", true},
		{"mq", "", true},
		{"mq", "sqs", false},
		{"mq", "nats", false},
		// The spelling the documentation used for years and the factory never
		// accepted: `type = "queue"` parses and fails here.
		{"queue", "rabbitmq", false},
		{"database", "", false},
	} {
		if got := f.Supports(c.connType, c.driver); got != c.want {
			t.Errorf("Supports(%q, %q) = %v, want %v", c.connType, c.driver, got, c.want)
		}
	}
}

func TestCreatingEachDriver(t *testing.T) {
	f := NewFactory(nil)
	ctx := context.Background()

	for _, c := range []struct {
		name string
		cfg  *connector.Config
		typ  string
	}{
		{
			name: "rabbitmq",
			cfg: &connector.Config{Name: "rabbit", Type: "mq", Driver: "rabbitmq",
				Properties: map[string]interface{}{"host": "broker", "port": 5672, "username": "mycel"}},
			typ: "mq",
		},
		{
			// No driver at all, which every example that writes only
			// `type = "mq"` depends on.
			name: "no driver named",
			cfg: &connector.Config{Name: "rabbit", Type: "mq",
				Properties: map[string]interface{}{"host": "broker"}},
			typ: "mq",
		},
		{
			name: "kafka",
			cfg: &connector.Config{Name: "events", Type: "mq", Driver: "kafka",
				Properties: map[string]interface{}{
					"brokers":  []interface{}{"one:9092", "two:9092"},
					"group_id": "mycel",
				}},
			typ: "mq",
		},
		{
			name: "redis",
			cfg: &connector.Config{Name: "events", Type: "mq", Driver: "redis",
				Properties: map[string]interface{}{
					"host":     "cache",
					"channels": []interface{}{"orders", "payments"},
				}},
			typ: "mq",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			conn, err := f.Create(ctx, c.cfg)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if conn == nil {
				t.Fatal("no connector came back")
			}
			if conn.Name() != c.cfg.Name {
				t.Errorf("name = %q, want %q", conn.Name(), c.cfg.Name)
			}
			if conn.Type() != c.typ {
				t.Errorf("type = %q, want %q", conn.Type(), c.typ)
			}
		})
	}

	if _, err := f.Create(ctx, &connector.Config{Name: "x", Type: "mq", Driver: "sqs"}); err == nil {
		t.Error("a driver this factory does not have was created anyway")
	}
}

// A Redis URL is honoured, which is the whole of the bug it was written for:
// `url` was read by nothing, so a connector written the documented way went to
// localhost:6379 whatever the URL said, and said nothing about it.
//
// Checked by connecting: a URL that is read reaches the server it names, and
// the default it would otherwise have used is not where this one is.
func TestARedisURLIsWhereTheConnectorGoes(t *testing.T) {
	address := os.Getenv("MYCEL_TEST_REDIS_URL")
	if address == "" {
		address = "redis://127.0.0.1:36379"
	}
	host := strings.TrimPrefix(address, "redis://")
	if !reachable(host) {
		t.Skipf("no Redis at %s (the integration stack publishes one)", host)
	}
	if strings.HasSuffix(host, ":6379") {
		t.Skip("this Redis is on the default port, so honouring the URL cannot be told apart from ignoring it")
	}

	conn, err := NewFactory(nil).Create(context.Background(), &connector.Config{
		Name: "events", Type: "mq", Driver: "redis",
		Properties: map[string]interface{}{
			"url":      address,
			"channels": []interface{}{"orders"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("the URL was not where the connector went: %v", err)
	}
	_ = conn.Close(context.Background())
}

// And one that is not a URL is refused, naming the connector.
func TestAMalformedRedisURLIsRefused(t *testing.T) {
	_, err := NewFactory(nil).Create(context.Background(), &connector.Config{
		Name: "events", Type: "mq", Driver: "redis",
		Properties: map[string]interface{}{"url": "not a url at all"},
	})
	if err == nil {
		t.Fatal("a connector was built from something that is not a URL")
	}
	if !strings.Contains(err.Error(), "events") || !strings.Contains(err.Error(), "url") {
		t.Errorf("the refusal reads %q; it should name the connector and the attribute", err)
	}
}

func reachable(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
