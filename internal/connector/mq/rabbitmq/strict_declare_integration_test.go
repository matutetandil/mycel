package rabbitmq

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// rabbitMQTestURL returns the AMQP URL for integration tests, or "" if no
// broker is reachable within 2 seconds. Reading the URL from the environment
// lets the integration suite point this at the docker-compose broker without
// having to use a build tag.
func rabbitMQTestURL(t *testing.T) string {
	t.Helper()
	target := os.Getenv("MYCEL_TEST_RABBITMQ_URL")
	if target == "" {
		target = "amqp://guest:guest@localhost:5672/"
	}

	u, err := url.Parse(target)
	if err != nil {
		t.Skipf("invalid MYCEL_TEST_RABBITMQ_URL %q: %v", target, err)
	}
	host := u.Host
	if host == "" || !strings.Contains(host, ":") {
		host = host + ":5672"
	}
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		t.Skipf("no RabbitMQ broker reachable at %s (set MYCEL_TEST_RABBITMQ_URL to enable): %v", host, err)
	}
	_ = conn.Close()
	return target
}

// newTestConnector builds a Connector wired to the test broker and connected.
// The caller is responsible for c.Close() when done.
func newTestConnector(t *testing.T, target string, queueCfg *QueueConfig, exCfg *ExchangeConfig) *Connector {
	t.Helper()
	cfg := DefaultConfig()
	cfg.URL = target
	cfg.Queue = queueCfg
	cfg.Exchange = exCfg
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	c, err := NewConnector(t.Name(), cfg, logger)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return c
}

// uniqueName returns a queue/exchange name unique to the test invocation so
// parallel and repeat runs do not collide.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// purgeOrDelete removes a queue best-effort. Used as a deferred cleanup so a
// failing test does not leave broker state behind.
func purgeOrDelete(target, name string) {
	conn, err := amqp.Dial(target)
	if err != nil {
		return
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return
	}
	defer ch.Close()
	_, _ = ch.QueueDelete(name, false, false, true)
}

// deleteExchange removes an exchange best-effort.
func deleteExchange(target, name string) {
	conn, err := amqp.Dial(target)
	if err != nil {
		return
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return
	}
	defer ch.Close()
	_ = ch.ExchangeDelete(name, false, true)
}

// TestSetupTopologyStrictDeclare_Integration is the v2.0.0 contract test: it
// covers the three scenarios that motivated the breaking change.
//
// Requires a reachable RabbitMQ broker. Skips fast (~2s connect timeout) when
// no broker is available so that running the unit-test suite (`go test ./...`)
// on a developer machine without docker stays cheap.
//
// The integration suite sets MYCEL_TEST_RABBITMQ_URL to point this at the
// docker-compose broker.
func TestSetupTopologyStrictDeclare_Integration(t *testing.T) {
	target := rabbitMQTestURL(t)

	t.Run("missing queue with default create_if_missing=false fails fast", func(t *testing.T) {
		name := uniqueName("strict_missing_default")
		defer purgeOrDelete(target, name)

		c := newTestConnector(t, target, &QueueConfig{
			Name:    name,
			Durable: true,
			// CreateIfMissing intentionally left false: this is the new default
		}, nil)
		defer c.Close(context.Background())

		err := c.setupTopology()
		if err == nil {
			t.Fatalf("expected setupTopology to fail for missing queue with create_if_missing=false; got nil")
		}
		if !strings.Contains(err.Error(), "does not exist on broker") {
			t.Errorf("error message should reference broker; got %v", err)
		}
		if !strings.Contains(err.Error(), "create_if_missing") {
			t.Errorf("error message should point operators at the opt-in flag; got %v", err)
		}

		// And confirm Mycel did NOT silently create the queue.
		probeConn, err := amqp.Dial(target)
		if err != nil {
			t.Fatalf("probe dial: %v", err)
		}
		defer probeConn.Close()
		probeCh, err := probeConn.Channel()
		if err != nil {
			t.Fatalf("probe channel: %v", err)
		}
		defer probeCh.Close()
		if _, err := probeCh.QueueDeclarePassive(name, true, false, false, false, nil); err == nil {
			t.Errorf("queue %q was created despite create_if_missing=false; v2.0.0 contract broken", name)
		}
	})

	t.Run("missing queue with create_if_missing=true is declared", func(t *testing.T) {
		name := uniqueName("strict_missing_optin")
		defer purgeOrDelete(target, name)

		c := newTestConnector(t, target, &QueueConfig{
			Name:            name,
			Durable:         true,
			CreateIfMissing: true,
		}, nil)
		defer c.Close(context.Background())

		if err := c.setupTopology(); err != nil {
			t.Fatalf("setupTopology with create_if_missing=true: %v", err)
		}

		// Confirm the queue was actually declared on the broker.
		probeConn, _ := amqp.Dial(target)
		defer probeConn.Close()
		probeCh, _ := probeConn.Channel()
		defer probeCh.Close()
		if _, err := probeCh.QueueDeclarePassive(name, true, false, false, false, nil); err != nil {
			t.Errorf("queue %q was not created despite create_if_missing=true: %v", name, err)
		}
	})

	t.Run("existing queue with default create_if_missing=false boots cleanly", func(t *testing.T) {
		name := uniqueName("strict_existing")
		defer purgeOrDelete(target, name)

		// Pre-create the queue out-of-band, simulating Terraform/ops-managed infra.
		setupConn, err := amqp.Dial(target)
		if err != nil {
			t.Fatalf("setup dial: %v", err)
		}
		setupCh, err := setupConn.Channel()
		if err != nil {
			setupConn.Close()
			t.Fatalf("setup channel: %v", err)
		}
		if _, err := setupCh.QueueDeclare(name, true, false, false, false, nil); err != nil {
			setupCh.Close()
			setupConn.Close()
			t.Fatalf("pre-create queue: %v", err)
		}
		setupCh.Close()
		setupConn.Close()

		c := newTestConnector(t, target, &QueueConfig{
			Name:    name,
			Durable: true,
			// CreateIfMissing left false: we want to verify the strict-default
			// path STILL works when the queue already exists.
		}, nil)
		defer c.Close(context.Background())

		if err := c.setupTopology(); err != nil {
			t.Fatalf("setupTopology against existing queue with create_if_missing=false: %v", err)
		}
	})

	t.Run("missing exchange with default create_if_missing=false fails fast", func(t *testing.T) {
		exName := uniqueName("strict_ex_missing")
		defer deleteExchange(target, exName)

		c := newTestConnector(t, target, nil, &ExchangeConfig{
			Name:    exName,
			Type:    ExchangeTopic,
			Durable: true,
			// CreateIfMissing intentionally left false
		})
		defer c.Close(context.Background())

		err := c.setupTopology()
		if err == nil {
			t.Fatalf("expected setupTopology to fail for missing exchange with create_if_missing=false")
		}
		if !strings.Contains(err.Error(), "exchange") || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("error message should be exchange-specific; got %v", err)
		}
	})
}
