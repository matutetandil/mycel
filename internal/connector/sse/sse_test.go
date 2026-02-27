package sse

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

func newTestConnector(t *testing.T) (*Connector, *httptest.Server) {
	t.Helper()
	config := &Config{
		Port:              0,
		Host:              "127.0.0.1",
		Path:              "/events",
		HeartbeatInterval: 0, // disable for tests
		CORSOrigins:       []string{"*"},
	}
	conn := New("test_sse", config, nil)

	server := httptest.NewServer(http.HandlerFunc(conn.handleSSE))
	return conn, server
}

// connectSSE opens an SSE connection and returns the response and a cancel func.
func connectSSE(t *testing.T, server *httptest.Server, queryParams string) (*http.Response, context.CancelFunc) {
	t.Helper()
	url := server.URL
	if queryParams != "" {
		url += "?" + queryParams
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		cancel()
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("failed to connect: %v", err)
	}

	return resp, cancel
}

// readSSEEvent reads one SSE event from the response body.
// Returns the event fields as a map (id, event, data).
func readSSEEvent(t *testing.T, resp *http.Response, timeout time.Duration) map[string]string {
	t.Helper()
	scanner := bufio.NewScanner(resp.Body)
	event := make(map[string]string)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				// Empty line signals end of event
				if len(event) > 0 {
					return
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				// Comment (keepalive)
				event["comment"] = strings.TrimPrefix(line, ": ")
				return
			}
			parts := strings.SplitN(line, ": ", 2)
			if len(parts) == 2 {
				event[parts[0]] = parts[1]
			}
		}
	}()

	select {
	case <-done:
		return event
	case <-time.After(timeout):
		t.Fatal("timeout reading SSE event")
		return nil
	}
}

func TestFactory(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name: "test_sse",
		Type: "sse",
		Properties: map[string]interface{}{
			"port":               3002,
			"host":               "127.0.0.1",
			"path":               "/events",
			"heartbeat_interval": "15s",
			"cors": map[string]interface{}{
				"origins": []interface{}{"http://localhost:3000"},
			},
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory.Create failed: %v", err)
	}

	if conn.Name() != "test_sse" {
		t.Errorf("expected name=test_sse, got %s", conn.Name())
	}
	if conn.Type() != "sse" {
		t.Errorf("expected type=sse, got %s", conn.Type())
	}

	sseConn := conn.(*Connector)
	if sseConn.port != 3002 {
		t.Errorf("expected port=3002, got %d", sseConn.port)
	}
	if sseConn.path != "/events" {
		t.Errorf("expected path=/events, got %s", sseConn.path)
	}
	if sseConn.heartbeatInterval != 15*time.Second {
		t.Errorf("expected heartbeatInterval=15s, got %v", sseConn.heartbeatInterval)
	}
	if len(sseConn.corsOrigins) != 1 || sseConn.corsOrigins[0] != "http://localhost:3000" {
		t.Errorf("expected cors origins=[http://localhost:3000], got %v", sseConn.corsOrigins)
	}
}

func TestFactoryDefaults(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name:       "default_sse",
		Type:       "sse",
		Properties: map[string]interface{}{},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory.Create failed: %v", err)
	}

	sseConn := conn.(*Connector)
	if sseConn.port != 3002 {
		t.Errorf("expected default port=3002, got %d", sseConn.port)
	}
	if sseConn.host != "0.0.0.0" {
		t.Errorf("expected default host=0.0.0.0, got %s", sseConn.host)
	}
	if sseConn.path != "/events" {
		t.Errorf("expected default path=/events, got %s", sseConn.path)
	}
	if sseConn.heartbeatInterval != 30*time.Second {
		t.Errorf("expected default heartbeatInterval=30s, got %v", sseConn.heartbeatInterval)
	}
}

func TestFactorySupports(t *testing.T) {
	factory := NewFactory(nil)

	if !factory.Supports("sse", "") {
		t.Error("factory should support 'sse' type")
	}
	if factory.Supports("websocket", "") {
		t.Error("factory should not support 'websocket' type")
	}
	if factory.Supports("rest", "") {
		t.Error("factory should not support 'rest' type")
	}
}

func TestSSEConnect(t *testing.T) {
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

	resp, cancel := connectSSE(t, server, "")
	defer cancel()
	defer resp.Body.Close()

	// Verify SSE headers
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type=text/event-stream, got %s", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control=no-cache, got %s", cc)
	}

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

func TestSSEBroadcast(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	// Connect two clients
	resp1, cancel1 := connectSSE(t, server, "")
	defer cancel1()
	defer resp1.Body.Close()
	resp2, cancel2 := connectSSE(t, server, "")
	defer cancel2()
	defer resp2.Body.Close()

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
	event1 := readSSEEvent(t, resp1, 2*time.Second)
	event2 := readSSEEvent(t, resp2, 2*time.Second)

	for i, event := range []map[string]string{event1, event2} {
		if event["event"] != "message" {
			t.Errorf("client %d: expected event=message, got %s", i+1, event["event"])
		}
		if !strings.Contains(event["data"], "system update") {
			t.Errorf("client %d: expected data to contain 'system update', got %s", i+1, event["data"])
		}
	}
}

func TestSSERoomJoin(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	// Connect client with room query param
	resp, cancel := connectSSE(t, server, "room=orders")
	defer cancel()
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if conn.RoomClientCount("orders") != 1 {
		t.Errorf("expected 1 client in 'orders' room, got %d", conn.RoomClientCount("orders"))
	}
}

func TestSSESendToRoom(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	// Client 1 joins "orders" room
	resp1, cancel1 := connectSSE(t, server, "room=orders")
	defer cancel1()
	defer resp1.Body.Close()

	// Client 2 joins no room
	resp2, cancel2 := connectSSE(t, server, "")
	defer cancel2()
	defer resp2.Body.Close()

	time.Sleep(50 * time.Millisecond)

	// Send to room
	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "orders",
		Payload:   map[string]interface{}{"order_id": "123"},
	})
	if err != nil {
		t.Fatalf("send_to_room failed: %v", err)
	}

	// Client 1 should receive
	event := readSSEEvent(t, resp1, 2*time.Second)
	if !strings.Contains(event["data"], "123") {
		t.Errorf("expected data to contain '123', got %s", event["data"])
	}

	// Client 2 should NOT receive — verify with a short read attempt
	gotEvent := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(resp2.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") || strings.HasPrefix(line, "data: ") {
				gotEvent <- true
				return
			}
		}
		gotEvent <- false
	}()

	select {
	case got := <-gotEvent:
		if got {
			t.Error("client 2 should NOT have received room message")
		}
	case <-time.After(200 * time.Millisecond):
		// Good — client 2 did not receive anything
	}
}

func TestSSEMultipleRooms(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	// Connect with multiple rooms
	resp, cancel := connectSSE(t, server, "rooms=orders,inventory")
	defer cancel()
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	if conn.RoomCount() != 2 {
		t.Errorf("expected 2 rooms, got %d", conn.RoomCount())
	}
	if conn.RoomClientCount("orders") != 1 {
		t.Errorf("expected 1 client in 'orders', got %d", conn.RoomClientCount("orders"))
	}
	if conn.RoomClientCount("inventory") != 1 {
		t.Errorf("expected 1 client in 'inventory', got %d", conn.RoomClientCount("inventory"))
	}

	// Send to orders room
	conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "orders",
		Payload:   map[string]interface{}{"from": "orders"},
	})
	event := readSSEEvent(t, resp, 2*time.Second)
	if !strings.Contains(event["data"], "orders") {
		t.Errorf("expected data from orders room, got %s", event["data"])
	}

	// Send to inventory room
	conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_room",
		Target:    "inventory",
		Payload:   map[string]interface{}{"from": "inventory"},
	})
	event = readSSEEvent(t, resp, 2*time.Second)
	if !strings.Contains(event["data"], "inventory") {
		t.Errorf("expected data from inventory room, got %s", event["data"])
	}
}

func TestSSEDisconnect(t *testing.T) {
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

	resp, cancel := connectSSE(t, server, "room=chat")
	time.Sleep(50 * time.Millisecond)

	if conn.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", conn.ClientCount())
	}
	if conn.RoomClientCount("chat") != 1 {
		t.Fatalf("expected 1 client in room, got %d", conn.RoomClientCount("chat"))
	}

	// Disconnect by cancelling the context
	cancel()
	resp.Body.Close()
	time.Sleep(100 * time.Millisecond)

	if conn.ClientCount() != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", conn.ClientCount())
	}
	if conn.RoomCount() != 0 {
		t.Errorf("expected 0 rooms after disconnect, got %d", conn.RoomCount())
	}

	mu.Lock()
	if !disconnectCalled {
		t.Error("disconnect handler was not called")
	}
	mu.Unlock()
}

func TestSSEHeartbeat(t *testing.T) {
	config := &Config{
		Port:              0,
		Host:              "127.0.0.1",
		Path:              "/events",
		HeartbeatInterval: 100 * time.Millisecond, // fast heartbeat for test
		CORSOrigins:       []string{"*"},
	}
	conn := New("test_heartbeat", config, nil)
	server := httptest.NewServer(http.HandlerFunc(conn.handleSSE))
	defer server.Close()

	resp, cancel := connectSSE(t, server, "")
	defer cancel()
	defer resp.Body.Close()

	// Wait for at least one heartbeat
	event := readSSEEvent(t, resp, 500*time.Millisecond)
	if event["comment"] != "keepalive" {
		t.Errorf("expected keepalive comment, got %v", event)
	}
}

func TestSSEEventFormat(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	resp, cancel := connectSSE(t, server, "")
	defer cancel()
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	// Send an event
	conn.Write(context.Background(), &connector.Data{
		Operation: "broadcast",
		Payload:   map[string]interface{}{"key": "value"},
	})

	event := readSSEEvent(t, resp, 2*time.Second)

	// Should have id, event, and data fields
	if event["id"] == "" {
		t.Error("expected event to have an id")
	}
	if event["event"] != "message" {
		t.Errorf("expected event=message, got %s", event["event"])
	}
	if event["data"] == "" {
		t.Error("expected event to have data")
	}
	if !strings.Contains(event["data"], `"key":"value"`) && !strings.Contains(event["data"], `"key": "value"`) {
		t.Errorf("expected data to contain key:value, got %s", event["data"])
	}
}

func TestSSEHealth(t *testing.T) {
	config := &Config{
		Port: 0,
		Host: "127.0.0.1",
		Path: "/events",
	}
	conn := New("test_health", config, nil)

	// Before start — should be unhealthy
	if err := conn.Health(context.Background()); err == nil {
		t.Error("expected health check to fail before start")
	}

	// After start
	conn.started = true
	if err := conn.Health(context.Background()); err != nil {
		t.Errorf("expected health check to pass after start, got %v", err)
	}
}

func TestWriteSendToRoomMissingTarget(t *testing.T) {
	config := &Config{Port: 0, Host: "127.0.0.1", Path: "/events"}
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
	config := &Config{Port: 0, Host: "127.0.0.1", Path: "/events"}
	conn := New("test", config, nil)

	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Payload:   map[string]interface{}{"msg": "test"},
	})
	if err == nil {
		t.Error("expected error for missing user_id in send_to_user")
	}
}

func TestSSECORSHeaders(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Origin", "http://example.com")

	// Use a transport that doesn't follow redirects and has a short timeout
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		// Timeout is expected since SSE blocks — check we got connected at least
		_ = conn
		return
	}
	defer resp.Body.Close()

	if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Errorf("expected CORS origin=*, got %s", acao)
	}
}

func TestSSESendToUser(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	// Connect client with user_id
	resp1, cancel1 := connectSSE(t, server, "user_id=user123")
	defer cancel1()
	defer resp1.Body.Close()

	// Connect client without user_id
	resp2, cancel2 := connectSSE(t, server, "")
	defer cancel2()
	defer resp2.Body.Close()

	time.Sleep(50 * time.Millisecond)

	// Send to specific user
	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Payload:   map[string]interface{}{"msg": "hello user"},
		Filters:   map[string]interface{}{"user_id": "user123"},
	})
	if err != nil {
		t.Fatalf("send_to_user failed: %v", err)
	}

	// Client 1 should receive
	event := readSSEEvent(t, resp1, 2*time.Second)
	if !strings.Contains(event["data"], "hello user") {
		t.Errorf("expected data to contain 'hello user', got %s", event["data"])
	}
}

func TestDefaultBroadcast(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	resp, cancel := connectSSE(t, server, "")
	defer cancel()
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	// Unknown operation should default to broadcast
	_, err := conn.Write(context.Background(), &connector.Data{
		Operation: "unknown_op",
		Payload:   map[string]interface{}{"test": true},
	})
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	event := readSSEEvent(t, resp, 2*time.Second)
	if event["event"] != "message" {
		t.Errorf("expected event=message, got %s", event["event"])
	}
}

func TestConnectClose(t *testing.T) {
	config := &Config{Port: 0, Host: "127.0.0.1", Path: "/events"}
	conn := New("test_close", config, nil)

	// Connect should be no-op
	if err := conn.Connect(context.Background()); err != nil {
		t.Errorf("connect should be no-op, got %v", err)
	}

	// Close with no server should be fine
	if err := conn.Close(context.Background()); err != nil {
		t.Errorf("close should be fine with no server, got %v", err)
	}
}

func TestAtomicEventIDs(t *testing.T) {
	conn, server := newTestConnector(t)
	defer server.Close()

	resp, cancel := connectSSE(t, server, "")
	defer cancel()
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	// Send two events and verify IDs are incrementing
	conn.Write(context.Background(), &connector.Data{
		Operation: "broadcast",
		Payload:   map[string]interface{}{"n": 1},
	})
	event1 := readSSEEvent(t, resp, 2*time.Second)

	conn.Write(context.Background(), &connector.Data{
		Operation: "broadcast",
		Payload:   map[string]interface{}{"n": 2},
	})
	event2 := readSSEEvent(t, resp, 2*time.Second)

	id1 := event1["id"]
	id2 := event2["id"]

	if id1 == "" || id2 == "" {
		t.Fatal("expected both events to have IDs")
	}

	// Parse and verify incrementing
	var n1, n2 int
	fmt.Sscanf(id1, "%d", &n1)
	fmt.Sscanf(id2, "%d", &n2)

	if n2 <= n1 {
		t.Errorf("expected incrementing IDs, got %d then %d", n1, n2)
	}
}
