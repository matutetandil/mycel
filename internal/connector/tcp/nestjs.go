package tcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// NestJSMessage represents the NestJS TCP message format.
// NestJS uses: {length}#{json_payload}
// Where json_payload is: {"pattern": "...", "data": {...}, "id": "..."}
type NestJSMessage struct {
	Pattern interface{}            `json:"pattern"`          // Can be string or {"cmd": "..."}
	Data    map[string]interface{} `json:"data"`             // Message payload
	ID      string                 `json:"id,omitempty"`     // Request ID for request-response
	Err     interface{}            `json:"err,omitempty"`    // Error field for responses
	IsDisposed bool                `json:"isDisposed,omitempty"` // NestJS internal
	Response   interface{}         `json:"response,omitempty"`   // Response data
}

// NestJSFramer handles NestJS TCP protocol framing.
// Wire format: {length}#{json}
// Example: 75#{"pattern":"cache","data":{"key":"foo"},"id":"uuid"}
type NestJSFramer struct {
	conn   net.Conn
	reader *bufio.Reader
}

// NewNestJSFramer creates a new NestJS protocol framer.
func NewNestJSFramer(conn net.Conn) *NestJSFramer {
	return &NestJSFramer{
		conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

// SetReadDeadline sets the read deadline on the underlying connection.
func (f *NestJSFramer) SetReadDeadline(t time.Time) error {
	return f.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the underlying connection.
func (f *NestJSFramer) SetWriteDeadline(t time.Time) error {
	return f.conn.SetWriteDeadline(t)
}

// ReadMessage reads a NestJS-formatted message.
func (f *NestJSFramer) ReadMessage() (*NestJSMessage, error) {
	// Read until we find the '#' delimiter
	lengthStr, err := f.reader.ReadString('#')
	if err != nil {
		return nil, fmt.Errorf("failed to read length: %w", err)
	}

	// Remove the '#' delimiter
	lengthStr = lengthStr[:len(lengthStr)-1]

	// Parse length
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return nil, fmt.Errorf("invalid length '%s': %w", lengthStr, err)
	}

	// Validate length
	if length <= 0 {
		return nil, fmt.Errorf("invalid message length: %d", length)
	}
	if length > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", length, MaxMessageSize)
	}

	// Read the JSON payload
	payload := make([]byte, length)
	if _, err := io.ReadFull(f.reader, payload); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	// Parse JSON
	var msg NestJSMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	return &msg, nil
}

// WriteMessage writes a NestJS-formatted message.
func (f *NestJSFramer) WriteMessage(msg *NestJSMessage) error {
	// Serialize to JSON
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Build wire format: {length}#{payload}
	wire := fmt.Sprintf("%d#%s", len(payload), payload)

	// Write to connection
	if _, err := f.conn.Write([]byte(wire)); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}

// Close closes the underlying connection.
func (f *NestJSFramer) Close() error {
	return f.conn.Close()
}

// RemoteAddr returns the remote address of the connection.
func (f *NestJSFramer) RemoteAddr() net.Addr {
	return f.conn.RemoteAddr()
}

// NestJSCodec provides encoding/decoding between Mycel Messages and NestJS format.
type NestJSCodec struct{}

// Encode converts a Mycel Message to NestJS wire format bytes.
func (c *NestJSCodec) Encode(v interface{}) ([]byte, error) {
	msg, ok := v.(*Message)
	if !ok {
		// Try to encode as raw NestJSMessage
		if nestMsg, ok := v.(*NestJSMessage); ok {
			return json.Marshal(nestMsg)
		}
		return nil, fmt.Errorf("NestJS codec requires *Message or *NestJSMessage, got %T", v)
	}

	// Convert Mycel Message to NestJS format
	nestMsg := &NestJSMessage{
		Pattern: msg.Type,
		Data:    msg.Data,
		ID:      msg.ID,
	}

	return json.Marshal(nestMsg)
}

// Decode converts NestJS wire format bytes to a Mycel Message.
func (c *NestJSCodec) Decode(data []byte, v interface{}) error {
	// First parse as NestJS message
	var nestMsg NestJSMessage
	if err := json.Unmarshal(data, &nestMsg); err != nil {
		return err
	}

	// Convert to Mycel Message if that's what's expected
	if msg, ok := v.(*Message); ok {
		msg.Type = patternToString(nestMsg.Pattern)
		msg.ID = nestMsg.ID
		msg.Data = nestMsg.Data

		// Check if this is a response
		if nestMsg.Response != nil {
			if respData, ok := nestMsg.Response.(map[string]interface{}); ok {
				msg.Data = respData
			}
		}

		// Check for errors
		if nestMsg.Err != nil {
			if errStr, ok := nestMsg.Err.(string); ok {
				msg.Error = errStr
			} else if errMap, ok := nestMsg.Err.(map[string]interface{}); ok {
				if errMsg, ok := errMap["message"].(string); ok {
					msg.Error = errMsg
				}
			}
		}

		return nil
	}

	// If expecting NestJSMessage directly
	if nestMsgPtr, ok := v.(*NestJSMessage); ok {
		*nestMsgPtr = nestMsg
		return nil
	}

	return fmt.Errorf("NestJS codec decode target must be *Message or *NestJSMessage, got %T", v)
}

// Name returns "nestjs".
func (c *NestJSCodec) Name() string {
	return "nestjs"
}

// patternToString converts a NestJS pattern to a string.
// Pattern can be a string or {"cmd": "..."} object.
func patternToString(pattern interface{}) string {
	switch p := pattern.(type) {
	case string:
		return p
	case map[string]interface{}:
		// Handle {"cmd": "sum"} style patterns
		if cmd, ok := p["cmd"].(string); ok {
			return cmd
		}
		// Serialize complex patterns as JSON
		data, _ := json.Marshal(p)
		return string(data)
	default:
		return fmt.Sprintf("%v", pattern)
	}
}

// NewNestJSRequest creates a new NestJS request message.
func NewNestJSRequest(pattern string, data map[string]interface{}) *NestJSMessage {
	return &NestJSMessage{
		Pattern: pattern,
		Data:    data,
		ID:      uuid.New().String(),
	}
}

// NewNestJSResponse creates a NestJS response message.
func NewNestJSResponse(requestID string, response interface{}, err interface{}) *NestJSMessage {
	return &NestJSMessage{
		ID:         requestID,
		Response:   response,
		Err:        err,
		IsDisposed: true,
	}
}

// ToMycelMessage converts a NestJS message to a Mycel message.
func (m *NestJSMessage) ToMycelMessage() *Message {
	msg := &Message{
		Type: patternToString(m.Pattern),
		ID:   m.ID,
		Data: m.Data,
	}

	// Handle response data
	if m.Response != nil {
		if respData, ok := m.Response.(map[string]interface{}); ok {
			msg.Data = respData
		}
	}

	// Handle error
	if m.Err != nil {
		if errStr, ok := m.Err.(string); ok {
			msg.Error = errStr
		}
	}

	return msg
}

// FromMycelMessage creates a NestJS message from a Mycel message.
func FromMycelMessage(msg *Message) *NestJSMessage {
	return &NestJSMessage{
		Pattern: msg.Type,
		Data:    msg.Data,
		ID:      msg.ID,
	}
}
