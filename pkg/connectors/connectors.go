// Package connectors provides all connector schema definitions for Mycel.
// This package is importable by external modules (e.g., Mycel Studio) and
// contains the complete set of ConnectorSchemaProvider implementations.
package connectors

import "github.com/matutetandil/mycel/pkg/schema"

// ---------------------------------------------------------------------------
// Shared helper blocks
// ---------------------------------------------------------------------------

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

func poolBlock() schema.Block {
	return schema.Block{
		Type: "pool", Doc: "Connection pool settings",
		Attrs: []schema.Attr{
			{Name: "max", Doc: "Maximum open connections", Type: schema.TypeNumber},
			{Name: "min", Doc: "Minimum idle connections", Type: schema.TypeNumber},
			{Name: "max_lifetime", Doc: "Maximum connection lifetime in seconds", Type: schema.TypeNumber},
		},
	}
}

func dbSourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "REST operation (e.g., GET /users)", Type: schema.TypeString},
		},
	}
}

func dbTargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "target", Doc: "Table name", Type: schema.TypeString},
			{Name: "query", Doc: "Raw SQL query", Type: schema.TypeString},
		},
	}
}

var mqSourceSchema = &schema.Block{
	Open: true,
	Attrs: []schema.Attr{
		{Name: "operation", Doc: "Queue/topic name to consume from", Type: schema.TypeString, Default: "*"},
	},
}

var mqTargetSchema = &schema.Block{
	Open: true,
	Attrs: []schema.Attr{
		{Name: "target", Doc: "Queue/topic/exchange to publish to", Type: schema.TypeString},
	},
}

// ---------------------------------------------------------------------------
// REST Server
// ---------------------------------------------------------------------------

// RESTSchema implements ConnectorSchemaProvider for REST server.
type RESTSchema struct{}

func (RESTSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "HTTP server port", Type: schema.TypeNumber, Required: true},
			{Name: "format", Doc: "Default response format", Type: schema.TypeString, Values: []string{"json", "xml"}},
		},
		Children: []schema.Block{
			{Type: "cors", Doc: "CORS settings", Attrs: []schema.Attr{
				{Name: "origins", Doc: "Allowed origins", Type: schema.TypeList},
				{Name: "methods", Doc: "Allowed HTTP methods", Type: schema.TypeList},
				{Name: "headers", Doc: "Allowed headers", Type: schema.TypeList},
			}},
			{Type: "auth", Doc: "Authentication", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type (jwt, api_key, basic)", Type: schema.TypeString, Values: []string{"jwt", "api_key", "basic"}},
				{Name: "public", Doc: "Public (unauthenticated) paths", Type: schema.TypeList},
			}},
		},
	}
}

func (RESTSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "HTTP method + path (e.g., GET /users)", Type: schema.TypeString, Required: true},
		},
	}
}

func (RESTSchema) TargetSchema() *schema.Block { return nil }

// ---------------------------------------------------------------------------
// HTTP Client
// ---------------------------------------------------------------------------

// HTTPSchema implements ConnectorSchemaProvider for HTTP client.
type HTTPSchema struct{}

func (HTTPSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "base_url", Doc: "Base URL for HTTP requests", Type: schema.TypeString, Required: true},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "retry_count", Doc: "Number of retries on failure", Type: schema.TypeNumber},
			{Name: "format", Doc: "Request/response format", Type: schema.TypeString, Values: []string{"json", "xml"}},
		},
		Children: []schema.Block{
			{Type: "headers", Doc: "Default request headers", Open: true},
			{Type: "auth", Doc: "Authentication", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type", Type: schema.TypeString, Values: []string{"bearer", "api_key", "basic", "oauth2"}},
				{Name: "token", Doc: "Bearer token", Type: schema.TypeString},
				{Name: "username", Doc: "Basic auth username", Type: schema.TypeString},
				{Name: "password", Doc: "Basic auth password", Type: schema.TypeString},
			}},
			{Type: "tls", Doc: "TLS settings", Attrs: []schema.Attr{
				{Name: "ca_cert", Doc: "CA certificate file", Type: schema.TypeString},
				{Name: "client_cert", Doc: "Client certificate file", Type: schema.TypeString},
				{Name: "client_key", Doc: "Client key file", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification", Type: schema.TypeBool},
			}},
		},
	}
}

func (HTTPSchema) SourceSchema() *schema.Block { return nil }
func (HTTPSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "HTTP method + path (e.g., GET /endpoint)", Type: schema.TypeString},
		},
	}
}

// ---------------------------------------------------------------------------
// GraphQL
// ---------------------------------------------------------------------------

// GraphQLSchema implements ConnectorSchemaProvider for GraphQL.
type GraphQLSchema struct{}

func (GraphQLSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "GraphQL server port", Type: schema.TypeNumber},
			{Name: "host", Doc: "Server host or client endpoint", Type: schema.TypeString},
			{Name: "endpoint", Doc: "Client endpoint URL", Type: schema.TypeString},
			{Name: "playground", Doc: "Enable GraphiQL playground", Type: schema.TypeBool},
			{Name: "playground_path", Doc: "Playground URL path", Type: schema.TypeString},
			{Name: "timeout", Doc: "Client request timeout", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "schema", Doc: "Schema configuration", Attrs: []schema.Attr{
				{Name: "path", Doc: "Schema file path", Type: schema.TypeString},
				{Name: "auto_generate", Doc: "Auto-generate from flows", Type: schema.TypeBool},
			}},
			{Type: "cors", Doc: "CORS settings", Attrs: []schema.Attr{
				{Name: "origins", Doc: "Allowed origins", Type: schema.TypeList},
				{Name: "methods", Doc: "Allowed methods", Type: schema.TypeList},
				{Name: "headers", Doc: "Allowed headers", Type: schema.TypeList},
				{Name: "allow_credentials", Doc: "Allow credentials", Type: schema.TypeBool},
			}},
			{Type: "federation", Doc: "Federation v2", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable Federation v2", Type: schema.TypeBool},
				{Name: "version", Doc: "Federation version", Type: schema.TypeNumber},
			}},
			{Type: "subscriptions", Doc: "WebSocket subscriptions", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable subscriptions", Type: schema.TypeBool},
				{Name: "path", Doc: "WebSocket path", Type: schema.TypeString},
				{Name: "keep_alive_interval", Doc: "Keep-alive interval", Type: schema.TypeDuration},
			}},
			{Type: "headers", Doc: "Default client headers", Open: true},
			{Type: "auth", Doc: "Authentication", Open: true},
		},
	}
}

func (GraphQLSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "Query.name / Mutation.name / Subscription.name", Type: schema.TypeString, Required: true},
		},
	}
}

func (GraphQLSchema) TargetSchema() *schema.Block {
	return &schema.Block{Open: true, Attrs: []schema.Attr{
		{Name: "operation", Doc: "Target operation", Type: schema.TypeString},
	}}
}

// ---------------------------------------------------------------------------
// gRPC
// ---------------------------------------------------------------------------

// GRPCSchema implements ConnectorSchemaProvider for gRPC.
type GRPCSchema struct{}

func (GRPCSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "gRPC server hostname", Type: schema.TypeString},
			{Name: "port", Doc: "gRPC server port", Type: schema.TypeNumber},
			{Name: "proto_path", Doc: "Path to .proto file or directory", Type: schema.TypeString},
			{Name: "reflection", Doc: "Enable gRPC server reflection", Type: schema.TypeBool},
			{Name: "max_recv_mb", Doc: "Maximum receive message size in MB", Type: schema.TypeNumber},
			{Name: "max_send_mb", Doc: "Maximum send message size in MB", Type: schema.TypeNumber},
			{Name: "proto_files", Doc: "List of proto file paths", Type: schema.TypeList},
		},
		Children: []schema.Block{
			{Type: "tls", Doc: "TLS/SSL settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS", Type: schema.TypeBool},
				{Name: "cert_file", Doc: "TLS certificate file", Type: schema.TypeString},
				{Name: "key_file", Doc: "TLS key file", Type: schema.TypeString},
				{Name: "ca_file", Doc: "CA certificate file", Type: schema.TypeString},
				{Name: "server_name", Doc: "TLS server name override", Type: schema.TypeString},
				{Name: "skip_verify", Doc: "Skip TLS certificate verification", Type: schema.TypeBool},
			}},
			{Type: "auth", Doc: "Authentication settings", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type", Type: schema.TypeString},
				{Name: "public", Doc: "Public (unauthenticated) methods", Type: schema.TypeList},
			}},
		},
	}
}

func (GRPCSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "gRPC method to expose (e.g., GetUser)", Type: schema.TypeString, Required: true},
		},
	}
}

func (GRPCSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "gRPC method to call", Type: schema.TypeString},
		},
	}
}

// ---------------------------------------------------------------------------
// TCP
// ---------------------------------------------------------------------------

// TCPSchema implements ConnectorSchemaProvider for TCP.
type TCPSchema struct{}

func (TCPSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "TCP server port", Type: schema.TypeNumber, Required: true},
			{Name: "host", Doc: "TCP server hostname", Type: schema.TypeString},
			{Name: "protocol", Doc: "Wire protocol", Type: schema.TypeString, Values: []string{"json", "msgpack", "nestjs"}},
			{Name: "max_connections", Doc: "Maximum concurrent connections", Type: schema.TypeNumber},
			{Name: "read_timeout", Doc: "Read timeout duration", Type: schema.TypeDuration},
			{Name: "write_timeout", Doc: "Write timeout duration", Type: schema.TypeDuration},
			{Name: "pool_size", Doc: "Connection pool size", Type: schema.TypeNumber},
			{Name: "connect_timeout", Doc: "Connection timeout duration", Type: schema.TypeDuration},
			{Name: "idle_timeout", Doc: "Idle connection timeout", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "tls", Doc: "TLS/SSL settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS", Type: schema.TypeBool},
				{Name: "cert", Doc: "TLS certificate file", Type: schema.TypeString},
				{Name: "key", Doc: "TLS key file", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification", Type: schema.TypeBool},
				{Name: "ca_cert", Doc: "CA certificate file", Type: schema.TypeString},
			}},
		},
	}
}

func (TCPSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "TCP message pattern to handle", Type: schema.TypeString, Required: true},
		},
	}
}

func (TCPSchema) TargetSchema() *schema.Block { return nil }

// ---------------------------------------------------------------------------
// SOAP
// ---------------------------------------------------------------------------

// SOAPSchema implements ConnectorSchemaProvider for SOAP.
type SOAPSchema struct{}

func (SOAPSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "endpoint", Doc: "SOAP service endpoint URL", Type: schema.TypeString},
			{Name: "port", Doc: "SOAP server port", Type: schema.TypeNumber},
			{Name: "soap_version", Doc: "SOAP protocol version", Type: schema.TypeString, Values: []string{"1.1", "1.2"}},
			{Name: "namespace", Doc: "XML namespace for the service", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "auth", Doc: "Authentication settings", Open: true, Attrs: []schema.Attr{
				{Name: "type", Doc: "Auth type (basic, wsse, token)", Type: schema.TypeString},
				{Name: "username", Doc: "Auth username", Type: schema.TypeString},
				{Name: "password", Doc: "Auth password", Type: schema.TypeString},
				{Name: "token", Doc: "Auth token", Type: schema.TypeString},
			}},
			{Type: "headers", Doc: "Custom SOAP headers", Open: true},
		},
	}
}

func (SOAPSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "SOAP operation to expose", Type: schema.TypeString, Required: true},
		},
	}
}

func (SOAPSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "SOAP operation to call", Type: schema.TypeString},
		},
	}
}

// ---------------------------------------------------------------------------
// WebSocket
// ---------------------------------------------------------------------------

// WebSocketSchema implements ConnectorSchemaProvider for WebSocket.
type WebSocketSchema struct{}

func (WebSocketSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "WebSocket server port", Type: schema.TypeNumber},
			{Name: "host", Doc: "WebSocket server hostname", Type: schema.TypeString},
			{Name: "path", Doc: "WebSocket endpoint path", Type: schema.TypeString},
			{Name: "ping_interval", Doc: "Ping interval for keep-alive", Type: schema.TypeDuration},
			{Name: "pong_timeout", Doc: "Pong response timeout", Type: schema.TypeDuration},
		},
	}
}

func (WebSocketSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "WebSocket event to handle", Type: schema.TypeString, Default: "*"},
		},
	}
}

func (WebSocketSchema) TargetSchema() *schema.Block { return nil }

// ---------------------------------------------------------------------------
// SSE
// ---------------------------------------------------------------------------

// SSESchema implements ConnectorSchemaProvider for SSE.
type SSESchema struct{}

func (SSESchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "port", Doc: "SSE server port", Type: schema.TypeNumber},
			{Name: "host", Doc: "SSE server hostname", Type: schema.TypeString},
			{Name: "path", Doc: "SSE endpoint path", Type: schema.TypeString},
			{Name: "heartbeat_interval", Doc: "Heartbeat interval for keep-alive", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "cors", Doc: "CORS settings", Attrs: []schema.Attr{
				{Name: "origins", Doc: "Allowed origins", Type: schema.TypeList},
			}},
		},
	}
}

func (SSESchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "SSE event type to handle", Type: schema.TypeString, Required: true},
		},
	}
}

func (SSESchema) TargetSchema() *schema.Block { return nil }

// ---------------------------------------------------------------------------
// Database: PostgreSQL
// ---------------------------------------------------------------------------

// PostgresSchema implements ConnectorSchemaProvider for PostgreSQL.
type PostgresSchema struct{}

func (PostgresSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "Database server host", Type: schema.TypeString},
			{Name: "port", Doc: "Database server port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
			{Name: "user", Doc: "Username", Type: schema.TypeString},
			{Name: "password", Doc: "Password", Type: schema.TypeString},
			{Name: "sslmode", Doc: "SSL mode (disable, require, verify-ca, verify-full)", Type: schema.TypeString},
			{Name: "use_replicas", Doc: "Enable read replicas", Type: schema.TypeBool},
		},
		Children: []schema.Block{
			poolBlock(),
			{Type: "replicas", Doc: "Read replica configuration", Open: true, Attrs: []schema.Attr{
				{Name: "host", Doc: "Replica host", Type: schema.TypeString, Required: true},
				{Name: "port", Doc: "Replica port", Type: schema.TypeNumber},
				{Name: "weight", Doc: "Load balancing weight", Type: schema.TypeNumber},
				{Name: "max_connections", Doc: "Max connections for this replica", Type: schema.TypeNumber},
			}},
		},
	}
}

func (PostgresSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (PostgresSchema) TargetSchema() *schema.Block  { return dbTargetSchema() }

// ---------------------------------------------------------------------------
// Database: MySQL
// ---------------------------------------------------------------------------

// MySQLSchema implements ConnectorSchemaProvider for MySQL.
type MySQLSchema struct{}

func (MySQLSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "Database server host", Type: schema.TypeString},
			{Name: "port", Doc: "Database server port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
			{Name: "user", Doc: "Username", Type: schema.TypeString},
			{Name: "password", Doc: "Password", Type: schema.TypeString},
			{Name: "charset", Doc: "Character set", Type: schema.TypeString},
			{Name: "use_replicas", Doc: "Enable read replicas", Type: schema.TypeBool},
		},
		Children: []schema.Block{
			poolBlock(),
			{Type: "replicas", Doc: "Read replica configuration", Open: true},
		},
	}
}

func (MySQLSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (MySQLSchema) TargetSchema() *schema.Block  { return dbTargetSchema() }

// ---------------------------------------------------------------------------
// Database: SQLite
// ---------------------------------------------------------------------------

// SQLiteSchema implements ConnectorSchemaProvider for SQLite.
type SQLiteSchema struct{}

func (SQLiteSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "database", Doc: "Database file path", Type: schema.TypeString, Required: true},
		},
	}
}

func (SQLiteSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (SQLiteSchema) TargetSchema() *schema.Block  { return dbTargetSchema() }

// ---------------------------------------------------------------------------
// Database: MongoDB
// ---------------------------------------------------------------------------

// MongoDBSchema implements ConnectorSchemaProvider for MongoDB.
type MongoDBSchema struct{}

func (MongoDBSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "uri", Doc: "MongoDB connection URI", Type: schema.TypeString},
			{Name: "host", Doc: "MongoDB host", Type: schema.TypeString},
			{Name: "port", Doc: "MongoDB port", Type: schema.TypeNumber},
			{Name: "user", Doc: "Username", Type: schema.TypeString},
			{Name: "password", Doc: "Password", Type: schema.TypeString},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
		},
		Children: []schema.Block{
			{Type: "pool", Doc: "Connection pool settings", Attrs: []schema.Attr{
				{Name: "max", Doc: "Maximum pool size", Type: schema.TypeNumber},
				{Name: "min", Doc: "Minimum pool size", Type: schema.TypeNumber},
				{Name: "connect_timeout", Doc: "Connection timeout in seconds", Type: schema.TypeNumber},
			}},
		},
	}
}

func (MongoDBSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (MongoDBSchema) TargetSchema() *schema.Block  { return dbTargetSchema() }

// ---------------------------------------------------------------------------
// MQ: RabbitMQ
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// MQ: Kafka
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// MQ: Redis Pub/Sub
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// File
// ---------------------------------------------------------------------------

// FileSchema implements ConnectorSchemaProvider for File.
type FileSchema struct{}

func (FileSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "base_path", Doc: "Base directory path for file operations", Type: schema.TypeString, Required: true},
			{Name: "format", Doc: "File format", Type: schema.TypeString, Values: []string{"json", "csv", "tsv", "yaml", "xlsx"}},
			{Name: "watch", Doc: "Enable file watching", Type: schema.TypeBool},
			{Name: "create_dirs", Doc: "Create directories if they do not exist", Type: schema.TypeBool},
			{Name: "permissions", Doc: "File permissions (numeric, e.g., 0644)", Type: schema.TypeNumber},
			{Name: "watch_interval", Doc: "File watch polling interval", Type: schema.TypeString},
			{Name: "csv_delimiter", Doc: "CSV field delimiter", Type: schema.TypeString},
			{Name: "csv_comment", Doc: "CSV comment character", Type: schema.TypeString},
			{Name: "csv_no_header", Doc: "CSV has no header row", Type: schema.TypeBool},
			{Name: "csv_trim_space", Doc: "Trim leading space in CSV fields", Type: schema.TypeBool},
			{Name: "csv_skip_rows", Doc: "Number of rows to skip at start", Type: schema.TypeNumber},
		},
	}
}

func (FileSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "File operation (read, list, watch)", Type: schema.TypeString, Default: "*"},
		},
	}
}

func (FileSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// S3
// ---------------------------------------------------------------------------

// S3Schema implements ConnectorSchemaProvider for S3.
type S3Schema struct{}

func (S3Schema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "bucket", Doc: "S3 bucket name", Type: schema.TypeString, Required: true},
			{Name: "region", Doc: "AWS region", Type: schema.TypeString},
			{Name: "endpoint", Doc: "Custom S3 endpoint URL", Type: schema.TypeString},
			{Name: "access_key", Doc: "AWS access key ID", Type: schema.TypeString},
			{Name: "secret_key", Doc: "AWS secret access key", Type: schema.TypeString},
			{Name: "session_token", Doc: "AWS session token", Type: schema.TypeString},
			{Name: "prefix", Doc: "Key prefix for all operations", Type: schema.TypeString},
			{Name: "format", Doc: "File format", Type: schema.TypeString, Values: []string{"json", "csv", "tsv", "yaml", "xlsx"}},
			{Name: "use_path_style", Doc: "Use path-style addressing", Type: schema.TypeBool},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
	}
}

func (S3Schema) SourceSchema() *schema.Block { return nil }

func (S3Schema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// FTP/SFTP
// ---------------------------------------------------------------------------

// FTPSchema implements ConnectorSchemaProvider for FTP/SFTP.
type FTPSchema struct{}

func (FTPSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "FTP/SFTP server hostname", Type: schema.TypeString, Required: true},
			{Name: "port", Doc: "Server port", Type: schema.TypeNumber},
			{Name: "username", Doc: "Authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "Authentication password", Type: schema.TypeString},
			{Name: "protocol", Doc: "Transfer protocol", Type: schema.TypeString, Values: []string{"ftp", "sftp"}},
			{Name: "base_path", Doc: "Base directory on remote server", Type: schema.TypeString},
			{Name: "key_file", Doc: "SSH private key file for SFTP", Type: schema.TypeString},
			{Name: "passive", Doc: "Use passive mode for FTP", Type: schema.TypeBool},
			{Name: "tls", Doc: "Enable TLS for FTP", Type: schema.TypeBool},
			{Name: "timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
		},
	}
}

func (FTPSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

func (FTPSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// Exec
// ---------------------------------------------------------------------------

// ExecSchema implements ConnectorSchemaProvider for Exec.
type ExecSchema struct{}

func (ExecSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "command", Doc: "Command to execute", Type: schema.TypeString, Required: true},
			{Name: "workdir", Doc: "Working directory for the command", Type: schema.TypeString},
			{Name: "timeout", Doc: "Execution timeout", Type: schema.TypeDuration},
			{Name: "shell", Doc: "Shell to use (e.g., /bin/sh)", Type: schema.TypeString},
			{Name: "input_format", Doc: "Input data format", Type: schema.TypeString},
			{Name: "output_format", Doc: "Output data format", Type: schema.TypeString},
		},
		Children: []schema.Block{
			{Type: "env", Doc: "Environment variables", Open: true},
			{Type: "ssh", Doc: "SSH remote execution settings", Attrs: []schema.Attr{
				{Name: "host", Doc: "SSH server hostname", Type: schema.TypeString},
				{Name: "port", Doc: "SSH server port", Type: schema.TypeNumber},
				{Name: "user", Doc: "SSH username", Type: schema.TypeString},
				{Name: "key_file", Doc: "SSH private key file", Type: schema.TypeString},
				{Name: "password", Doc: "SSH password", Type: schema.TypeString},
				{Name: "known_hosts", Doc: "Known hosts file path", Type: schema.TypeString},
			}},
		},
	}
}

func (ExecSchema) SourceSchema() *schema.Block { return nil }

func (ExecSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

// CacheSchema implements ConnectorSchemaProvider for Cache.
type CacheSchema struct{}

func (CacheSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Cache backend driver", Type: schema.TypeString, Required: true, Values: []string{"memory", "redis"}},
			{Name: "mode", Doc: "Cache mode", Type: schema.TypeString},
			{Name: "url", Doc: "Redis connection URL", Type: schema.TypeString},
			{Name: "prefix", Doc: "Key prefix for namespacing", Type: schema.TypeString},
			{Name: "max_items", Doc: "Maximum number of cached items", Type: schema.TypeNumber},
			{Name: "eviction", Doc: "Eviction policy", Type: schema.TypeString},
			{Name: "default_ttl", Doc: "Default time-to-live for entries", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "pool", Doc: "Connection pool settings", Attrs: []schema.Attr{
				{Name: "max_connections", Doc: "Maximum pool connections", Type: schema.TypeNumber},
				{Name: "min_idle", Doc: "Minimum idle connections", Type: schema.TypeNumber},
				{Name: "max_idle_time", Doc: "Maximum idle time before eviction", Type: schema.TypeDuration},
				{Name: "connect_timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
			}},
			{Type: "cluster", Doc: "Redis cluster settings", Open: true, Attrs: []schema.Attr{
				{Name: "nodes", Doc: "Cluster node addresses", Type: schema.TypeList},
				{Name: "password", Doc: "Cluster password", Type: schema.TypeString},
			}},
			{Type: "sentinel", Doc: "Redis sentinel settings", Open: true, Attrs: []schema.Attr{
				{Name: "master_name", Doc: "Sentinel master name", Type: schema.TypeString},
				{Name: "nodes", Doc: "Sentinel node addresses", Type: schema.TypeList},
				{Name: "password", Doc: "Sentinel password", Type: schema.TypeString},
			}},
		},
	}
}

func (CacheSchema) SourceSchema() *schema.Block { return nil }
func (CacheSchema) TargetSchema() *schema.Block  { return nil }

// ---------------------------------------------------------------------------
// PDF
// ---------------------------------------------------------------------------

// PDFSchema implements ConnectorSchemaProvider for PDF.
type PDFSchema struct{}

func (PDFSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "template", Doc: "HTML template file path", Type: schema.TypeString, Required: true},
			{Name: "output_dir", Doc: "Output directory for generated PDFs", Type: schema.TypeString},
			{Name: "page_size", Doc: "Page size (e.g., A4, Letter)", Type: schema.TypeString},
			{Name: "font", Doc: "Default font family", Type: schema.TypeString},
			{Name: "margin_left", Doc: "Left margin in mm", Type: schema.TypeNumber},
			{Name: "margin_top", Doc: "Top margin in mm", Type: schema.TypeNumber},
			{Name: "margin_right", Doc: "Right margin in mm", Type: schema.TypeNumber},
		},
	}
}

func (PDFSchema) SourceSchema() *schema.Block { return nil }

func (PDFSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "PDF operation", Type: schema.TypeString, Values: []string{"generate", "save"}},
		},
	}
}

// ---------------------------------------------------------------------------
// Elasticsearch
// ---------------------------------------------------------------------------

// ElasticsearchSchema implements ConnectorSchemaProvider for Elasticsearch.
type ElasticsearchSchema struct{}

func (ElasticsearchSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "url", Doc: "Elasticsearch URL", Type: schema.TypeString, Required: true},
			{Name: "username", Doc: "Authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "Authentication password", Type: schema.TypeString},
			{Name: "index", Doc: "Default index name", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "nodes", Doc: "Additional cluster node URLs", Type: schema.TypeList},
		},
	}
}

func (ElasticsearchSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

func (ElasticsearchSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "Elasticsearch operation", Type: schema.TypeString, Values: []string{"index", "bulk", "delete"}},
		},
	}
}

// ---------------------------------------------------------------------------
// OAuth
// ---------------------------------------------------------------------------

// OAuthSchema implements ConnectorSchemaProvider for OAuth.
type OAuthSchema struct{}

func (OAuthSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "OAuth provider", Type: schema.TypeString, Required: true, Values: []string{"google", "github", "apple", "oidc", "custom"}},
			{Name: "client_id", Doc: "OAuth client ID", Type: schema.TypeString, Required: true},
			{Name: "client_secret", Doc: "OAuth client secret", Type: schema.TypeString, Required: true},
			{Name: "redirect_uri", Doc: "OAuth redirect URI", Type: schema.TypeString},
			{Name: "scopes", Doc: "Requested OAuth scopes", Type: schema.TypeList},
			{Name: "team_id", Doc: "Apple team ID", Type: schema.TypeString},
			{Name: "key_id", Doc: "Apple key ID", Type: schema.TypeString},
			{Name: "private_key", Doc: "Apple private key path", Type: schema.TypeString},
			{Name: "issuer_url", Doc: "OIDC issuer URL", Type: schema.TypeString},
			{Name: "name", Doc: "Custom provider name", Type: schema.TypeString},
			{Name: "auth_url", Doc: "Custom authorization URL", Type: schema.TypeString},
			{Name: "token_url", Doc: "Custom token URL", Type: schema.TypeString},
			{Name: "userinfo_url", Doc: "Custom userinfo URL", Type: schema.TypeString},
		},
	}
}

func (OAuthSchema) SourceSchema() *schema.Block { return nil }
func (OAuthSchema) TargetSchema() *schema.Block  { return nil }

// ---------------------------------------------------------------------------
// CDC (Change Data Capture)
// ---------------------------------------------------------------------------

// CDCSchema implements ConnectorSchemaProvider for CDC.
type CDCSchema struct{}

func (CDCSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "PostgreSQL host", Type: schema.TypeString, Required: true},
			{Name: "port", Doc: "PostgreSQL port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
			{Name: "user", Doc: "Database user", Type: schema.TypeString, Required: true},
			{Name: "password", Doc: "Database password", Type: schema.TypeString},
			{Name: "sslmode", Doc: "SSL mode (disable, require, verify-full)", Type: schema.TypeString},
			{Name: "slot_name", Doc: "Replication slot name", Type: schema.TypeString},
			{Name: "publication", Doc: "PostgreSQL publication name", Type: schema.TypeString},
		},
	}
}

func (CDCSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "Table or event filter", Type: schema.TypeString, Default: "*"},
		},
	}
}

func (CDCSchema) TargetSchema() *schema.Block { return nil }

// ---------------------------------------------------------------------------
// MQTT
// ---------------------------------------------------------------------------

// MQTTSchema implements ConnectorSchemaProvider for MQTT.
type MQTTSchema struct{}

func (MQTTSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "broker", Doc: "MQTT broker URL", Type: schema.TypeString, Required: true},
			{Name: "client_id", Doc: "MQTT client identifier", Type: schema.TypeString},
			{Name: "username", Doc: "Authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "Authentication password", Type: schema.TypeString},
			{Name: "topic", Doc: "Default MQTT topic", Type: schema.TypeString},
			{Name: "qos", Doc: "Quality of Service level (0, 1, 2)", Type: schema.TypeNumber},
			{Name: "clean_session", Doc: "Start with a clean session", Type: schema.TypeBool},
			{Name: "keep_alive", Doc: "Keep-alive interval", Type: schema.TypeDuration},
			{Name: "connect_timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
			{Name: "auto_reconnect", Doc: "Enable automatic reconnection", Type: schema.TypeBool},
			{Name: "max_reconnect_interval", Doc: "Maximum interval between reconnection attempts", Type: schema.TypeDuration},
		},
		Children: []schema.Block{
			{Type: "tls", Doc: "TLS/SSL settings", Attrs: []schema.Attr{
				{Name: "enabled", Doc: "Enable TLS", Type: schema.TypeBool},
				{Name: "cert", Doc: "Client certificate file", Type: schema.TypeString},
				{Name: "key", Doc: "Client key file", Type: schema.TypeString},
				{Name: "ca_cert", Doc: "CA certificate file", Type: schema.TypeString},
				{Name: "insecure_skip_verify", Doc: "Skip certificate verification", Type: schema.TypeBool},
			}},
		},
	}
}

func (MQTTSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "MQTT topic to subscribe to", Type: schema.TypeString, Default: "*"},
		},
	}
}

func (MQTTSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// Email
// ---------------------------------------------------------------------------

// EmailSchema implements ConnectorSchemaProvider for Email.
type EmailSchema struct{}

func (EmailSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Email delivery driver", Type: schema.TypeString, Values: []string{"smtp", "sendgrid", "ses"}},
			{Name: "template", Doc: "HTML template file path", Type: schema.TypeString},
			{Name: "from", Doc: "Sender email address", Type: schema.TypeString, Required: true},
			{Name: "from_name", Doc: "Sender display name", Type: schema.TypeString},
			{Name: "reply_to", Doc: "Reply-to email address", Type: schema.TypeString},
			{Name: "host", Doc: "SMTP server hostname", Type: schema.TypeString},
			{Name: "port", Doc: "SMTP server port", Type: schema.TypeNumber},
			{Name: "username", Doc: "SMTP authentication username", Type: schema.TypeString},
			{Name: "password", Doc: "SMTP authentication password", Type: schema.TypeString},
			{Name: "tls", Doc: "Enable TLS for SMTP", Type: schema.TypeString},
			{Name: "timeout", Doc: "Connection timeout", Type: schema.TypeDuration},
			{Name: "pool_size", Doc: "SMTP connection pool size", Type: schema.TypeNumber},
			{Name: "api_key", Doc: "SendGrid API key", Type: schema.TypeString},
			{Name: "endpoint", Doc: "SES endpoint URL", Type: schema.TypeString},
			{Name: "region", Doc: "AWS region for SES", Type: schema.TypeString},
			{Name: "access_key_id", Doc: "AWS access key ID for SES", Type: schema.TypeString},
			{Name: "secret_access_key", Doc: "AWS secret access key for SES", Type: schema.TypeString},
			{Name: "configuration_set", Doc: "SES configuration set name", Type: schema.TypeString},
		},
	}
}

func (EmailSchema) SourceSchema() *schema.Block { return nil }

func (EmailSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// Slack
// ---------------------------------------------------------------------------

// SlackSchema implements ConnectorSchemaProvider for Slack.
type SlackSchema struct{}

func (SlackSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "webhook_url", Doc: "Slack incoming webhook URL", Type: schema.TypeString},
			{Name: "token", Doc: "Slack Bot/User token", Type: schema.TypeString},
			{Name: "api_url", Doc: "Slack API base URL", Type: schema.TypeString},
			{Name: "channel", Doc: "Default channel to post to", Type: schema.TypeString},
			{Name: "username", Doc: "Bot display name", Type: schema.TypeString},
			{Name: "icon_emoji", Doc: "Bot icon emoji (e.g., :robot_face:)", Type: schema.TypeString},
			{Name: "icon_url", Doc: "Bot icon image URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
	}
}

func (SlackSchema) SourceSchema() *schema.Block { return nil }

func (SlackSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// Discord
// ---------------------------------------------------------------------------

// DiscordSchema implements ConnectorSchemaProvider for Discord.
type DiscordSchema struct{}

func (DiscordSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "webhook_url", Doc: "Discord webhook URL", Type: schema.TypeString},
			{Name: "bot_token", Doc: "Discord bot token", Type: schema.TypeString},
			{Name: "api_url", Doc: "Discord API base URL", Type: schema.TypeString},
			{Name: "channel_id", Doc: "Default channel ID", Type: schema.TypeString},
			{Name: "username", Doc: "Bot display name", Type: schema.TypeString},
			{Name: "avatar_url", Doc: "Bot avatar image URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
		},
	}
}

func (DiscordSchema) SourceSchema() *schema.Block { return nil }

func (DiscordSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// SMS
// ---------------------------------------------------------------------------

// SMSSchema implements ConnectorSchemaProvider for SMS.
type SMSSchema struct{}

func (SMSSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "SMS delivery driver", Type: schema.TypeString, Values: []string{"twilio", "sns"}},
			{Name: "from", Doc: "Sender phone number", Type: schema.TypeString, Required: true},
			{Name: "account_sid", Doc: "Twilio account SID", Type: schema.TypeString},
			{Name: "auth_token", Doc: "Twilio auth token", Type: schema.TypeString},
			{Name: "api_url", Doc: "Twilio API base URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "region", Doc: "AWS region for SNS", Type: schema.TypeString},
			{Name: "access_key_id", Doc: "AWS access key ID for SNS", Type: schema.TypeString},
			{Name: "secret_access_key", Doc: "AWS secret access key for SNS", Type: schema.TypeString},
			{Name: "sender_id", Doc: "SNS sender ID", Type: schema.TypeString},
			{Name: "sms_type", Doc: "SNS SMS type (Transactional, Promotional)", Type: schema.TypeString},
		},
	}
}

func (SMSSchema) SourceSchema() *schema.Block { return nil }

func (SMSSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// Push Notifications
// ---------------------------------------------------------------------------

// PushSchema implements ConnectorSchemaProvider for Push notifications.
type PushSchema struct{}

func (PushSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Push notification driver", Type: schema.TypeString, Values: []string{"fcm", "apns"}},
			{Name: "server_key", Doc: "FCM server key (legacy)", Type: schema.TypeString},
			{Name: "project_id", Doc: "FCM project ID", Type: schema.TypeString},
			{Name: "service_account_json", Doc: "FCM service account JSON file path", Type: schema.TypeString},
			{Name: "api_url", Doc: "FCM API base URL", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "team_id", Doc: "APNs team ID", Type: schema.TypeString},
			{Name: "key_id", Doc: "APNs key ID", Type: schema.TypeString},
			{Name: "private_key", Doc: "APNs private key file path", Type: schema.TypeString},
			{Name: "bundle_id", Doc: "APNs app bundle ID", Type: schema.TypeString},
			{Name: "production", Doc: "Use APNs production environment", Type: schema.TypeBool},
		},
	}
}

func (PushSchema) SourceSchema() *schema.Block { return nil }

func (PushSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

// WebhookSchema implements ConnectorSchemaProvider for Webhook.
type WebhookSchema struct{}

func (WebhookSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "mode", Doc: "Webhook mode", Type: schema.TypeString, Values: []string{"inbound", "outbound"}},
			{Name: "url", Doc: "Outbound webhook URL", Type: schema.TypeString},
			{Name: "path", Doc: "Inbound webhook path", Type: schema.TypeString},
			{Name: "method", Doc: "HTTP method for outbound", Type: schema.TypeString},
			{Name: "secret", Doc: "Webhook signing secret", Type: schema.TypeString},
			{Name: "signature_header", Doc: "Header name for signature", Type: schema.TypeString},
			{Name: "signature_algorithm", Doc: "Signature algorithm (hmac-sha256, etc.)", Type: schema.TypeString},
			{Name: "include_timestamp", Doc: "Include timestamp in signature", Type: schema.TypeBool},
			{Name: "timestamp_header", Doc: "Header name for timestamp", Type: schema.TypeString},
			{Name: "timestamp_tolerance", Doc: "Acceptable timestamp drift", Type: schema.TypeString},
			{Name: "timeout", Doc: "Request timeout", Type: schema.TypeDuration},
			{Name: "require_https", Doc: "Require HTTPS for webhooks", Type: schema.TypeBool},
			{Name: "allowed_ips", Doc: "Allowed source IP addresses", Type: schema.TypeList},
		},
		Children: []schema.Block{
			{Type: "headers", Doc: "Custom HTTP headers", Open: true},
			{Type: "retry", Doc: "Retry policy for outbound webhooks", Attrs: []schema.Attr{
				{Name: "max_attempts", Doc: "Maximum retry attempts", Type: schema.TypeNumber},
				{Name: "initial_delay", Doc: "Initial delay between retries", Type: schema.TypeDuration},
				{Name: "max_delay", Doc: "Maximum delay between retries", Type: schema.TypeDuration},
				{Name: "multiplier", Doc: "Backoff multiplier", Type: schema.TypeNumber},
			}},
		},
	}
}

func (WebhookSchema) SourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

func (WebhookSchema) TargetSchema() *schema.Block {
	return &schema.Block{
		Open: true,
	}
}

// ---------------------------------------------------------------------------
// RegisterAll registers all connector schemas into the given registry.
// ---------------------------------------------------------------------------

// RegisterAll registers every built-in connector schema provider into the registry.
func RegisterAll(reg *schema.Registry) {
	// Protocol connectors
	reg.Register("rest", "", RESTSchema{})
	reg.Register("http", "", HTTPSchema{})
	reg.Register("graphql", "", GraphQLSchema{})
	reg.Register("grpc", "", GRPCSchema{})
	reg.Register("tcp", "", TCPSchema{})
	reg.Register("soap", "", SOAPSchema{})
	reg.Register("websocket", "", WebSocketSchema{})
	reg.Register("sse", "", SSESchema{})

	// Database connectors
	reg.Register("database", "", PostgresSchema{})
	reg.Register("database", "postgres", PostgresSchema{})
	reg.Register("database", "mysql", MySQLSchema{})
	reg.Register("database", "sqlite", SQLiteSchema{})
	reg.Register("database", "mongodb", MongoDBSchema{})

	// Message queue connectors
	reg.Register("mq", "", RabbitMQSchema{})
	reg.Register("mq", "rabbitmq", RabbitMQSchema{})
	reg.Register("mq", "kafka", KafkaSchema{})
	reg.Register("mq", "redis", RedisPubSubSchema{})

	// File/storage connectors
	reg.Register("file", "", FileSchema{})
	reg.Register("s3", "", S3Schema{})
	reg.Register("ftp", "", FTPSchema{})

	// Utility connectors
	reg.Register("exec", "", ExecSchema{})
	reg.Register("cache", "", CacheSchema{})
	reg.Register("pdf", "", PDFSchema{})
	reg.Register("elasticsearch", "", ElasticsearchSchema{})
	reg.Register("oauth", "", OAuthSchema{})
	reg.Register("cdc", "", CDCSchema{})
	reg.Register("mqtt", "", MQTTSchema{})

	// Notification connectors
	reg.Register("email", "", EmailSchema{})
	reg.Register("slack", "", SlackSchema{})
	reg.Register("discord", "", DiscordSchema{})
	reg.Register("sms", "", SMSSchema{})
	reg.Register("push", "", PushSchema{})
	reg.Register("webhook", "", WebhookSchema{})
}

// FullRegistry creates a schema registry with all built-in block schemas
// AND all connector-specific schemas. This is the main entry point for
// Studio and any external consumer that needs the complete Mycel schema.
//
// Usage from Studio:
//
//	import (
//	    "github.com/matutetandil/mycel/pkg/connectors"
//	    "github.com/matutetandil/mycel/pkg/ide"
//	)
//
//	reg := connectors.FullRegistry()
//	engine := ide.NewEngine(dir, ide.WithRegistry(reg))
func FullRegistry() *schema.Registry {
	reg := schema.DefaultRegistry()
	RegisterAll(reg)
	return reg
}
