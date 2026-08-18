package graphql

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/graphql-go/graphql"
)

// Keeping a subscription open while nothing is happening.
//
// A subscription is idle by nature: waiting for something to happen is what it
// is for. Nothing kept the socket busy — the server answered a ping and never
// sent one — so `keep_alive_interval`, whose documented purpose is the ping
// period on an idle socket, did nothing. And the read deadline was a hardcoded
// minute refreshed only by incoming messages, so a subscription whose client
// had nothing to say was dropped by this server after sixty seconds. Even one
// actively delivering events, because those are writes and the deadline
// watches reads.

func subscriptionServer(t *testing.T, keepAlive, idleTimeout time.Duration) (*httptest.Server, *SubscriptionManager) {
	t.Helper()

	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{
			Name: "Query",
			Fields: graphql.Fields{
				"ping": &graphql.Field{Type: graphql.String},
			},
		}),
	})
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	manager := NewSubscriptionManagerWithTimings(&schema,
		slog.New(slog.NewTextHandler(quietWriter{}, nil)), keepAlive, idleTimeout)

	server := httptest.NewServer(manager.Handler())
	t.Cleanup(server.Close)
	return server, manager
}

type quietWriter struct{}

func (quietWriter) Write(p []byte) (int, error) { return len(p), nil }

func dialSubscription(t *testing.T, server *httptest.Server) *gorilla.Conn {
	t.Helper()

	address := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := gorilla.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	conn, _, err := dialer.Dial(address, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestAnIdleSubscriptionIsPinged(t *testing.T) {
	// Without this the socket looks dead to every box between here and the
	// client, and the first one with an idle timeout closes it — the client
	// still believes it is subscribed and no events arrive.
	server, _ := subscriptionServer(t, 150*time.Millisecond, 5*time.Second)
	conn := dialSubscription(t, server)

	// Say hello and then nothing at all, which is what a subscriber does.
	send(t, conn, wsMessage{Type: msgConnectionInit})

	deadline := time.Now().Add(5 * time.Second)
	var pings int
	for time.Now().Before(deadline) && pings < 2 {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("the connection ended while waiting for a ping: %v", err)
		}
		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Type == msgPing {
			pings++
		}
	}

	if pings < 2 {
		t.Errorf("the server sent %d pings on an idle connection, so nothing keeps it open", pings)
	}
}

func TestAQuietSubscriptionIsNotDropped(t *testing.T) {
	// The failure this fixes: a client that says nothing after subscribing was
	// disconnected by this server on its own read deadline.
	server, _ := subscriptionServer(t, 100*time.Millisecond, 300*time.Millisecond)
	conn := dialSubscription(t, server)

	send(t, conn, wsMessage{Type: msgConnectionInit})

	// Well past the idle timeout, answering pings the way a client library
	// does and saying nothing else.
	stopAt := time.Now().Add(2 * time.Second)
	for time.Now().Before(stopAt) {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("the subscription was dropped after %v of quiet: %v",
				time.Until(stopAt), err)
		}
		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err == nil && msg.Type == msgPing {
			send(t, conn, wsMessage{Type: msgPong})
		}
	}
}

func TestAConnectionThatStopsAnsweringIsDropped(t *testing.T) {
	// The other half: a client that has gone away without closing must not
	// hold a subscription and its resources for ever.
	server, manager := subscriptionServer(t, 100*time.Millisecond, 250*time.Millisecond)
	conn := dialSubscription(t, server)

	send(t, conn, wsMessage{Type: msgConnectionInit})

	// Read nothing and answer nothing: the socket is open and the peer is
	// not there.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.RLock()
		open := len(manager.clients)
		manager.mu.RUnlock()
		if open == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("a client that stopped answering is still connected")
}

func TestTheTimingsComeFromTheConfiguration(t *testing.T) {
	// And a manager built with none still has sensible ones, because the
	// alternative is a ticker that never fires and a deadline of zero.
	_, configured := subscriptionServer(t, 5*time.Second, 20*time.Second)
	if configured.keepAlive != 5*time.Second || configured.idleTimeout != 20*time.Second {
		t.Errorf("timings = %v / %v", configured.keepAlive, configured.idleTimeout)
	}

	_, defaults := subscriptionServer(t, 0, 0)
	if defaults.keepAlive <= 0 || defaults.idleTimeout <= 0 {
		t.Errorf("a manager with nothing configured got %v / %v",
			defaults.keepAlive, defaults.idleTimeout)
	}

	// A timeout at or below the ping period would close every connection on
	// schedule, before an answer could arrive.
	_, tooTight := subscriptionServer(t, 30*time.Second, 10*time.Second)
	if tooTight.idleTimeout <= tooTight.keepAlive {
		t.Errorf("a connection is dropped after %v while pinged every %v",
			tooTight.idleTimeout, tooTight.keepAlive)
	}
}

func send(t *testing.T, conn *gorilla.Conn, msg wsMessage) {
	t.Helper()
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(gorilla.TextMessage, body); err != nil {
		t.Fatalf("write: %v", err)
	}
}
