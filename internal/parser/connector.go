package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/connector/profile"
)

// parseConnectorBlock parses a connector block from HCL.
func parseConnectorBlock(block *hcl.Block, ctx *hcl.EvalContext) (*connector.Config, error) {
	if len(block.Labels) < 1 {
		return nil, fmt.Errorf("connector block requires a name label")
	}

	config := &connector.Config{
		Name:       block.Labels[0],
		Properties: make(map[string]interface{}),
	}

	schema := &hcl.BodySchema{
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
			{Name: "address"},     // Redis address

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
			{Name: "max_pool"},     // Max pool size
			{Name: "min_pool"},     // Min pool size
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
			{Name: "from"},       // From email address
			{Name: "from_name"},  // From display name
			{Name: "reply_to"},   // Reply-to address
			{Name: "api_key"},    // SendGrid API key
			{Name: "pool_size"},  // Connection pool size

			// Slack/Discord connector specific
			{Name: "webhook_url"}, // Webhook URL
			{Name: "token"},       // Bot token
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
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "pool"},
			{Type: "cors"},
			{Type: "auth"},
			{Type: "retry"},
			{Type: "mock"},
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
			// Named operations
			{Type: "operation", LabelNames: []string{"name"}}, // Named operations for flows
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("connector content error: %s", diags.Error())
	}

	// Parse required type attribute
	if attr, ok := content.Attributes["type"]; ok {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("type attribute error: %s", diags.Error())
		}
		config.Type = val.AsString()
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
			config.Driver = val.AsString()
		}

		config.Properties[name] = ctyValueToGo(val)
	}

	// Parse nested blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "pool":
			pool, err := parsePoolBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("pool block error: %w", err)
			}
			config.Properties["pool"] = pool

		case "cors":
			cors, err := parseCorsBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("cors block error: %w", err)
			}
			config.Properties["cors"] = cors

		case "auth":
			auth, err := parseAuthBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("auth block error: %w", err)
			}
			config.Properties["auth"] = auth

		case "retry":
			retry, err := parseRetryBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("retry block error: %w", err)
			}
			config.Properties["retry"] = retry

		case "mock":
			mock, err := parseMockBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("mock block error: %w", err)
			}
			config.Properties["mock"] = mock

		case "headers":
			headers, err := parseHeadersBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("headers block error: %w", err)
			}
			config.Properties["headers"] = headers

		case "schema":
			schema, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("schema block error: %w", err)
			}
			config.Properties["schema"] = schema

		case "ssh":
			ssh, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("ssh block error: %w", err)
			}
			config.Properties["ssh"] = ssh

		case "tls":
			tls, err := parseTLSBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("tls block error: %w", err)
			}
			config.Properties["tls"] = tls

		case "queue":
			queue, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("queue block error: %w", err)
			}
			config.Properties["queue"] = queue

		case "exchange":
			exchange, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("exchange block error: %w", err)
			}
			config.Properties["exchange"] = exchange

		case "publisher":
			pub, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("publisher block error: %w", err)
			}
			config.Properties["publisher"] = pub

		case "consumer":
			consumer, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("consumer block error: %w", err)
			}
			config.Properties["consumer"] = consumer

		case "producer":
			producer, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("producer block error: %w", err)
			}
			config.Properties["producer"] = producer

		case "federation":
			federation, err := parseFederationBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("federation block error: %w", err)
			}
			config.Properties["federation"] = federation

		case "subscriptions":
			subscriptions, err := parseSubscriptionsBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("subscriptions block error: %w", err)
			}
			config.Properties["subscriptions"] = subscriptions

		case "profile":
			if len(nestedBlock.Labels) < 1 {
				return nil, fmt.Errorf("profile block requires a name label")
			}
			profileDef, err := parseProfileBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("profile %s error: %w", nestedBlock.Labels[0], err)
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
				return nil, fmt.Errorf("cluster block error: %w", err)
			}
			config.Properties["cluster"] = cluster

		// Redis Sentinel block
		case "sentinel":
			sentinel, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("sentinel block error: %w", err)
			}
			config.Properties["sentinel"] = sentinel

		// gRPC keep-alive block
		case "keep_alive":
			keepAlive, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("keep_alive block error: %w", err)
			}
			config.Properties["keep_alive"] = keepAlive

		// gRPC load balancing block
		case "load_balancing":
			loadBalancing, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("load_balancing block error: %w", err)
			}
			config.Properties["load_balancing"] = loadBalancing

		// Kafka SASL block
		case "sasl":
			sasl, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("sasl block error: %w", err)
			}
			config.Properties["sasl"] = sasl

		// Kafka Schema Registry block
		case "schema_registry":
			schemaRegistry, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("schema_registry block error: %w", err)
			}
			config.Properties["schema_registry"] = schemaRegistry

		// Named operations
		case "operation":
			if len(nestedBlock.Labels) < 1 {
				return nil, fmt.Errorf("operation block requires a name label")
			}
			operation, err := parseOperationBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("operation %s error: %w", nestedBlock.Labels[0], err)
			}
			config.Operations = append(config.Operations, operation)
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

	// Profile uses the same schema as a regular connector plus transform
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type", Required: true},
			{Name: "driver"},
			{Name: "host"},
			{Name: "port"},
			{Name: "database"},
			{Name: "user"},
			{Name: "username"},
			{Name: "password"},
			{Name: "base_url"},
			{Name: "timeout"},
			{Name: "retry_count"},
			{Name: "endpoint"},
			{Name: "playground"},
			{Name: "brokers"},
			{Name: "uri"},
			{Name: "url"},
			{Name: "address"},
			{Name: "bucket"},
			{Name: "region"},
			{Name: "access_key"},
			{Name: "secret_key"},
			{Name: "charset"},
			{Name: "ssl_mode"},
			{Name: "sslmode"},
			// Cache attributes
			{Name: "mode"},
			{Name: "prefix"},
			{Name: "max_items"},
			{Name: "eviction"},
			{Name: "default_ttl"},
			// gRPC attributes
			{Name: "proto_path"},
			{Name: "proto_files"},
			{Name: "reflection"},
			{Name: "target"},
			{Name: "insecure"},
			// File attributes
			{Name: "base_path"},
			{Name: "format"},
			{Name: "permissions"},
			// S3 attributes
			{Name: "use_path_style"},
			// MongoDB attributes
			{Name: "replica_set"},
			{Name: "auth_source"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "pool"},
			{Type: "auth"},
			{Type: "headers"},
			{Type: "transform"},
			{Type: "tls"},
			{Type: "cluster"},
			{Type: "sentinel"},
		},
	}

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

	profileDef := &profile.ProfileDef{
		Name:            profileName,
		ConnectorConfig: connConfig,
		Transform:       make(map[string]string),
	}

	// Parse nested blocks
	for _, nestedBlock := range content.Blocks {
		switch nestedBlock.Type {
		case "pool":
			pool, err := parsePoolBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("pool block error: %w", err)
			}
			connConfig.Properties["pool"] = pool

		case "auth":
			auth, err := parseAuthBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("auth block error: %w", err)
			}
			connConfig.Properties["auth"] = auth

		case "headers":
			headers, err := parseHeadersBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("headers block error: %w", err)
			}
			connConfig.Properties["headers"] = headers

		case "transform":
			transform, err := parseProfileTransformBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("transform block error: %w", err)
			}
			profileDef.Transform = transform

		case "tls":
			tls, err := parseTLSBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("tls block error: %w", err)
			}
			connConfig.Properties["tls"] = tls

		case "cluster":
			cluster, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("cluster block error: %w", err)
			}
			connConfig.Properties["cluster"] = cluster

		case "sentinel":
			sentinel, err := parseGenericBlock(nestedBlock, ctx)
			if err != nil {
				return nil, fmt.Errorf("sentinel block error: %w", err)
			}
			connConfig.Properties["sentinel"] = sentinel
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
		// Store as string (CEL expression)
		transform[name] = val.AsString()
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

// parseRetryBlock parses a retry configuration block.
func parseRetryBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "attempts"},
			{Name: "backoff"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("retry block content error: %s", diags.Error())
	}

	retry := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("retry %s error: %s", name, diags.Error())
		}
		retry[name] = ctyValueToGo(val)
	}

	return retry, nil
}

// parseTLSBlock parses a TLS configuration block for HTTP clients.
func parseTLSBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "ca_cert"},
			{Name: "client_cert"},
			{Name: "client_key"},
			{Name: "insecure_skip_verify"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("tls block content error: %s", diags.Error())
	}

	tls := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("tls %s error: %s", name, diags.Error())
		}
		tls[name] = ctyValueToGo(val)
	}

	return tls, nil
}

// parseMockBlock parses a mock configuration block.
func parseMockBlock(block *hcl.Block, ctx *hcl.EvalContext) (map[string]interface{}, error) {
	schema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "source"},
		},
	}

	content, diags := block.Body.Content(schema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("mock block content error: %s", diags.Error())
	}

	mock := make(map[string]interface{})
	for name, attr := range content.Attributes {
		val, diags := attr.Expr.Value(ctx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("mock %s error: %s", name, diags.Error())
		}
		mock[name] = ctyValueToGo(val)
	}

	return mock, nil
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

		switch name {
		// Common
		case "description":
			operation.Description = val.AsString()
		case "input":
			operation.Input = val.AsString()
		case "output":
			operation.Output = val.AsString()
		case "timeout":
			operation.Timeout = toInt(ctyValueToGo(val))

		// REST
		case "method":
			operation.Method = val.AsString()
		case "path":
			operation.Path = val.AsString()

		// Database
		case "query":
			operation.Query = val.AsString()
		case "table":
			operation.Table = val.AsString()

		// GraphQL
		case "operation_type":
			operation.OperationType = val.AsString()
		case "field":
			operation.Field = val.AsString()

		// gRPC
		case "service":
			operation.Service = val.AsString()
		case "rpc":
			operation.RPC = val.AsString()

		// MQ
		case "exchange":
			operation.Exchange = val.AsString()
		case "routing_key":
			operation.RoutingKey = val.AsString()
		case "queue":
			operation.Queue = val.AsString()

		// TCP
		case "protocol":
			operation.Protocol = val.AsString()
		case "action":
			operation.Action = val.AsString()

		// File/S3
		case "path_pattern":
			operation.PathPattern = val.AsString()

		// Cache
		case "key_pattern":
			operation.KeyPattern = val.AsString()
		case "ttl":
			operation.TTL = toInt(ctyValueToGo(val))

		// Exec
		case "command":
			operation.Command = val.AsString()
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
