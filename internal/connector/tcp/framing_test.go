package tcp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Framing: where one message ends and the next begins. A stream has no message
// boundaries of its own, so getting this wrong does not lose one message — it
// desynchronises the connection, and everything after it is read at the wrong
// offset.

// connected gives both ends of an in-memory connection.
func connected(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client, server
}

func TestAMessageComesBackTheWayItWasSent(t *testing.T) {
	// Two messages, so the test also says the second one starts where the
	// first ended.
	client, server := connected(t)
	codec, err := NewCodec("json")
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}

	writer := NewFramer(client, codec)
	reader := NewFramer(server, codec)

	sent := []map[string]interface{}{
		{"sku": "WIDGET-1", "quantity": float64(3)},
		{"sku": "WIDGET-2", "quantity": float64(1)},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, message := range sent {
			if err := writer.WriteMessage(message); err != nil {
				t.Errorf("WriteMessage: %v", err)
				return
			}
		}
	}()

	for i := range sent {
		var got map[string]interface{}
		if err := reader.ReadMessage(&got); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if got["sku"] != sent[i]["sku"] || got["quantity"] != sent[i]["quantity"] {
			t.Errorf("message %d = %v, want %v", i, got, sent[i])
		}
	}
	wg.Wait()
}

func TestAMessageTooLargeIsRefusedRatherThanRead(t *testing.T) {
	// The length is whatever the other end says it is, so without this a peer
	// can ask for a 4GB allocation with four bytes.
	client, server := connected(t)
	reader := NewFramer(server, &RawCodec{})

	go func() {
		header := make([]byte, HeaderSize)
		binary.BigEndian.PutUint32(header, uint32(MaxMessageSize+1))
		_, _ = client.Write(header)
	}()

	_, err := reader.ReadRaw()
	if err == nil {
		t.Fatal("a message larger than the limit was read")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q", err)
	}

	// And the same on the way out, so this end cannot send one either.
	writer := NewFramer(client, &RawCodec{})
	if err := writer.WriteRaw(make([]byte, MaxMessageSize+1)); err == nil {
		t.Error("a message larger than the limit was sent")
	}
}

func TestAnEmptyMessageIsStillAMessage(t *testing.T) {
	// A heartbeat, or a request with no arguments: the frame is what says one
	// arrived, not the payload.
	client, server := connected(t)
	writer := NewFramer(client, &RawCodec{})
	reader := NewFramer(server, &RawCodec{})

	go func() { _ = writer.WriteRaw(nil) }()

	got, err := reader.ReadRaw()
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d bytes, want an empty message", len(got))
	}
}

func TestAConnectionThatEndsMidMessageIsReported(t *testing.T) {
	// A peer that dies between the header and the payload. Reading on would
	// block for ever, or worse, read the next connection's bytes as this
	// message's.
	client, server := connected(t)
	reader := NewFramer(server, &RawCodec{})

	go func() {
		header := make([]byte, HeaderSize)
		binary.BigEndian.PutUint32(header, 64)
		_, _ = client.Write(header)
		_ = client.Close()
	}()

	if _, err := reader.ReadRaw(); err == nil {
		t.Error("a message whose payload never arrived was read as complete")
	}
}

// --- The NestJS wire format --------------------------------------------------

func TestANestJSMessageComesBackTheWayItWasSent(t *testing.T) {
	// NestJS frames as {length}#{json}, which is what a Nest microservice
	// speaks — the reason this connector exists.
	client, server := connected(t)
	writer := NewNestJSFramer(client)
	reader := NewNestJSFramer(server)

	go func() {
		_ = writer.WriteMessage(&NestJSMessage{
			Pattern: "get_user",
			Data:    map[string]interface{}{"id": "u-1"},
			ID:      "request-1",
		})
	}()

	got, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Pattern != "get_user" || got.ID != "request-1" {
		t.Errorf("message = %+v", got)
	}
	if got.Data["id"] != "u-1" {
		t.Errorf("data = %v", got.Data)
	}
}

func TestANestJSPatternCanBeARecord(t *testing.T) {
	// Nest writes both { cmd: 'get_user' } and a plain string, and a client
	// that only understands one of them talks to half the services out there.
	client, server := connected(t)
	writer := NewNestJSFramer(client)
	reader := NewNestJSFramer(server)

	go func() {
		_ = writer.WriteMessage(&NestJSMessage{
			Pattern: map[string]interface{}{"cmd": "get_user"},
			Data:    map[string]interface{}{"id": "u-1"},
		})
	}()

	got, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	pattern, ok := got.Pattern.(map[string]interface{})
	if !ok || pattern["cmd"] != "get_user" {
		t.Errorf("pattern = %#v", got.Pattern)
	}
}

func TestANestJSFrameThatIsNotOneIsRefused(t *testing.T) {
	// Each of these is a peer speaking something else on the port, and each
	// has to be an error rather than a read that hangs or allocates.
	for name, wire := range map[string]string{
		"a length that is not a number":  "abc#{}",
		"nothing before the delimiter":   "#{}",
		"a length of zero":               "0#",
		"a length larger than the limit": fmt.Sprintf("%d#{}", MaxMessageSize+1),
		"a payload that is not JSON":     "5#hello",
	} {
		t.Run(name, func(t *testing.T) {
			client, server := connected(t)
			reader := NewNestJSFramer(server)

			go func() {
				_, _ = client.Write([]byte(wire))
				_ = client.Close()
			}()

			if _, err := reader.ReadMessage(); err == nil {
				t.Error("it was read as a message")
			}
		})
	}
}

func TestWhatANestJSFrameLooksLikeOnTheWire(t *testing.T) {
	// The length counts the JSON bytes and the delimiter is a hash, which is
	// what the other end is counting on.
	client, server := connected(t)
	writer := NewNestJSFramer(client)

	go func() {
		_ = writer.WriteMessage(&NestJSMessage{Pattern: "ping"})
	}()

	buffer := make([]byte, 256)
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := server.Read(buffer)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	wire := string(buffer[:n])
	hash := strings.Index(wire, "#")
	if hash <= 0 {
		t.Fatalf("wire = %q, want a length then a hash", wire)
	}

	var length int
	if _, err := fmt.Sscanf(wire[:hash], "%d", &length); err != nil {
		t.Fatalf("the prefix is not a length: %q", wire[:hash])
	}
	payload := wire[hash+1:]
	if length != len(payload) {
		t.Errorf("the length says %d and the payload is %d bytes", length, len(payload))
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}
	if decoded["pattern"] != "ping" {
		t.Errorf("payload = %v", decoded)
	}
}
