// Package types provides common types for message queue connectors.
package types

import (
	"time"

	"github.com/google/uuid"
)

// Message represents a message queue message.
type Message struct {
	ID          string                 `json:"id"`
	Body        map[string]interface{} `json:"body"`
	Headers     map[string]string      `json:"headers,omitempty"`
	RoutingKey  string                 `json:"routing_key,omitempty"`
	Exchange    string                 `json:"exchange,omitempty"`
	Timestamp   int64                  `json:"timestamp"`
	ContentType string                 `json:"content_type,omitempty"`

	// Delivery info (for consumers)
	DeliveryTag uint64 `json:"-"`
	Redelivered bool   `json:"-"`
}

// NewMessage creates a new message with a generated ID and timestamp.
func NewMessage(body map[string]interface{}) *Message {
	return &Message{
		ID:          uuid.New().String(),
		Body:        body,
		Timestamp:   time.Now().Unix(),
		ContentType: "application/json",
	}
}

// NewMessageWithRouting creates a new message with routing information.
func NewMessageWithRouting(body map[string]interface{}, exchange, routingKey string) *Message {
	msg := NewMessage(body)
	msg.Exchange = exchange
	msg.RoutingKey = routingKey
	return msg
}

// SetHeader sets a header on the message.
func (m *Message) SetHeader(key, value string) {
	if m.Headers == nil {
		m.Headers = make(map[string]string)
	}
	m.Headers[key] = value
}

// GetHeader returns a header value.
func (m *Message) GetHeader(key string) string {
	if m.Headers == nil {
		return ""
	}
	return m.Headers[key]
}

// AckMode defines how messages should be acknowledged.
type AckMode int

const (
	// AckModeAuto automatically acknowledges messages after processing.
	AckModeAuto AckMode = iota
	// AckModeManual requires explicit acknowledgment from the handler.
	AckModeManual
	// AckModeNone disables acknowledgment (messages may be redelivered).
	AckModeNone
)

// String returns the string representation of an AckMode.
func (m AckMode) String() string {
	switch m {
	case AckModeAuto:
		return "auto"
	case AckModeManual:
		return "manual"
	case AckModeNone:
		return "none"
	default:
		return "unknown"
	}
}

// ParseAckMode parses a string into an AckMode.
func ParseAckMode(s string) AckMode {
	switch s {
	case "auto":
		return AckModeAuto
	case "manual":
		return AckModeManual
	case "none":
		return AckModeNone
	default:
		return AckModeAuto
	}
}

// DeliveryMode defines message persistence.
type DeliveryMode int

const (
	// DeliveryModeTransient means message may be lost if broker restarts.
	DeliveryModeTransient DeliveryMode = 1
	// DeliveryModePersistent means message will survive broker restarts.
	DeliveryModePersistent DeliveryMode = 2
)
