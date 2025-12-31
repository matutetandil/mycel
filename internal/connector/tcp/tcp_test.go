package tcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// TestCodecs tests the JSON and Raw codecs.
func TestCodecs(t *testing.T) {
	tests := []struct {
		name  string
		codec Codec
	}{
		{"json", &JSONCodec{}},
		{"raw", &RawCodec{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.codec.Name() != tt.name {
				t.Errorf("codec.Name() = %v, want %v", tt.codec.Name(), tt.name)
			}
		})
	}
}

func TestJSONCodec(t *testing.T) {
	codec := &JSONCodec{}

	t.Run("encode/decode message", func(t *testing.T) {
		msg := &Message{
			Type: "test",
			ID:   "123",
			Data: map[string]interface{}{"key": "value"},
		}

		data, err := codec.Encode(msg)
		if err != nil {
			t.Fatalf("encode error: %v", err)
		}

		var decoded Message
		if err := codec.Decode(data, &decoded); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if decoded.Type != msg.Type {
			t.Errorf("Type = %v, want %v", decoded.Type, msg.Type)
		}
		if decoded.ID != msg.ID {
			t.Errorf("ID = %v, want %v", decoded.ID, msg.ID)
		}
	})
}

func TestRawCodec(t *testing.T) {
	codec := &RawCodec{}

	t.Run("encode/decode bytes", func(t *testing.T) {
		input := []byte("hello world")

		data, err := codec.Encode(input)
		if err != nil {
			t.Fatalf("encode error: %v", err)
		}

		var decoded []byte
		if err := codec.Decode(data, &decoded); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if !bytes.Equal(decoded, input) {
			t.Errorf("decoded = %v, want %v", decoded, input)
		}
	})

	t.Run("encode/decode string", func(t *testing.T) {
		input := "hello world"

		data, err := codec.Encode(input)
		if err != nil {
			t.Fatalf("encode error: %v", err)
		}

		var decoded string
		if err := codec.Decode(data, &decoded); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if decoded != input {
			t.Errorf("decoded = %v, want %v", decoded, input)
		}
	})
}

func TestFramer(t *testing.T) {
	// Create a pair of connected sockets
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	codec := &JSONCodec{}
	serverFramer := NewFramer(server, codec)
	clientFramer := NewFramer(client, codec)

	t.Run("write and read message", func(t *testing.T) {
		msg := &Message{
			Type: "test",
			ID:   "123",
			Data: map[string]interface{}{"name": "John"},
		}

		// Write from client
		go func() {
			if err := clientFramer.WriteMessage(msg); err != nil {
				t.Errorf("write error: %v", err)
			}
		}()

		// Read from server
		var received Message
		if err := serverFramer.ReadMessage(&received); err != nil {
			t.Fatalf("read error: %v", err)
		}

		if received.Type != msg.Type {
			t.Errorf("Type = %v, want %v", received.Type, msg.Type)
		}
		if received.ID != msg.ID {
			t.Errorf("ID = %v, want %v", received.ID, msg.ID)
		}
	})

	t.Run("write and read raw", func(t *testing.T) {
		data := []byte("raw test data")

		// Write from client
		go func() {
			if err := clientFramer.WriteRaw(data); err != nil {
				t.Errorf("write error: %v", err)
			}
		}()

		// Read from server
		received, err := serverFramer.ReadRaw()
		if err != nil {
			t.Fatalf("read error: %v", err)
		}

		if !bytes.Equal(received, data) {
			t.Errorf("received = %v, want %v", received, data)
		}
	})
}

func TestMessage(t *testing.T) {
	t.Run("NewMessage", func(t *testing.T) {
		msg := NewMessage("test_type", map[string]interface{}{"key": "value"})

		if msg.Type != "test_type" {
			t.Errorf("Type = %v, want test_type", msg.Type)
		}
		if msg.ID == "" {
			t.Error("ID should not be empty")
		}
		if msg.Data["key"] != "value" {
			t.Errorf("Data[key] = %v, want value", msg.Data["key"])
		}
		if msg.Timestamp == 0 {
			t.Error("Timestamp should not be 0")
		}
	})

	t.Run("NewResponse", func(t *testing.T) {
		response := NewResponse("req-123", map[string]interface{}{"result": "ok"})

		if response.Type != "response" {
			t.Errorf("Type = %v, want response", response.Type)
		}
		if response.ID != "req-123" {
			t.Errorf("ID = %v, want req-123", response.ID)
		}
	})

	t.Run("NewErrorResponse", func(t *testing.T) {
		response := NewErrorResponse("req-123", "something went wrong")

		if response.Type != "error" {
			t.Errorf("Type = %v, want error", response.Type)
		}
		if !response.IsError() {
			t.Error("IsError() should return true")
		}
	})
}

func TestServerConnector(t *testing.T) {
	ctx := context.Background()

	t.Run("create and start server", func(t *testing.T) {
		server, err := NewServer("test", "127.0.0.1", 0, "json")
		if err != nil {
			t.Fatalf("NewServer error: %v", err)
		}

		if server.Name() != "test" {
			t.Errorf("Name() = %v, want test", server.Name())
		}
		if server.Type() != "tcp" {
			t.Errorf("Type() = %v, want tcp", server.Type())
		}

		// Use port 0 for automatic port assignment
		// Start will fail because port is 0, let's use a real port
	})

	t.Run("server handles connections", func(t *testing.T) {
		// Find a free port
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()

		server, err := NewServer("test", "127.0.0.1", port, "json",
			WithMaxConnections(10),
			WithServerTimeouts(5*time.Second, 5*time.Second),
		)
		if err != nil {
			t.Fatalf("NewServer error: %v", err)
		}

		// Register a handler
		var handlerCalled bool
		var handlerMu sync.Mutex
		server.RegisterRoute("echo", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			handlerMu.Lock()
			handlerCalled = true
			handlerMu.Unlock()
			return input, nil
		})

		// Start server
		if err := server.Start(ctx); err != nil {
			t.Fatalf("Start error: %v", err)
		}
		defer server.Close(ctx)

		// Give server time to start
		time.Sleep(50 * time.Millisecond)

		// Connect as client
		conn, err := net.Dial("tcp", server.Address())
		if err != nil {
			t.Fatalf("Dial error: %v", err)
		}
		defer conn.Close()

		// Send message
		msg := &Message{
			Type: "echo",
			ID:   "test-1",
			Data: map[string]interface{}{"message": "hello"},
		}

		// Write length-prefixed JSON
		data, _ := json.Marshal(msg)
		header := make([]byte, 4)
		binary.BigEndian.PutUint32(header, uint32(len(data)))
		conn.Write(header)
		conn.Write(data)

		// Read response
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Read(header); err != nil {
			t.Fatalf("Read header error: %v", err)
		}

		length := binary.BigEndian.Uint32(header)
		respData := make([]byte, length)
		if _, err := conn.Read(respData); err != nil {
			t.Fatalf("Read body error: %v", err)
		}

		var response Message
		if err := json.Unmarshal(respData, &response); err != nil {
			t.Fatalf("Unmarshal error: %v", err)
		}

		handlerMu.Lock()
		if !handlerCalled {
			t.Error("Handler was not called")
		}
		handlerMu.Unlock()

		if response.ID != "test-1" {
			t.Errorf("Response ID = %v, want test-1", response.ID)
		}
	})
}

func TestClientConnector(t *testing.T) {
	ctx := context.Background()

	t.Run("create client", func(t *testing.T) {
		client, err := NewClient("test", "127.0.0.1", 9999, "json")
		if err != nil {
			t.Fatalf("NewClient error: %v", err)
		}

		if client.Name() != "test" {
			t.Errorf("Name() = %v, want test", client.Name())
		}
		if client.Type() != "tcp" {
			t.Errorf("Type() = %v, want tcp", client.Type())
		}
		if client.Address() != "127.0.0.1:9999" {
			t.Errorf("Address() = %v, want 127.0.0.1:9999", client.Address())
		}
	})

	t.Run("client connects and communicates", func(t *testing.T) {
		// Start a test server
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		addr := listener.Addr().(*net.TCPAddr)

		// Handle one connection
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			codec := &JSONCodec{}
			framer := NewFramer(conn, codec)

			// Read request
			var msg Message
			if err := framer.ReadMessage(&msg); err != nil {
				return
			}

			// Send response
			response := NewResponse(msg.ID, map[string]interface{}{
				"echo": msg.Data,
			})
			framer.WriteMessage(response)
		}()

		// Create client
		client, err := NewClient("test", addr.IP.String(), addr.Port, "json",
			WithPoolSize(1),
			WithClientTimeouts(5*time.Second, 5*time.Second, 5*time.Second, time.Minute),
			WithRetry(1, 100*time.Millisecond),
		)
		if err != nil {
			t.Fatalf("NewClient error: %v", err)
		}

		if err := client.Connect(ctx); err != nil {
			t.Fatalf("Connect error: %v", err)
		}
		defer client.Close(ctx)

		// Send write request
		result, err := client.Write(ctx, &connector.Data{
			Target:  "echo",
			Payload: map[string]interface{}{"message": "hello"},
		})
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}

		if result.Affected != 1 {
			t.Errorf("Affected = %v, want 1", result.Affected)
		}
	})
}

func TestFactory(t *testing.T) {
	factory := NewFactory(nil)

	t.Run("supports tcp", func(t *testing.T) {
		if !factory.Supports("tcp", "") {
			t.Error("factory should support tcp")
		}
		if !factory.Supports("tcp", "server") {
			t.Error("factory should support tcp/server")
		}
		if !factory.Supports("tcp", "client") {
			t.Error("factory should support tcp/client")
		}
		if factory.Supports("http", "") {
			t.Error("factory should not support http")
		}
	})

	t.Run("create server", func(t *testing.T) {
		cfg := &connector.Config{
			Name:   "test-server",
			Type:   "tcp",
			Driver: "server",
			Properties: map[string]interface{}{
				"host":     "127.0.0.1",
				"port":     9000,
				"protocol": "json",
			},
		}

		conn, err := factory.Create(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Create error: %v", err)
		}

		server, ok := conn.(*ServerConnector)
		if !ok {
			t.Fatal("expected ServerConnector")
		}

		if server.Name() != "test-server" {
			t.Errorf("Name() = %v, want test-server", server.Name())
		}
	})

	t.Run("create client", func(t *testing.T) {
		cfg := &connector.Config{
			Name:   "test-client",
			Type:   "tcp",
			Driver: "client",
			Properties: map[string]interface{}{
				"host":     "api.example.com",
				"port":     9000,
				"protocol": "json",
			},
		}

		conn, err := factory.Create(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Create error: %v", err)
		}

		client, ok := conn.(*ClientConnector)
		if !ok {
			t.Fatal("expected ClientConnector")
		}

		if client.Name() != "test-client" {
			t.Errorf("Name() = %v, want test-client", client.Name())
		}
		if client.Address() != "api.example.com:9000" {
			t.Errorf("Address() = %v, want api.example.com:9000", client.Address())
		}
	})

	t.Run("client requires host", func(t *testing.T) {
		cfg := &connector.Config{
			Name:       "test",
			Type:       "tcp",
			Driver:     "client",
			Properties: map[string]interface{}{},
		}

		_, err := factory.Create(context.Background(), cfg)
		if err == nil {
			t.Error("expected error for missing host")
		}
	})
}

func TestIntegration_ServerClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create and start server
	server, err := NewServer("test-server", "127.0.0.1", port, "json",
		WithMaxConnections(10),
	)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	// Register echo handler
	server.RegisterRoute("echo", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"echo":      input,
			"processed": true,
		}, nil
	})

	// Register error handler
	server.RegisterRoute("error", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("not found")
	})

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer server.Close(ctx)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create client
	client, err := NewClient("test-client", "127.0.0.1", port, "json",
		WithPoolSize(5),
	)
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close(ctx)

	t.Run("echo request", func(t *testing.T) {
		result, err := client.Write(ctx, &connector.Data{
			Target: "echo",
			Payload: map[string]interface{}{
				"message": "hello world",
				"count":   42,
			},
		})
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}

		if len(result.Rows) == 0 {
			t.Fatal("expected response data")
		}

		data := result.Rows[0]
		if processed, ok := data["processed"].(bool); !ok || !processed {
			t.Errorf("processed = %v, want true", data["processed"])
		}
	})

	t.Run("concurrent requests", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				_, err := client.Write(ctx, &connector.Data{
					Target: "echo",
					Payload: map[string]interface{}{
						"request": n,
					},
				})
				if err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("concurrent request error: %v", err)
		}
	})

	t.Run("fire and forget", func(t *testing.T) {
		result, err := client.Write(ctx, &connector.Data{
			Target:  "echo",
			Payload: map[string]interface{}{"fire": true},
			Params: map[string]interface{}{
				"mode": "fire_and_forget",
			},
		})
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}

		if result.Affected != 1 {
			t.Errorf("Affected = %v, want 1", result.Affected)
		}
	})
}

func TestNewCodec(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"json", "json", false},
		{"", "json", false}, // default
		{"raw", "raw", false},
		{"msgpack", "msgpack", false},
		{"nestjs", "nestjs", false},
		{"unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewCodec(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if codec.Name() != tt.want {
				t.Errorf("codec.Name() = %v, want %v", codec.Name(), tt.want)
			}
		})
	}
}

// ============================================================================
// NestJS Protocol Tests
// ============================================================================

func TestNestJSCodec(t *testing.T) {
	codec := &NestJSCodec{}

	if codec.Name() != "nestjs" {
		t.Errorf("Name() = %v, want nestjs", codec.Name())
	}

	t.Run("encode Mycel message to NestJS", func(t *testing.T) {
		msg := &Message{
			Type: "cache",
			ID:   "req-123",
			Data: map[string]interface{}{"key": "test"},
		}

		data, err := codec.Encode(msg)
		if err != nil {
			t.Fatalf("encode error: %v", err)
		}

		// Verify it's valid JSON with NestJS structure
		var nestMsg NestJSMessage
		if err := json.Unmarshal(data, &nestMsg); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if nestMsg.Pattern != "cache" {
			t.Errorf("Pattern = %v, want cache", nestMsg.Pattern)
		}
		if nestMsg.ID != "req-123" {
			t.Errorf("ID = %v, want req-123", nestMsg.ID)
		}
	})

	t.Run("decode NestJS message to Mycel", func(t *testing.T) {
		nestData := `{"pattern":"cache","data":{"key":"value"},"id":"req-456"}`

		var msg Message
		if err := codec.Decode([]byte(nestData), &msg); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if msg.Type != "cache" {
			t.Errorf("Type = %v, want cache", msg.Type)
		}
		if msg.ID != "req-456" {
			t.Errorf("ID = %v, want req-456", msg.ID)
		}
	})

	t.Run("decode NestJS response", func(t *testing.T) {
		nestData := `{"id":"req-789","response":{"value":"cached"},"isDisposed":true}`

		var msg Message
		if err := codec.Decode([]byte(nestData), &msg); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if msg.ID != "req-789" {
			t.Errorf("ID = %v, want req-789", msg.ID)
		}
		// Response data should be in Data field
		if msg.Data["value"] != "cached" {
			t.Errorf("Data[value] = %v, want cached", msg.Data["value"])
		}
	})

	t.Run("decode NestJS error response", func(t *testing.T) {
		nestData := `{"id":"req-err","err":"key not found","isDisposed":true}`

		var msg Message
		if err := codec.Decode([]byte(nestData), &msg); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		if msg.Error != "key not found" {
			t.Errorf("Error = %v, want key not found", msg.Error)
		}
	})

	t.Run("handle cmd pattern object", func(t *testing.T) {
		nestData := `{"pattern":{"cmd":"sum"},"data":{"numbers":[1,2,3]},"id":"req-cmd"}`

		var msg Message
		if err := codec.Decode([]byte(nestData), &msg); err != nil {
			t.Fatalf("decode error: %v", err)
		}

		// Pattern {cmd: "sum"} should be converted to "sum"
		if msg.Type != "sum" {
			t.Errorf("Type = %v, want sum", msg.Type)
		}
	})
}

func TestNestJSFramer(t *testing.T) {
	// Create a pair of connected sockets
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverFramer := NewNestJSFramer(server)
	clientFramer := NewNestJSFramer(client)

	t.Run("write and read NestJS message", func(t *testing.T) {
		msg := &NestJSMessage{
			Pattern: "cache",
			Data:    map[string]interface{}{"key": "test"},
			ID:      "req-123",
		}

		// Write from client
		go func() {
			if err := clientFramer.WriteMessage(msg); err != nil {
				t.Errorf("write error: %v", err)
			}
		}()

		// Read from server
		received, err := serverFramer.ReadMessage()
		if err != nil {
			t.Fatalf("read error: %v", err)
		}

		if received.Pattern != "cache" {
			t.Errorf("Pattern = %v, want cache", received.Pattern)
		}
		if received.ID != "req-123" {
			t.Errorf("ID = %v, want req-123", received.ID)
		}
	})
}

func TestNestJSMessageConversion(t *testing.T) {
	t.Run("FromMycelMessage", func(t *testing.T) {
		mycelMsg := &Message{
			Type: "cache",
			ID:   "req-123",
			Data: map[string]interface{}{"key": "value"},
		}

		nestMsg := FromMycelMessage(mycelMsg)

		if nestMsg.Pattern != "cache" {
			t.Errorf("Pattern = %v, want cache", nestMsg.Pattern)
		}
		if nestMsg.ID != "req-123" {
			t.Errorf("ID = %v, want req-123", nestMsg.ID)
		}
		if nestMsg.Data["key"] != "value" {
			t.Errorf("Data[key] = %v, want value", nestMsg.Data["key"])
		}
	})

	t.Run("ToMycelMessage", func(t *testing.T) {
		nestMsg := &NestJSMessage{
			Pattern: "cache",
			ID:      "req-456",
			Data:    map[string]interface{}{"key": "value"},
		}

		mycelMsg := nestMsg.ToMycelMessage()

		if mycelMsg.Type != "cache" {
			t.Errorf("Type = %v, want cache", mycelMsg.Type)
		}
		if mycelMsg.ID != "req-456" {
			t.Errorf("ID = %v, want req-456", mycelMsg.ID)
		}
	})

	t.Run("ToMycelMessage with response", func(t *testing.T) {
		nestMsg := &NestJSMessage{
			ID:         "req-789",
			Response:   map[string]interface{}{"value": "cached"},
			IsDisposed: true,
		}

		mycelMsg := nestMsg.ToMycelMessage()

		if mycelMsg.Data["value"] != "cached" {
			t.Errorf("Data[value] = %v, want cached", mycelMsg.Data["value"])
		}
	})

	t.Run("ToMycelMessage with error", func(t *testing.T) {
		nestMsg := &NestJSMessage{
			ID:         "req-err",
			Err:        "something went wrong",
			IsDisposed: true,
		}

		mycelMsg := nestMsg.ToMycelMessage()

		if mycelMsg.Error != "something went wrong" {
			t.Errorf("Error = %v, want something went wrong", mycelMsg.Error)
		}
	})

	t.Run("NewNestJSRequest", func(t *testing.T) {
		req := NewNestJSRequest("cache", map[string]interface{}{"key": "test"})

		if req.Pattern != "cache" {
			t.Errorf("Pattern = %v, want cache", req.Pattern)
		}
		if req.ID == "" {
			t.Error("ID should not be empty")
		}
	})

	t.Run("NewNestJSResponse", func(t *testing.T) {
		resp := NewNestJSResponse("req-123", map[string]interface{}{"value": "ok"}, nil)

		if resp.ID != "req-123" {
			t.Errorf("ID = %v, want req-123", resp.ID)
		}
		if !resp.IsDisposed {
			t.Error("IsDisposed should be true")
		}
	})
}

func TestPatternToString(t *testing.T) {
	tests := []struct {
		name    string
		pattern interface{}
		want    string
	}{
		{"string pattern", "cache", "cache"},
		{"cmd object", map[string]interface{}{"cmd": "sum"}, "sum"},
		{"complex object", map[string]interface{}{"service": "users"}, `{"service":"users"}`},
		{"number pattern", 42, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := patternToString(tt.pattern)
			if got != tt.want {
				t.Errorf("patternToString(%v) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestIntegration_NestJSProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Create and start NestJS protocol server
	server, err := NewServer("nestjs-server", "127.0.0.1", port, "nestjs",
		WithMaxConnections(10),
	)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	// Register cache handler (simulating NestJS microservice)
	server.RegisterRoute("cache", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		action := input["action"]
		key := input["key"]

		switch action {
		case "get":
			return map[string]interface{}{
				"key":   key,
				"value": "cached_value_for_" + key.(string),
			}, nil
		case "set":
			return map[string]interface{}{
				"success": true,
			}, nil
		default:
			return nil, fmt.Errorf("unknown action: %v", action)
		}
	})

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer server.Close(ctx)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create NestJS protocol client
	client, err := NewClient("nestjs-client", "127.0.0.1", port, "nestjs",
		WithPoolSize(5),
	)
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close(ctx)

	t.Run("cache get request", func(t *testing.T) {
		result, err := client.Write(ctx, &connector.Data{
			Target: "cache",
			Payload: map[string]interface{}{
				"action": "get",
				"key":    "mykey",
			},
		})
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}

		if len(result.Rows) == 0 {
			t.Fatal("expected response data")
		}

		data := result.Rows[0]
		if data["value"] != "cached_value_for_mykey" {
			t.Errorf("value = %v, want cached_value_for_mykey", data["value"])
		}
	})

	t.Run("cache set request", func(t *testing.T) {
		result, err := client.Write(ctx, &connector.Data{
			Target: "cache",
			Payload: map[string]interface{}{
				"action": "set",
				"key":    "newkey",
				"value":  "newvalue",
			},
		})
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}

		if len(result.Rows) == 0 {
			t.Fatal("expected response data")
		}

		data := result.Rows[0]
		if success, ok := data["success"].(bool); !ok || !success {
			t.Errorf("success = %v, want true", data["success"])
		}
	})

	t.Run("concurrent NestJS requests", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				_, err := client.Write(ctx, &connector.Data{
					Target: "cache",
					Payload: map[string]interface{}{
						"action": "get",
						"key":    fmt.Sprintf("key-%d", n),
					},
				})
				if err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("concurrent request error: %v", err)
		}
	})
}

func TestNestJSWireFormat(t *testing.T) {
	// Test that we can correctly parse NestJS wire format: {length}#{json}
	t.Run("parse wire format", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		// Simulate NestJS client sending message
		go func() {
			msg := `{"pattern":"cache","data":{"key":"test"},"id":"req-wire"}`
			wire := fmt.Sprintf("%d#%s", len(msg), msg)
			client.Write([]byte(wire))
		}()

		framer := NewNestJSFramer(server)
		received, err := framer.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage error: %v", err)
		}

		if received.Pattern != "cache" {
			t.Errorf("Pattern = %v, want cache", received.Pattern)
		}
		if received.ID != "req-wire" {
			t.Errorf("ID = %v, want req-wire", received.ID)
		}
	})

	t.Run("write wire format", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		// Write NestJS message
		go func() {
			framer := NewNestJSFramer(server)
			msg := &NestJSMessage{
				Pattern: "cache",
				Data:    map[string]interface{}{"key": "test"},
				ID:      "req-write",
			}
			framer.WriteMessage(msg)
		}()

		// Read raw wire format
		buf := make([]byte, 1024)
		n, err := client.Read(buf)
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}

		wire := string(buf[:n])
		// Should be in format: {length}#{json}
		if wire[0] < '0' || wire[0] > '9' {
			t.Errorf("Wire format should start with length, got: %s", wire)
		}

		// Find the # delimiter
		hashIdx := -1
		for i, c := range wire {
			if c == '#' {
				hashIdx = i
				break
			}
		}

		if hashIdx == -1 {
			t.Errorf("Wire format should contain #, got: %s", wire)
		}

		// Parse JSON after #
		jsonPart := wire[hashIdx+1:]
		var nestMsg NestJSMessage
		if err := json.Unmarshal([]byte(jsonPart), &nestMsg); err != nil {
			t.Fatalf("JSON unmarshal error: %v", err)
		}

		if nestMsg.Pattern != "cache" {
			t.Errorf("Pattern = %v, want cache", nestMsg.Pattern)
		}
	})
}
