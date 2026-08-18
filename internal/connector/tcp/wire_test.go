package tcp

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// What a peer on the other end of the socket actually receives.
//
// The codec tests check the encoding; this checks that the encoding is what
// leaves the process. A peer that is not Mycel — a NestJS microservice, a
// device, somebody's Python service — reads these bytes with a library that
// knows only the protocol's name.

// socketPair returns two ends of a real TCP connection.
//
// A real socket rather than net.Pipe: a pipe is synchronous and unbuffered, so
// a writer blocks until the reader has taken each piece, and a test written
// against it passes or times out depending on how the two goroutines happen to
// be scheduled. The kernel's buffer makes this deterministic.
func socketPair(t *testing.T) (client, server net.Conn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()

	client, err = net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server = <-accepted
	if server == nil {
		t.Fatal("nothing connected")
	}

	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client, server
}

// framedWrite writes one message through a framer and returns the body the
// peer read off the socket.
func framedWrite(t *testing.T, codecName string, value interface{}) []byte {
	t.Helper()

	ours, theirs := socketPair(t)

	codec, err := NewCodec(codecName)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}

	framer := NewFramer(ours, codec)
	if err := framer.WriteMessage(value); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	_ = theirs.SetReadDeadline(time.Now().Add(10 * time.Second))
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(theirs, header); err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	body := make([]byte, binary.BigEndian.Uint32(header))
	if _, err := io.ReadFull(theirs, body); err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	return body
}

func TestAPeerSpeakingMessagePackCanReadWhatWeSend(t *testing.T) {
	// The point of the whole thing: decoded by a MessagePack library, with no
	// knowledge of Mycel, off the bytes that left the socket.
	body := framedWrite(t, "msgpack", map[string]interface{}{
		"order_id": "order-1",
		"total":    42.5,
	})

	var received map[string]interface{}
	if err := msgpack.Unmarshal(body, &received); err != nil {
		t.Fatalf("a MessagePack peer could not read what we sent: %v (% x)", err, body)
	}
	if received["order_id"] != "order-1" {
		t.Errorf("the peer received %v", received)
	}

	// And it is not JSON with another name on it, which is what it used to be.
	if json.Valid(body) || bytes.HasPrefix(body, []byte("{")) {
		t.Errorf("what went over the socket is JSON: %q", body)
	}
}

func TestAPeerSpeakingJSONCanReadWhatWeSend(t *testing.T) {
	body := framedWrite(t, "json", map[string]interface{}{"order_id": "order-1"})

	var received map[string]interface{}
	if err := json.Unmarshal(body, &received); err != nil {
		t.Fatalf("a JSON peer could not read what we sent: %v (%q)", err, body)
	}
	if received["order_id"] != "order-1" {
		t.Errorf("the peer received %v", received)
	}
}

func TestTheLengthPrefixMatchesTheMessage(t *testing.T) {
	// Every message is preceded by its length. A header that disagrees with
	// the body desynchronises the stream: the peer reads the next message
	// starting in the middle of this one, and everything after it is rubbish.
	ours, theirs := socketPair(t)

	codec, _ := NewCodec("msgpack")
	value := map[string]interface{}{"order_id": "order-1", "total": 42.5}

	framer := NewFramer(ours, codec)
	if err := framer.WriteMessage(value); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	_ = theirs.SetReadDeadline(time.Now().Add(10 * time.Second))
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(theirs, header); err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	declared := binary.BigEndian.Uint32(header)

	body := make([]byte, declared)
	if _, err := io.ReadFull(theirs, body); err != nil {
		t.Fatalf("the header promised %d bytes and reading them failed: %v", declared, err)
	}

	// The body is a complete document: read short, it would not decode; read
	// long, the read above would have blocked. Comparing bytes against a
	// second encoding does not work — MessagePack walks a Go map in whatever
	// order the runtime gives it, so two encodings of one map differ.
	var received map[string]interface{}
	if err := msgpack.Unmarshal(body, &received); err != nil {
		t.Fatalf("the body the header described is not a whole message: %v", err)
	}
	if received["order_id"] != "order-1" {
		t.Errorf("the peer received %v", received)
	}
	if int(declared) != len(body) {
		t.Errorf("the header says %d bytes and %d were read", declared, len(body))
	}

	// Nothing left over: bytes the header did not account for would be read
	// as the start of the next message.
	_ = theirs.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	extra := make([]byte, 1)
	if n, _ := theirs.Read(extra); n > 0 {
		t.Error("bytes were sent that the header did not account for")
	}
}
