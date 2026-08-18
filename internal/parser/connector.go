package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/connector/profile"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// connectorBodySchema is the set of attributes and blocks a connector may
// contain.
//
// It is a function rather than a literal inside the parser so that the schema
// registries can be checked against it: an attribute accepted here and declared
// by no connector schema is either a setting missing from the schema or a word
// that does nothing, and both have happened.
func connectorBodySchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type"}, // Not required - profiled connectors don't have type at root
			{Name: "driver"},
			{Name: "host"},
			{Name: "port"},
			{Name: "database"},
			{Name: "user"},
			{Name: "username"}, // Alias for user (MQ connectors)
			{Name: "password"},
			{Name: "base_url"},
			{Name: "timeout"},
			{Name: "retry_count"},

			// GraphQL specific
			{Name: "endpoint"},
			{Name: "playground"},
			{Name: "playground_path"},
			{Name: "introspection"},

			// TCP specific
			{Name: "protocol"},
			{Name: "max_connections"},
			{Name: "read_timeout"},
			{Name: "write_timeout"},

			// MQ specific (RabbitMQ)
			{Name: "brokers"},
			{Name: "vhost"},           // RabbitMQ virtual host
			{Name: "connection_name"}, // Connection identifier
			{Name: "max_reconnects"},  // Max reconnection attempts
			// MQ specific (Kafka)
			{Name: "client_id"},

			// Exec specific
			{Name: "command"},
			{Name: "args"},
			{Name: "shell"},
			{Name: "env"},
			{Name: "working_dir"},
			{Name: "input_format"},
			{Name: "output_format"},
			{Name: "retry_delay"},

			// Profile-specific attributes
			{Name: "select"},   // CEL expression for profile selection
			{Name: "default"},  // Default profile name
			{Name: "fallback"}, // Fallback profile list

			// Cache specific
			{Name: "mode"},        // standalone/cluster/sentinel
			{Name: "url"},         // Redis connection URL
			{Name: "prefix"},      // Key prefix for namespacing
			{Name: "max_items"},   // Memory cache max items
			{Name: "eviction"},    // Eviction policy (lru)
			{Name: "default_ttl"}, // Default TTL for entries

			// gRPC specific
			{Name: "proto_path"},     // Path to .proto files directory
			{Name: "proto_files"},    // Specific .proto files to load
			{Name: "reflection"},     // Enable gRPC reflection
			{Name: "max_recv_mb"},    // Max receive message size (MB)
			{Name: "max_send_mb"},    // Max send message size (MB)
			{Name: "target"},         // Server address (host:port) for client
			{Name: "insecure"},       // Disable TLS
			{Name: "wait_for_ready"}, // Wait for server ready

			// File connector specific
			{Name: "base_path"},      // Base directory for operations
			{Name: "format"},         // Default format (json/csv/text/binary)
			{Name: "watch"},          // Enable file watching
			{Name: "watch_interval"}, // Polling interval
			{Name: "create_dirs"},    // Auto-create directories
			{Name: "permissions"},    // Default file permissions

			// S3 connector specific
			{Name: "bucket"},         // S3 bucket name
			{Name: "region"},         // AWS region
			{Name: "access_key"},     // AWS access key ID
			{Name: "secret_key"},     // AWS secret access key
			{Name: "session_token"},  // AWS session token (STS)
			{Name: "use_path_style"}, // Use path-style URLs (MinIO)

			// MongoDB specific
			{Name: "uri"},          // MongoDB connection URI
			{Name: "replica_set"},  // Replica set name
			{Name: "auth_source"},  // Authentication database
			{Name: "auth_db"},      // Alias for auth_source
			{Name: "srv"},          // Use SRV record lookup
			{Name: "direct"},       // Direct connection mode
			{Name: "read_concern"}, // Read concern level

			// PostgreSQL/MySQL specific
			{Name: "sslmode"},      // SSL mode
			{Name: "ssl_mode"},     // Alias for sslmode
			{Name: "charset"},      // Character set (MySQL)
			{Name: "replicas"},     // Read replicas configuration
			{Name: "use_replicas"}, // Enable read replicas

			// Email connector specific
			{Name: "from"},      // From email address
			{Name: "from_name"}, // From display name
			{Name: "reply_to"},  // Reply-to address
			{Name: "api_key"},   // SendGrid API key
			{Name: "pool_size"}, // Connection pool size

			// Slack/Discord connector specific
			{Name: "webhook_url"}, // Webhook URL
			{Name: "token"},       // Bot token
			{Name: "api_url"},     // API base URL (Slack)
			{Name: "channel"},     // Default channel
			{Name: "icon_emoji"},  // Icon emoji
			{Name: "icon_url"},    // Icon URL
			{Name: "bot_token"},   // Discord bot token
			{Name: "channel_id"},  // Discord channel ID
			{Name: "avatar_url"},  // Discord avatar URL

			// SMS connector specific (Twilio)
			{Name: "account_sid"}, // Twilio account SID
			{Name: "auth_token"},  // Twilio auth token

			// AWS specific (SES, SNS)
			{Name: "access_key_id"},     // AWS access key
			{Name: "secret_access_key"}, // AWS secret key
			{Name: "configuration_set"}, // SES configuration set
			{Name: "sender_id"},         // SNS sender ID
			{Name: "sms_type"},          // SNS SMS type

			// Push connector specific (FCM)
			{Name: "server_key"},           // FCM server key (legacy)
			{Name: "project_id"},           // Firebase project ID
			{Name: "service_account_json"}, // Service account JSON
			// Push connector specific (APNS)
			{Name: "team_id"},     // Apple team ID
			{Name: "key_id"},      // Apple key ID
			{Name: "private_key"}, // Private key (PEM)
			{Name: "bundle_id"},   // iOS bundle ID
			{Name: "production"},  // Use production APNS

			// Webhook connector specific
			{Name: "secret"},              // Signature secret
			{Name: "signature_header"},    // Signature header name
			{Name: "signature_algorithm"}, // hmac-sha256, etc
			{Name: "path"},                // Webhook endpoint path
			{Name: "timestamp_header"},    // Timestamp header
			{Name: "timestamp_tolerance"}, // Tolerance duration
			{Name: "method"},              // HTTP method

			// SOAP connector specific
			{Name: "soap_version"}, // SOAP version: "1.1" or "1.2"
			{Name: "namespace"},    // SOAP service namespace

			// MQTT connector specific
			{Name: "broker"},                 // Broker URL (tcp://, ssl://, ws://)
			{Name: "qos"},                    // Quality of Service level (0, 1, 2)
			{Name: "topic"},                  // Default publish topic
			{Name: "clean_session"},          // Start clean session on connect
			{Name: "keep_alive"},             // PINGREQ interval
			{Name: "connect_timeout"},        // Connection timeout
			{Name: "auto_reconnect"},         // Reconnect on disconnect
			{Name: "max_reconnect_interval"}, // Max wait between reconnects

			// FTP/SFTP connector specific
			{Name: "key_file"}, // SSH private key file (SFTP)
			{Name: "passive"},  // FTP passive mode

			// Redis Pub/Sub specific (MQ driver = "redis")
			{Name: "channels"}, // Channels to subscribe to
			{Name: "patterns"}, // Glob patterns for PSUBSCRIBE
			{Name: "db"},       // Redis database number

			// CDC specific (Change Data Capture)
			{Name: "slot_name"},   // PostgreSQL logical replication slot
			{Name: "publication"}, // PostgreSQL publication name

			// WebSocket specific
			{Name: "ping_interval"}, // Ping frame interval
			{Name: "pong_timeout"},  // Pong response timeout

			// SSE specific (Server-Sent Events)
			{Name: "heartbeat_interval"}, // Heartbeat comment interval

			// Elasticsearch specific
			{Name: "nodes"}, // Cluster node URLs
			{Name: "index"}, // Default index name

			// OAuth specific
			{Name: "client_secret"}, // OAuth client secret
			{Name: "redirect_uri"},  // OAuth redirect URI
			{Name: "scopes"},        // Requested OAuth scopes
			{Name: "issuer_url"},    // OIDC issuer URL
			{Name: "auth_url"},      // Authorization endpoint
			{Name: "token_url"},     // Token endpoint
			{Name: "userinfo_url"},  // Userinfo endpoint

			// Read by their connectors and refused here until 2.19.0, which
			// made each of these a documented setting with no way to write it.
			{Name: "template"},          // email, pdf
			{Name: "tls"},               // email: starttls|tls|none · ftp: FTPS on/off
			{Name: "csv_delimiter"},     // file
			{Name: "csv_comment"},       // file
			{Name: "csv_no_header"},     // file
			{Name: "csv_trim_space"},    // file
			{Name: "csv_skip_rows"},     // file
			{Name: "heartbeat"},         // mq
			{Name: "reconnect_delay"},   // mq
			{Name: "name"},              // oauth (OIDC provider name)
			{Name: "output_dir"},        // pdf
			{Name: "page_size"},         // pdf
			{Name: "font"},              // pdf
			{Name: "margin_left"},       // pdf
			{Name: "margin_top"},        // pdf
			{Name: "margin_right"},      // pdf
			{Name: "idle_timeout"},      // tcp
			{Name: "include_timestamp"}, // webhook
			{Name: "require_https"},     // webhook
			{Name: "allowed_ips"},       // webhook
			{Name: "trusted_proxies"},   // webhook
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "pool"},
			{Type: "cors"},
			{Type: "auth"},
			{Type: "retry"},
			{Type: "headers"},
			{Type: "schema"},
			{Type: "ssh"},
			{Type: "tls"}, // TLS configuration for HTTP/gRPC
			{Type: "queue"},
			{Type: "exchange"}, // MQ exchange configuration
			{Type: "publisher"},
			{Type: "consumer"},
			{Type: "producer"},
			{Type: "federation"},
			{Type: "subscriptions"}, // GraphQL subscriptions configuration
			{Type: "profile", LabelNames: []string{"name"}}, // Profile blocks
			// Redis Cluster/Sentinel blocks
			{Type: "cluster"},  // Redis Cluster configuration
			{Type: "sentinel"}, // Redis Sentinel configuration
			// gRPC blocks
			{Type: "keep_alive"},     // gRPC keep-alive settings
			{Type: "load_balancing"}, // gRPC load balancing config
			// Kafka blocks
			{Type: "sasl"},            // Kafka SASL authentication
			{Type: "schema_registry"}, // Kafka Schema Registry config
			// Notification connectors
			{Type: "batch"}, // Slack batching: window/max_size/group_by/summary
			// Environment for exec
			{Type: "env"}, // exec: arbitrary NAME = "value" pairs
			// Read replicas for the SQL databases
			{Type: "replicas"}, // one block per replica; collected into a list
			// Named operations
			{Type: "operation", LabelNames: []string{"name"}}, // Named operations for flows
		},
	}
}

// parseConnectorBlock parses a connector block from HCL.
func parseConnectorBlock(block *hcl.Block, ctx *hcl.EvalContext) (*connector.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("connector block requires a name label")
	}

	config := &connector.Config{
		Name:       block.Labels[0],
		Properties: make(map[string]interface{}),
	}

	schema := connectorBodySchema()

	// A connector of a type this runtime ships is held to the list of
	// attributes that exist, because a mistyped one is a setting that silently
	// does nothing. A connector whose type arrives with a plugin cannot be:
	// its attributes are declared in the plugin's own manifest, which the
	// parser has never seen. Those used to be rejected by name, so a plugin
	// connector could not be configured at all — the type loaded, and the one
	// setting it needed was an "unsupported argument".
	content, extra, diags := connectorContent(block, schema, ctx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("connector content error: %s", diags.Error())
	}

	// Record env() calls that resolved to nothing so registration failures can
	// name the missing variable instead of just the empty property.
	config.MissingEnv = collectMissingEnv(block.Body)

	// Parse required type attribute
	if attr, ok := content.Attributes["type"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("type attribute error: %s", diags.Error())
		}
		// AsString on anything that is not one takes the binary down with a Go
		// stack trace before the service has said a word about its
		// configuration.
		typeName, err := stringValue("type", val)
		if err != nil {
			return nil, fmt.Errorf("connector %q: %w", config.Name, err)
		}
		config.Type = typeName
	}

	// Parse optional attributes
	for name, attr := range content.Attributes {
		if name == "type" {
			continue // Already handled
		}

		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}

		// Set driver on config directly for factory lookup
		if name == "driver" {
			driver, err := stringValue("driver", val)
			if err != nil {
				return nil, fmt.Errorf("connector %q: %w", config.Name, err)
			}
			config.Driver = driver
		}

		config.Properties[name] = ctyValueToGo(val)
	}

	// Whatever the plugin declared for itself, which the runtime hands to the
	// module as its configuration.
	for name, val := range extra {
		config.Properties[name] = val
	}

	// Parse nested blocks
	for _, nestedBlock := range content.Blocks {
		if _, err := applyConnectorBlock(config, nestedBlock, ctx); err != nil {
			return nil, err
		}
	}

	// Handle profile configuration
	if profileConfig, ok := config.Properties["_profiles"].(*profile.Config); ok {
		// Get select, default, fallback from properties
		if sel, ok := config.Properties["select"].(string); ok {
			profileConfig.Select = sel
		}
		if def, ok := config.Properties["default"].(string); ok {
			profileConfig.Default = def
		}
		if fb, ok := config.Properties["fallback"].([]interface{}); ok {
			for _, f := range fb {
				if s, ok := f.(string); ok {
					profileConfig.Fallback = append(profileConfig.Fallback, s)
				}
			}
		}

		// Validate: profiled connector needs either select or default
		if profileConfig.Select == "" && profileConfig.Default == "" {
			return nil, fmt.Errorf("profiled connector %s requires 'select' or 'default' attribute", config.Name)
		}

		// Mark as profiled connector
		config.Type = "profiled"
	} else if config.Type == "" {
		return nil, fmt.Errorf("connector %s requires 'type' attribute or 'profile' blocks", config.Name)
	}

	return config, nil
}

// parseProfileBlock parses a profile block inside a connector.
func parseProfileBlock(block *hcl.Block, ctx *hcl.EvalContext) (*profile.ProfileDef, error) {
	profileName := block.Labels[0]

	// A profile is a connector that is chosen at runtime, so it accepts what a
	// connector accepts — plus a transform, which is what makes one profile
	// stand in for another whose payload is shaped differently. It used to
	// carry its own list of 40 attributes against the connector's 159, so a
	// profile could not name a queue, a vhost, a path or a command and could
	// not hold a consumer, retry or tls block; per-environment configuration is
	// the reason profiles exist, which is where that gap was felt.
	schema := connectorBodySchema()
	schema.Blocks = append(schema.Blocks, hcl.BlockHeaderSchema{Type: "transform"})

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("profile content error: %s", diags.Error())
	}

	// Build connector config for this profile
	connConfig := &connector.Config{
		Name:       profileName,
		Properties: make(map[string]interface{}),
	}

	// Parse attributes
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}

		if name == "type" {
			connConfig.Type = val.AsString()
		} else if name == "driver" {
			connConfig.Driver = val.AsString()
		}
		connConfig.Properties[name] = ctyValueToGo(val)
	}

	// A profile declares what it is. The connector schema leaves type optional
	// — a connector made of profiles has none of its own — but a profile with
	// no type is a profile that cannot be built, and it has to say so here
	// rather than at startup.
	if connConfig.Type == "" {
		return nil, fmt.Errorf("profile %q requires a 'type' attribute", profileName)
	}

	profileDef := &profile.ProfileDef{
		Name:            profileName,
		ConnectorConfig: connConfig,
		Transform:       make(map[string]string),
	}

	// Parse nested blocks with exactly the handling a connector gets.
	for _, nestedBlock := range content.Blocks {
		if nestedBlock.Type == "transform" {
			transform, err := parseProfileTransformBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("transform block error: %w", err)
			}
			profileDef.Transform = transform
			continue
		}
		if nestedBlock.Type == "profile" {
			return nil, fmt.Errorf("profile %q cannot contain another profile", profileName)
		}
		if _, err := applyConnectorBlock(connConfig, nestedBlock, ctx); err != nil {
			return nil, err
		}
	}

	return profileDef, nil
}

// parseProfileTransformBlock parses a transform block inside a profile.
func parseProfileTransformBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]string, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("transform block content error: %s", diags.Error())
	}

	transform := make(map[string]string)
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("transform %s error: %s", name, diags.Error())
		}
		// Same rule as a flow's transform, and for the same reason: calling
		// AsString on anything that is not one takes the whole binary down
		// with a Go stack trace at startup.
		expr, err := mappingExpression(name, val)
		if err != nil {
			return nil, err
		}
		transform[name] = expr
	}

	return transform, nil
}

// parseFederationBlock parses a GraphQL Federation configuration block.
func parseFederationBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "version"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("federation block content error: %s", diags.Error())
	}

	federation := make(map[string]interface{})

	// Default enabled to true if block exists
	federation["enabled"] = true

	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("federation %s error: %s", name, diags.Error())
		}
		federation[name] = ctyValueToGo(val)
	}

	return federation, nil
}

// parseSubscriptionsBlock parses a GraphQL subscriptions configuration block.
func parseSubscriptionsBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "path"},
			{Name: "keep_alive_interval"},
			{Name: "connection_timeout"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("subscriptions block content error: %s", diags.Error())
	}

	subscriptions := make(map[string]interface{})

	// Default enabled to true if block exists
	subscriptions["enabled"] = true

	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("subscriptions %s error: %s", name, diags.Error())
		}
		subscriptions[name] = ctyValueToGo(val)
	}

	return subscriptions, nil
}

// parseConsumerBlock parses a consumer block, accepting an optional nested
// dlq block alongside arbitrary attributes. The MQ connector schema declares
// dlq as a child of consumer, so plain attribute parsing rejects it.
func parseConsumerBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	syntaxBody, ok := block.Body.(*hclsyntax.Body)
	if !ok {
		// Fallback: no nested blocks possible, parse as attribute-only block.
		return parseGenericBlock(block, ctx)
	}

	for name, attr := range syntaxBody.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}
		result[name] = ctyValueToGo(val)
	}

	for _, nested := range syntaxBody.Blocks {
		if nested.Type == "dlq" {
			dlqBlock := nested.AsHCLBlock()
			dlq, err := parseGenericBlock(dlqBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("dlq block error: %w", err)
			}
			result["dlq"] = dlq
		}
	}

	return result, nil
}

// parseGenericBlock parses a block with arbitrary attributes.
func parseGenericBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("block content error: %s", diags.Error())
	}

	result := make(map[string]interface{})
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}
		result[name] = ctyValueToGo(val)
	}

	return result, nil
}

// parsePoolBlock parses a pool configuration block.
func parsePoolBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "min"},
			{Name: "max"},
			{Name: "max_lifetime"},
			{Name: "max_idle_time"},
			{Name: "connect_timeout"},
			{Name: "max_connections"}, // cache
			{Name: "min_idle"},        // cache
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("pool block content error: %s", diags.Error())
	}

	pool := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("pool %s error: %s", name, diags.Error())
		}
		pool[name] = ctyValueToGo(val)
	}

	return pool, nil
}

// parseCorsBlock parses a CORS configuration block.
func parseCorsBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "origins"},
			{Name: "methods"},
			{Name: "headers"},
			{Name: "allow_credentials"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("cors block content error: %s", diags.Error())
	}

	cors := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("cors %s error: %s", name, diags.Error())
		}
		cors[name] = ctyValueToGo(val)
	}

	return cors, nil
}

// parseAuthBlock parses an auth configuration block.
func parseAuthBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type"},
			// Bearer token (client mode)
			{Name: "token"},
			{Name: "header"},
			// OAuth2 (client mode)
			{Name: "grant_type"}, // refresh_token or client_credentials
			{Name: "refresh_token"},
			{Name: "token_url"},
			{Name: "client_id"},
			{Name: "client_secret"},
			{Name: "scopes"},
			// API Key (client mode)
			{Name: "api_key"},
			{Name: "api_key_header"},
			{Name: "api_key_query"},
			// Basic auth (client mode)
			{Name: "username"},
			{Name: "password"},
			// JWT validation (server mode)
			{Name: "secret"},
			{Name: "jwks_url"},
			{Name: "issuer"},
			{Name: "audience"},
			{Name: "algorithms"},
			{Name: "scheme"},
			// API Key validation (server mode)
			{Name: "keys"},
			{Name: "query_param"},
			// Basic auth validation (server mode)
			{Name: "users"},
			{Name: "realm"},
			// Common (server mode)
			{Name: "public"},
			{Name: "required_headers"},
			{Name: "response_headers"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("auth block content error: %s", diags.Error())
	}

	auth := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("auth %s error: %s", name, diags.Error())
		}
		auth[name] = ctyValueToGo(val)
	}

	return auth, nil
}

// parseHeadersBlock parses a headers configuration block.
func parseHeadersBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	// Headers block uses dynamic attributes
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("headers block content error: %s", diags.Error())
	}

	headers := make(map[string]interface{})
	for name, attr := range attrs {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("header %s error: %s", name, diags.Error())
		}
		headers[name] = ctyValueToGo(val)
	}

	return headers, nil
}

// parseRetryBlock parses a retry configuration block on a connector.
//
// The comment here used to say that only "attempts" was honoured and the wait
// was a fixed exponential backoff. That stopped being true: the http connector
// reads delay, max_delay and backoff, so the note was telling people not to
// write settings that work. The vocabulary is kept aligned with the flow-level
// retry block (parseRetryConfigBlock) so users see one consistent name.
func parseRetryBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	// Same vocabulary as error_handling.retry, deliberately: two blocks called
	// retry that accept different words is a trap. The documented `count` /
	// `interval` spelling is not accepted — see TestConnectorRetryBlockRejectsCount.
	//
	// The webhook connector grew its own words for the same three settings —
	// max_attempts, initial_delay and multiplier — which the parser refused, so
	// none of them could be written. They are accepted and folded onto the
	// canonical names, the same way the tls block handles its older spellings.
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "attempts"},
			{Name: "delay"},
			{Name: "max_delay"},
			{Name: "backoff"},
			{Name: "max_attempts"},  // webhook's word for attempts
			{Name: "initial_delay"}, // webhook's word for delay
			// Webhook-specific, with no equivalent in the shared vocabulary.
			{Name: "multiplier"},
			{Name: "retryable_statuses"},
		},
	}

	retryAliases := map[string]string{
		"max_attempts":  "attempts",
		"initial_delay": "delay",
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("retry block content error: %s", diags.Error())
	}

	for alias, canonical := range retryAliases {
		if _, wroteAlias := content.Attributes[alias]; !wroteAlias {
			continue
		}
		if _, wroteCanonical := content.Attributes[canonical]; wroteCanonical {
			return nil, fmt.Errorf("retry block sets both %q and %q, which are the same setting", canonical, alias)
		}
	}

	retry := make(map[string]interface{})
	for name, attr := range content.Attributes {
		if canonical, isAlias := retryAliases[name]; isAlias {
			name = canonical
		}
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("retry %s error: %s", name, diags.Error())
		}
		retry[name] = ctyValueToGo(val)
	}

	return retry, nil
}

// ctyValueToGo converts a cty.Value to a native Go value.
func ctyValueToGo(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}

	switch val.Type() {
	case cty.String:
		return val.AsString()

	case cty.Number:
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return int(i)
		}
		f, _ := bf.Float64()
		return f

	case cty.Bool:
		return val.True()

	default:
		// Handle lists
		if val.Type().IsListType() || val.Type().IsTupleType() {
			var result []interface{}
			for it := val.ElementIterator(); it.Next(); {
				_, v := it.Element()
				result = append(result, ctyValueToGo(v))
			}
			return result
		}

		// Handle maps
		if val.Type().IsMapType() || val.Type().IsObjectType() {
			result := make(map[string]interface{})
			for it := val.ElementIterator(); it.Next(); {
				k, v := it.Element()
				result[k.AsString()] = ctyValueToGo(v)
			}
			return result
		}

		return val.GoString()
	}
}

// parseOperationBlock parses an operation block inside a connector.
func parseOperationBlock(block *hcl.Block, ctx *hcl.EvalContext) (*connector.OperationDef, error) {
	opName := block.Labels[0]

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			// Common
			{Name: "description"},
			{Name: "input"},
			{Name: "output"},
			{Name: "timeout"},

			// REST-specific
			{Name: "method"},
			{Name: "path"},

			// Database-specific
			{Name: "query"},
			{Name: "table"},

			// GraphQL-specific
			{Name: "operation_type"}, // Query, Mutation, Subscription
			{Name: "field"},

			// gRPC-specific
			{Name: "service"},
			{Name: "rpc"},

			// MQ-specific
			{Name: "exchange"},
			{Name: "routing_key"},
			{Name: "queue"},

			// TCP-specific
			{Name: "protocol"},
			{Name: "action"},

			// File/S3-specific
			{Name: "path_pattern"},

			// Cache-specific
			{Name: "key_pattern"},
			{Name: "ttl"},

			// Exec-specific
			{Name: "command"},
			{Name: "args"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "param", LabelNames: []string{"name"}},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("operation content error: %s", diags.Error())
	}

	operation := &connector.OperationDef{
		Name:   opName,
		Params: make([]*connector.ParamDef, 0),
	}

	// Parse attributes
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}

		// Every string attribute below goes through the same reader: AsString
		// on a number or a boolean panics the binary at startup.
		text := func() (string, error) { return stringValue(name, val) }
		assign := func(target *string) error {
			s, err := text()
			if err != nil {
				return fmt.Errorf("operation %q: %w", operation.Name, err)
			}
			*target = s
			return nil
		}

		switch name {
		// Common
		case "description":
			if err := assign(&operation.Description); err != nil {
				return nil, err
			}
		case "input":
			if err := assign(&operation.Input); err != nil {
				return nil, err
			}
		case "output":
			if err := assign(&operation.Output); err != nil {
				return nil, err
			}
		case "timeout":
			operation.Timeout = toInt(ctyValueToGo(val))

		// REST
		case "method":
			if err := assign(&operation.Method); err != nil {
				return nil, err
			}
		case "path":
			if err := assign(&operation.Path); err != nil {
				return nil, err
			}

		// Database
		case "query":
			if err := assign(&operation.Query); err != nil {
				return nil, err
			}
		case "table":
			if err := assign(&operation.Table); err != nil {
				return nil, err
			}

		// GraphQL
		case "operation_type":
			if err := assign(&operation.OperationType); err != nil {
				return nil, err
			}
		case "field":
			if err := assign(&operation.Field); err != nil {
				return nil, err
			}

		// gRPC
		case "service":
			if err := assign(&operation.Service); err != nil {
				return nil, err
			}
		case "rpc":
			if err := assign(&operation.RPC); err != nil {
				return nil, err
			}

		// MQ
		case "exchange":
			if err := assign(&operation.Exchange); err != nil {
				return nil, err
			}
		case "routing_key":
			if err := assign(&operation.RoutingKey); err != nil {
				return nil, err
			}
		case "queue":
			if err := assign(&operation.Queue); err != nil {
				return nil, err
			}

		// TCP
		case "protocol":
			if err := assign(&operation.Protocol); err != nil {
				return nil, err
			}
		case "action":
			if err := assign(&operation.Action); err != nil {
				return nil, err
			}

		// File/S3
		case "path_pattern":
			if err := assign(&operation.PathPattern); err != nil {
				return nil, err
			}

		// Cache
		case "key_pattern":
			if err := assign(&operation.KeyPattern); err != nil {
				return nil, err
			}
		case "ttl":
			operation.TTL = toInt(ctyValueToGo(val))

		// Exec
		case "command":
			if err := assign(&operation.Command); err != nil {
				return nil, err
			}
		case "args":
			args := ctyValueToGo(val)
			if arr, ok := args.([]interface{}); ok {
				for _, a := range arr {
					if s, ok := a.(string); ok {
						operation.Args = append(operation.Args, s)
					}
				}
			}
		}
	}

	// Parse param blocks
	for _, paramBlock := range content.Blocks {
		if paramBlock.Type == "param" {
			if len(paramBlock.Labels) < 1 {
				return nil, fmt.Errorf("param block requires a name label")
			}
			param, err := parseParamBlock(paramBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("param %s error: %w", paramBlock.Labels[0], err)
			}
			operation.Params = append(operation.Params, param)
		}
	}

	return operation, nil
}

// parseParamBlock parses a param block inside an operation.
func parseParamBlock(block *hcl.Block, ctx *hcl.EvalContext) (*connector.ParamDef, error) {
	paramName := block.Labels[0]

	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type"},
			{Name: "required"},
			{Name: "default"},
			{Name: "description"},
			{Name: "in"},
			{Name: "min"},
			{Name: "max"},
			{Name: "min_length"},
			{Name: "max_length"},
			{Name: "pattern"},
			{Name: "enum"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("param content error: %s", diags.Error())
	}

	param := &connector.ParamDef{
		Name: paramName,
	}

	// Parse attributes
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("attribute %s error: %s", name, diags.Error())
		}

		switch name {
		case "type":
			param.Type = val.AsString()
		case "required":
			param.Required = val.True()
		case "default":
			param.Default = ctyValueToGo(val)
		case "description":
			param.Description = val.AsString()
		case "in":
			param.In = val.AsString()
		case "min":
			v := toFloat64(ctyValueToGo(val))
			param.Min = &v
		case "max":
			v := toFloat64(ctyValueToGo(val))
			param.Max = &v
		case "min_length":
			v := toInt(ctyValueToGo(val))
			param.MinLength = &v
		case "max_length":
			v := toInt(ctyValueToGo(val))
			param.MaxLength = &v
		case "pattern":
			param.Pattern = val.AsString()
		case "enum":
			enumVals := ctyValueToGo(val)
			if arr, ok := enumVals.([]interface{}); ok {
				for _, e := range arr {
					if s, ok := e.(string); ok {
						param.Enum = append(param.Enum, s)
					}
				}
			}
		}
	}

	return param, nil
}

// applyConnectorBlock reads one nested block of a connector into its config.
//
// A profile is a connector — the same settings, chosen at runtime — so it is
// parsed by this rather than by a second list beside it. The two used to be
// separate, and the profile list had 40 attributes against the connector's 159:
// a profile could not name a queue, a vhost, a path or a command, and could not
// carry a consumer, retry or tls block at all. Per-environment configuration is
// exactly what profiles are for, so that gap was in the way of the recommended
// way to do it.
//
// Returns false for a block type it does not know, so a caller can handle its
// own (a profile has a transform; a connector has profiles).
func applyConnectorBlock(config *connector.Config, nestedBlock *hcl.Block, ctx *hcl.EvalContext) (bool, error) {
	switch nestedBlock.Type {
	case "pool":
		pool, err := parsePoolBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("pool block error: %w", err)
		}
		config.Properties["pool"] = pool

	case "cors":
		cors, err := parseCorsBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("cors block error: %w", err)
		}
		config.Properties["cors"] = cors

	case "auth":
		auth, err := parseAuthBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("auth block error: %w", err)
		}
		config.Properties["auth"] = auth

	case "retry":
		retry, err := parseRetryBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("retry block error: %w", err)
		}
		config.Properties["retry"] = retry

	case "headers":
		headers, err := parseHeadersBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("headers block error: %w", err)
		}
		config.Properties["headers"] = headers

	case "schema":
		schema, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("schema block error: %w", err)
		}
		config.Properties["schema"] = schema

	case "ssh":
		ssh, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("ssh block error: %w", err)
		}
		config.Properties["ssh"] = ssh

	case "tls":
		tls, err := parseTLSBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("tls block error: %w", err)
		}
		config.Properties["tls"] = tls

	case "env":
		// exec reads Properties["env"] as a map of NAME to value, so the
		// block is collected rather than given a fixed set of attributes.
		env, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("env block error: %w", err)
		}
		config.Properties["env"] = env

	case "replicas":
		// One block per replica. The SQL factories read this as a list of
		// maps, so repeated blocks are collected into one.
		replica, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("replicas block error: %w", err)
		}
		existing, _ := config.Properties["replicas"].([]interface{})
		config.Properties["replicas"] = append(existing, replica)

	case "queue":
		queue, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("queue block error: %w", err)
		}
		config.Properties["queue"] = queue

	case "exchange":
		exchange, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("exchange block error: %w", err)
		}
		config.Properties["exchange"] = exchange

	case "publisher":
		pub, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("publisher block error: %w", err)
		}
		config.Properties["publisher"] = pub

	case "consumer":
		consumer, err := parseConsumerBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("consumer block error: %w", err)
		}
		config.Properties["consumer"] = consumer

	case "producer":
		producer, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("producer block error: %w", err)
		}
		config.Properties["producer"] = producer

	case "federation":
		federation, err := parseFederationBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("federation block error: %w", err)
		}
		config.Properties["federation"] = federation

	case "subscriptions":
		subscriptions, err := parseSubscriptionsBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("subscriptions block error: %w", err)
		}
		config.Properties["subscriptions"] = subscriptions

	case "profile":
		if len(nestedBlock.Labels) < 1 {
			return false, fmt.Errorf("profile block requires a name label")
		}
		profileDef, err := parseProfileBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("profile %s error: %w", nestedBlock.Labels[0], err)
		}

		// Initialize profiles map if needed
		profiles, ok := config.Properties["_profiles"].(*profile.Config)
		if !ok {
			profiles = &profile.Config{
				Profiles: make(map[string]*profile.ProfileDef),
			}
			config.Properties["_profiles"] = profiles
		}
		profiles.Profiles[profileDef.Name] = profileDef

	// Redis Cluster block
	case "cluster":
		cluster, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("cluster block error: %w", err)
		}
		config.Properties["cluster"] = cluster

	// Redis Sentinel block
	case "sentinel":
		sentinel, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("sentinel block error: %w", err)
		}
		config.Properties["sentinel"] = sentinel

	// gRPC keep-alive block
	case "keep_alive":
		keepAlive, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("keep_alive block error: %w", err)
		}
		config.Properties["keep_alive"] = keepAlive

	// gRPC load balancing block
	case "load_balancing":
		loadBalancing, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("load_balancing block error: %w", err)
		}
		config.Properties["load_balancing"] = loadBalancing

	// Kafka SASL block
	case "sasl":
		sasl, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("sasl block error: %w", err)
		}
		config.Properties["sasl"] = sasl

	// Kafka Schema Registry block
	case "schema_registry":
		schemaRegistry, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("schema_registry block error: %w", err)
		}
		config.Properties["schema_registry"] = schemaRegistry

	case "batch":
		batch, err := parseGenericBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("batch block error: %w", err)
		}
		config.Properties["batch"] = batch

	// Named operations
	case "operation":
		if len(nestedBlock.Labels) < 1 {
			return false, fmt.Errorf("operation block requires a name label")
		}
		operation, err := parseOperationBlock(nestedBlock, ctx)
		if err != nil {
			return false, fmt.Errorf("operation %s error: %w", nestedBlock.Labels[0], err)
		}
		config.Operations = append(config.Operations, operation)
	default:
		return false, nil
	}
	return true, nil
}

// connectorContent reads a connector block, returning the attributes this
// runtime knows and, separately, the ones it does not.
//
// Unknown attributes are an error for a connector of a type Mycel ships: the
// list is the only thing standing between a mistyped setting and a service
// that starts and quietly ignores it. For a type that arrives with a plugin
// there is no list to check against — the plugin's manifest declares its own
// attributes, and the parser has never read it — so they are carried through
// to the connector's properties, which is where the plugin is handed them.
func connectorContent(block *hcl.Block, bodySchema *hcl.BodySchema, ctx *hcl.EvalContext) (*hcl.BodyContent, map[string]interface{}, hcl.Diagnostics) {
	content, _, diags := block.Body.PartialContent(bodySchema)
	if diags.HasErrors() {
		return nil, nil, diags
	}

	leftover := unconsumedAttributes(block.Body, content)
	if len(leftover) == 0 {
		return content, nil, nil
	}

	// The type decides whether anything unknown is a mistake. Read strictly
	// again so the message names the attribute and its position exactly as it
	// always has.
	if isBuiltInConnectorType(connectorTypeOf(content, ctx)) {
		_, strictDiags := block.Body.Content(bodySchema)
		if !strictDiags.HasErrors() {
			// Cannot happen — something was left over — but returning no
			// content and no error would take the process down.
			strictDiags = append(strictDiags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Unsupported argument",
				Detail:   fmt.Sprintf("connector %q: %s is not an argument this connector type accepts", block.Labels[0], anyKey(leftover)),
				Subject:  block.DefRange.Ptr(),
			})
		}
		return nil, nil, strictDiags
	}

	extra := make(map[string]interface{}, len(leftover))
	for name, attr := range leftover {
		val, valDiags := attr.Expr.Value(ctx)
		if valDiags.HasErrors() {
			return nil, nil, valDiags
		}
		extra[name] = ctyValueToGo(val)
	}
	return content, extra, nil
}

// unconsumedAttributes is what the block holds that the schema did not name.
//
// The remaining body handed back by PartialContent cannot simply be asked for
// its attributes: for the syntax the parser reads, that field still holds every
// attribute in the block, and JustAttributes refuses a body with any nested
// block in it — which a connector nearly always has. Taking the difference is
// the reliable form.
func unconsumedAttributes(body hcl.Body, content *hcl.BodyContent) hcl.Attributes {
	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil
	}

	leftover := make(hcl.Attributes)
	for name, attr := range syntaxBody.Attributes {
		if _, consumed := content.Attributes[name]; consumed {
			continue
		}
		leftover[name] = attr.AsHCLAttribute()
	}
	return leftover
}

// anyKey names one of them, for a message that would otherwise say nothing.
func anyKey(attrs hcl.Attributes) string {
	for name := range attrs {
		return name
	}
	return ""
}

// connectorTypeOf reads the type attribute, which decides how strictly the
// rest of the block is read.
func connectorTypeOf(content *hcl.BodyContent, ctx *hcl.EvalContext) string {
	attr, ok := content.Attributes["type"]
	if !ok {
		return ""
	}
	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return ""
	}
	typeName, err := stringValue("type", val)
	if err != nil {
		return ""
	}
	return typeName
}

// isBuiltInConnectorType reports whether the runtime ships this type.
//
// A connector with no type at all is a profiled one, whose type lives inside
// its profiles — it is held to the list, the way it always has been.
func isBuiltInConnectorType(typeName string) bool {
	if typeName == "" {
		return true
	}
	for _, known := range schema.ConnectorTypeNames() {
		if known == typeName {
			return true
		}
	}
	return false
}
