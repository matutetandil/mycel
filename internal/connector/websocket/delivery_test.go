package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Who receives what.
//
// This connector's whole job is deciding which of the open sockets a message
// goes to: everybody, a room, or one person. Sending to the wrong one is a
// customer reading somebody else's order, and sending to nobody is a customer
// waiting for an update that was written, logged and delivered to no one.
// None of the three was tested.

func serverOn(t *testing.T) (*Connector, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	c := New("ws", &Config{
		Host: "127.0.0.1",
		Port: port,
		Path: "/ws",
	}, slog.New(slog.NewTextHandler(quiet{}, nil)))

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	address := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	waitForServer(t, fmt.Sprintf("127.0.0.1:%d", port))
	return c, address
}

type quiet struct{}

func (quiet) Write(p []byte) (int, error) { return len(p), nil }

func waitForServer(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the websocket server never came up")
}

// dial opens a client connection, optionally saying who it belongs to.
func dial(t *testing.T, address, query string) *gorilla.Conn {
	t.Helper()
	if query != "" {
		address += "?" + query
	}
	conn, _, err := gorilla.DefaultDialer.Dial(address, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", address, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// nextMessage reads one message, or reports that none arrived.
func nextMessage(t *testing.T, conn *gorilla.Conn) (map[string]interface{}, bool) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, false
	}
	var message map[string]interface{}
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("the server sent something that is not JSON: %s", raw)
	}
	return message, true
}

func TestAMessageForEverybody(t *testing.T) {
	c, address := serverOn(t)

	first := dial(t, address, "")
	second := dial(t, address, "")
	waitForClients(t, c, 2)

	result, err := c.Write(context.Background(), &connector.Data{
		Operation: "broadcast",
		Payload:   map[string]interface{}{"order_id": "order-1"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("result = %+v", result)
	}

	for name, conn := range map[string]*gorilla.Conn{"first": first, "second": second} {
		message, ok := nextMessage(t, conn)
		if !ok {
			t.Fatalf("the %s client received nothing", name)
		}
		data, _ := message["data"].(map[string]interface{})
		if data["order_id"] != "order-1" {
			t.Errorf("the %s client received %v", name, message)
		}
	}
}

func TestAMessageForOneRoom(t *testing.T) {
	// Rooms are how a service sends an order update to the people watching
	// that order and nobody else.
	c, address := serverOn(t)

	inside := dial(t, address, "")
	outside := dial(t, address, "")
	waitForClients(t, c, 2)

	join(t, inside, "order-1")
	waitForRoom(t, c, "order-1", 1)

	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "order-1",
		Payload:   map[string]interface{}{"status": "shipped"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	message, ok := nextMessage(t, inside)
	if !ok {
		t.Fatal("the client in the room received nothing")
	}
	if data, _ := message["data"].(map[string]interface{}); data["status"] != "shipped" {
		t.Errorf("received %v", message)
	}

	// The one that matters: somebody who is not in the room must not see it.
	if message, ok := nextMessage(t, outside); ok {
		t.Errorf("a client outside the room received %v", message)
	}

	// And leaving stops it arriving.
	leave(t, inside, "order-1")
	waitForRoom(t, c, "order-1", 0)
	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_room", Target: "order-1",
		Payload: map[string]interface{}{"status": "delivered"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if message, ok := nextMessage(t, inside); ok {
		t.Errorf("a client that left the room received %v", message)
	}
}

func TestAMessageForOnePerson(t *testing.T) {
	// Nothing ever set a connection's user, so this matched no client at all:
	// it delivered to nobody, logged nothing, and answered that it had
	// written one row.
	c, address := serverOn(t)

	alice := dial(t, address, "user_id=alice")
	bob := dial(t, address, "user_id=bob")
	waitForClients(t, c, 2)

	// In the filters, which is one of the two forms the documentation shows.
	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Filters:   map[string]interface{}{"user_id": "alice"},
		Payload:   map[string]interface{}{"status": "shipped"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	message, ok := nextMessage(t, alice)
	if !ok {
		t.Fatal("the message reached nobody")
	}
	if data, _ := message["data"].(map[string]interface{}); data["status"] != "shipped" {
		t.Errorf("alice received %v", message)
	}
	if message, ok := nextMessage(t, bob); ok {
		t.Errorf("somebody else's message reached bob: %v", message)
	}

	// And in the payload, which is the other form, and the one the
	// documentation's own example uses. A fresh connection, because a read
	// that times out leaves a gorilla socket unusable — which is what the
	// check above did to bob's.
	carol := dial(t, address, "user_id=carol")
	waitForClients(t, c, 3)

	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Payload:   map[string]interface{}{"user_id": "carol", "status": "delayed"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, ok := nextMessage(t, carol); !ok {
		t.Error("a user named in the payload received nothing")
	}
}

func TestSendingToSomebodyWhoIsNotNamed(t *testing.T) {
	c, _ := serverOn(t)

	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Payload:   map[string]interface{}{"status": "shipped"},
	}); err == nil {
		t.Error("a message for one person was sent with nobody named")
	}

	if _, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Payload:   map[string]interface{}{"status": "shipped"},
	}); err == nil {
		t.Error("a message for a room was sent with no room named")
	}
}

func TestAMessageFromAClientReachesItsFlow(t *testing.T) {
	c, address := serverOn(t)

	received := make(chan map[string]interface{}, 4)
	c.RegisterRoute("message", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		received <- input
		return map[string]interface{}{"ack": true}, nil
	})

	client := dial(t, address, "user_id=alice&tenant=acme")
	waitForClients(t, c, 1)

	send(t, client, Message{Type: "message", Data: map[string]interface{}{"order_id": "order-1"}})

	select {
	case input := <-received:
		if input["order_id"] != "order-1" {
			t.Errorf("the flow received %v", input)
		}
		// Which connection it came from: a flow answering one customer needs
		// to know which one asked.
		if input["user_id"] != "alice" {
			t.Errorf("the flow was not told who sent it: %v", input)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the message never reached the flow")
	}

	// What the flow returns goes back down the same socket.
	if message, ok := nextMessage(t, client); !ok {
		t.Error("the client got no answer")
	} else if data, _ := message["data"].(map[string]interface{}); data["ack"] != true {
		t.Errorf("the answer was %v", message)
	}
}

func TestSomethingThatIsNotAMessage(t *testing.T) {
	// A client sending rubbish must be told, and must not take the socket or
	// the server down with it.
	c, address := serverOn(t)
	client := dial(t, address, "")
	waitForClients(t, c, 1)

	if err := client.WriteMessage(gorilla.TextMessage, []byte("this is not JSON")); err != nil {
		t.Fatalf("write: %v", err)
	}

	message, ok := nextMessage(t, client)
	if !ok {
		t.Fatal("the client was told nothing")
	}
	if message["type"] != "error" {
		t.Errorf("the client received %v", message)
	}

	// The connection still works afterwards.
	if err := client.WriteMessage(gorilla.TextMessage, []byte(`{"type":"join_room","room":"orders"}`)); err != nil {
		t.Errorf("the socket was closed by a bad message: %v", err)
	}
	waitForRoom(t, c, "orders", 1)
}

func TestWhenAClientGoesAway(t *testing.T) {
	// A socket that closes has to be forgotten, or a service accumulates
	// dead clients and broadcasts to them for ever.
	c, address := serverOn(t)

	client := dial(t, address, "")
	waitForClients(t, c, 1)

	disconnected := make(chan map[string]interface{}, 1)
	c.RegisterRoute("disconnect", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		disconnected <- input
		return nil, nil
	})

	_ = client.Close()

	waitForClients(t, c, 0)
	select {
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Error("nothing was told that the client had gone")
	}
}

func TestTheConnectorDescribesItself(t *testing.T) {
	c := New("ws", &Config{Host: "127.0.0.1", Port: 0, Path: "/ws"}, slog.New(slog.NewTextHandler(quiet{}, nil)))

	if c.Name() != "ws" || c.Type() != "websocket" {
		t.Errorf("name/type = %s/%s", c.Name(), c.Type())
	}
	kind, name := c.SourceInfo()
	if kind != "websocket" || name != "ws" {
		t.Errorf("source info = %s/%s", kind, name)
	}
	// A server that has not started is not healthy, and saying so is what
	// stops a load balancer sending it traffic.
	if err := c.Health(context.Background()); err == nil {
		t.Error("a server that never started reported itself healthy")
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if !c.InboundOnly() {
		t.Error("a websocket server said it was not a source")
	}

	c.SetDebugMode(true)
	c.AllowOne()
	c.SetDebugMode(false)
}

func TestStartingTwice(t *testing.T) {
	c, _ := serverOn(t)
	if err := c.Start(context.Background()); err == nil {
		t.Error("a server that was already listening started again")
	} else if !strings.Contains(err.Error(), "already") {
		t.Errorf("error = %v", err)
	}
}

func join(t *testing.T, conn *gorilla.Conn, room string) {
	t.Helper()
	send(t, conn, Message{Type: "join_room", Room: room})
}

func leave(t *testing.T, conn *gorilla.Conn, room string) {
	t.Helper()
	send(t, conn, Message{Type: "leave_room", Room: room})
}

func send(t *testing.T, conn *gorilla.Conn, message Message) {
	t.Helper()
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(gorilla.TextMessage, body); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func waitForClients(t *testing.T, c *Connector, want int) {
	t.Helper()
	waitUntil(t, func() bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return len(c.clients) == want
	}, fmt.Sprintf("the server never saw %d clients", want))
}

func waitForRoom(t *testing.T, c *Connector, room string, want int) {
	t.Helper()
	waitUntil(t, func() bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return len(c.rooms[room]) == want
	}, fmt.Sprintf("the room %s never held %d clients", room, want))
}

func waitUntil(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
