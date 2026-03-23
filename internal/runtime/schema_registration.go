package runtime

import (
	"github.com/matutetandil/mycel/internal/connector/cache"
	"github.com/matutetandil/mycel/internal/connector/cdc"
	"github.com/matutetandil/mycel/internal/connector/database"
	"github.com/matutetandil/mycel/internal/connector/discord"
	"github.com/matutetandil/mycel/internal/connector/elasticsearch"
	"github.com/matutetandil/mycel/internal/connector/email"
	"github.com/matutetandil/mycel/internal/connector/exec"
	"github.com/matutetandil/mycel/internal/connector/file"
	"github.com/matutetandil/mycel/internal/connector/ftp"
	gql "github.com/matutetandil/mycel/internal/connector/graphql"
	grpcconn "github.com/matutetandil/mycel/internal/connector/grpc"
	httpconn "github.com/matutetandil/mycel/internal/connector/http"
	"github.com/matutetandil/mycel/internal/connector/mq"
	mqttconn "github.com/matutetandil/mycel/internal/connector/mqtt"
	"github.com/matutetandil/mycel/internal/connector/oauth"
	pdfconn "github.com/matutetandil/mycel/internal/connector/pdf"
	"github.com/matutetandil/mycel/internal/connector/push"
	"github.com/matutetandil/mycel/internal/connector/rest"
	s3conn "github.com/matutetandil/mycel/internal/connector/s3"
	"github.com/matutetandil/mycel/internal/connector/slack"
	"github.com/matutetandil/mycel/internal/connector/sms"
	soapconn "github.com/matutetandil/mycel/internal/connector/soap"
	sseconn "github.com/matutetandil/mycel/internal/connector/sse"
	"github.com/matutetandil/mycel/internal/connector/tcp"
	"github.com/matutetandil/mycel/internal/connector/webhook"
	wsconn "github.com/matutetandil/mycel/internal/connector/websocket"
	"github.com/matutetandil/mycel/pkg/schema"
)

// RegisterBuiltinSchemas populates a schema registry with all built-in
// connector schemas. Exported so Studio and CLI can use it:
//
//	reg := schema.NewRegistryWith(runtime.RegisterBuiltinSchemas)
func RegisterBuiltinSchemas(reg *schema.Registry) {
	// Server/protocol connectors
	reg.Register("rest", "", rest.Schema{})
	reg.Register("http", "", httpconn.Schema{})
	reg.Register("graphql", "", gql.ConnectorSchemaDef{})
	reg.Register("grpc", "", grpcconn.ConnectorSchemaDef{})
	reg.Register("tcp", "", tcp.ConnectorSchemaDef{})
	reg.Register("soap", "", soapconn.ConnectorSchemaDef{})
	reg.Register("websocket", "", wsconn.ConnectorSchemaDef{})
	reg.Register("sse", "", sseconn.ConnectorSchemaDef{})

	// Database connectors
	reg.Register("database", "", database.PostgresSchema{})
	reg.Register("database", "postgres", database.PostgresSchema{})
	reg.Register("database", "mysql", database.MySQLSchema{})
	reg.Register("database", "sqlite", database.SQLiteSchema{})
	reg.Register("database", "mongodb", database.MongoDBSchema{})

	// Message queue connectors
	reg.Register("mq", "", mq.RabbitMQSchema{})
	reg.Register("mq", "rabbitmq", mq.RabbitMQSchema{})
	reg.Register("mq", "kafka", mq.KafkaSchema{})
	reg.Register("mq", "redis", mq.RedisPubSubSchema{})

	// Storage connectors
	reg.Register("file", "", file.ConnectorSchemaDef{})
	reg.Register("s3", "", s3conn.ConnectorSchemaDef{})
	reg.Register("ftp", "", ftp.ConnectorSchemaDef{})
	reg.Register("cache", "", cache.ConnectorSchemaDef{})

	// Data connectors
	reg.Register("elasticsearch", "", elasticsearch.ConnectorSchemaDef{})
	reg.Register("cdc", "", cdc.ConnectorSchemaDef{})
	reg.Register("exec", "", exec.ConnectorSchemaDef{})
	reg.Register("pdf", "", pdfconn.ConnectorSchemaDef{})

	// IoT
	reg.Register("mqtt", "", mqttconn.ConnectorSchemaDef{})

	// Auth
	reg.Register("oauth", "", oauth.ConnectorSchemaDef{})

	// Notification connectors
	reg.Register("email", "", email.ConnectorSchemaDef{})
	reg.Register("slack", "", slack.ConnectorSchemaDef{})
	reg.Register("discord", "", discord.ConnectorSchemaDef{})
	reg.Register("sms", "", sms.ConnectorSchemaDef{})
	reg.Register("push", "", push.ConnectorSchemaDef{})
	reg.Register("webhook", "", webhook.ConnectorSchemaDef{})
}

// NewSchemaRegistry creates a fully-populated schema registry with all
// built-in block schemas and connector schemas. This is the single entry
// point for consumers that need the complete Mycel schema.
// NewSchemaRegistry creates a fully-populated schema registry with all
// built-in block schemas and connector schemas.
func NewSchemaRegistry() *schema.Registry {
	return schema.NewRegistryWith(RegisterBuiltinSchemas)
}
