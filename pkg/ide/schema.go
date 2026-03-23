package ide

// AttrType describes the expected type of an attribute value.
type AttrType string

const (
	AttrString AttrType = "string"
	AttrNumber AttrType = "number"
	AttrBool   AttrType = "bool"
	AttrMap    AttrType = "map"
	AttrList   AttrType = "list"
)

// RefKind indicates what kind of entity an attribute references.
type RefKind int

const (
	RefNone RefKind = iota
	RefConnector
	RefType
	RefTransform
	RefCache
	RefValidator
	RefFlow
	RefStateMachine
)

// BlockSchema describes the valid structure of an HCL block.
type BlockSchema struct {
	Type     string
	Doc      string
	Labels   int  // number of labels (0 or 1)
	Open     bool // if true, allows any attribute (connector params, step params, type fields)
	Attrs    []AttrSchema
	Children []BlockSchema
}

// AttrSchema describes a valid attribute within a block.
type AttrSchema struct {
	Name     string
	Doc      string
	Type     AttrType
	Required bool
	Values   []string // enumerated valid values
	Ref      RefKind  // reference to another entity
}

// rootSchema returns the schema for all top-level blocks.
func rootSchema() []BlockSchema {
	return []BlockSchema{
		connectorSchema(),
		flowSchema(),
		typeSchema(),
		transformSchema(),
		aspectSchema(),
		serviceSchema(),
		{Type: "validator", Doc: "Custom validation rule (regex, CEL, or WASM)", Labels: 1},
		{Type: "saga", Doc: "Distributed transaction with automatic compensation", Labels: 1},
		{Type: "state_machine", Doc: "Entity lifecycle with guards, actions, and final states", Labels: 1},
		{Type: "functions", Doc: "Custom CEL functions", Labels: 1},
		{Type: "plugin", Doc: "WASM plugin for extending Mycel", Labels: 1},
		{Type: "auth", Doc: "Authentication configuration"},
		{Type: "security", Doc: "Security and sanitization rules"},
		{Type: "mocks", Doc: "Mock data for testing"},
		{Type: "cache", Doc: "Named cache definition", Labels: 1},
		{Type: "environment", Doc: "Environment-specific variables", Labels: 1},
	}
}

func connectorSchema() BlockSchema {
	return BlockSchema{
		Type:   "connector",
		Doc:    "Bidirectional adapter for databases, APIs, queues, and other services",
		Labels: 1,
		Attrs: []AttrSchema{
			{Name: "type", Doc: "Connector type", Type: AttrString, Required: true, Values: connectorTypes()},
			{Name: "driver", Doc: "Driver for the connector type", Type: AttrString, Values: connectorDrivers()},
			{Name: "host", Doc: "Server hostname or address", Type: AttrString},
			{Name: "port", Doc: "Server port", Type: AttrNumber},
			{Name: "database", Doc: "Database name or path", Type: AttrString},
			{Name: "user", Doc: "Authentication username", Type: AttrString},
			{Name: "username", Doc: "Authentication username", Type: AttrString},
			{Name: "password", Doc: "Authentication password", Type: AttrString},
			{Name: "url", Doc: "Connection URL", Type: AttrString},
			{Name: "token", Doc: "Authentication token", Type: AttrString},
			{Name: "channel", Doc: "Channel name (Slack, Discord)", Type: AttrString},
			{Name: "api_url", Doc: "API base URL", Type: AttrString},
			{Name: "base_url", Doc: "Base URL for HTTP client", Type: AttrString},
			{Name: "path", Doc: "File system path", Type: AttrString},
			{Name: "bucket", Doc: "S3 bucket name", Type: AttrString},
			{Name: "region", Doc: "Cloud region", Type: AttrString},
			{Name: "vhost", Doc: "RabbitMQ virtual host", Type: AttrString},
			{Name: "template", Doc: "Template file path (PDF, email)", Type: AttrString},
		},
		Children: []BlockSchema{
			{Type: "pool", Doc: "Connection pool settings"},
			{Type: "consumer", Doc: "Message queue consumer settings"},
			{Type: "queue", Doc: "Queue declaration settings"},
			{Type: "exchange", Doc: "Exchange declaration settings"},
			{Type: "tls", Doc: "TLS/SSL settings"},
			{Type: "cors", Doc: "CORS settings (REST)"},
			{Type: "retry", Doc: "Connection retry settings"},
		},
	}
}

func flowSchema() BlockSchema {
	return BlockSchema{
		Type:   "flow",
		Doc:    "Data flow from source to destination",
		Labels: 1,
		Attrs: []AttrSchema{
			{Name: "returns", Doc: "GraphQL return type (HCL-first mode)", Type: AttrString},
			{Name: "when", Doc: "Trigger schedule (cron or @every)", Type: AttrString},
			{Name: "entity", Doc: "Federation entity resolver type name", Type: AttrString},
			{Name: "cache", Doc: "Reference to named cache", Type: AttrString, Ref: RefCache},
		},
		Children: []BlockSchema{
			fromSchema(),
			toSchema(),
			acceptSchema(),
			stepSchema(),
			transformBlockSchema(),
			{Type: "response", Doc: "Transform output AFTER destination write"},
			validateSchema(),
			{Type: "enrich", Doc: "Data enrichment from external source", Labels: 1},
			{Type: "lock", Doc: "Mutex lock for this flow"},
			{Type: "semaphore", Doc: "Concurrency limiter for this flow"},
			{Type: "coordinate", Doc: "Signal/wait coordination"},
			{Type: "cache", Doc: "Cache configuration for this flow"},
			{Type: "require", Doc: "Authorization requirements"},
			{Type: "after", Doc: "Post-execution actions"},
			errorHandlingSchema(),
			{Type: "dedupe", Doc: "Deduplication configuration"},
			{Type: "idempotency", Doc: "Idempotency key configuration"},
			{Type: "async", Doc: "Async execution (202 + polling)"},
			{Type: "batch", Doc: "Batch processing configuration"},
			{Type: "state_transition", Doc: "State machine transition"},
		},
	}
}

func fromSchema() BlockSchema {
	return BlockSchema{
		Type: "from",
		Doc:  "Source connector and operation for this flow",
		Open: true, // accepts connector-specific params
		Attrs: []AttrSchema{
			{Name: "connector", Doc: "Source connector name", Type: AttrString, Required: true, Ref: RefConnector},
			{Name: "operation", Doc: "Source operation (e.g., GET /users, queue name)", Type: AttrString},
			{Name: "filter", Doc: "CEL expression to filter incoming messages", Type: AttrString},
			{Name: "on_reject", Doc: "What to do with filtered messages", Type: AttrString, Values: []string{"ack", "reject", "requeue"}},
			{Name: "format", Doc: "Input format (json, xml, csv)", Type: AttrString, Values: []string{"json", "xml", "csv"}},
		},
		Children: []BlockSchema{
			{Type: "filter", Doc: "Extended filter with rejection policy", Attrs: []AttrSchema{
				{Name: "condition", Doc: "CEL filter expression", Type: AttrString, Required: true},
				{Name: "on_reject", Doc: "Rejection policy", Type: AttrString, Values: []string{"ack", "reject", "requeue"}},
				{Name: "id_field", Doc: "CEL expression for message ID (requeue dedup)", Type: AttrString},
				{Name: "max_requeue", Doc: "Max requeue attempts", Type: AttrNumber},
			}},
		},
	}
}

func toSchema() BlockSchema {
	return BlockSchema{
		Type: "to",
		Doc:  "Destination connector and target for this flow",
		Open: true, // accepts connector-specific params (query_filter, update, body, params, etc.)
		Attrs: []AttrSchema{
			{Name: "connector", Doc: "Destination connector name", Type: AttrString, Required: true, Ref: RefConnector},
			{Name: "target", Doc: "Target resource (table, topic, endpoint)", Type: AttrString},
			{Name: "operation", Doc: "Target operation", Type: AttrString},
			{Name: "when", Doc: "CEL condition for conditional write", Type: AttrString},
			{Name: "parallel", Doc: "Write in parallel with other destinations", Type: AttrBool},
			{Name: "query", Doc: "SQL query for database writes", Type: AttrString},
			{Name: "format", Doc: "Output format (json, xml)", Type: AttrString, Values: []string{"json", "xml"}},
			{Name: "filter", Doc: "Per-user filter (WebSocket, SSE, subscriptions)", Type: AttrString},
		},
	}
}

func acceptSchema() BlockSchema {
	return BlockSchema{
		Type: "accept",
		Doc:  "Business-level gate after filter, before transform. Determines if this flow should process the message.",
		Attrs: []AttrSchema{
			{Name: "when", Doc: "CEL expression — must return true to proceed", Type: AttrString, Required: true},
			{Name: "on_reject", Doc: "What to do when condition is false", Type: AttrString, Values: []string{"ack", "reject", "requeue"}},
		},
	}
}

func stepSchema() BlockSchema {
	return BlockSchema{
		Type:   "step",
		Doc:    "Intermediate connector call — results available as step.<name>.* in transform",
		Labels: 1,
		Open:   true, // accepts connector-specific params (query, target, body, params, etc.)
		Attrs: []AttrSchema{
			{Name: "connector", Doc: "Connector to call", Type: AttrString, Required: true, Ref: RefConnector},
			{Name: "operation", Doc: "Operation to execute", Type: AttrString},
			{Name: "target", Doc: "Target resource", Type: AttrString},
			{Name: "query", Doc: "SQL query", Type: AttrString},
			{Name: "when", Doc: "CEL condition for conditional execution", Type: AttrString},
			{Name: "timeout", Doc: "Timeout duration (e.g., 5s)", Type: AttrString},
			{Name: "on_error", Doc: "Error handling: fail, skip, or default", Type: AttrString, Values: []string{"fail", "skip", "default"}},
		},
	}
}

func transformBlockSchema() BlockSchema {
	return BlockSchema{
		Type: "transform",
		Doc:  "CEL transformation rules applied to input before writing to destination",
		Attrs: []AttrSchema{
			{Name: "use", Doc: "Reference to named transform(s)", Type: AttrString, Ref: RefTransform},
		},
	}
}

func validateSchema() BlockSchema {
	return BlockSchema{
		Type: "validate",
		Doc:  "Input and output type validation",
		Attrs: []AttrSchema{
			{Name: "input", Doc: "Input type name for validation", Type: AttrString, Ref: RefType},
			{Name: "output", Doc: "Output type name for validation", Type: AttrString, Ref: RefType},
		},
	}
}

func errorHandlingSchema() BlockSchema {
	return BlockSchema{
		Type: "error_handling",
		Doc:  "Error handling with retry, fallback, and custom responses",
		Children: []BlockSchema{
			{Type: "retry", Doc: "Automatic retry on failure", Attrs: []AttrSchema{
				{Name: "attempts", Doc: "Maximum retry attempts", Type: AttrNumber},
				{Name: "delay", Doc: "Initial delay between retries", Type: AttrString},
				{Name: "max_delay", Doc: "Maximum delay (exponential backoff cap)", Type: AttrString},
				{Name: "backoff", Doc: "Backoff strategy", Type: AttrString, Values: []string{"linear", "exponential", "constant"}},
			}},
			{Type: "fallback", Doc: "Dead letter queue / fallback destination", Attrs: []AttrSchema{
				{Name: "connector", Doc: "Fallback connector", Type: AttrString, Ref: RefConnector},
				{Name: "target", Doc: "Fallback target", Type: AttrString},
				{Name: "include_error", Doc: "Include error details", Type: AttrBool},
			}},
			{Type: "error_response", Doc: "Custom HTTP error response", Attrs: []AttrSchema{
				{Name: "status", Doc: "HTTP status code", Type: AttrNumber},
			}},
		},
	}
}

func aspectSchema() BlockSchema {
	return BlockSchema{
		Type:   "aspect",
		Doc:    "Cross-cutting concern applied via flow name pattern matching (AOP)",
		Labels: 1,
		Attrs: []AttrSchema{
			{Name: "on", Doc: "Flow name patterns to match (glob)", Type: AttrList},
			{Name: "when", Doc: "When to execute: before, after, around, on_error", Type: AttrString, Values: []string{"before", "after", "around", "on_error"}},
			{Name: "if", Doc: "CEL condition for conditional execution", Type: AttrString},
			{Name: "priority", Doc: "Execution priority (lower = first)", Type: AttrNumber},
		},
		Children: []BlockSchema{
			{Type: "action", Doc: "Aspect action to execute", Attrs: []AttrSchema{
				{Name: "connector", Doc: "Connector to call", Type: AttrString, Ref: RefConnector},
				{Name: "flow", Doc: "Flow to invoke", Type: AttrString, Ref: RefFlow},
			}, Children: []BlockSchema{
				{Type: "transform", Doc: "Transform for the action payload"},
			}},
			{Type: "cache", Doc: "Cache configuration"},
			{Type: "invalidate", Doc: "Cache invalidation"},
			{Type: "rate_limit", Doc: "Rate limiting"},
			{Type: "circuit_breaker", Doc: "Circuit breaker"},
			{Type: "response", Doc: "Response modification (headers, fields)"},
		},
	}
}

func typeSchema() BlockSchema {
	return BlockSchema{
		Type:   "type",
		Doc:    "Schema definition for input/output validation",
		Labels: 1,
		Open:   true, // fields are user-defined (email = string { format = "email" })
	}
}

func transformSchema() BlockSchema {
	return BlockSchema{
		Type:   "transform",
		Doc:    "Reusable named transformation (CEL expressions)",
		Labels: 1,
	}
}

func serviceSchema() BlockSchema {
	return BlockSchema{
		Type: "service",
		Doc:  "Global service configuration",
		Attrs: []AttrSchema{
			{Name: "name", Doc: "Service name", Type: AttrString, Required: true},
			{Name: "version", Doc: "Service version", Type: AttrString},
			{Name: "admin_port", Doc: "Admin server port (health, metrics, debug)", Type: AttrNumber},
		},
	}
}

// connectorTypes returns all valid connector type values.
func connectorTypes() []string {
	return []string{
		"rest", "database", "mq", "graphql", "grpc", "file", "s3",
		"cache", "tcp", "exec", "soap", "mqtt", "ftp", "cdc",
		"websocket", "sse", "elasticsearch", "oauth",
		"email", "slack", "discord", "sms", "push", "webhook", "pdf",
	}
}

// connectorDrivers returns all valid driver values across all connector types.
func connectorDrivers() []string {
	return []string{
		"sqlite", "postgres", "mysql", "mongodb",
		"rabbitmq", "kafka", "redis",
		"memory", "json", "msgpack", "nestjs",
	}
}

// lookupBlockSchema finds the schema for a block at the given path.
// For example, path ["flow", "from"] returns the fromSchema.
func lookupBlockSchema(path []string) *BlockSchema {
	schemas := rootSchema()
	var current *BlockSchema

	for _, segment := range path {
		found := false
		for i := range schemas {
			if schemas[i].Type == segment {
				current = &schemas[i]
				schemas = current.Children
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return current
}

// validRootBlockTypes returns the type names of all valid root-level blocks.
func validRootBlockTypes() []string {
	schemas := rootSchema()
	types := make([]string, len(schemas))
	for i, s := range schemas {
		types[i] = s.Type
	}
	return types
}
