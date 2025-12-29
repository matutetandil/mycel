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

	"github.com/mycel-labs/mycel/internal/connector"
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
