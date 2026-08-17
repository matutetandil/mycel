// Package connectors holds the schema for every built-in connector.
//
// These schemas are the description of Mycel's configuration language: what a
// connector of each type accepts, and what a flow may write in a from, to or
// step block that names one. Completions, `mycel add`, exported documentation
// and the validation `mycel validate` performs are all built from them, so they
// are the single place a connector says what it takes.
//
// They live here, in a package with no dependency beyond pkg/schema, because
// external tooling reads them: a program that wants completions should not have
// to compile the database drivers, message-queue clients and cloud SDKs the
// connectors themselves need. One file per connector.
package connectors

import "github.com/matutetandil/mycel/v2/pkg/schema"

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
	reg.Register("profiled", "", ProfileSchema{})

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
//	    "github.com/matutetandil/mycel/v2/pkg/connectors"
//	    "github.com/matutetandil/mycel/v2/pkg/ide"
//	)
//
//	reg := connectors.FullRegistry()
//	engine := ide.NewEngine(dir, ide.WithRegistry(reg))
func FullRegistry() *schema.Registry {
	reg := schema.DefaultRegistry()
	RegisterAll(reg)
	return reg
}
