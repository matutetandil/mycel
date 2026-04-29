package schema

// BuiltinRootSchemas returns the schemas for all root-level block types.
// This is the canonical source of truth for the Mycel HCL structure.
func BuiltinRootSchemas() []Block {
	return []Block{
		BaseConnectorSchema(),
		FlowSchema(),
		TypeSchema(),
		TransformSchema(),
		AspectSchema(),
		ServiceSchema(),
		ValidatorSchema(),
		SagaSchema(),
		StateMachineSchema(),
		FunctionsSchema(),
		PluginSchema(),
		AuthSchema(),
		SecuritySchema(),
		MocksSchema(),
		CacheDefSchema(),
		EnvironmentSchema(),
	}
}

// --- Flow and sub-blocks ---

func FlowSchema() Block {
	return Block{
		Type:   "flow",
		Doc:    "Data flow from source to destination",
		Labels: 1,
		Attrs: []Attr{
			{Name: "returns", Doc: "GraphQL return type (HCL-first mode)", Type: TypeString},
			{Name: "when", Doc: "Trigger schedule (cron or @every)", Type: TypeString},
			{Name: "entity", Doc: "Federation entity resolver type name", Type: TypeString},
			{Name: "cache", Doc: "Reference to named cache", Type: TypeString, Ref: RefCache},
		},
		Children: []Block{
			FromSchema(),
			ToSchema(),
			AcceptSchema(),
			StepSchema(),
			TransformBlockSchema(),
			ResponseBlockSchema(),
			ValidateBlockSchema(),
			EnrichSchema(),
			LockSchema(),
			SemaphoreSchema(),
			CoordinateSchema(),
			FlowCacheSchema(),
			RequireSchema(),
			AfterSchema(),
			ErrorHandlingSchema(),
			DedupeSchema(),
			IdempotencySchema(),
			AsyncSchema(),
			BatchSchema(),
			StateTransitionSchema(),
		},
	}
}

func FromSchema() Block {
	return Block{
		Type: "from",
		Doc:  "Source connector and operation for this flow",
		Open: true, // accepts connector-specific params
		Attrs: []Attr{
			{Name: "connector", Doc: "Source connector name", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "operation", Doc: "Source operation (e.g., GET /users, queue name)", Type: TypeString},
			{Name: "filter", Doc: "CEL expression to filter incoming messages", Type: TypeString},
			{Name: "on_reject", Doc: "What to do with filtered messages", Type: TypeString, Values: []string{"ack", "reject", "requeue"}},
			{Name: "format", Doc: "Input format", Type: TypeString, Values: []string{"json", "xml", "csv", "tsv"}},
		},
		Children: []Block{
			FilterBlockSchema(),
		},
	}
}

func FilterBlockSchema() Block {
	return Block{
		Type: "filter",
		Doc:  "Extended filter with rejection policy",
		Attrs: []Attr{
			{Name: "condition", Doc: "CEL filter expression", Type: TypeString, Required: true},
			{Name: "on_reject", Doc: "Rejection policy", Type: TypeString, Values: []string{"ack", "reject", "requeue"}},
			{Name: "id_field", Doc: "CEL expression for message ID (requeue dedup)", Type: TypeString},
			{Name: "max_requeue", Doc: "Max requeue attempts", Type: TypeNumber},
		},
	}
}

func ToSchema() Block {
	return Block{
		Type: "to",
		Doc:  "Destination connector and target for this flow",
		Open: true, // accepts connector-specific params
		Attrs: []Attr{
			{Name: "connector", Doc: "Destination connector name", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "target", Doc: "Target resource (table, topic, endpoint)", Type: TypeString},
			{Name: "operation", Doc: "Target operation", Type: TypeString},
			{Name: "when", Doc: "CEL condition for conditional write", Type: TypeString},
			{Name: "parallel", Doc: "Write in parallel with other destinations", Type: TypeBool},
			{Name: "envelope", Doc: "Wrap the outgoing payload under a single root key (Magento webapi / Spring @RequestBody / SOAP-style REST)", Type: TypeString},
			{Name: "query", Doc: "SQL query for database writes", Type: TypeString},
			{Name: "format", Doc: "Output format", Type: TypeString, Values: []string{"json", "xml", "csv", "tsv"}},
			{Name: "filter", Doc: "Per-user filter (WebSocket, SSE, subscriptions)", Type: TypeString},
		},
	}
}

func AcceptSchema() Block {
	return Block{
		Type: "accept",
		Doc:  "Business-level gate after filter, before transform. Determines if this flow should process the message.",
		Attrs: []Attr{
			{Name: "when", Doc: "CEL expression — must return true to proceed", Type: TypeString, Required: true},
			{Name: "on_reject", Doc: "What to do when condition is false", Type: TypeString, Values: []string{"ack", "reject", "requeue"}},
		},
	}
}

func StepSchema() Block {
	return Block{
		Type:   "step",
		Doc:    "Intermediate connector call — results available as step.<name>.* in transform",
		Labels: 1,
		Open:   true, // accepts connector-specific params
		Attrs: []Attr{
			{Name: "connector", Doc: "Connector to call", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "operation", Doc: "Operation to execute", Type: TypeString},
			{Name: "target", Doc: "Target resource", Type: TypeString},
			{Name: "query", Doc: "SQL query", Type: TypeString},
			{Name: "when", Doc: "CEL condition for conditional execution", Type: TypeString},
			{Name: "timeout", Doc: "Timeout duration (e.g., 5s)", Type: TypeDuration},
			{Name: "on_error", Doc: "Error handling: fail, skip, or default", Type: TypeString, Values: []string{"fail", "skip", "default"}},
			{Name: "envelope", Doc: "Wrap the step's body under a single root key", Type: TypeString},
		},
	}
}

func TransformBlockSchema() Block {
	return Block{
		Type: "transform",
		Doc:  "CEL transformation rules applied to input before writing to destination",
		Open: true, // attributes are CEL field mappings
		Attrs: []Attr{
			{Name: "use", Doc: "Reference to named transform(s)", Type: TypeString, Ref: RefTransform},
		},
	}
}

func ResponseBlockSchema() Block {
	return Block{
		Type: "response",
		Doc:  "Transform output AFTER destination write. Available variables: input, output.",
		Open: true, // attributes are CEL field mappings
	}
}

func ValidateBlockSchema() Block {
	return Block{
		Type: "validate",
		Doc:  "Input and output type validation",
		Attrs: []Attr{
			{Name: "input", Doc: "Input type name for validation", Type: TypeString, Ref: RefType},
			{Name: "output", Doc: "Output type name for validation", Type: TypeString, Ref: RefType},
		},
	}
}

func EnrichSchema() Block {
	return Block{
		Type:   "enrich",
		Doc:    "Data enrichment from external source",
		Labels: 1,
		Open:   true, // accepts connector-specific params
		Attrs: []Attr{
			{Name: "connector", Doc: "Connector for the lookup", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "operation", Doc: "Operation to execute", Type: TypeString},
		},
	}
}

func SyncStorageSchema() Block {
	return Block{
		Type: "storage",
		Doc:  "Storage backend for sync primitive",
		Attrs: []Attr{
			{Name: "driver", Doc: "Storage driver", Type: TypeString, Required: true, Values: []string{"redis", "memory"}},
			{Name: "url", Doc: "Redis connection URL (redis://[:password@]host:port[/db])", Type: TypeString},
			{Name: "host", Doc: "Redis host (alternative to url)", Type: TypeString},
			{Name: "port", Doc: "Redis port (default: 6379)", Type: TypeNumber},
			{Name: "password", Doc: "Redis password", Type: TypeString},
			{Name: "db", Doc: "Redis database number (default: 0)", Type: TypeNumber},
		},
	}
}

func LockSchema() Block {
	return Block{
		Type: "lock",
		Doc:  "Mutex lock for this flow",
		Attrs: []Attr{
			{Name: "key", Doc: "CEL expression for the lock key", Type: TypeString, Required: true},
			{Name: "timeout", Doc: "Max time to hold the lock", Type: TypeDuration},
			{Name: "wait", Doc: "Wait for lock or fail immediately", Type: TypeBool},
			{Name: "retry", Doc: "Retry interval", Type: TypeDuration},
		},
		Children: []Block{
			SyncStorageSchema(),
		},
	}
}

func SemaphoreSchema() Block {
	return Block{
		Type: "semaphore",
		Doc:  "Concurrency limiter for this flow",
		Attrs: []Attr{
			{Name: "key", Doc: "CEL expression for the semaphore key", Type: TypeString, Required: true},
			{Name: "max_permits", Doc: "Maximum concurrent permits", Type: TypeNumber, Required: true},
			{Name: "timeout", Doc: "Max time to wait for a permit", Type: TypeDuration},
			{Name: "lease", Doc: "Max time to hold a permit", Type: TypeDuration},
		},
		Children: []Block{
			SyncStorageSchema(),
		},
	}
}

func CoordinateSchema() Block {
	return Block{
		Type: "coordinate",
		Doc:  "Signal/wait coordination between flows",
		Attrs: []Attr{
			{Name: "timeout", Doc: "Max time to wait", Type: TypeDuration},
			{Name: "on_timeout", Doc: "Behavior on timeout", Type: TypeString, Values: []string{"fail", "retry", "skip", "pass"}},
			{Name: "max_retries", Doc: "Max retries when on_timeout is retry", Type: TypeNumber},
			{Name: "max_concurrent_waits", Doc: "Limit simultaneous waiting processes (0 = unlimited)", Type: TypeNumber},
		},
		Children: []Block{
			SyncStorageSchema(),
			{Type: "wait", Doc: "Wait condition", Attrs: []Attr{
				{Name: "when", Doc: "CEL condition to trigger wait", Type: TypeString},
				{Name: "for", Doc: "CEL expression for signal to wait for", Type: TypeString},
			}},
			{Type: "signal", Doc: "Signal emission", Attrs: []Attr{
				{Name: "when", Doc: "CEL condition to trigger signal", Type: TypeString},
				{Name: "emit", Doc: "CEL expression for signal to emit", Type: TypeString},
				{Name: "ttl", Doc: "Signal time-to-live", Type: TypeDuration},
			}},
			{Type: "preflight", Doc: "Check before waiting", Attrs: []Attr{
				{Name: "connector", Doc: "Connector for the check", Type: TypeString, Ref: RefConnector},
				{Name: "query", Doc: "Query to execute", Type: TypeString},
				{Name: "if_exists", Doc: "Behavior if query returns results", Type: TypeString, Values: []string{"pass", "fail"}},
			}},
		},
	}
}

func FlowCacheSchema() Block {
	return Block{
		Type: "cache",
		Doc:  "Cache configuration for this flow",
		Attrs: []Attr{
			{Name: "storage", Doc: "Cache storage connector", Type: TypeString, Ref: RefConnector},
			{Name: "ttl", Doc: "Cache entry time-to-live", Type: TypeDuration},
			{Name: "key", Doc: "Cache key template with ${...} interpolation", Type: TypeString},
			{Name: "use", Doc: "Reference to named cache definition", Type: TypeString, Ref: RefCache},
		},
	}
}

func RequireSchema() Block {
	return Block{
		Type: "require",
		Doc:  "Authorization requirements",
		Attrs: []Attr{
			{Name: "roles", Doc: "Required roles", Type: TypeList},
			{Name: "permissions", Doc: "Required permissions", Type: TypeList},
		},
	}
}

func AfterSchema() Block {
	return Block{
		Type: "after",
		Doc:  "Post-execution actions (cache invalidation, etc.)",
		Children: []Block{
			{Type: "invalidate", Doc: "Cache invalidation", Attrs: []Attr{
				{Name: "storage", Doc: "Cache storage connector", Type: TypeString, Ref: RefConnector},
				{Name: "keys", Doc: "Specific keys to invalidate", Type: TypeList},
				{Name: "patterns", Doc: "Key patterns to invalidate (with * wildcards)", Type: TypeList},
			}},
		},
	}
}

func ErrorHandlingSchema() Block {
	return Block{
		Type: "error_handling",
		Doc:  "Error handling with retry, fallback, and custom responses",
		Children: []Block{
			{Type: "retry", Doc: "Automatic retry on failure", Attrs: []Attr{
				{Name: "attempts", Doc: "Maximum retry attempts", Type: TypeNumber},
				{Name: "delay", Doc: "Initial delay between retries", Type: TypeDuration},
				{Name: "max_delay", Doc: "Maximum delay (exponential backoff cap)", Type: TypeDuration},
				{Name: "backoff", Doc: "Backoff strategy", Type: TypeString, Values: []string{"linear", "exponential", "constant"}},
			}},
			{Type: "fallback", Doc: "Dead letter queue / fallback destination", Attrs: []Attr{
				{Name: "connector", Doc: "Fallback connector", Type: TypeString, Ref: RefConnector},
				{Name: "target", Doc: "Fallback target", Type: TypeString},
				{Name: "include_error", Doc: "Include error details", Type: TypeBool},
			}},
			{Type: "error_response", Doc: "Custom HTTP error response", Open: true, Attrs: []Attr{
				{Name: "status", Doc: "HTTP status code", Type: TypeNumber},
			}},
		},
	}
}

func DedupeSchema() Block {
	return Block{
		Type: "dedupe",
		Doc:  "Deduplication configuration",
		Attrs: []Attr{
			{Name: "storage", Doc: "Storage connector for dedup state", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "key", Doc: "CEL expression for the deduplication key", Type: TypeString, Required: true},
			{Name: "ttl", Doc: "How long to remember seen keys", Type: TypeDuration},
			{Name: "on_duplicate", Doc: "Behavior on duplicate", Type: TypeString, Values: []string{"skip", "fail"}},
		},
	}
}

func IdempotencySchema() Block {
	return Block{
		Type: "idempotency",
		Doc:  "Idempotency key configuration — returns cached result for duplicate keys",
		Attrs: []Attr{
			{Name: "storage", Doc: "Cache storage connector", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "key", Doc: "CEL expression for the idempotency key", Type: TypeString, Required: true},
			{Name: "ttl", Doc: "How long to keep cached results", Type: TypeDuration},
		},
	}
}

func AsyncSchema() Block {
	return Block{
		Type: "async",
		Doc:  "Async execution — returns 202 immediately, processes in background",
		Attrs: []Attr{
			{Name: "storage", Doc: "Cache storage for job results", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "ttl", Doc: "How long to keep job results", Type: TypeDuration},
		},
	}
}

func BatchSchema() Block {
	return Block{
		Type: "batch",
		Doc:  "Batch processing — reads in chunks, transforms, writes",
		Attrs: []Attr{
			{Name: "source", Doc: "Source connector name", Type: TypeString, Required: true, Ref: RefConnector},
			{Name: "query", Doc: "SQL query to read data", Type: TypeString},
			{Name: "chunk_size", Doc: "Records per chunk (default 100)", Type: TypeNumber},
			{Name: "on_error", Doc: "Behavior on chunk failure", Type: TypeString, Values: []string{"continue", "stop"}},
		},
		Children: []Block{
			TransformBlockSchema(),
			ToSchema(),
		},
	}
}

func StateTransitionSchema() Block {
	return Block{
		Type: "state_transition",
		Doc:  "State machine transition within a flow",
		Attrs: []Attr{
			{Name: "machine", Doc: "State machine name", Type: TypeString, Required: true, Ref: RefStateMachine},
			{Name: "entity", Doc: "Entity table name", Type: TypeString, Required: true},
			{Name: "id", Doc: "CEL expression for entity ID", Type: TypeString, Required: true},
			{Name: "event", Doc: "CEL expression for event name", Type: TypeString, Required: true},
			{Name: "data", Doc: "CEL expression for transition data", Type: TypeString},
		},
	}
}

// --- Connector base ---

func BaseConnectorSchema() Block {
	return Block{
		Type:   "connector",
		Doc:    "Bidirectional adapter for databases, APIs, queues, and other services",
		Labels: 1,
		Attrs: []Attr{
			{Name: "type", Doc: "Connector type", Type: TypeString, Required: true, Values: connectorTypes()},
			{Name: "driver", Doc: "Driver for the connector type", Type: TypeString},
		},
	}
}

// --- Aspect ---

func AspectSchema() Block {
	return Block{
		Type:   "aspect",
		Doc:    "Cross-cutting concern applied via flow name pattern matching (AOP)",
		Labels: 1,
		Attrs: []Attr{
			{Name: "on", Doc: "Flow name patterns to match (glob)", Type: TypeList},
			{Name: "when", Doc: "When to execute", Type: TypeString, Values: []string{"before", "after", "around", "on_error"}},
			{Name: "if", Doc: "CEL condition for conditional execution", Type: TypeString},
			{Name: "priority", Doc: "Execution priority (lower = first)", Type: TypeNumber},
		},
		Children: []Block{
			{Type: "action", Doc: "Aspect action to execute", Attrs: []Attr{
				{Name: "connector", Doc: "Connector to call", Type: TypeString, Ref: RefConnector},
				{Name: "flow", Doc: "Flow to invoke", Type: TypeString, Ref: RefFlow},
			}, Children: []Block{
				TransformBlockSchema(),
			}},
			FlowCacheSchema(),
			{Type: "invalidate", Doc: "Cache invalidation", Attrs: []Attr{
				{Name: "storage", Doc: "Cache storage connector", Type: TypeString, Ref: RefConnector},
				{Name: "keys", Doc: "Specific keys to invalidate", Type: TypeList},
				{Name: "patterns", Doc: "Key patterns to invalidate", Type: TypeList},
			}},
			{Type: "rate_limit", Doc: "Rate limiting", Open: true},
			{Type: "circuit_breaker", Doc: "Circuit breaker", Open: true},
			{Type: "response", Doc: "Response modification (headers, fields)", Open: true},
		},
	}
}

// --- Other root blocks ---

func TypeSchema() Block {
	return Block{
		Type:   "type",
		Doc:    "Schema definition for input/output validation",
		Labels: 1,
		Open:   true, // fields are user-defined
	}
}

func TransformSchema() Block {
	return Block{
		Type:   "transform",
		Doc:    "Reusable named transformation (CEL expressions)",
		Labels: 1,
		Open:   true, // attributes are CEL mappings
	}
}

func ServiceSchema() Block {
	return Block{
		Type: "service",
		Doc:  "Global service configuration",
		Attrs: []Attr{
			{Name: "name", Doc: "Service name", Type: TypeString, Required: true},
			{Name: "version", Doc: "Service version", Type: TypeString},
			{Name: "admin_port", Doc: "Admin server port (health, metrics, debug)", Type: TypeNumber},
		},
		Children: []Block{
			{Type: "rate_limit", Doc: "Global rate limiting", Open: true},
			{Type: "workflow", Doc: "Workflow engine configuration", Attrs: []Attr{
				{Name: "storage", Doc: "Workflow persistence connector", Type: TypeString, Ref: RefConnector},
			}},
		},
	}
}

func ValidatorSchema() Block {
	return Block{
		Type:   "validator",
		Doc:    "Custom validation rule (regex, CEL, or WASM)",
		Labels: 1,
		Open:   true,
	}
}

func SagaSchema() Block {
	return Block{
		Type:   "saga",
		Doc:    "Distributed transaction with automatic compensation",
		Labels: 1,
		Open:   true,
	}
}

func StateMachineSchema() Block {
	return Block{
		Type:   "state_machine",
		Doc:    "Entity lifecycle with guards, actions, and final states",
		Labels: 1,
		Open:   true,
	}
}

func FunctionsSchema() Block {
	return Block{
		Type:   "functions",
		Doc:    "Custom CEL functions",
		Labels: 1,
		Open:   true,
	}
}

func PluginSchema() Block {
	return Block{
		Type:   "plugin",
		Doc:    "WASM plugin for extending Mycel",
		Labels: 1,
		Attrs: []Attr{
			{Name: "source", Doc: "Plugin source (git URL or local path)", Type: TypeString, Required: true},
			{Name: "version", Doc: "Version constraint (semver)", Type: TypeString},
		},
	}
}

func AuthSchema() Block {
	return Block{
		Type: "auth",
		Doc:  "Authentication configuration",
		Open: true,
	}
}

func SecuritySchema() Block {
	return Block{
		Type: "security",
		Doc:  "Security and sanitization rules",
		Open: true,
	}
}

func MocksSchema() Block {
	return Block{
		Type: "mocks",
		Doc:  "Mock data for testing",
		Open: true,
	}
}

func CacheDefSchema() Block {
	return Block{
		Type:   "cache",
		Doc:    "Named cache definition",
		Labels: 1,
		Attrs: []Attr{
			{Name: "storage", Doc: "Cache storage connector", Type: TypeString, Ref: RefConnector},
			{Name: "ttl", Doc: "Default TTL", Type: TypeDuration},
			{Name: "prefix", Doc: "Key prefix", Type: TypeString},
		},
	}
}

func EnvironmentSchema() Block {
	return Block{
		Type:   "environment",
		Doc:    "Environment-specific variables",
		Labels: 1,
		Open:   true,
	}
}

// connectorTypes returns all known connector type values.
func connectorTypes() []string {
	return []string{
		"rest", "http", "database", "mq", "graphql", "grpc", "file", "s3",
		"cache", "tcp", "exec", "soap", "mqtt", "ftp", "cdc",
		"websocket", "sse", "elasticsearch", "oauth", "profiled",
		"email", "slack", "discord", "sms", "push", "webhook", "pdf",
	}
}
