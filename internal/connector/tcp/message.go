package tcp

import (
	"time"

	"github.com/google/uuid"
)

// Message represents a TCP message with routing information.
// This is the standard message format for JSON protocol.
type Message struct {
	// Type is the message type, used for routing to flow handlers.
	// Example: "create_user", "get_order", "ping"
	Type string `json:"type"`

	// ID is a unique identifier for request-response correlation.
	// Generated automatically if empty on send.
	ID string `json:"id,omitempty"`

	// Data is the message payload.
	Data map[string]interface{} `json:"data,omitempty"`

	// Error contains error information for error responses.
	Error string `json:"error,omitempty"`

	// Timestamp is the Unix timestamp when the message was created.
	Timestamp int64 `json:"timestamp,omitempty"`
}

// NewMessage creates a new message with the given type and data.
func NewMessage(msgType string, data map[string]interface{}) *Message {
	return &Message{
		Type:      msgType,
		ID:        uuid.New().String(),
		Data:      data,
		Timestamp: time.Now().Unix(),
	}
}

// NewRequest creates a new request message.
func NewRequest(msgType string, data map[string]interface{}) *Message {
	return NewMessage(msgType, data)
}

// NewResponse creates a response message for a given request.
func NewResponse(requestID string, data map[string]interface{}) *Message {
	return &Message{
		Type:      "response",
		ID:        requestID,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}
}

// NewErrorResponse creates an error response for a given request.
func NewErrorResponse(requestID string, err string) *Message {
	return &Message{
		Type:      "error",
		ID:        requestID,
		Error:     err,
		Timestamp: time.Now().Unix(),
	}
}

// IsError returns true if this message is an error response.
func (m *Message) IsError() bool {
	return m.Error != "" || m.Type == "error"
}

// IsResponse returns true if this message is a response.
func (m *Message) IsResponse() bool {
	return m.Type == "response" || m.Type == "error"
}

// Response represents a structured response message.
type Response struct {
	// ID is the request ID this response corresponds to.
	ID string `json:"id"`

	// Success indicates whether the request was successful.
	Success bool `json:"success"`

	// Data contains the response data on success.
	Data map[string]interface{} `json:"data,omitempty"`

	// Error contains the error message on failure.
	Error string `json:"error,omitempty"`
}

// NewSuccessResponse creates a successful response.
func NewSuccessResponse(requestID string, data map[string]interface{}) *Response {
	return &Response{
		ID:      requestID,
		Success: true,
		Data:    data,
	}
}

// NewFailureResponse creates a failure response.
func NewFailureResponse(requestID string, err string) *Response {
	return &Response{
		ID:      requestID,
		Success: false,
		Error:   err,
	}
}

// ToMessage converts a Response to a Message.
func (r *Response) ToMessage() *Message {
	msgType := "response"
	if !r.Success {
		msgType = "error"
	}

	return &Message{
		Type:      msgType,
		ID:        r.ID,
		Data:      r.Data,
		Error:     r.Error,
		Timestamp: time.Now().Unix(),
	}
}
