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
		ConstantsSchema(),
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
			SequenceGuardSchema(),
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
			{Name: "facet", Doc: "Dedupe facet this destination satisfies. Skipped when that facet did not change; the facet is committed only once every destination naming it succeeded. Omit for a destination that always runs", Type: TypeString},
			{Name: "query", Doc: "SQL query for database writes", Type: TypeString},
			{Name: "format", Doc: "Output format", Type: TypeString, Values: []string{"json", "xml", "csv", "tsv"}},
			{Name: "filter", Doc: "Per-user filter (WebSocket, SSE, subscriptions)", Type: TypeString},
		},
		Children: []Block{
			TransactionSchema(),
		},
	}
}

// TransactionSchema describes the to{transaction} multi-statement write: an
// ordered list of exec/each statements run in a single database transaction.
func TransactionSchema() Block {
	return Block{
		Type: "transaction",
		Doc:  "Multi-statement, iterative write run atomically in one DB transaction (database connectors only). Mutually exclusive with query/target/operation/envelope.",
		Attrs: []Attr{
			{Name: "use", Doc: "Reference a named transaction block (use = \"transaction.<name>\"); inline statements replace it wholesale", Type: TypeString, Ref: RefTransaction},
		},
		Children: []Block{
			TxExecSchema(),
			TxEachSchema(),
		},
	}
}

func TxExecSchema() Block {
	return Block{
		Type: "exec",
		Doc:  "A single SQL statement inside a transaction.",
		Attrs: []Attr{
			{Name: "query", Doc: "SQL with :named placeholders", Type: TypeString, Required: true},
			{Name: "params", Doc: "Map of placeholder name -> CEL expression (scope: input, output, step, captured, each vars)", Type: TypeMap},
			{Name: "when", Doc: "CEL gate — statement is skipped when false", Type: TypeString},
			{Name: "capture", Doc: "Store the result under captured.<name>: last insert id for INSERT/UPDATE/DELETE, first column of first row for SELECT", Type: TypeString},
		},
	}
}

func TxEachSchema() Block {
	return Block{
		Type:   "each",
		Doc:    `Iterate a CEL list, running nested statements per element. Written as: each "<var>" in "<listExpr>" { ... }. The element binds to <var> and its index to <var>_index.`,
		Labels: 3, // <var> in <listExpr>
		Children: []Block{
			TxExecSchema(),
		},
	}
}

func AcceptSchema() Block {
	return Block{
		Type: "accept",
		Doc:  "Business-level gate after filter, before transform. Determines if this flow should process the message.",
		Attrs: []Attr{
			{Name: "use", Doc: "Reference a named accept block (use = \"accept.<name>\"); other attrs override it", Type: TypeString, Ref: RefAccept},
			{Name: "when", Doc: "CEL expression — must return true to proceed", Type: TypeString},
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
		Attrs: []Attr{
			{Name: "use", Doc: "Reference a named response block (use = \"response.<name>\"); inline field mappings override it key by key", Type: TypeString, Ref: RefResponse},
		},
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
			{Name: "use", Doc: "Reference a named lock block (use = \"lock.<name>\"); other attrs override it", Type: TypeString, Ref: RefLock},
			{Name: "key", Doc: "CEL expression for the lock key", Type: TypeString},
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
			{Name: "use", Doc: "Reference a named semaphore block (use = \"semaphore.<name>\"); other attrs override it", Type: TypeString, Ref: RefSemaphore},
			{Name: "key", Doc: "CEL expression for the semaphore key", Type: TypeString},
			{Name: "max_permits", Doc: "Maximum concurrent permits", Type: TypeNumber},
			{Name: "limit", Doc: "Maximum concurrent permits; the same setting as max_permits", Type: TypeNumber},
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
			{Name: "use", Doc: "Reference a named coordinate block (use = \"coordinate.<name>\"); other attrs override it", Type: TypeString, Ref: RefCoordinate},
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

func SequenceGuardSchema() Block {
	return Block{
		Type: "sequence_guard",
		Doc:  "Monotonic sequence dedup — rejects messages whose sequence is not strictly greater than the last one stored for the same key. Compose with lock for atomicity.",
		Attrs: []Attr{
			{Name: "use", Doc: "Reference a named sequence_guard block (use = \"sequence_guard.<name>\"); other attrs override it", Type: TypeString, Ref: RefSequenceGuard},
			{Name: "key", Doc: "CEL expression for the per-resource key (e.g. 'sku:' + input.body.sku)", Type: TypeString},
			{Name: "sequence", Doc: "CEL expression yielding a monotonic numeric sequence (e.g. input.body.jobId)", Type: TypeString},
			{Name: "on_older", Doc: "What to do when current sequence is not strictly greater than stored", Type: TypeString, Values: []string{"ack", "reject", "requeue"}},
			{Name: "ttl", Doc: "How long to retain stored sequences after the last update (e.g. 30d)", Type: TypeDuration},
		},
		Children: []Block{
			SyncStorageSchema(),
		},
	}
}

// AspectCacheSchema is the cache an aspect's action is kept under.
//
// It is not the flow's cache block: an aspect caches what its own action
// returned, so there is no `invalidate_on` — nothing else writes the thing it
// holds — and its three attributes are all required. Declaring one block for
// both said an aspect took an attribute its parser refuses.
func AspectCacheSchema() Block {
	return Block{
		Type: "cache",
		Doc:  "Cache the result of this aspect's action",
		Attrs: []Attr{
			{Name: "storage", Doc: "Cache storage connector", Type: TypeString, Ref: RefConnector, Required: true},
			{Name: "ttl", Doc: "Cache entry time-to-live", Type: TypeDuration, Required: true},
			{Name: "key", Doc: "Cache key template with ${...} interpolation", Type: TypeString, Required: true},
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
			{Name: "invalidate_on", Doc: "Flows whose writes drop this flow's cached entries", Type: TypeList},
			{Name: "use", Doc: "Reference to named cache definition", Type: TypeString, Ref: RefCache},
			{Name: "encoding", Doc: "How entries are written and read, applied in order on the way out and reversed on the way in: [\"json\"] (the default), or e.g. [\"json\", \"base64\", \"gzip\"] to share a namespace with a service that stores gzip(base64(JSON.stringify(v)))", Type: TypeList, Values: []string{"json", "base64", "gzip"}},
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
				{Name: "keys_from", Doc: "CEL expression yielding a list of keys, for a set whose size is only known once the flow has run. input.*, output.* and step.* in scope; unioned with keys", Type: TypeString},
				{Name: "patterns_from", Doc: "CEL expression yielding a list of patterns; unioned with patterns", Type: TypeString},
			}},
		},
	}
}

func ErrorHandlingSchema() Block {
	return Block{
		Type: "error_handling",
		Doc:  "Error handling with retry, fallback, and custom responses",
		Attrs: []Attr{
			{Name: "use", Doc: "Reference a named error_handling block (use = \"error_handling.<name>\"); sub-blocks override it wholesale", Type: TypeString, Ref: RefErrorHandling},
		},
		Children: []Block{
			{Type: "retry", Doc: "Automatic retry on failure", Attrs: []Attr{
				{Name: "use", Doc: "Reference a named retry block (use = \"retry.<name>\"); other attrs override it", Type: TypeString, Ref: RefRetry},
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
			{Type: "on_timeout", Doc: "Disposition for timeout / deadline-exceeded failures (overrides default retry)", Attrs: []Attr{
				{Name: "action", Doc: "Broker disposition", Type: TypeString, Required: true, Values: []string{"ack", "retry", "requeue", "reject"}},
			}},
			{Type: "on_error", Doc: "Disposition for transient, non-timeout, non-permanent failures", Attrs: []Attr{
				{Name: "action", Doc: "Broker disposition", Type: TypeString, Required: true, Values: []string{"ack", "retry", "requeue", "reject"}},
			}},
		},
	}
}

func DedupeSchema() Block {
	return Block{
		Type: "dedupe",
		Doc:  "Content-based, biphasic deduplication. Compares a canonical fingerprint of a named projection of the message against the previously-stored fingerprint for the same key; on match, drops without invoking `to`. Stores the new fingerprint only after `to` succeeds.",
		Attrs: []Attr{
			{Name: "use", Doc: "Reference a named dedupe block (use = \"dedupe.<name>\"); other attrs override it", Type: TypeString, Ref: RefDedupe},
			{Name: "cache", Doc: "Cache-typed connector used to store fingerprints", Type: TypeString, Ref: RefConnector},
			{Name: "key", Doc: "CEL expression for the per-resource fingerprint key (evaluated against input.*)", Type: TypeString},
			{Name: "ttl", Doc: "How long to keep stored fingerprints after the last update", Type: TypeDuration},
			{Name: "on_duplicate", Doc: "Behavior on fingerprint match", Type: TypeString, Values: []string{"ack", "reject", "requeue"}},
			{Name: "compare_when", Doc: "CEL predicate gating the comparison only (input.* and output.* in scope). False: the stored fingerprint is not consulted and the message cannot be dropped, but a successful write still stores the new one. Use for an existence check the fingerprint cannot express; never put that check in fingerprint", Type: TypeString},
		},
		Children: []Block{
			{
				Type:  "fingerprint",
				Doc:   "Named CEL expressions whose values form the projection. Must list every field the flow persists downstream — omitting one would silently drop real changes. Both input.* and output.* (transform result) are in scope.",
				Open:  true,
				Attrs: []Attr{},
			},
			{
				Type:   "facet",
				Labels: 1,
				Doc:    "An independently-tracked part of the projection, named by its label. Each facet is fingerprinted, stored and committed on its own; a `to` naming it runs only when it changed, and the message is dropped only when no facet did. Use instead of a bare fingerprint, not alongside it. Mycel does not check that facets are independent — two whose destinations write the same thing will race.",
				Attrs:  []Attr{},
				Children: []Block{
					{
						Type:  "fingerprint",
						Doc:   "The facet's projection, in the same form and scope as the dedupe-level fingerprint block.",
						Open:  true,
						Attrs: []Attr{},
					},
				},
			},
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
			{Name: "connector", Doc: "Connector holding the entity (defaults to the flow's destination)", Type: TypeString, Ref: RefConnector},
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
			// Profiles: one connector that resolves to different backends.
			{Name: "select", Doc: "CEL expression naming the profile to use", Type: TypeString},
			{Name: "default", Doc: "Profile used when select names none", Type: TypeString},
			{Name: "fallback", Doc: "Profiles to try, in order, when the selected one fails", Type: TypeList},
		},
		Children: []Block{
			OperationSchema(),
			{
				Type:   "profile",
				Doc:    "One backend this connector can resolve to. A profile declares what it is, so profiles of a connector may differ in type",
				Labels: 1,
				Open:   true,
				Attrs: []Attr{
					{Name: "type", Doc: "Connector type for this profile", Type: TypeString, Required: true, Values: connectorTypes()},
					{Name: "driver", Doc: "Driver for this profile", Type: TypeString},
				},
			},
		},
	}
}

// OperationSchema describes a named operation.
//
// A connector declares what it can do once, and flows refer to it by name
// instead of repeating the inline form. Which attributes apply depends on the
// connector: method and path for REST, query or table for a database, service
// and rpc for gRPC, and so on.
func OperationSchema() Block {
	return Block{
		Type:   "operation",
		Doc:    "Named operation a flow can refer to by name instead of its inline form",
		Labels: 1,
		Attrs: []Attr{
			{Name: "description", Doc: "What the operation does", Type: TypeString},
			{Name: "input", Doc: "Type validating the input", Type: TypeString, Ref: RefType},
			{Name: "output", Doc: "Type validating the output", Type: TypeString, Ref: RefType},
			{Name: "timeout", Doc: "Operation timeout", Type: TypeNumber},

			{Name: "method", Doc: "HTTP method (rest, http)", Type: TypeString},
			{Name: "path", Doc: "HTTP path, with :name for path parameters (rest, http)", Type: TypeString},

			{Name: "query", Doc: "Raw SQL, with :name for parameters (database)", Type: TypeString},
			{Name: "table", Doc: "Table the operation reads or writes (database)", Type: TypeString},

			{Name: "operation_type", Doc: "Query, Mutation or Subscription (graphql)", Type: TypeString,
				Values: []string{"Query", "Mutation", "Subscription"}},
			{Name: "field", Doc: "Schema field (graphql)", Type: TypeString},

			{Name: "service", Doc: "Service name (grpc)", Type: TypeString},
			{Name: "rpc", Doc: "RPC name (grpc)", Type: TypeString},

			{Name: "exchange", Doc: "Exchange to publish to (mq, rabbitmq)", Type: TypeString},
			{Name: "routing_key", Doc: "Routing key (mq, rabbitmq)", Type: TypeString},
			{Name: "queue", Doc: "Queue or topic (mq)", Type: TypeString},

			{Name: "protocol", Doc: "Wire protocol (tcp)", Type: TypeString},
			{Name: "action", Doc: "Action identifier (tcp)", Type: TypeString},

			{Name: "path_pattern", Doc: "Path pattern (file, s3)", Type: TypeString},

			{Name: "key_pattern", Doc: "Key pattern (cache)", Type: TypeString},
			{Name: "ttl", Doc: "Time to live (cache)", Type: TypeString},

			{Name: "command", Doc: "Command to run (exec)", Type: TypeString},
			{Name: "args", Doc: "Command arguments (exec)", Type: TypeList},
		},
		Children: []Block{{
			Type:   "param",
			Doc:    "Parameter contract: defaults are filled in, and types and constraints are checked before the flow runs",
			Labels: 1,
			Attrs: []Attr{
				{Name: "type", Doc: "Declared type; a value that can be converted to it is", Type: TypeString,
					Values: []string{"string", "number", "boolean", "array", "object"}},
				{Name: "required", Doc: "Reject the request when it is absent and there is no default", Type: TypeBool},
				{Name: "default", Doc: "Value used when the parameter is not supplied"},
				{Name: "description", Doc: "What the parameter means", Type: TypeString},
				{Name: "in", Doc: "Where the parameter comes from (path, query, header, body)", Type: TypeString,
					Values: []string{"path", "query", "header", "body"}},
				{Name: "min", Doc: "Smallest allowed value (numbers)", Type: TypeNumber},
				{Name: "max", Doc: "Largest allowed value (numbers)", Type: TypeNumber},
				{Name: "min_length", Doc: "Shortest allowed value (strings)", Type: TypeNumber},
				{Name: "max_length", Doc: "Longest allowed value (strings)", Type: TypeNumber},
				{Name: "pattern", Doc: "Regular expression the value must match (strings)", Type: TypeString},
				{Name: "enum", Doc: "The complete set of allowed values", Type: TypeList},
			},
		}},
	}
}

// --- Aspect ---

func AspectSchema() Block {
	return Block{
		Type:   "aspect",
		Doc:    "Cross-cutting concern applied via flow name pattern matching (AOP)",
		Labels: 1,
		Attrs: []Attr{
			{Name: "on", Doc: "Flow name patterns to match (glob)", Type: TypeList, Required: true},
			// Kept in step with internal/aspect.When, which is the authority.
			// on_drop was added to the runtime and never reached here, so
			// completions and generated skeletons did not offer it.
			{Name: "when", Doc: "When to execute", Type: TypeString, Required: true, Values: []string{"before", "after", "around", "on_error", "on_drop"}},
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
			AspectCacheSchema(),
			{Type: "invalidate", Doc: "Cache invalidation", Attrs: []Attr{
				{Name: "storage", Doc: "Cache storage connector", Type: TypeString, Ref: RefConnector},
				{Name: "keys", Doc: "Specific keys to invalidate", Type: TypeList},
				{Name: "patterns", Doc: "Key patterns to invalidate", Type: TypeList},
			}},
			{Type: "rate_limit", Doc: "Rate limiting", Attrs: []Attr{
				{Name: "key", Doc: "CEL expression the limit is counted per (e.g. the caller's IP)", Type: TypeString, Required: true},
				{Name: "requests_per_second", Doc: "Sustained rate allowed", Type: TypeNumber, Required: true},
				{Name: "burst", Doc: "How far above the rate a short spike may go", Type: TypeNumber},
			}},
			{Type: "circuit_breaker", Doc: "Circuit breaker", Attrs: []Attr{
				{Name: "name", Doc: "Breaker name, shared by every flow naming it", Type: TypeString},
				{Name: "failure_threshold", Doc: "Consecutive failures that open the circuit", Type: TypeNumber, Required: true},
				{Name: "success_threshold", Doc: "Successes needed to close it again", Type: TypeNumber},
				{Name: "timeout", Doc: "How long the circuit stays open before a trial call", Type: TypeDuration, Required: true},
			}},
			{Type: "response", Doc: "Response modification: headers, plus any field as a CEL expression",
				Open: true, // every other attribute is an output field
				Attrs: []Attr{
					{Name: "headers", Doc: "Map of header name -> value", Type: TypeMap},
				}},
		},
	}
}

// --- Other root blocks ---

func TypeSchema() Block {
	return Block{
		Type:   "type",
		Doc:    "Schema definition for input/output validation",
		Labels: 1,
		// Field names are user-defined, so the block stays open. What is not
		// open is the shape of a field: the value is one of a fixed set of
		// types, optionally carrying a constraint block. Declaring that here
		// is what lets the IDE complete constraints and `mycel add type`
		// generate a field that is correct by construction.
		Open: true,
	}
}

// FieldTypes are the value types a type field may declare.
func FieldTypes() []string {
	// "id" is an identifier: GraphQL publishes it as ID, which accepts a
	// number or a string. The GraphQL converter has mapped it since it was
	// written, and this list did not name it — so it was a type that worked
	// and that nothing offered or documented.
	return []string{"string", "number", "boolean", "id", "array", "object"}
}

// StringFormats are the values the `format` constraint accepts.
func StringFormats() []string {
	return []string{"email", "url", "uuid", "date", "datetime", "phone", "ip"}
}

// FieldConstraints are the constraints a type field may carry.
//
// They are arguments to a call, not a nested block:
//
//	email = string({ format = "email" })
//
// The brace form without parentheses does not parse — HCL reads it as an
// argument followed by a block, and the type body only accepts attributes.
//
// The set is a union across value types: min_length is meaningful on a string
// and min on a number, and a field's type is the value rather than the label,
// so it cannot be narrowed here.
func FieldConstraints() []Attr {
	return []Attr{
		{Name: "required", Doc: "Field must be present and non-null", Type: TypeBool, Default: true},
		{Name: "format", Doc: "Well-known string format", Type: TypeString, Values: StringFormats()},
		{Name: "min_length", Doc: "Minimum string length", Type: TypeNumber},
		{Name: "max_length", Doc: "Maximum string length", Type: TypeNumber},
		{Name: "pattern", Doc: "Regular expression the value must match", Type: TypeString},
		{Name: "min", Doc: "Minimum numeric value (inclusive)", Type: TypeNumber},
		{Name: "max", Doc: "Maximum numeric value (inclusive)", Type: TypeNumber},
		{Name: "enum", Doc: "Value must be one of these", Type: TypeList},
		// `validator`, not `validate`: the parser accepts the first and
		// refuses the second, so anything completing from this schema was
		// offering a word that does not work.
		{Name: "validator", Doc: "Name of a custom validator to apply", Type: TypeString, Ref: RefValidator},
		{Name: "description", Doc: "What the field is, for generated documentation", Type: TypeString},
	}
}

// TransformSchema describes the named, reusable transform block.
//
// Open is not a placeholder here: every attribute is a CEL field mapping whose
// name is chosen by the author, so there is no fixed set to declare. What was
// missing is the enrich child block, which the parser accepts and the schema
// did not mention.
func TransformSchema() Block {
	return Block{
		Type:   "transform",
		Doc:    "Reusable named transformation (CEL expressions)",
		Labels: 1,
		Open:   true, // attributes are CEL mappings: output_field = "<CEL>"
		Children: []Block{
			EnrichSchema(),
		},
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
			{Type: "rate_limit", Doc: "Global rate limiting", Attrs: []Attr{
				{Name: "enabled", Doc: "Whether the limit applies", Type: TypeBool},
				{Name: "requests_per_second", Doc: "Sustained rate allowed", Type: TypeNumber},
				{Name: "burst", Doc: "How far above the rate a short spike may go", Type: TypeNumber},
				{Name: "key_extractor", Doc: "What the limit is counted per (e.g. the caller's IP)", Type: TypeString},
				{Name: "exclude_paths", Doc: "Paths the limit does not apply to", Type: TypeList},
				{Name: "enable_headers", Doc: "Send the standard rate-limit response headers", Type: TypeBool},
				{Name: "storage", Doc: "Connector holding the counters; shared across instances", Type: TypeString, Ref: RefConnector},
			}},
			{Type: "workflow", Doc: "Workflow engine configuration", Attrs: []Attr{
				{Name: "storage", Doc: "Workflow persistence connector", Type: TypeString, Ref: RefConnector},
				{Name: "table", Doc: "Table holding workflow instances", Type: TypeString},
				{Name: "auto_create", Doc: "Create the table on startup", Type: TypeBool},
			}, Children: []Block{
				{Type: "api", Doc: "HTTP interface to running workflows, on its own port and never on the admin server", Attrs: []Attr{
					{Name: "port", Doc: "Port to listen on (default 9091); may not be the admin port", Type: TypeNumber},
					{Name: "host", Doc: "Address to bind to (default every interface)", Type: TypeString},
				}, Children: []Block{
					{Type: "auth", Doc: "How callers are checked. Required: these endpoints wake and cancel workflows", Open: true, Attrs: []Attr{
						{Name: "type", Doc: "jwt, api_key or basic", Type: TypeString, Required: true, Values: []string{"jwt", "api_key", "basic"}},
						{Name: "header", Doc: "Header carrying the key (api_key)", Type: TypeString},
						{Name: "keys", Doc: "Accepted API keys", Type: TypeList},
						{Name: "secret", Doc: "Secret tokens are signed with (jwt)", Type: TypeString},
						{Name: "jwks_url", Doc: "Where the signing keys are published (jwt)", Type: TypeString},
					}},
				}},
			}},
		},
	}
}

// ValidatorSchema describes the validator block. Transcribed from
// internal/parser/validator.go, which is the authority.
//
// Which attribute is required depends on `type`, and the schema has no way to
// say "required when type is regex" — so the three carriers are declared
// optional and their pairing is documented. The parser rejects the wrong
// combination by name, which is where that check belongs.
func ValidatorSchema() Block {
	return Block{
		Type:   "validator",
		Doc:    "Custom validation rule (regex, CEL, or WASM)",
		Labels: 1,
		Attrs: []Attr{
			{Name: "type", Doc: "How the rule is expressed", Type: TypeString, Required: true, Values: []string{"regex", "cel", "wasm"}},
			{Name: "pattern", Doc: "Regular expression the value must match (type = regex)", Type: TypeString},
			{Name: "expr", Doc: "CEL expression that must return true (type = cel)", Type: TypeString},
			{Name: "wasm", Doc: "Path to the .wasm module (type = wasm)", Type: TypeString},
			{Name: "entrypoint", Doc: "Exported function to call in the module", Type: TypeString, Default: "validate"},
			{Name: "message", Doc: "Error message shown when validation fails", Type: TypeString},
		},
	}
}

// SagaSchema describes the saga block. Transcribed from internal/parser/saga.go,
// which is the authority: every attribute and child here is one the parser
// accepts, and nothing the parser rejects appears.
func SagaSchema() Block {
	return Block{
		Type:   "saga",
		Doc:    "Distributed transaction with automatic compensation",
		Labels: 1,
		Attrs: []Attr{
			{Name: "timeout", Doc: "Deadline for the saga as a whole", Type: TypeDuration},
		},
		Children: []Block{
			{Type: "from", Doc: "What starts the saga", Attrs: []Attr{
				{Name: "connector", Doc: "Connector the saga listens on", Type: TypeString, Required: true, Ref: RefConnector},
				{Name: "operation", Doc: "Operation to trigger on", Type: TypeString},
				{Name: "filter", Doc: "CEL condition; the saga runs only when it holds", Type: TypeString},
			}},
			SagaStepSchema(),
			{Type: "on_complete", Doc: "Action to run once every step has succeeded", Attrs: sagaActionAttrs()},
			{Type: "on_failure", Doc: "Action to run after compensation has unwound the saga", Attrs: sagaActionAttrs()},
		},
	}
}

// SagaStepSchema describes one step of a saga.
//
// A step must carry an action, a delay or an await — the parser rejects one
// with none of the three — but no single one of them can be marked Required,
// so the constraint lives in the docs and in what the generator emits.
func SagaStepSchema() Block {
	return Block{
		Type:   "step",
		Doc:    "One step of the saga, executed in declaration order",
		Labels: 1,
		Attrs: []Attr{
			{Name: "timeout", Doc: "Deadline for this step", Type: TypeDuration},
			// The executor tests for "skip" and compensates on anything else,
			// so those are the two behaviours there are (internal/saga/executor.go).
			{Name: "on_error", Doc: "What a failure does: compensate the saga, or skip the step and carry on", Type: TypeString, Values: []string{"compensate", "skip"}},
			{Name: "delay", Doc: "Wait this long before continuing; a step with only a delay needs no action", Type: TypeDuration},
			{Name: "await", Doc: "Pause until an external signal with this event name arrives", Type: TypeString},
		},
		Children: []Block{
			{Type: "action", Doc: "The work this step performs", Attrs: sagaActionAttrs()},
			{Type: "compensate", Doc: "How to undo this step when a later one fails", Attrs: sagaActionAttrs()},
		},
	}
}

// sagaActionAttrs is the shape shared by action, compensate, on_complete and
// on_failure — the parser reads all four with the same function.
func sagaActionAttrs() []Attr {
	return []Attr{
		{Name: "connector", Doc: "Connector to call", Type: TypeString, Ref: RefConnector},
		{Name: "operation", Doc: "Operation to perform", Type: TypeString},
		{Name: "target", Doc: "Table, queue, path or endpoint to act on", Type: TypeString},
		{Name: "query", Doc: "Raw query, for database connectors", Type: TypeString},
		{Name: "data", Doc: "Values to write, as CEL expressions", Type: TypeMap},
		{Name: "body", Doc: "Request body, as CEL expressions", Type: TypeMap},
		{Name: "set", Doc: "Columns to update, as CEL expressions", Type: TypeMap},
		{Name: "where", Doc: "Row selection, as CEL expressions", Type: TypeMap},
		{Name: "params", Doc: "Named query parameters, as CEL expressions", Type: TypeMap},
		{Name: "template", Doc: "Notification template name", Type: TypeString},
		{Name: "to", Doc: "Notification recipient", Type: TypeString},
	}
}

// StateMachineSchema describes the state_machine block. Transcribed from
// internal/parser/statemachine.go, which is the authority.
func StateMachineSchema() Block {
	return Block{
		Type:   "state_machine",
		Doc:    "Entity lifecycle with guards, actions, and final states",
		Labels: 1,
		Attrs: []Attr{
			{Name: "initial", Doc: "State an entity starts in", Type: TypeString, Required: true},
		},
		Children: []Block{
			{Type: "state", Doc: "One state of the lifecycle", Labels: 1, Attrs: []Attr{
				{Name: "final", Doc: "No transition may leave this state", Type: TypeBool},
			}, Children: []Block{
				{Type: "on", Doc: `Transition taken when this event arrives, written as: on "<event>" { }`, Labels: 1, Attrs: []Attr{
					{Name: "transition_to", Doc: "State to move to", Type: TypeString, Required: true},
					{Name: "guard", Doc: "CEL condition; the transition is refused when it does not hold", Type: TypeString},
				}, Children: []Block{
					{Type: "action", Doc: "Side effect to run on the transition", Attrs: stateMachineActionAttrs()},
				}},
			}},
		},
	}
}

// stateMachineActionAttrs is the action shape a transition can carry. It is a
// subset of the saga action — no query, set or where — so it is written out
// rather than shared, which would let one grow attributes the other rejects.
func stateMachineActionAttrs() []Attr {
	return []Attr{
		{Name: "connector", Doc: "Connector to call", Type: TypeString, Ref: RefConnector},
		{Name: "operation", Doc: "Operation to perform", Type: TypeString},
		{Name: "target", Doc: "Table, queue, path or endpoint to act on", Type: TypeString},
		{Name: "data", Doc: "Values to write, as CEL expressions", Type: TypeMap},
		{Name: "body", Doc: "Request body, as CEL expressions", Type: TypeMap},
		{Name: "params", Doc: "Named query parameters, as CEL expressions", Type: TypeMap},
		{Name: "template", Doc: "Notification template name", Type: TypeString},
		{Name: "to", Doc: "Notification recipient", Type: TypeString},
	}
}

// FunctionsSchema describes the functions block, which registers the exports of
// a WASM module as CEL functions.
//
// Not Open: the parser reads this body with Content, so the two attributes
// below are the only ones it accepts, and it requires both.
func FunctionsSchema() Block {
	return Block{
		Type:   "functions",
		Doc:    "Custom CEL functions from a WASM module",
		Labels: 1,
		Attrs: []Attr{
			{Name: "wasm", Doc: "Path to the .wasm module", Type: TypeString, Required: true},
			{Name: "exports", Doc: "Exported function names to register as CEL functions", Type: TypeList, Required: true},
		},
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

// AuthSchema describes the auth block. Transcribed from authSchema in
// internal/parser/auth.go, which reads this body with Content — so it is not
// Open, and an attribute not listed here is rejected.
//
// The nested blocks are named but their contents are not described yet; auth is
// by far the largest block in the language. Naming them is still worth more
// than the Open it replaces, which claimed every attribute was valid.
// hookAttrs describes one auth hook. Every hook takes the same three.
func hookAttrs() []Attr {
	return []Attr{
		{Name: "flow", Doc: "Flow to invoke; the event arrives as auth.*", Type: TypeString, Required: true, Ref: RefFlow},
		{Name: "condition", Doc: "CEL over the event, such as auth.reason == \"new_device\"; the flow runs only when it holds", Type: TypeString},
		{Name: "on_error", Doc: "What a failing flow means. Only a before_ hook may refuse what it is attached to", Type: TypeString, Values: []string{"ignore", "fail"}},
	}
}

func AuthSchema() Block {
	return Block{
		Type: "auth",
		Doc:  "Authentication configuration",
		Attrs: []Attr{
			{Name: "preset", Doc: "Baseline to start from", Type: TypeString, Values: []string{"strict", "standard", "relaxed", "development"}},
			{Name: "base_url", Doc: "Where this service is reached at. A social or SSO provider sends people back here, so declaring one without this is refused at startup", Type: TypeString},
			{Name: "secret", Doc: "Signing secret for issued tokens", Type: TypeString},
			{Name: "storage", Doc: "Connector used to store users and sessions", Type: TypeString, Ref: RefConnector},
		},
		Children: []Block{
			{Type: "storage", Doc: "Where users and sessions are kept", Attrs: []Attr{
				{Name: "driver", Doc: "Storage backend", Type: TypeString, Required: true, Values: []string{"memory", "redis", "database"}},
				{Name: "address", Doc: "Server address (redis)", Type: TypeString},
				{Name: "password", Doc: "Server password (redis)", Type: TypeString},
				{Name: "db", Doc: "Database index (redis)", Type: TypeNumber},
				{Name: "connector", Doc: "Connector to store into (database)", Type: TypeString, Ref: RefConnector},
				{Name: "table", Doc: "Table name (database)", Type: TypeString},
			}},
			{Type: "users", Doc: "Where user records live", Attrs: []Attr{
				{Name: "connector", Doc: "Connector holding the users table", Type: TypeString, Ref: RefConnector},
				{Name: "table", Doc: "Table name; defaults to users", Type: TypeString},
			}, Children: []Block{
				{Type: "fields", Doc: "Column names, when the table already exists under other ones", Attrs: []Attr{
					{Name: "id", Doc: "Identifier column; defaults to id", Type: TypeString},
					{Name: "email", Doc: "Email column; defaults to email", Type: TypeString},
					{Name: "password_hash", Doc: "Password hash column; defaults to password_hash", Type: TypeString},
					{Name: "created_at", Doc: "Creation timestamp column; defaults to created_at", Type: TypeString},
					{Name: "updated_at", Doc: "Update timestamp column; defaults to updated_at", Type: TypeString},
					{Name: "roles", Doc: "Roles column. Naming one turns roles on for a database-backed store; without it roles are neither written nor read", Type: TypeString},
					{Name: "password_changed_at", Doc: "Column recording when a password was last set. Naming one turns password max_age on for a database-backed store; without it nothing expires", Type: TypeString},
				}},
			}},
			{Type: "jwt", Doc: "Token issuing and validation", Attrs: []Attr{
				{Name: "algorithm", Doc: "Signing algorithm. HS256 signs with a shared secret; RS256 and ES256 sign with private_key and are verified with public_key", Type: TypeString, Values: []string{"HS256", "HS384", "HS512", "RS256", "RS384", "RS512", "ES256", "ES384"}},
				{Name: "secret", Doc: "Shared signing secret, for the HS algorithms", Type: TypeString},
				{Name: "private_key", Doc: "Path to the key tokens are signed with, for the RS and ES algorithms", Type: TypeString},
				{Name: "public_key", Doc: "Path to the key tokens are verified with", Type: TypeString},
				{Name: "access_lifetime", Doc: "How long an access token is good for, such as 15m", Type: TypeString},
				{Name: "refresh_lifetime", Doc: "How long a refresh token is good for, such as 720h", Type: TypeString},
				{Name: "issuer", Doc: "The iss claim written into every token", Type: TypeString},
				{Name: "audience", Doc: "The aud claim written into every token", Type: TypeList},
				{Name: "rotation", Doc: "Issue a new refresh token each time one is used, and retire the old one", Type: TypeBool},
				{Name: "claims", Doc: "Extra claims to write into a token, as name to CEL expression", Type: TypeMap},
			}},
			{Type: "password", Doc: "What a password has to be, how long it may be used for, and how it is hashed", Attrs: []Attr{
				{Name: "required", Doc: "Whether registering requires a password at all", Type: TypeBool},
				{Name: "min_length", Doc: "Shortest password accepted", Type: TypeNumber},
				{Name: "max_length", Doc: "Longest password accepted", Type: TypeNumber},
				{Name: "require_upper", Doc: "Demand an upper-case letter", Type: TypeBool},
				{Name: "require_lower", Doc: "Demand a lower-case letter", Type: TypeBool},
				{Name: "require_number", Doc: "Demand a digit", Type: TypeBool},
				{Name: "require_special", Doc: "Demand a symbol", Type: TypeBool},
				{Name: "reject_patterns", Doc: "Passwords matching any of these are refused", Type: TypeList},
				{Name: "history", Doc: "Refuse the last N passwords, the one in use counting as the most recent", Type: TypeNumber},
				{Name: "max_age", Doc: "How long a password may be used for, such as 90d. On a database-backed store this needs the users fields block to name password_changed_at", Type: TypeString},
				{Name: "warn_before", Doc: "How long before expiry a sign-in says so, such as 7d", Type: TypeString},
				{Name: "reset_token_ttl", Doc: "How long a password reset token is good for; one hour unless written", Type: TypeString},
				{Name: "breach_check", Doc: "Check candidates against known breached passwords", Type: TypeBool},
				{Name: "algorithm", Doc: "Hashing algorithm", Type: TypeString, Values: []string{"argon2id", "bcrypt"}},
				{Name: "memory", Doc: "Argon2 memory cost in KiB", Type: TypeNumber},
				{Name: "iterations", Doc: "Hashing iterations", Type: TypeNumber},
				{Name: "parallelism", Doc: "Argon2 parallelism", Type: TypeNumber},
				{Name: "salt_length", Doc: "Salt length in bytes", Type: TypeNumber},
				{Name: "key_length", Doc: "Derived key length in bytes", Type: TypeNumber},
			}},
			{Type: "mfa", Doc: "Multi-factor authentication; present means enabled", Attrs: []Attr{
				{Name: "enabled", Doc: "Whether MFA runs at all; writing the block is what turns it on", Type: TypeBool},
				{Name: "required", Doc: "Whether accounts must have a second factor, or are only offered one", Type: TypeString, Values: []string{"optional", "true", "false", "admin_only"}},
				{Name: "require_for", Doc: "Roles a second factor is required of, when it is not required of everybody", Type: TypeList},
				{Name: "methods", Doc: "How a second factor may be enrolled", Type: TypeList, Values: []string{"totp", "webauthn"}},
				{Name: "require_multiple", Doc: "Demand more than one enrolled factor", Type: TypeBool},
				{Name: "min_factors", Doc: "How many factors an account must have enrolled", Type: TypeNumber},
				{Name: "grace_period", Doc: "How long a new account may go without enrolling, such as 7d; without it, from the start", Type: TypeString},
			}},
			{Type: "security", Doc: "Lockout, rate limiting and related defences", Children: []Block{
				{Type: "brute_force", Doc: "Slow down and then lock out repeated failures", Attrs: []Attr{
					{Name: "enabled", Doc: "Whether failures are counted at all", Type: TypeBool},
					{Name: "max_attempts", Doc: "Failures allowed inside the window before locking out", Type: TypeNumber},
					{Name: "window", Doc: "How long failures are remembered for, such as 15m", Type: TypeString},
					{Name: "lockout_time", Doc: "How long an account stays locked, such as 30m", Type: TypeString},
					{Name: "track_by", Doc: "What failures are counted against; the pair unless written", Type: TypeString, Values: []string{"ip", "user", "ip+user"}},
					{Name: "fail_delay", Doc: "A flat wait added to every refusal, such as 500ms, so an attacker cannot tell a wrong password from a missing account by timing", Type: TypeString},
				}, Children: []Block{
					{Type: "progressive_delay", Doc: "Make each successive failure wait longer than the last", Attrs: []Attr{
						{Name: "enabled", Doc: "Whether the wait grows", Type: TypeBool},
						{Name: "initial", Doc: "Wait after the first failure, such as 1s", Type: TypeString},
						{Name: "multiplier", Doc: "What the wait is multiplied by each time", Type: TypeNumber},
						{Name: "max", Doc: "Cap on the wait however far it grows, such as 30s", Type: TypeString},
					}},
				}},
				{Type: "replay_protection", Doc: "Refuse a token that has already been presented", Attrs: []Attr{
					{Name: "enabled", Doc: "Whether tokens are remembered", Type: TypeBool},
					{Name: "window", Doc: "How long one is remembered for, such as 5m", Type: TypeString},
				}},
				{Type: "ip_rules", Doc: "Who may reach the auth endpoints at all", Attrs: []Attr{
					{Name: "allowlist", Doc: "Addresses or ranges allowed; naming any refuses everything else", Type: TypeList},
					{Name: "blocklist", Doc: "Addresses or ranges refused", Type: TypeList},
					{Name: "block_countries", Doc: "Country codes refused; needs a geoip source", Type: TypeList},
					{Name: "allow_countries", Doc: "Country codes allowed; naming any refuses everything else", Type: TypeList},
				}},
				{Type: "rate_limit", Doc: "How often the auth endpoints may be called, in general and one by one", Attrs: []Attr{
					{Name: "enabled", Doc: "Whether calls are counted at all", Type: TypeBool},
					{Name: "rate", Doc: "Calls allowed per window", Type: TypeNumber},
					{Name: "burst", Doc: "How far above the rate a short burst may go", Type: TypeNumber},
					{Name: "window", Doc: "The period the rate is counted over, such as 1m", Type: TypeString},
					{Name: "key_by", Doc: "What calls are counted against", Type: TypeString, Values: []string{"ip", "user", "ip+user"}},
				}, Children: authRateLimitBlocks()},
				{Type: "impossible_travel", Doc: "Notice two sign-ins too far apart for the time between them", Attrs: []Attr{
					{Name: "enabled", Doc: "Whether distances are measured at all; needs a geoip block", Type: TypeBool},
					{Name: "max_speed_kmh", Doc: "Above this, two sign-ins are not the same person; 900 unless written, which is faster than a flight", Type: TypeNumber},
					{Name: "on_detect", Doc: "What a sign-in that could not have got there means", Type: TypeString, Values: []string{"notify", "challenge", "block"}},
				}, Children: []Block{
					{Type: "geoip", Doc: "Where an address is looked up; name one of these, not both", Attrs: []Attr{
						{Name: "database", Doc: "Path to a MaxMind City database, which you download under MaxMind's licence", Type: TypeString},
						{Name: "api", Doc: "URL of an HTTP service with {ip} in it, such as https://geo.example/{ip}", Type: TypeString},
					}},
				}},
				{Type: "device_binding", Doc: "Notice when an account signs in from something it has not used before", Attrs: []Attr{
					{Name: "enabled", Doc: "Whether devices are watched at all", Type: TypeBool},
					{Name: "fingerprint", Doc: "What identifies a device, from what a server can see", Type: TypeList, Values: []string{"user_agent", "ip", "device_id"}},
					{Name: "trust_duration", Doc: "How long a device stays recognised without being used, such as 30d", Type: TypeString},
					{Name: "max_devices", Doc: "How many devices to remember per account; the least recently used is dropped", Type: TypeNumber},
					{Name: "on_new_device", Doc: "What a device the account has not used means", Type: TypeString, Values: []string{"notify", "allow", "challenge", "block"}},
				}},
			}},
			{Type: "sessions", Doc: "How long a sign-in lasts, how many at once, and what is kept about it", Attrs: []Attr{
				{Name: "max_active", Doc: "How many sessions one person may hold at once; 0 is unlimited", Type: TypeNumber},
				{Name: "idle_timeout", Doc: "End a session left untouched for this long, such as 30m", Type: TypeString},
				{Name: "absolute_timeout", Doc: "End a session this long after it began however active it is, such as 24h", Type: TypeString},
				{Name: "on_max_reached", Doc: "What happens to a sign-in beyond max_active", Type: TypeString, Values: []string{"revoke_oldest", "reject_new", "deny"}},
				{Name: "extend_on_activity", Doc: "Push the idle timeout forward on each request", Type: TypeBool},
				{Name: "allow_list", Doc: "Serve GET /auth/sessions, letting somebody see where they are signed in; on unless written false", Type: TypeBool},
				{Name: "allow_revoke", Doc: "Serve DELETE /auth/sessions/{id}, letting somebody end one; on unless written false", Type: TypeBool},
				{Name: "track", Doc: "What to record about a sign-in. Naming none records address and browser both; naming some records only those", Type: TypeList, Values: []string{"ip", "user_agent"}},
			}},
			{Type: "social", Doc: "Social login providers", Children: []Block{
				{Type: "google", Doc: "Sign in with Google", Attrs: oauthProviderAttrs()},
				{Type: "github", Doc: "Sign in with GitHub", Attrs: oauthProviderAttrs()},
				{Type: "apple", Doc: "Sign in with Apple, which signs its client secret rather than handing you one", Attrs: []Attr{
					{Name: "client_id", Doc: "Services ID from Apple", Type: TypeString, Required: true},
					{Name: "team_id", Doc: "Apple developer team identifier", Type: TypeString, Required: true},
					{Name: "key_id", Doc: "Identifier of the signing key", Type: TypeString, Required: true},
					{Name: "private_key", Doc: "Path to the .p8 signing key", Type: TypeString, Required: true},
				}},
			}},
			{Type: "sso", Doc: "Single sign-on against an identity provider", Children: []Block{
				{Type: "oidc", Doc: "An OpenID Connect provider", Labels: 1, Attrs: []Attr{
					{Name: "issuer", Doc: "Issuer URL, whose /.well-known/openid-configuration describes the rest", Type: TypeString, Required: true},
					{Name: "client_id", Doc: "Client identifier registered with the provider", Type: TypeString, Required: true},
					{Name: "client_secret", Doc: "Client secret registered with the provider", Type: TypeString, Required: true},
					{Name: "scopes", Doc: "Scopes to ask for; openid, profile and email unless written", Type: TypeList},
					{Name: "claims", Doc: "Which claim fills which user field, as field to claim name", Type: TypeMap},
				}},
				{Type: "saml", Doc: "A SAML identity provider", Labels: 1, Attrs: []Attr{
					{Name: "metadata_url", Doc: "Provider metadata URL, which supplies the SSO URL and certificate", Type: TypeString},
					{Name: "idp_sso_url", Doc: "Sign-in URL, when there is no metadata to read it from", Type: TypeString},
					{Name: "idp_certificate", Doc: "Path to the certificate assertions are verified with", Type: TypeString},
					{Name: "entity_id", Doc: "This service's entity ID, as registered with the provider", Type: TypeString, Required: true},
					{Name: "acs_url", Doc: "Where the provider posts its assertion back to", Type: TypeString, Required: true},
					{Name: "attributes", Doc: "Which assertion attribute fills which user field", Type: TypeMap},
				}},
				{Type: "linking", Doc: "How an identity from this provider joins an account that already exists", Attrs: accountLinkingAttrs()},
			}},
			{Type: "provider", Doc: "Validate a credential against an external HTTP endpoint", Labels: 1, Attrs: []Attr{
				{Name: "type", Doc: "What kind of credential this provider checks", Type: TypeString, Required: true},
				{Name: "validate", Doc: "URL the credential is sent to, with {token} standing for it", Type: TypeString, Required: true},
				{Name: "request", Doc: "Headers to send, with {token} standing for the credential", Type: TypeMap},
				{Name: "sync_to", Doc: "Connector to copy the identity into. Not implemented; naming one logs a warning", Type: TypeString, Ref: RefConnector},
			}, Children: []Block{
				{Type: "response", Doc: "How the endpoint's answer becomes a user. Each is a CEL expression over status and body", Attrs: []Attr{
					{Name: "success", Doc: "Whether the credential is good, such as status == 200", Type: TypeString, Required: true},
					{Name: "token", Doc: "Expression yielding the token to carry forward", Type: TypeString},
					{Name: "user_id", Doc: "Expression yielding the identifier of whoever presented it", Type: TypeString},
					{Name: "email", Doc: "Expression yielding their address", Type: TypeString},
					{Name: "roles", Doc: "Expression yielding their roles", Type: TypeString},
				}},
			}},
			{Type: "account_linking", Doc: "Joining identities that belong to one person", Attrs: accountLinkingAttrs()},
			{Type: "endpoints", Doc: "Paths the auth routes are served on. Naming one overrides that one; the rest keep their defaults. Turning one off is enabled = false inside its own block", Attrs: []Attr{
				{Name: "prefix", Doc: "Path every auth route hangs off; /auth unless written", Type: TypeString},
			}, Children: authEndpointBlocks()},
			{Type: "hooks", Doc: "Flows invoked around auth events", Children: []Block{
				{Type: "before_login", Doc: "Runs before a sign-in is checked; with on_error = \"fail\" it can refuse one", Attrs: hookAttrs()},
				{Type: "after_login", Doc: "Runs once somebody has signed in", Attrs: hookAttrs()},
				{Type: "after_register", Doc: "Runs once an account has been created", Attrs: hookAttrs()},
				{Type: "on_failed_login", Doc: "Runs when a sign-in is refused", Attrs: hookAttrs()},
				{Type: "on_suspicious_activity", Doc: "Runs when something about a sign-in is out of the ordinary", Attrs: hookAttrs()},
				{Type: "on_password_reset", Doc: "Delivers a reset token; the flow is given auth.reset_token and must get it to the person", Attrs: hookAttrs()},
				{Type: "before_password_change", Doc: "Runs before a password is changed; with on_error = \"fail\" it can refuse one", Attrs: hookAttrs()},
				{Type: "after_password_change", Doc: "Runs once a password has been changed", Attrs: hookAttrs()},
			}},
			{Type: "audit", Doc: "What to record", Attrs: []Attr{
				{Name: "connector", Doc: "Where audit records are written", Type: TypeString, Required: true, Ref: RefConnector},
				{Name: "enabled", Doc: "Whether auditing runs", Type: TypeBool},
				{Name: "table", Doc: "Table or collection to write into", Type: TypeString},
				{Name: "events", Doc: "Events to record", Type: TypeList},
				{Name: "include", Doc: "Extra fields to include on each record", Type: TypeList},
				{Name: "retention", Doc: "How long records are kept", Type: TypeDuration},
				{Name: "stream_to", Doc: "Connector to stream records to as well", Type: TypeString, Ref: RefConnector},
			}},
		},
	}
}

// SecuritySchema describes the security block. Transcribed from
// internal/parser/security.go; the parser reads it with Content, so it is not
// Open.
func SecuritySchema() Block {
	return Block{
		Type: "security",
		Doc:  "Input limits and sanitization rules",
		Attrs: []Attr{
			{Name: "max_input_length", Doc: "Largest accepted payload, in bytes", Type: TypeNumber},
			{Name: "max_field_length", Doc: "Largest accepted single field, in bytes", Type: TypeNumber},
			{Name: "max_field_depth", Doc: "How deeply nested a payload may be", Type: TypeNumber},
			{Name: "allowed_control_chars", Doc: "Control characters to let through rather than strip", Type: TypeList},
		},
		Children: []Block{
			{Type: "sanitizer", Doc: "A named sanitization rule", Labels: 1, Attrs: []Attr{
				{Name: "source", Doc: "Built-in sanitizer to use", Type: TypeString},
				{Name: "wasm", Doc: "Path to a .wasm module implementing it", Type: TypeString},
				{Name: "entrypoint", Doc: "Exported function to call in the module", Type: TypeString},
				{Name: "apply_to", Doc: "Which payloads it applies to", Type: TypeList},
				{Name: "fields", Doc: "Specific fields it applies to", Type: TypeList},
			}},
			{Type: "flow", Doc: "Per-flow overrides of the limits above", Labels: 1, Attrs: []Attr{
				{Name: "max_input_length", Doc: "Largest accepted payload for this flow, in bytes", Type: TypeNumber},
				{Name: "max_field_length", Doc: "Largest accepted single field for this flow, in bytes", Type: TypeNumber},
				{Name: "sanitizers", Doc: "Sanitizers to run for this flow", Type: TypeList},
			}},
		},
	}
}

func MocksSchema() Block {
	return Block{
		Type: "mocks",
		Doc:  "Answer from recorded data instead of the real service",
		Attrs: []Attr{
			{Name: "enabled", Doc: "Answer from mocks rather than the real connectors", Type: TypeBool},
			{Name: "path", Doc: "Directory holding the recorded answers (default: ./mocks)", Type: TypeString},
		},
		Children: []Block{MockConnectorsSchema()},
	}
}

// MockConnectorsSchema describes the per-connector settings inside mocks.
//
// Its attributes are connector names, so the block stays open — but what each
// one takes is not open at all, and was described nowhere: latency and
// fail_rate are the reason to mock a connector rather than point it at a test
// double, and neither appears in the documentation or in any schema.
func MockConnectorsSchema() Block {
	return Block{
		Type: "connectors",
		Doc:  "Per-connector mock settings, keyed by connector name: db = { latency = \"50ms\" }",
		Open: true,
	}
}

// ConstantsSchema is the block that declares values used in more than one
// place.
//
// Open, because the names are the author's: a constants block holds whatever
// that configuration needs twice. What it may not hold is a nested block —
// these are values, not configuration.
func ConstantsSchema() Block {
	return Block{
		Type: "constants",
		Doc:  "Values declared once and referred to as constants.<name>, in HCL attributes and in CEL expressions alike",
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
			// invalidate_on was accepted by the parser and undeclared here.
			// The parity test only reads schema → parser, so an attribute the
			// schema omits is invisible to it: completions never offered this
			// one and generated documentation never mentioned it.
			{Name: "invalidate_on", Doc: "Events that invalidate entries in this cache", Type: TypeList},
			{Name: "encoding", Doc: "Wire format for entries in this cache, inherited by any flow that references it", Type: TypeList, Values: []string{"json", "base64", "gzip"}},
		},
	}
}

// EnvironmentSchema is deliberately not in BuiltinRootSchemas: no `environment`
// block exists. The parser accepts no such block type, so a document containing
// one fails outright — but the schema advertised it, which is what feeds
// completions and generated documentation.
//
// Per-environment configuration is done with connector profiles and env(),
// selected by `--env`. Kept only so a caller referencing it keeps building;
// there is nothing to describe.
//
// Deprecated: there is no environment block.
func EnvironmentSchema() Block {
	return Block{
		Type:   "environment",
		Doc:    "Not a block Mycel accepts — use connector profiles and env() with --env",
		Labels: 1,
	}
}

// connectorTypes returns all known connector type values.
// ConnectorTypeNames is every connector type this runtime ships.
//
// The parser reads it to decide how strictly a connector block is checked: a
// type from this list is held to the attributes that exist, and one that is
// not — a type a plugin brought — carries its own, declared in the plugin's
// manifest rather than here.
func ConnectorTypeNames() []string {
	return connectorTypes()
}

func connectorTypes() []string {
	return []string{
		"rest", "http", "database", "mq", "graphql", "grpc", "file", "s3",
		"cache", "tcp", "exec", "soap", "mqtt", "ftp", "cdc",
		"websocket", "sse", "elasticsearch", "oauth", "profiled",
		"email", "slack", "discord", "sms", "push", "webhook", "pdf",
	}
}

// oauthProviderAttrs are what a social provider that hands out a client secret
// takes — Google and GitHub both.
func oauthProviderAttrs() []Attr {
	return []Attr{
		{Name: "client_id", Doc: "Client identifier registered with the provider", Type: TypeString, Required: true},
		{Name: "client_secret", Doc: "Client secret registered with the provider", Type: TypeString, Required: true},
		{Name: "scopes", Doc: "Scopes to ask for", Type: TypeList},
	}
}

// accountLinkingAttrs describe how a second identity joins an account that
// already exists. The same block appears at the top of auth and inside sso.
func accountLinkingAttrs() []Attr {
	return []Attr{
		{Name: "enabled", Doc: "Whether identities are joined at all", Type: TypeBool},
		{Name: "match_by", Doc: "What decides two identities are one person", Type: TypeString, Values: []string{"email", "phone", "custom"}},
		{Name: "require_verification", Doc: "Only join when the provider says the address is verified", Type: TypeBool},
		{Name: "on_match", Doc: "What happens when an existing account matches", Type: TypeString, Values: []string{"link", "reject", "prompt"}},
		{Name: "custom_match", Doc: "CEL expression deciding the match, when match_by is custom", Type: TypeString},
	}
}

// authEndpointBlocks are the seventeen routes the auth system serves, each of
// which can be moved, renamed or switched off.
func authEndpointBlocks() []Block {
	routes := []struct{ name, doc string }{
		{"login", "Sign in"},
		{"logout", "Sign out"},
		{"register", "Create an account"},
		{"refresh", "Exchange a refresh token for a new access token"},
		{"me", "Who the caller is"},
		{"password_forgot", "Ask for a reset token"},
		{"password_reset", "Set a new password with a reset token"},
		{"password_change", "Change a password from inside a session"},
		{"sessions_list", "Where this account is signed in"},
		{"sessions_revoke", "End one of those sessions"},
		{"mfa_setup", "Begin enrolling a second factor"},
		{"mfa_verify", "Finish enrolling, or answer a challenge"},
		{"mfa_disable", "Remove a second factor"},
		{"mfa_recovery", "Use a recovery code"},
		{"social_callback", "Where a social provider returns to"},
		{"sso_callback", "Where an SSO provider returns to"},
	}

	blocks := make([]Block, 0, len(routes))
	for _, route := range routes {
		blocks = append(blocks, Block{Type: route.name, Doc: route.doc, Attrs: []Attr{
			{Name: "path", Doc: "Path this route is served on", Type: TypeString},
			{Name: "method", Doc: "HTTP method it answers", Type: TypeString},
			{Name: "enabled", Doc: "Whether it is served at all", Type: TypeBool},
		}})
	}
	return blocks
}

// authRateLimitBlocks are the per-endpoint limits, which take the same three
// attributes as the general one they override.
func authRateLimitBlocks() []Block {
	endpoints := []struct{ name, doc string }{
		{"login", "Sign-in attempts"},
		{"register", "Account creation"},
		{"refresh", "Token refreshes"},
		{"logout", "Sign-outs"},
		{"change_password", "Password changes"},
		{"sessions", "Session listing and revocation"},
	}

	blocks := make([]Block, 0, len(endpoints))
	for _, endpoint := range endpoints {
		blocks = append(blocks, Block{Type: endpoint.name, Doc: endpoint.doc, Attrs: []Attr{
			{Name: "rate", Doc: "Calls allowed per window", Type: TypeNumber, Required: true},
			{Name: "burst", Doc: "How far above the rate a short burst may go", Type: TypeNumber},
			{Name: "window", Doc: "The period the rate is counted over, such as 1m", Type: TypeString},
		}})
	}
	return blocks
}
