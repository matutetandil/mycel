package tcp

import (
	"strings"
	"testing"
)

// How a value becomes bytes on the wire, and how a request is matched to its
// answer. Both are the parts of a protocol that a peer on the other side is
// counting on.

func TestTheCodecsAConnectorCanSpeak(t *testing.T) {
	// The name comes from the configuration, so one nobody implements has to
	// be refused at startup rather than falling back to something the peer is
	// not expecting.
	for _, name := range []string{"json", "raw", "msgpack", "nestjs"} {
		codec, err := NewCodec(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if codec.Name() == "" {
			t.Errorf("%s: the codec does not name itself", name)
		}
	}

	// Nothing said is JSON, which is what most peers speak.
	codec, err := NewCodec("")
	if err != nil || codec.Name() != "json" {
		t.Errorf("codec = %v, err = %v, want JSON by default", codec, err)
	}

	if _, err := NewCodec("morse"); err == nil {
		t.Error("a codec nobody implements was accepted")
	}
}

func TestJSONCarriesARecordBothWays(t *testing.T) {
	codec := &JSONCodec{}

	encoded, err := codec.Encode(map[string]interface{}{"sku": "WIDGET-1"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var decoded map[string]interface{}
	if err := codec.Decode(encoded, &decoded); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded["sku"] != "WIDGET-1" {
		t.Errorf("decoded = %v", decoded)
	}

	// Something that cannot be encoded is reported rather than sent as
	// whatever it happens to marshal to.
	if _, err := codec.Encode(func() {}); err == nil {
		t.Error("a value that cannot be encoded was accepted")
	}
}

func TestRawPassesBytesThroughUntouched(t *testing.T) {
	// For a peer speaking a binary protocol of its own: anything this end
	// added or stripped would corrupt it.
	codec := &RawCodec{}
	payload := []byte{0x00, 0x01, 0xff, 0x7f}

	encoded, err := codec.Encode(payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(encoded) != string(payload) {
		t.Errorf("encoded = %v, want the bytes unchanged", encoded)
	}

	var decoded []byte
	if err := codec.Decode(payload, &decoded); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if string(decoded) != string(payload) {
		t.Errorf("decoded = %v", decoded)
	}

	// A string is bytes too, which is what a flow writing text produces.
	encoded, err = codec.Encode("hello")
	if err != nil || string(encoded) != "hello" {
		t.Errorf("encoded = %q, err = %v", encoded, err)
	}
	var text string
	if err := codec.Decode([]byte("hello"), &text); err != nil || text != "hello" {
		t.Errorf("decoded = %q, err = %v", text, err)
	}

	// And a Message falls back to JSON, so the request/response protocol still
	// works over a raw connection.
	if _, err := codec.Encode(&Message{Type: "request", ID: "1"}); err != nil {
		t.Errorf("a message could not be sent over a raw codec: %v", err)
	}

	// Anything else is refused rather than sent as its Go rendering.
	if _, err := codec.Encode(map[string]string{"sku": "W-1"}); err == nil {
		t.Error("a record was accepted by the raw codec")
	}
	var target int
	if err := codec.Decode([]byte("hello"), &target); err == nil {
		t.Error("bytes were decoded into something that cannot hold them")
	}
}

func TestARequestIsMatchedToItsAnswer(t *testing.T) {
	// The id is the only thing tying them together on a connection carrying
	// several at once — without it an answer goes to whoever asked last.
	request := NewRequest("get_user", map[string]interface{}{"id": "u-1"})
	if request.ID == "" {
		t.Fatal("a request carries no id, so its answer cannot be matched")
	}
	if request.IsResponse() {
		t.Error("a request says it is a response")
	}

	answer := NewResponse(request.ID, map[string]interface{}{"name": "Ada"})
	if answer.ID != request.ID {
		t.Errorf("the answer carries %q, want the request's id", answer.ID)
	}
	if !answer.IsResponse() || answer.IsError() {
		t.Errorf("answer = %+v", answer)
	}

	// And a failure is a response too: a caller waiting on the id has to be
	// released rather than left until its timeout.
	failure := NewErrorResponse(request.ID, "no such user")
	if !failure.IsResponse() {
		t.Error("a failure is not treated as an answer, so the caller waits for its timeout")
	}
	if !failure.IsError() || !strings.Contains(failure.Error, "no such user") {
		t.Errorf("failure = %+v", failure)
	}
}

func TestAStructuredResponseSaysWhetherItWorked(t *testing.T) {
	success := NewSuccessResponse("request-1", map[string]interface{}{"name": "Ada"})
	if !success.Success || success.Data["name"] != "Ada" {
		t.Errorf("success = %+v", success)
	}

	failure := NewFailureResponse("request-1", "no such user")
	if failure.Success {
		t.Error("a failure says it succeeded")
	}
	if failure.Error == "" {
		t.Error("a failure says nothing about what went wrong")
	}

	// Both become messages the framer can write, and the type is what the peer
	// switches on.
	if got := success.ToMessage(); got.Type != "response" || got.ID != "request-1" {
		t.Errorf("message = %+v", got)
	}
	if got := failure.ToMessage(); got.Type != "error" {
		t.Errorf("message = %+v", got)
	}
}
