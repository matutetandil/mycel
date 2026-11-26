package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// These pin what a client sees at the transport level, the part that changed
// when the server moved from gorilla/websocket to coder/websocket. The client
// side stays gorilla on purpose: a server checked only against its own
// library's client proves nothing about the clients people actually use.

func newKeepaliveConnector(t *testing.T, ping, pong time.Duration) *httptest.Server {
	t.Helper()
	c := New("ws_keepalive", &Config{Host: "127.0.0.1", Path: "/ws", PingInterval: ping, PongTimeout: pong}, nil)
	server := httptest.NewServer(http.HandlerFunc(c.handleWebSocket))
	t.Cleanup(server.Close)
	return server
}

func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

// readUntilClosed reads until the connection fails or the time runs out, and
// says how long the connection lived. Reading is what answers pings.
func readUntilClosed(ws *websocket.Conn, limit time.Duration) (time.Duration, bool) {
	start := time.Now()
	_ = ws.SetReadDeadline(start.Add(limit))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				return time.Since(start), false
			}
			return time.Since(start), true
		}
	}
}

// A client that answers pings stays connected well past the pong timeout,
// even though it never sends a message.
func TestKeepaliveAnsweredPingsKeepTheConnection(t *testing.T) {
	server := newKeepaliveConnector(t, 50*time.Millisecond, 200*time.Millisecond)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	pings := 0
	ws.SetPingHandler(func(data string) error {
		pings++
		return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})

	if lived, closed := readUntilClosed(ws, 700*time.Millisecond); closed {
		t.Fatalf("a client answering every ping was dropped after %v", lived)
	}
	if pings < 3 {
		t.Fatalf("expected a ping every 50ms, got %d in 700ms", pings)
	}
}

// A client that stops answering is dropped once the pong timeout passes.
func TestKeepaliveSilentClientIsDropped(t *testing.T) {
	server := newKeepaliveConnector(t, 50*time.Millisecond, 200*time.Millisecond)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetPingHandler(func(string) error { return nil }) // never pong

	lived, closed := readUntilClosed(ws, 2*time.Second)
	if !closed {
		t.Fatal("a client that answered no ping was never dropped")
	}
	if lived < 150*time.Millisecond {
		t.Fatalf("dropped after %v, before the 200ms pong timeout", lived)
	}
}

// Any origin may connect. The library this server moved to refuses a
// cross-origin handshake by default; browsers on another host connected
// before the move and must still connect after it.
func TestCrossOriginHandshakeIsAccepted(t *testing.T) {
	_, server := newTestConnector(t)
	defer server.Close()

	header := http.Header{"Origin": []string{"https://app.example.com"}}
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("cross-origin handshake refused (HTTP %d): %v", status, err)
	}
	ws.Close()
}

// A message up to 512KB is read; the library's own default is 32KB, so a
// 100KB message is the one that tells the limit apart.
func TestMessagesUpTo512KBAreAccepted(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	got := make(chan int, 1)
	conn.RegisterRoute("message", func(_ context.Context, input map[string]interface{}) (interface{}, error) {
		s, _ := input["blob"].(string)
		got <- len(s)
		return nil, nil
	})

	ws := dialWS(t, server)
	defer ws.Close()

	blob := strings.Repeat("x", 100*1024)
	sendMsg(t, ws, map[string]interface{}{"type": "message", "data": map[string]interface{}{"blob": blob}})

	select {
	case n := <-got:
		if n != len(blob) {
			t.Fatalf("flow received %d bytes, want %d", n, len(blob))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a 100KB message never reached the flow")
	}
}

func TestMessagesOver512KBCloseTheConnection(t *testing.T) {
	_, server := newTestConnector(t)
	defer server.Close()

	ws := dialWS(t, server)
	defer ws.Close()

	blob := strings.Repeat("x", 600*1024)
	sendMsg(t, ws, map[string]interface{}{"type": "message", "data": blob})

	if _, closed := readUntilClosed(ws, 2*time.Second); !closed {
		t.Fatal("a 600KB message did not close the connection")
	}
}

// pong_timeout is how long to wait for a pong, counted from the ping. With the
// defaults (ping every 30s, pong within 10s) a client with nothing to say was
// dropped ten seconds after connecting, before the first ping was ever sent,
// and again ten seconds after every reconnect.
func TestPongTimeoutShorterThanThePingIntervalKeepsAnAnsweringClient(t *testing.T) {
	server := newKeepaliveConnector(t, 200*time.Millisecond, 50*time.Millisecond)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	pings := 0
	ws.SetPingHandler(func(data string) error {
		pings++
		return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})

	if lived, closed := readUntilClosed(ws, 900*time.Millisecond); closed {
		t.Fatalf("a client answering every ping was dropped after %v (%d pings seen)", lived, pings)
	}
	if pings < 3 {
		t.Fatalf("expected a ping every 200ms, got %d in 900ms", pings)
	}
}

// And a client that stops answering is still dropped: pong_timeout after the
// ping it did not answer.
func TestPongTimeoutShorterThanThePingIntervalStillDropsASilentClient(t *testing.T) {
	server := newKeepaliveConnector(t, 200*time.Millisecond, 50*time.Millisecond)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetPingHandler(func(string) error { return nil }) // never pong

	lived, closed := readUntilClosed(ws, 2*time.Second)
	if !closed {
		t.Fatal("a client that answered no ping was never dropped")
	}
	if lived < 200*time.Millisecond {
		t.Fatalf("dropped after %v, before the first ping was even sent", lived)
	}
}
