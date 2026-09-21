package redis

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Publishing and receiving against a real Redis.
//
// Connect, Close, Health, Write, Publish and the receive loop were all at
// zero: the whole of what this connector does. What exercises it today is the
// integration suite, which runs the binary, so none of it is visible here —
// and pub/sub is the one transport where a message nobody is subscribed to is
// simply gone, so which handler a channel reaches is worth pinning down.
func liveRedis(t *testing.T) (host string, port int) {
	t.Helper()

	declared := os.Getenv("MYCEL_TEST_REDIS_URL")
	address := strings.TrimPrefix(declared, "redis://")
	if address == "" {
		address = "127.0.0.1:36379"
	}

	h, p, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("MYCEL_TEST_REDIS_URL: %v", err)
	}
	n, _ := strconv.Atoi(p)

	if err := redisAnswers(address); err != nil {
		// Somebody named this address, so nothing answering there is a
		// failure rather than a reason to quietly run no tests: the
		// integration runner sets it, and a suite that skips itself in the
		// run that exists to exercise it is the same as not having written
		// it.
		if declared != "" {
			t.Fatalf("MYCEL_TEST_REDIS_URL names %s and no Redis answers there: %v", address, err)
		}
		t.Skipf("no Redis at %s (%v) — the integration stack publishes one", address, err)
	}

	return h, n
}

// redisAnswers reports whether the thing listening at this address is Redis.
//
// A dial says only that something accepted a connection, and that is not the
// same question: a CI runner had something HTTP on the port this defaults to,
// so the dial succeeded, the client sent RESP and got back
// `HTTP/1.1 400 Bad Request` — three tests failing in a job that was supposed
// to skip them, with a message about Redis being unparseable.
//
// PING is the cheapest thing Redis answers, sent as RESP rather than as an
// inline command: a real server takes either, and miniredis — which this
// package's own probe test runs against — only takes RESP. A server that wants
// AUTH first replies `-NOAUTH …`, which is still Redis saying so, so any RESP
// reply counts and anything else does not.
func redisAnswers(address string) error {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		return err
	}

	reply := make([]byte, 1)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("nothing answered a PING: %w", err)
	}
	// +PONG, or -NOAUTH / -ERR: the first byte is what makes it RESP.
	if reply[0] != '+' && reply[0] != '-' {
		rest := make([]byte, 40)
		n, _ := conn.Read(rest)
		return fmt.Errorf("something is listening but it is not Redis: it answered %q",
			strings.TrimSpace(string(append(reply, rest[:n]...))))
	}
	return nil
}

func subscriber(t *testing.T, channels, patterns []string) *Connector {
	t.Helper()

	host, port := liveRedis(t)
	config := DefaultConfig()
	config.Host = host
	config.Port = port
	config.Channels = channels
	config.Patterns = patterns

	c, err := NewConnector("events", config, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestAPublishedMessageReachesTheFlowSubscribedToItsChannel(t *testing.T) {
	unique := strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	channel := "orders-" + unique

	c := subscriber(t, []string{channel}, nil)

	var (
		mu       sync.Mutex
		received []map[string]interface{}
	)
	arrived := make(chan struct{}, 4)
	c.RegisterRoute(channel, func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		received = append(received, input)
		mu.Unlock()
		arrived <- struct{}{}
		return nil, nil
	})

	if err := c.Health(context.Background()); err != nil {
		t.Errorf("health before starting: %v", err)
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	// A subscription is not in place the instant Start returns.
	time.Sleep(300 * time.Millisecond)

	// Published through the same connector, which is what a flow writing to a
	// channel does.
	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  channel,
		Payload: map[string]interface{}{"order_id": "ord-1", "total": 99.5},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the message was published and never arrived")
	}

	mu.Lock()
	defer mu.Unlock()
	input := received[0]

	// Redis pub/sub merges the payload straight onto input — it does not wrap
	// it under `body` the way RabbitMQ and Kafka do. Worth pinning: it is the
	// difference between `input.order_id` and `input.body.order_id`, and the
	// reference page is the only place that says so.
	if input["order_id"] != "ord-1" {
		t.Errorf("order_id = %#v, want the payload merged onto input", input["order_id"])
	}
	if _, wrapped := input["body"]; wrapped {
		t.Errorf("the payload came back wrapped: %#v", input)
	}
	if input["_channel"] != channel {
		t.Errorf("_channel = %#v, want %q", input["_channel"], channel)
	}
}

// A pattern subscription reaches what matches it, and the channel it arrived
// on is still the exact one.
func TestAPatternSubscriptionReachesWhatMatchesIt(t *testing.T) {
	unique := strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	pattern := "events-" + unique + ".*"
	channel := "events-" + unique + ".created"

	c := subscriber(t, nil, []string{pattern})

	arrived := make(chan map[string]interface{}, 4)
	c.RegisterRoute(pattern, func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		arrived <- input
		return nil, nil
	})

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	if _, err := c.Write(context.Background(), &connector.Data{
		Target:  channel,
		Payload: map[string]interface{}{"id": 1},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case input := <-arrived:
		if input["_channel"] != channel {
			t.Errorf("_channel = %#v, want the channel it was published on", input["_channel"])
		}
		if input["_pattern"] != pattern {
			t.Errorf("_pattern = %#v, want the pattern that matched", input["_pattern"])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a message on a channel the pattern matches never arrived")
	}
}

// A connector that was closed says so rather than answering.
func TestAClosedConnectorRefusesToPublish(t *testing.T) {
	host, port := liveRedis(t)
	config := DefaultConfig()
	config.Host = host
	config.Port = port

	c, err := NewConnector("events", config, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := c.Health(context.Background()); err == nil {
		t.Error("a closed connector reported itself healthy")
	}
	if _, err := c.Write(context.Background(), &connector.Data{
		Target: "x", Payload: map[string]interface{}{"a": 1},
	}); err == nil {
		t.Error("a closed connector published")
	}
}
