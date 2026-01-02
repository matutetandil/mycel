// Package asyncapi provides AsyncAPI 2.6 specification generation.
package asyncapi

// Spec represents an AsyncAPI 2.6 specification.
type Spec struct {
	AsyncAPI string              `json:"asyncapi" yaml:"asyncapi"`
	Info     Info                `json:"info" yaml:"info"`
	Servers  map[string]Server   `json:"servers,omitempty" yaml:"servers,omitempty"`
	Channels map[string]Channel  `json:"channels" yaml:"channels"`
	Components *Components       `json:"components,omitempty" yaml:"components,omitempty"`
}

// Info provides metadata about the API.
type Info struct {
	Title       string   `json:"title" yaml:"title"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Version     string   `json:"version" yaml:"version"`
	Contact     *Contact `json:"contact,omitempty" yaml:"contact,omitempty"`
	License     *License `json:"license,omitempty" yaml:"license,omitempty"`
}

// Contact information for the API.
type Contact struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty"`
	URL   string `json:"url,omitempty" yaml:"url,omitempty"`
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
}

// License information for the API.
type License struct {
	Name string `json:"name" yaml:"name"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}

// Server represents a message broker server.
type Server struct {
	URL         string                 `json:"url" yaml:"url"`
	Protocol    string                 `json:"protocol" yaml:"protocol"` // amqp, kafka, etc.
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Variables   map[string]ServerVariable `json:"variables,omitempty" yaml:"variables,omitempty"`
}

// ServerVariable describes a server URL variable.
type ServerVariable struct {
	Default     string   `json:"default,omitempty" yaml:"default,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Enum        []string `json:"enum,omitempty" yaml:"enum,omitempty"`
}

// Channel describes a message channel (queue, topic, etc.).
type Channel struct {
	Description string     `json:"description,omitempty" yaml:"description,omitempty"`
	Subscribe   *Operation `json:"subscribe,omitempty" yaml:"subscribe,omitempty"`
	Publish     *Operation `json:"publish,omitempty" yaml:"publish,omitempty"`
	Parameters  map[string]Parameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Bindings    *ChannelBindings     `json:"bindings,omitempty" yaml:"bindings,omitempty"`
}

// Operation describes a subscribe or publish operation.
type Operation struct {
	OperationID string              `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Summary     string              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string              `json:"description,omitempty" yaml:"description,omitempty"`
	Message     *Message            `json:"message,omitempty" yaml:"message,omitempty"`
	Bindings    *OperationBindings  `json:"bindings,omitempty" yaml:"bindings,omitempty"`
}

// Message describes a message payload.
type Message struct {
	Name          string                `json:"name,omitempty" yaml:"name,omitempty"`
	Title         string                `json:"title,omitempty" yaml:"title,omitempty"`
	Summary       string                `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description   string                `json:"description,omitempty" yaml:"description,omitempty"`
	ContentType   string                `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	Payload       *Schema               `json:"payload,omitempty" yaml:"payload,omitempty"`
	Headers       *Schema               `json:"headers,omitempty" yaml:"headers,omitempty"`
	CorrelationID *CorrelationID        `json:"correlationId,omitempty" yaml:"correlationId,omitempty"`
	Bindings      *MessageBindings      `json:"bindings,omitempty" yaml:"bindings,omitempty"`
}

// CorrelationID specifies how to find the correlation ID in a message.
type CorrelationID struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Location    string `json:"location" yaml:"location"` // e.g., "$message.header#/correlationId"
}

// Parameter describes a channel parameter.
type Parameter struct {
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
	Location    string  `json:"location,omitempty" yaml:"location,omitempty"`
}

// Schema describes the structure of message data (JSON Schema compatible).
type Schema struct {
	Type        string             `json:"type,omitempty" yaml:"type,omitempty"`
	Format      string             `json:"format,omitempty" yaml:"format,omitempty"`
	Description string             `json:"description,omitempty" yaml:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Required    []string           `json:"required,omitempty" yaml:"required,omitempty"`
	Items       *Schema            `json:"items,omitempty" yaml:"items,omitempty"`
	Ref         string             `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Enum        []interface{}      `json:"enum,omitempty" yaml:"enum,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	MinLength   *int               `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`
	Pattern     string             `json:"pattern,omitempty" yaml:"pattern,omitempty"`
}

// Components holds reusable schema objects.
type Components struct {
	Schemas  map[string]*Schema  `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	Messages map[string]*Message `json:"messages,omitempty" yaml:"messages,omitempty"`
}

// ChannelBindings contains protocol-specific channel bindings.
type ChannelBindings struct {
	AMQP  *AMQPChannelBinding  `json:"amqp,omitempty" yaml:"amqp,omitempty"`
	Kafka *KafkaChannelBinding `json:"kafka,omitempty" yaml:"kafka,omitempty"`
}

// AMQPChannelBinding contains AMQP-specific channel binding.
type AMQPChannelBinding struct {
	Is       string           `json:"is,omitempty" yaml:"is,omitempty"` // queue, routingKey
	Queue    *AMQPQueueConfig `json:"queue,omitempty" yaml:"queue,omitempty"`
	Exchange *AMQPExchangeConfig `json:"exchange,omitempty" yaml:"exchange,omitempty"`
}

// AMQPQueueConfig describes an AMQP queue.
type AMQPQueueConfig struct {
	Name       string `json:"name,omitempty" yaml:"name,omitempty"`
	Durable    bool   `json:"durable,omitempty" yaml:"durable,omitempty"`
	Exclusive  bool   `json:"exclusive,omitempty" yaml:"exclusive,omitempty"`
	AutoDelete bool   `json:"autoDelete,omitempty" yaml:"autoDelete,omitempty"`
}

// AMQPExchangeConfig describes an AMQP exchange.
type AMQPExchangeConfig struct {
	Name       string `json:"name,omitempty" yaml:"name,omitempty"`
	Type       string `json:"type,omitempty" yaml:"type,omitempty"` // topic, direct, fanout, headers
	Durable    bool   `json:"durable,omitempty" yaml:"durable,omitempty"`
	AutoDelete bool   `json:"autoDelete,omitempty" yaml:"autoDelete,omitempty"`
}

// KafkaChannelBinding contains Kafka-specific channel binding.
type KafkaChannelBinding struct {
	Topic      string `json:"topic,omitempty" yaml:"topic,omitempty"`
	Partitions int    `json:"partitions,omitempty" yaml:"partitions,omitempty"`
	Replicas   int    `json:"replicas,omitempty" yaml:"replicas,omitempty"`
}

// OperationBindings contains protocol-specific operation bindings.
type OperationBindings struct {
	AMQP  *AMQPOperationBinding  `json:"amqp,omitempty" yaml:"amqp,omitempty"`
	Kafka *KafkaOperationBinding `json:"kafka,omitempty" yaml:"kafka,omitempty"`
}

// AMQPOperationBinding contains AMQP-specific operation binding.
type AMQPOperationBinding struct {
	Expiration   int    `json:"expiration,omitempty" yaml:"expiration,omitempty"`
	UserID       string `json:"userId,omitempty" yaml:"userId,omitempty"`
	CC           []string `json:"cc,omitempty" yaml:"cc,omitempty"`
	Priority     int    `json:"priority,omitempty" yaml:"priority,omitempty"`
	DeliveryMode int    `json:"deliveryMode,omitempty" yaml:"deliveryMode,omitempty"`
	Mandatory    bool   `json:"mandatory,omitempty" yaml:"mandatory,omitempty"`
	Ack          bool   `json:"ack,omitempty" yaml:"ack,omitempty"`
}

// KafkaOperationBinding contains Kafka-specific operation binding.
type KafkaOperationBinding struct {
	GroupID  string `json:"groupId,omitempty" yaml:"groupId,omitempty"`
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty"`
}

// MessageBindings contains protocol-specific message bindings.
type MessageBindings struct {
	AMQP  *AMQPMessageBinding  `json:"amqp,omitempty" yaml:"amqp,omitempty"`
	Kafka *KafkaMessageBinding `json:"kafka,omitempty" yaml:"kafka,omitempty"`
}

// AMQPMessageBinding contains AMQP-specific message binding.
type AMQPMessageBinding struct {
	ContentEncoding string `json:"contentEncoding,omitempty" yaml:"contentEncoding,omitempty"`
	MessageType     string `json:"messageType,omitempty" yaml:"messageType,omitempty"`
}

// KafkaMessageBinding contains Kafka-specific message binding.
type KafkaMessageBinding struct {
	Key *Schema `json:"key,omitempty" yaml:"key,omitempty"`
}
