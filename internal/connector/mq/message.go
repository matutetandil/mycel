// Package mq provides message queue connector support.
// This file re-exports types from the types subpackage for convenience.
package mq

import "github.com/matutetandil/mycel/internal/connector/mq/types"

// Type aliases for backward compatibility
type (
	Message      = types.Message
	AckMode      = types.AckMode
	DeliveryMode = types.DeliveryMode
)

// Re-export constants
const (
	AckModeAuto   = types.AckModeAuto
	AckModeManual = types.AckModeManual
	AckModeNone   = types.AckModeNone

	DeliveryModeTransient  = types.DeliveryModeTransient
	DeliveryModePersistent = types.DeliveryModePersistent
)

// Re-export functions
var (
	NewMessage            = types.NewMessage
	NewMessageWithRouting = types.NewMessageWithRouting
	ParseAckMode          = types.ParseAckMode
)

// ExchangeType defines the type of exchange.
type ExchangeType string

const (
	// ExchangeDirect routes to queues by exact routing key match.
	ExchangeDirect ExchangeType = "direct"
	// ExchangeFanout broadcasts to all bound queues.
	ExchangeFanout ExchangeType = "fanout"
	// ExchangeTopic routes by routing key pattern matching.
	ExchangeTopic ExchangeType = "topic"
	// ExchangeHeaders routes by message header matching.
	ExchangeHeaders ExchangeType = "headers"
)
