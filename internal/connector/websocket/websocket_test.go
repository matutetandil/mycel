package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matutetandil/mycel/internal/connector"
)

func newTestConnector(t *testing.T) (*Connector, *httptest.Server) {
	t.Helper()
	config := &Config{
		Port:         0,
		Host:         "127.0.0.1",
		Path:         "/ws",
		PingInterval: 0, // disable for tests
		PongTimeout:  0,
	}
	conn := New("test_ws", config, nil)

	// Use httptest server instead of real server
	server := httptest.NewServer(http.HandlerFunc(conn.handleWebSocket))
	return conn, server
}

func dialWS(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	return ws
}

func readMsg(t *testing.T, ws *websocket.Conn) map[string]interface{} {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}
	return msg
}

func sendMsg(t *testing.T, ws *websocket.Conn, msg interface{}) {
	t.Helper()
	if err := ws.WriteJSON(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}
}

func TestWebSocketConnect(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	var connectCalled bool
	var mu sync.Mutex
	conn.RegisterRoute("connect", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		connectCalled = true
		mu.Unlock()
		return nil, nil
	})

	ws := dialWS(t, server)
	defer ws.Close()

	// Give handler time to fire
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !connectCalled {
		t.Error("connect handler was not called")
	}
	mu.Unlock()

	if conn.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", conn.ClientCount())
	}
}

func TestWebSocketMessage(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	received := make(chan map[string]interface{}, 1)
	conn.RegisterRoute("message", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		received <- input
		return map[string]interface{}{"status": "ok"}, nil
	})

	ws := dialWS(t, server)
	defer ws.Close()
	time.Sleep(50 * time.Millisecond)

	// Send a message
	sendMsg(t, ws, Message{
		Type: "message",
		Data: map[string]interface{}{"text": "hello"},
	})

	select {
	case msg := <-received:
		if msg["text"] != "hello" {
			t.Errorf("expected text=hello, got %v", msg["text"])
		}
		if msg["event"] != "message" {
			t.Errorf("expected event=message, got %v", msg["event"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	// Read the response sent back to client
	resp := readMsg(t, ws)
	if resp["type"] != "message" {
		t.Errorf("expected response type=message, got %v", resp["type"])
	}
	data := resp["data"].(map[string]interface{})
	if data["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", data["status"])
	}
}

func TestWebSocketBroadcast(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	// Connect two clients
	ws1 := dialWS(t, server)
	defer ws1.Close()
	ws2 := dialWS(t, server)
	defer ws2.Close()

	time.Sleep(50 * time.Millisecond)

	if conn.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", conn.ClientCount())
	}

	// Broadcast via Write
	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "broadcast",
		Payload:   map[string]interface{}{"alert": "system update"},
	})
	if err != nil {
		t.Fatalf("broadcast failed: %v", err)
	}

	// Both clients should receive
	msg1 := readMsg(t, ws1)
	msg2 := readMsg(t, ws2)

	for i, msg := range []map[string]interface{}{msg1, msg2} {
		data, ok := msg["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("client %d: expected data map, got %T", i+1, msg["data"])
		}
		if data["alert"] != "system update" {
			t.Errorf("client %d: expected alert='system update', got %v", i+1, data["alert"])
		}
	}
}

func TestWebSocketRooms(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	// Connect two clients
	ws1 := dialWS(t, server)
	defer ws1.Close()
	ws2 := dialWS(t, server)
	defer ws2.Close()

	time.Sleep(50 * time.Millisecond)

	// Client 1 joins "orders" room
	sendMsg(t, ws1, Message{Type: "join_room", Room: "orders"})
	time.Sleep(50 * time.Millisecond)

	if conn.RoomClientCount("orders") != 1 {
		t.Errorf("expected 1 client in room, got %d", conn.RoomClientCount("orders"))
	}

	// Send to room via Write
	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "orders",
		Payload:   map[string]interface{}{"order_id": "123"},
	})
	if err != nil {
		t.Fatalf("send_to_room failed: %v", err)
	}

	// Client 1 should receive
	msg := readMsg(t, ws1)
	data := msg["data"].(map[string]interface{})
	if data["order_id"] != "123" {
		t.Errorf("expected order_id=123, got %v", data["order_id"])
	}

	// Client 2 should NOT receive (timeout expected)
	ws2.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _, err = ws2.ReadMessage()
	if err == nil {
		t.Error("client 2 should NOT have received room message")
	}

	// Leave room
	sendMsg(t, ws1, Message{Type: "leave_room", Room: "orders"})
	time.Sleep(50 * time.Millisecond)

	if conn.RoomClientCount("orders") != 0 {
		t.Errorf("expected 0 clients in room after leave, got %d", conn.RoomClientCount("orders"))
	}
}

func TestWebSocketDisconnect(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	var disconnectCalled bool
	var mu sync.Mutex
	conn.RegisterRoute("disconnect", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		disconnectCalled = true
		mu.Unlock()
		return nil, nil
	})

	ws := dialWS(t, server)
	time.Sleep(50 * time.Millisecond)

	if conn.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", conn.ClientCount())
	}

	// Close the client
	ws.Close()
	time.Sleep(100 * time.Millisecond)

	if conn.ClientCount() != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", conn.ClientCount())
	}

	mu.Lock()
	if !disconnectCalled {
		t.Error("disconnect handler was not called")
	}
	mu.Unlock()
}

func TestWebSocketDisconnectCleansRooms(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	ws := dialWS(t, server)
	time.Sleep(50 * time.Millisecond)

	// Join a room
	sendMsg(t, ws, Message{Type: "join_room", Room: "chat"})
	time.Sleep(50 * time.Millisecond)

	if conn.RoomClientCount("chat") != 1 {
		t.Fatalf("expected 1 client in room, got %d", conn.RoomClientCount("chat"))
	}

	// Disconnect
	ws.Close()
	time.Sleep(100 * time.Millisecond)

	// Room should be cleaned up
	if conn.RoomCount() != 0 {
		t.Errorf("expected 0 rooms after disconnect, got %d", conn.RoomCount())
	}
}

func TestFactory(t *testing.T) {
	factory := NewFactory(nil)

	if !factory.Supports("websocket", "") {
		t.Error("factory should support 'websocket' type")
	}
	if factory.Supports("rest", "") {
		t.Error("factory should not support 'rest' type")
	}

	cfg := &connector.Config{
		Name: "test_ws",
		Type: "websocket",
		Properties: map[string]interface{}{
			"port":          3001,
			"host":          "127.0.0.1",
			"path":          "/ws",
			"ping_interval": "30s",
			"pong_timeout":  "10s",
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory.Create failed: %v", err)
	}

	if conn.Name() != "test_ws" {
		t.Errorf("expected name=test_ws, got %s", conn.Name())
	}
	if conn.Type() != "websocket" {
		t.Errorf("expected type=websocket, got %s", conn.Type())
	}

	wsConn := conn.(*Connector)
	if wsConn.port != 3001 {
		t.Errorf("expected port=3001, got %d", wsConn.port)
	}
	if wsConn.path != "/ws" {
		t.Errorf("expected path=/ws, got %s", wsConn.path)
	}
	if wsConn.pingInterval != 30*time.Second {
		t.Errorf("expected pingInterval=30s, got %v", wsConn.pingInterval)
	}
	if wsConn.pongTimeout != 10*time.Second {
		t.Errorf("expected pongTimeout=10s, got %v", wsConn.pongTimeout)
	}
}

func TestFactoryDefaults(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name:       "default_ws",
		Type:       "websocket",
		Properties: map[string]interface{}{},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory.Create failed: %v", err)
	}

	wsConn := conn.(*Connector)
	if wsConn.port != 3001 {
		t.Errorf("expected default port=3001, got %d", wsConn.port)
	}
	if wsConn.host != "0.0.0.0" {
		t.Errorf("expected default host=0.0.0.0, got %s", wsConn.host)
	}
	if wsConn.path != "/ws" {
		t.Errorf("expected default path=/ws, got %s", wsConn.path)
	}
}

func TestWriteSendToRoomMissingTarget(t *testing.T) {
	config := &Config{Port: 0, Host: "127.0.0.1", Path: "/ws"}
	conn := New("test", config, nil)

	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "",
		Payload:   map[string]interface{}{"msg": "test"},
	})
	if err == nil {
		t.Error("expected error for missing target in send_to_room")
	}
}

func TestWriteSendToUserMissingFilter(t *testing.T) {
	config := &Config{Port: 0, Host: "127.0.0.1", Path: "/ws"}
	conn := New("test", config, nil)

	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Payload:   map[string]interface{}{"msg": "test"},
	})
	if err == nil {
		t.Error("expected error for missing user_id in send_to_user")
	}
}

func TestWebSocketMultipleRooms(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	ws := dialWS(t, server)
	defer ws.Close()
	time.Sleep(50 * time.Millisecond)

	// Join two rooms
	sendMsg(t, ws, Message{Type: "join_room", Room: "room1"})
	sendMsg(t, ws, Message{Type: "join_room", Room: "room2"})
	time.Sleep(50 * time.Millisecond)

	if conn.RoomCount() != 2 {
		t.Errorf("expected 2 rooms, got %d", conn.RoomCount())
	}

	// Send to room1
	conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "room1",
		Payload:   map[string]interface{}{"from": "room1"},
	})
	msg := readMsg(t, ws)
	data := msg["data"].(map[string]interface{})
	if data["from"] != "room1" {
		t.Errorf("expected from=room1, got %v", data["from"])
	}

	// Send to room2
	conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "room2",
		Payload:   map[string]interface{}{"from": "room2"},
	})
	msg = readMsg(t, ws)
	data = msg["data"].(map[string]interface{})
	if data["from"] != "room2" {
		t.Errorf("expected from=room2, got %v", data["from"])
	}
}

func TestWebSocketErrorMessage(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	conn.RegisterRoute("message", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("something went wrong")
	})

	ws := dialWS(t, server)
	defer ws.Close()
	time.Sleep(50 * time.Millisecond)

	sendMsg(t, ws, Message{Type: "message", Data: map[string]interface{}{"test": true}})

	msg := readMsg(t, ws)
	if msg["type"] != "error" {
		t.Errorf("expected type=error, got %v", msg["type"])
	}
	if msg["message"] != "something went wrong" {
		t.Errorf("expected error message, got %v", msg["message"])
	}
}
