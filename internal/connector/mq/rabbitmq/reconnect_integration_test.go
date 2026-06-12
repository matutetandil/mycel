package rabbitmq

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// tcpProxy is a minimal man-in-the-middle TCP proxy used to simulate a broker
// dropping a live connection (CloudAMQP idle-disconnect, network blip) without
// needing the RabbitMQ management API (port 15672 is not exposed to the host in
// the integration compose). The connector dials the proxy; the proxy forwards
// to the real broker. Calling drop() closes the in-flight TCP pairs abruptly,
// which makes amqp091 deliver a non-nil *amqp.Error on NotifyClose — exactly
// the path a server-side close takes (unlike a graceful conn.Close(), which
// delivers nil and is intentionally ignored by monitorConnection).
type tcpProxy struct {
	ln       net.Listener
	upstream string
	mu       sync.Mutex
	conns    []net.Conn
}

func newTCPProxy(t *testing.T, upstream string) *tcpProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := &tcpProxy{ln: ln, upstream: upstream}
	go p.acceptLoop()
	return p
}

func (p *tcpProxy) acceptLoop() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return // listener closed
		}
		go p.handle(client)
	}
}

func (p *tcpProxy) handle(client net.Conn) {
	up, err := net.Dial("tcp", p.upstream)
	if err != nil {
		_ = client.Close()
		return
	}
	p.mu.Lock()
	p.conns = append(p.conns, client, up)
	p.mu.Unlock()

	go func() { _, _ = io.Copy(up, client); _ = up.Close(); _ = client.Close() }()
	go func() { _, _ = io.Copy(client, up); _ = client.Close(); _ = up.Close() }()
}

// drop abruptly closes every tracked TCP pair, simulating the broker dropping
// the connection. The proxy keeps listening, so the connector's reconnect can
// re-dial and establish a fresh upstream connection.
func (p *tcpProxy) drop() {
	p.mu.Lock()
	conns := p.conns
	p.conns = nil
	p.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (p *tcpProxy) addr() string { return p.ln.Addr().String() }

func (p *tcpProxy) Close() {
	_ = p.ln.Close()
	p.drop()
}

// publishToQueue publishes a JSON body directly to the real broker (bypassing
// the proxy) via the default exchange, routing key = queue name.
func publishToQueue(t *testing.T, target, queue, body string) {
	t.Helper()
	conn, err := amqp.Dial(target)
	if err != nil {
		t.Fatalf("publish dial: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("publish channel: %v", err)
	}
	defer ch.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ch.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(body),
	}); err != nil {
		t.Fatalf("publish %q: %v", body, err)
	}
}

// requireRecv asserts that want arrives on got within timeout.
func requireRecv(t *testing.T, got <-chan string, want string, timeout time.Duration) {
	t.Helper()
	select {
	case id := <-got:
		if id != want {
			t.Fatalf("received %q, want %q", id, want)
		}
	case <-time.After(timeout):
		t.Fatalf("timed out after %s waiting for %q (consumer did not deliver — likely a non-resuming zombie)", timeout, want)
	}
}

// requireRecvEventually waits until want is seen on got, tolerating repeats of
// ignore (a message left unacked when the connection dropped is redelivered
// after reconnect — that redelivery is expected and must not fail the test).
func requireRecvEventually(t *testing.T, got <-chan string, want, ignore string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case id := <-got:
			if id == want {
				return
			}
			if id != ignore {
				t.Fatalf("received unexpected %q while waiting for %q", id, want)
			}
			// redelivered ignore value — keep waiting
		case <-deadline:
			t.Fatalf("timed out after %s waiting for %q (consumer did not resume after the drop — likely a non-resuming zombie)", timeout, want)
		}
	}
}

// upstreamHostPort extracts host:port from an AMQP URL (defaulting the port).
func upstreamHostPort(t *testing.T, target string) string {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	host := u.Host
	if host == "" {
		host = "localhost:5672"
	} else if !strings.Contains(host, ":") {
		host += ":5672"
	}
	return host
}

// proxyURL rewrites target to point at the proxy address, preserving creds and vhost.
func proxyURL(t *testing.T, target, proxyAddr string) string {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	user, pass := "guest", "guest"
	if u.User != nil {
		user = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			pass = pw
		}
	}
	vhost := u.Path
	if vhost == "" {
		vhost = "/"
	}
	return fmt.Sprintf("amqp://%s:%s@%s%s", user, pass, proxyAddr, vhost)
}

// TestConsumerResumesAfterConnectionDrop is the regression test for the
// idle-disconnect zombie bug: a consumer connector that stops draining its
// queue after a single broker-initiated connection drop and never re-subscribes
// until a full process restart.
//
// Root cause (fixed): Start() returned early on the consumer path, so a
// consumer never launched its monitorConnection goroutine — nothing watched the
// closeChan, so handleReconnect (which re-issues basic.consume) never ran.
//
// The test drives the real connector against a real broker through a TCP proxy,
// drops the connection mid-flight, and asserts a message published AFTER the
// drop is still consumed. Before the fix this assertion times out; after the
// fix the consumer reconnects, re-subscribes, and drains it.
//
// Skips fast when no broker is reachable (same convention as the strict-declare
// integration test). The integration suite sets MYCEL_TEST_RABBITMQ_URL.
func TestConsumerResumesAfterConnectionDrop(t *testing.T) {
	target := rabbitMQTestURL(t)

	proxy := newTCPProxy(t, upstreamHostPort(t, target))
	defer proxy.Close()

	queueName := uniqueName("reconnect_resume")
	defer purgeOrDelete(target, queueName)

	cfg := DefaultConfig()
	cfg.URL = proxyURL(t, target, proxy.addr())
	cfg.ConnectionName = "mycel-reconnect-test"
	cfg.ReconnectDelay = 500 * time.Millisecond // keep the test snappy (default is 5s)
	cfg.Queue = &QueueConfig{
		Name:            queueName,
		Durable:         false,
		CreateIfMissing: true, // the test owns this queue
	}
	cfg.Consumer = &ConsumerConfig{
		AutoAck:     false,
		Concurrency: 1,
		Prefetch:    1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c, err := NewConnector(t.Name(), cfg, logger)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	got := make(chan string, 16)
	c.RegisterRoute(queueName, func(ctx context.Context, in map[string]interface{}) (interface{}, error) {
		if body, ok := in["body"].(map[string]interface{}); ok {
			if id, ok := body["id"].(string); ok {
				got <- id
			}
		}
		return nil, nil
	})

	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close(context.Background())
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Baseline: the consumer drains a message on the original connection.
	publishToQueue(t, target, queueName, `{"id":"before"}`)
	requireRecv(t, got, "before", 5*time.Second)

	// Simulate the broker dropping the idle connection. The original delivery
	// loop dies; recovery now depends entirely on the consumer's reconnect path.
	proxy.drop()

	// A message published AFTER the drop must still be consumed once the
	// consumer reconnects and re-subscribes. Generous timeout: ReconnectDelay
	// (500ms) + dial + re-declare + re-consume. Tolerate a redelivered "before"
	// (its ack may have been lost when the connection died mid-flight).
	publishToQueue(t, target, queueName, `{"id":"after"}`)
	requireRecvEventually(t, got, "after", "before", 20*time.Second)
}
