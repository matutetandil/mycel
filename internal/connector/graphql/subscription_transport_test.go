package graphql

import (
	"net/http"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
)

// What a client sees of the subscription server's transport: the part that
// changed when it moved from gorilla/websocket to coder/websocket. The client
// stays gorilla on purpose, so the server is checked against an
// implementation other than its own.

// The handshake picks graphql-transport-ws and accepts a page served from
// another host. The new library refuses cross-origin handshakes by default,
// and a browser client on another origin is the ordinary case for a GraphQL
// endpoint.
func TestSubscriptionHandshakeNegotiatesTheProtocolAcrossOrigins(t *testing.T) {
	server, _ := subscriptionServer(t, 30*time.Second, time.Minute)

	address := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := gorilla.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	conn, resp, err := dialer.Dial(address, http.Header{"Origin": []string{"https://app.example.com"}})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("cross-origin handshake refused (HTTP %d): %v", status, err)
	}
	defer conn.Close()

	if got := conn.Subprotocol(); got != "graphql-transport-ws" {
		t.Fatalf("negotiated subprotocol %q, want graphql-transport-ws", got)
	}
}

// connection_init can carry a large payload — tokens, client metadata — and
// anything up to 512KB is read. The new library's own default is 32KB.
func TestALargeConnectionInitIsRead(t *testing.T) {
	server, _ := subscriptionServer(t, 30*time.Second, time.Minute)
	conn := dialSubscription(t, server)

	init := map[string]interface{}{
		"type":    "connection_init",
		"payload": map[string]interface{}{"token": strings.Repeat("x", 100*1024)},
	}
	if err := conn.WriteJSON(init); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var reply map[string]interface{}
	if err := conn.ReadJSON(&reply); err != nil {
		t.Fatalf("a 100KB connection_init got no answer: %v", err)
	}
	if reply["type"] != "connection_ack" {
		t.Fatalf("answered %v, want connection_ack", reply["type"])
	}
}

func TestAMessageOver512KBClosesTheSubscriptionConnection(t *testing.T) {
	server, _ := subscriptionServer(t, 30*time.Second, time.Minute)
	conn := dialSubscription(t, server)

	if err := conn.WriteJSON(map[string]interface{}{
		"type":    "connection_init",
		"payload": map[string]interface{}{"token": strings.Repeat("x", 600*1024)},
	}); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				t.Fatal("a 600KB message did not close the connection")
			}
			return
		}
	}
}
