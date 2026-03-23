package mq

import "github.com/matutetandil/mycel/pkg/schema"

var mqSourceSchema = &schema.Block{
	Open: true,
	Attrs: []schema.Attr{
		{Name: "operation", Doc: "Queue/topic name to consume from", Type: schema.TypeString},
	},
}

var mqTargetSchema = &schema.Block{
	Open: true,
	Attrs: []schema.Attr{
		{Name: "target", Doc: "Queue/topic/exchange to publish to", Type: schema.TypeString},
	},
}

// RabbitMQSchema implements ConnectorSchemaProvider for RabbitMQ.
type RabbitMQSchema struct{}

func (RabbitMQSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "url", Doc: "AMQP connection URL", Type: schema.TypeString},
			{Name: "host", Doc: "Broker hostname", Type: schema.TypeString},
			{Name: "port", Doc: "Broker port", Type: schema.TypeNumber},
			{Name: "username", Doc: "Authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "Authentication password", Type: schema.TypeString},
			{Name: "vhost", Doc: "Virtual host", Type: schema.TypeString},
			{Name: "heartbeat", Doc: "Heartbeat interval", Type: schema.TypeDuration},
			{Name: "connection_name", Doc: "Connection name for identification", Type: schema.TypeString},
			{Name: "reconnect_delay", Doc: "Delay between reconnection attempts", Type: schema.TypeDuration},
			{Name: "max_reconnects", Doc: "Maximum reconnection attempts", Type: schema.TypeNumber},
		},
		Children: []schema.Block{
			tlsBlock(),
			{Type: "queue", Doc: "Queue declaration", Attrs: []schema.Attr{
				{Name: "name", Doc: "Queue name", Type: schema.TypeString},
				{Name: "durable", Doc: "Survives broker restart", Type: schema.TypeBool},
				{Name: "auto_delete", Doc: "Delete when last consumer disconnects", Type: schema.TypeBool},
				{Name: "exclusive", Doc: "Only this connection can access", Type: schema.TypeBool},
				{Name: "no_wait", Doc: "Do not wait for server confirmation", Type: schema.TypeBool},
			}},
			{Type: "exchange", Doc: "Exchange declaration", Attrs: []schema.Attr{
				{Name: "name", Doc: "Exchange name", Type: schema.TypeString},
				{Name: "type", Doc: "Exchange type", Type: schema.TypeString, Values: []string{"direct", "fanout", "topic", "headers"}},
				{Name: "durable", Doc: "Survives broker restart", Type: schema.TypeBool},
				{Name: "auto_delete", Doc: "Delete when no queues bound", Type: schema.TypeBool},
				{Name: "routing_key", Doc: "Routing key for bindings", Type: schema.TypeString},
			}},
			{Type: "consumer", Doc: "Consumer settings", Attrs: []schema.Attr{
				{Name: "queue", Doc: "Queue name (shorthand)", Type: schema.TypeString},
				{Name: "tag", Doc: "Consumer tag identifier", Type: schema.TypeString},
				{Name: "auto_ack", Doc: "Automatic message acknowledgement", Type: schema.TypeBool},
				{Name: "exclusive", Doc: "Exclusive consumer", Type: schema.TypeBool},
				{Name: "no_local", Doc: "Do not receive own messages", Type: schema.TypeBool},
				{Name: "no_wait", Doc: "Do not wait for server confirmation", Type: schema.TypeBool},
				{Name: "concurrency", Doc: "Number of concurrent consumers", Type: schema.TypeNumber},
				{Name: "workers", Doc: "Alias for concurrency", Type: schema.TypeNumber},
				{Name: "prefetch", Doc: "Prefetch count", Type: schema.TypeNumber},
			}, Children: []schema.Block{
				{Type: "dlq", Doc: "Dead letter queue", Attrs: []schema.Attr{
					{Name: "enabled", Doc: "Enable DLQ processing", Type: schema.TypeBool},
					{Name: "exchange", Doc: "DLQ exchange name", Type: schema.TypeString},
					{Name: "queue", Doc: "DLQ queue name", Type: schema.TypeString},
					{Name: "routing_key", Doc: "DLQ routing key", Type: schema.TypeString},
					{Name: "max_retries", Doc: "Max retries before DLQ", Type: schema.TypeNumber},
					{Name: "retry_delay", Doc: "Delay between retries", Type: schema.TypeDuration},
					{Name: "retry_header", Doc: "Header tracking retry count", Type: schema.TypeString},
				}},
			}},
			{Type: "publisher", Doc: "Publisher settings", Attrs: []schema.Attr{
				{Name: "exchange", Doc: "Target exchange", Type: schema.TypeString},
				{Name: "routing_key", Doc: "Routing key", Type: schema.TypeString},
				{Name: "mandatory", Doc: "Require at least one queue binding", Type: schema.TypeBool},
				{Name: "persistent", Doc: "Persistent message delivery", Type: schema.TypeBool},
				{Name: "content_type", Doc: "Message content type", Type: schema.TypeString},
				{Name: "confirms", Doc: "Enable publisher confirms", Type: schema.TypeBool},
			}},
		},
	}
}

func (RabbitMQSchema) SourceSchema() *schema.Block { return mqSourceSchema }
func (RabbitMQSchema) TargetSchema() *schema.Block  { return mqTargetSchema }

// KafkaSchema implements ConnectorSchemaProvider for Kafka.
type KafkaSchema struct{}

func (KafkaSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "brokers", Doc: "Kafka broker addresses", Type: schema.TypeList},
			{Name: "client_id", Doc: "Client identifier", Type: schema.TypeString},
		},
		Children: []schema.Block{
			tlsBlock(),
			{Type: "sasl", Doc: "SASL authentication", Attrs: []schema.Attr{
				{Name: "mechanism", Doc: "SASL mechanism (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)", Type: schema.TypeString},
				{Name: "username", Doc: "SASL username", Type: schema.TypeString},
				{Name: "password", Doc: "SASL password", Type: schema.TypeString},
			}},
			{Type: "consumer", Doc: "Consumer settings", Attrs: []schema.Attr{
				{Name: "group_id", Doc: "Consumer group ID", Type: schema.TypeString},
				{Name: "topics", Doc: "Topics to subscribe", Type: schema.TypeList},
				{Name: "auto_offset_reset", Doc: "Where to start reading (earliest, latest)", Type: schema.TypeString, Values: []string{"earliest", "latest"}},
				{Name: "auto_commit", Doc: "Auto-commit offsets", Type: schema.TypeBool},
				{Name: "concurrency", Doc: "Number of concurrent consumers", Type: schema.TypeNumber},
			}},
			{Type: "producer", Doc: "Producer settings", Attrs: []schema.Attr{
				{Name: "topic", Doc: "Default target topic", Type: schema.TypeString},
				{Name: "acks", Doc: "Ack requirement (none, leader, all)", Type: schema.TypeString, Values: []string{"none", "leader", "all"}},
				{Name: "compression", Doc: "Compression codec", Type: schema.TypeString, Values: []string{"none", "gzip", "snappy", "lz4", "zstd"}},
			}},
			{Type: "schema_registry", Doc: "Schema Registry settings", Attrs: []schema.Attr{
				{Name: "url", Doc: "Schema Registry URL", Type: schema.TypeString},
				{Name: "username", Doc: "Username", Type: schema.TypeString},
				{Name: "password", Doc: "Password", Type: schema.TypeString},
				{Name: "format", Doc: "Serialization format", Type: schema.TypeString, Values: []string{"avro", "protobuf", "json"}},
			}},
		},
	}
}

func (KafkaSchema) SourceSchema() *schema.Block { return mqSourceSchema }
func (KafkaSchema) TargetSchema() *schema.Block  { return mqTargetSchema }

// RedisPubSubSchema implements ConnectorSchemaProvider for Redis Pub/Sub.
type RedisPubSubSchema struct{}

func (RedisPubSubSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "Redis host", Type: schema.TypeString},
			{Name: "port", Doc: "Redis port", Type: schema.TypeNumber},
			{Name: "password", Doc: "Redis password", Type: schema.TypeString},
			{Name: "db", Doc: "Redis database number", Type: schema.TypeNumber},
			{Name: "channels", Doc: "Channels to subscribe (exact match)", Type: schema.TypeList},
			{Name: "patterns", Doc: "Channels to subscribe (pattern match)", Type: schema.TypeList},
		},
	}
}

func (RedisPubSubSchema) SourceSchema() *schema.Block { return mqSourceSchema }
func (RedisPubSubSchema) TargetSchema() *schema.Block  { return mqTargetSchema }

// Shared

func tlsBlock() schema.Block {
	return schema.Block{
		Type: "tls", Doc: "TLS/SSL settings",
		Attrs: []schema.Attr{
			{Name: "enabled", Doc: "Enable TLS", Type: schema.TypeBool},
			{Name: "cert", Doc: "Client certificate file", Type: schema.TypeString},
			{Name: "key", Doc: "Client key file", Type: schema.TypeString},
			{Name: "ca_cert", Doc: "CA certificate file", Type: schema.TypeString},
			{Name: "insecure_skip_verify", Doc: "Skip certificate verification", Type: schema.TypeBool},
		},
	}
}
