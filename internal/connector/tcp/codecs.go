package tcp

import (
	"encoding/json"
	"fmt"
)

// Codec defines the interface for encoding/decoding messages.
type Codec interface {
	// Encode encodes a value to bytes.
	Encode(v interface{}) ([]byte, error)

	// Decode decodes bytes into a value.
	Decode(data []byte, v interface{}) error

	// Name returns the codec name.
	Name() string
}

// NewCodec creates a codec by name.
func NewCodec(name string) (Codec, error) {
	switch name {
	case "json", "":
		return &JSONCodec{}, nil
	case "raw":
		return &RawCodec{}, nil
	case "msgpack":
		return &MsgpackCodec{}, nil
	case "nestjs":
		return &NestJSCodec{}, nil
	default:
		return nil, fmt.Errorf("unknown codec: %s", name)
	}
}

// JSONCodec encodes/decodes messages as JSON.
type JSONCodec struct{}

// Encode encodes a value to JSON bytes.
func (c *JSONCodec) Encode(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Decode decodes JSON bytes into a value.
func (c *JSONCodec) Decode(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// Name returns "json".
func (c *JSONCodec) Name() string {
	return "json"
}

// RawCodec passes bytes through without encoding/decoding.
// Useful for binary protocols or when the application handles serialization.
type RawCodec struct{}

// Encode returns the input as-is if it's []byte, otherwise errors.
func (c *RawCodec) Encode(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	case *Message:
		// For Message types, fall back to JSON encoding
		return json.Marshal(val)
	default:
		return nil, fmt.Errorf("raw codec requires []byte or string, got %T", v)
	}
}

// Decode copies data into v if it's *[]byte, otherwise errors.
func (c *RawCodec) Decode(data []byte, v interface{}) error {
	switch val := v.(type) {
	case *[]byte:
		*val = make([]byte, len(data))
		copy(*val, data)
		return nil
	case *string:
		*val = string(data)
		return nil
	case *Message:
		// For Message types, fall back to JSON decoding
		return json.Unmarshal(data, val)
	default:
		return fmt.Errorf("raw codec requires *[]byte or *string target, got %T", v)
	}
}

// Name returns "raw".
func (c *RawCodec) Name() string {
	return "raw"
}

// MsgpackCodec encodes/decodes messages using MessagePack.
// Note: This is a placeholder - full implementation requires github.com/vmihailenco/msgpack/v5
type MsgpackCodec struct{}

// Encode encodes a value to MessagePack bytes.
// Currently falls back to JSON until msgpack dependency is added.
func (c *MsgpackCodec) Encode(v interface{}) ([]byte, error) {
	// TODO: Use msgpack.Marshal when dependency is added
	// For now, fall back to JSON
	return json.Marshal(v)
}

// Decode decodes MessagePack bytes into a value.
// Currently falls back to JSON until msgpack dependency is added.
func (c *MsgpackCodec) Decode(data []byte, v interface{}) error {
	// TODO: Use msgpack.Unmarshal when dependency is added
	// For now, fall back to JSON
	return json.Unmarshal(data, v)
}

// Name returns "msgpack".
func (c *MsgpackCodec) Name() string {
	return "msgpack"
}
