package tcp

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

// What actually goes over the socket.
//
// The codec is the wire format, and the whole point of naming one is that the
// thing on the other end is not us. MessagePack used to be encoded as JSON —
// selectable, documented, and unreadable by anything that implements the
// protocol. Two Mycel services configured that way did understand each other,
// which is what hid it: both were sending JSON. That is not compatibility.

func TestEachCodecIsTheFormatItSaysItIs(t *testing.T) {
	value := map[string]interface{}{"order_id": "order-1", "total": 42.5}

	t.Run("json", func(t *testing.T) {
		codec, err := NewCodec("json")
		if err != nil {
			t.Fatalf("NewCodec: %v", err)
		}
		encoded, err := codec.Encode(value)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		// Readable as JSON by anything that reads JSON.
		var back map[string]interface{}
		if err := json.Unmarshal(encoded, &back); err != nil {
			t.Fatalf("what was sent is not JSON: %v (%q)", err, encoded)
		}
		if back["order_id"] != "order-1" {
			t.Errorf("decoded = %v", back)
		}
		if codec.Name() != "json" {
			t.Errorf("name = %q", codec.Name())
		}
	})

	t.Run("msgpack", func(t *testing.T) {
		codec, err := NewCodec("msgpack")
		if err != nil {
			t.Fatalf("NewCodec: %v", err)
		}
		encoded, err := codec.Encode(value)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}

		// The assertion that matters: it is readable by a MessagePack
		// implementation that knows nothing about Mycel.
		var back map[string]interface{}
		if err := msgpack.Unmarshal(encoded, &back); err != nil {
			t.Fatalf("what was sent is not MessagePack: %v (% x)", err, encoded)
		}
		if back["order_id"] != "order-1" {
			t.Errorf("decoded = %v", back)
		}

		// And it is not JSON wearing the name. A map encoded as MessagePack
		// starts with a map header byte, never with '{'.
		if bytes.HasPrefix(encoded, []byte("{")) {
			t.Errorf("msgpack encoded a JSON document: %q", encoded)
		}
		if json.Valid(encoded) {
			t.Errorf("what was sent is readable as JSON, so it is not MessagePack: %q", encoded)
		}
		// Binary is also the reason to choose it: it is smaller than the
		// same document as text.
		asJSON, _ := json.Marshal(value)
		if len(encoded) >= len(asJSON) {
			t.Errorf("the encoding is %d bytes against %d as JSON", len(encoded), len(asJSON))
		}

		if codec.Name() != "msgpack" {
			t.Errorf("name = %q", codec.Name())
		}
	})
}

func TestAMessageSurvivesTheRoundTrip(t *testing.T) {
	// Whatever a flow produces has to come back out the same on the other
	// side — nested records and lists included, which is where an encoder
	// that only handles flat maps falls over.
	original := map[string]interface{}{
		"order_id": "order-1",
		"total":    42.5,
		"paid":     true,
		"items": []interface{}{
			map[string]interface{}{"sku": "SKU-1", "quantity": int8(2)},
		},
		"customer": map[string]interface{}{"country": "NZ"},
	}

	for _, name := range []string{"json", "msgpack"} {
		t.Run(name, func(t *testing.T) {
			codec, err := NewCodec(name)
			if err != nil {
				t.Fatalf("NewCodec: %v", err)
			}
			encoded, err := codec.Encode(original)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			var back map[string]interface{}
			if err := codec.Decode(encoded, &back); err != nil {
				t.Fatalf("Decode: %v", err)
			}

			if back["order_id"] != "order-1" || back["paid"] != true {
				t.Errorf("decoded = %v", back)
			}
			customer, ok := back["customer"].(map[string]interface{})
			if !ok || customer["country"] != "NZ" {
				t.Errorf("the nested record came back as %#v", back["customer"])
			}
			items, ok := back["items"].([]interface{})
			if !ok || len(items) != 1 {
				t.Fatalf("the list came back as %#v", back["items"])
			}
			if item, ok := items[0].(map[string]interface{}); !ok || item["sku"] != "SKU-1" {
				t.Errorf("the item came back as %#v", items[0])
			}
		})
	}
}

func TestBytesSentAsTheyAre(t *testing.T) {
	// The raw codec is for a peer with its own format: whatever the flow
	// produced goes out untouched.
	codec, err := NewCodec("raw")
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}

	encoded, err := codec.Encode([]byte("STX|order-1|ETX"))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(encoded) != "STX|order-1|ETX" {
		t.Errorf("the bytes were changed on the way out: %q", encoded)
	}

	// Text is bytes too, which is the common case from a transform.
	encoded, err = codec.Encode("STX|order-1|ETX")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(encoded) != "STX|order-1|ETX" {
		t.Errorf("text was changed on the way out: %q", encoded)
	}

	// Anything else has no obvious wire form, and guessing one would send a
	// Go value's printed shape to somebody else's parser.
	if _, err := codec.Encode(map[string]interface{}{"order_id": "order-1"}); err == nil {
		t.Error("a record was sent through a codec that carries bytes")
	}

	var into []byte
	if err := codec.Decode([]byte("STX|order-1|ETX"), &into); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if string(into) != "STX|order-1|ETX" {
		t.Errorf("the bytes were changed on the way in: %q", into)
	}
	if err := codec.Decode([]byte("x"), &struct{}{}); err == nil {
		t.Error("bytes were decoded into something that cannot hold them")
	}
	if codec.Name() != "raw" {
		t.Errorf("name = %q", codec.Name())
	}
}

func TestAFormatNobodyImplements(t *testing.T) {
	// Refused where it is written. Falling back to JSON is how a connector
	// ends up speaking a format nobody on the other end expects — which is
	// exactly what msgpack did.
	if _, err := NewCodec("protobuf"); err == nil {
		t.Error("a wire format this connector does not speak was accepted")
	}
	// Nothing written is JSON, which is the documented default — a connector
	// that named no codec should not fail to start.
	codec, err := NewCodec("")
	if err != nil {
		t.Fatalf("a connector that named no codec was refused: %v", err)
	}
	if codec.Name() != "json" {
		t.Errorf("the default codec is %q", codec.Name())
	}
}

func TestSomethingThatCannotBeEncoded(t *testing.T) {
	// A channel, a function: neither format has a representation, and both
	// have to say so rather than send half a message.
	for _, name := range []string{"json", "msgpack"} {
		codec, err := NewCodec(name)
		if err != nil {
			t.Fatalf("NewCodec: %v", err)
		}
		if _, err := codec.Encode(map[string]interface{}{"bad": make(chan int)}); err == nil {
			t.Errorf("%s encoded something with no wire form", name)
		}
		var into map[string]interface{}
		if err := codec.Decode([]byte("not a valid document at all"), &into); err == nil {
			t.Errorf("%s decoded rubbish without complaint", name)
		}
	}
}
