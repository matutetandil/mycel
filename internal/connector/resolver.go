package connector

import (
	"fmt"
	"strings"
	"sync"
)

// OperationResolver resolves named operations to their executable format.
// All operations must be defined in the connector configuration.
type OperationResolver struct {
	mu         sync.RWMutex
	operations map[string]map[string]*OperationDef // connector name -> operation name -> def
	configs    map[string]*Config                  // connector name -> config
}

// NewOperationResolver creates a new operation resolver.
func NewOperationResolver() *OperationResolver {
	return &OperationResolver{
		operations: make(map[string]map[string]*OperationDef),
		configs:    make(map[string]*Config),
	}
}

// Register registers a connector's configuration for operation resolution.
func (r *OperationResolver) Register(config *Config) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.configs[config.Name] = config

	// Index operations by name
	if len(config.Operations) > 0 {
		r.operations[config.Name] = make(map[string]*OperationDef)
		for _, op := range config.Operations {
			r.operations[config.Name][op.Name] = op
		}
	}
}

// ResolvedOperation holds the result of resolving an operation.
type ResolvedOperation struct {
	// Inline is the resolved inline format string for internal use
	Inline string

	// Operation is the operation definition
	Operation *OperationDef

	// Params holds resolved parameters with defaults applied
	Params map[string]interface{}
}

// Resolve converts an operation name to its executable format.
// Returns an error if the operation is not found.
func (r *OperationResolver) Resolve(connectorName, opName string) (*ResolvedOperation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Validate operation name
	if strings.TrimSpace(opName) == "" {
		return nil, fmt.Errorf("operation name cannot be empty")
	}

	// Look up connector operations
	ops, ok := r.operations[connectorName]
	if !ok {
		return nil, fmt.Errorf("connector '%s' has no operations defined", connectorName)
	}

	// Look up the named operation
	op, ok := ops[opName]
	if !ok {
		return nil, fmt.Errorf("operation '%s' not found in connector '%s'", opName, connectorName)
	}

	// Get connector config for type
	config := r.configs[connectorName]
	if config == nil {
		return nil, fmt.Errorf("connector config not found: %s", connectorName)
	}

	// Format operation for connector type
	inline, err := r.formatForConnector(config.Type, config.Driver, op)
	if err != nil {
		return nil, fmt.Errorf("failed to format operation: %w", err)
	}

	return &ResolvedOperation{
		Inline:    inline,
		Operation: op,
		Params:    op.DefaultValues(),
	}, nil
}

// formatForConnector converts an operation to its executable format based on connector type.
func (r *OperationResolver) formatForConnector(connType, driver string, op *OperationDef) (string, error) {
	switch connType {
	case "rest":
		return r.formatREST(op)
	case "database":
		return r.formatDatabase(driver, op)
	case "graphql":
		return r.formatGraphQL(op)
	case "grpc":
		return r.formatGRPC(op)
	case "queue", "mq":
		return r.formatQueue(driver, op)
	case "tcp":
		return r.formatTCP(op)
	case "file":
		return r.formatFile(op)
	case "s3":
		return r.formatS3(op)
	case "cache":
		return r.formatCache(op)
	case "exec":
		return r.formatExec(op)
	default:
		// For unknown types, try to find a sensible format
		if op.Method != "" && op.Path != "" {
			return r.formatREST(op)
		}
		if op.Query != "" || op.Table != "" {
			return r.formatDatabase(driver, op)
		}
		// Fall back to operation name
		return op.Name, nil
	}
}

// formatREST formats a REST operation: "GET /users"
func (r *OperationResolver) formatREST(op *OperationDef) (string, error) {
	if op.Method == "" || op.Path == "" {
		return "", fmt.Errorf("REST operation '%s' requires method and path", op.Name)
	}
	return fmt.Sprintf("%s %s", strings.ToUpper(op.Method), op.Path), nil
}

// formatDatabase formats a database operation.
func (r *OperationResolver) formatDatabase(driver string, op *OperationDef) (string, error) {
	// If raw query is specified, return it
	if op.Query != "" {
		return op.Query, nil
	}

	// Otherwise return the table name
	if op.Table != "" {
		return op.Table, nil
	}

	return "", fmt.Errorf("database operation '%s' requires query or table", op.Name)
}

// formatGraphQL formats a GraphQL operation: "Query.users"
func (r *OperationResolver) formatGraphQL(op *OperationDef) (string, error) {
	if op.OperationType == "" || op.Field == "" {
		return "", fmt.Errorf("GraphQL operation '%s' requires operation_type and field", op.Name)
	}

	opType := strings.Title(strings.ToLower(op.OperationType))
	return fmt.Sprintf("%s.%s", opType, op.Field), nil
}

// formatGRPC formats a gRPC operation: "UserService/GetUser"
func (r *OperationResolver) formatGRPC(op *OperationDef) (string, error) {
	if op.Service == "" || op.RPC == "" {
		return "", fmt.Errorf("gRPC operation '%s' requires service and rpc", op.Name)
	}
	return fmt.Sprintf("%s/%s", op.Service, op.RPC), nil
}

// formatQueue formats a queue operation: topic/queue name or routing key
func (r *OperationResolver) formatQueue(driver string, op *OperationDef) (string, error) {
	// For RabbitMQ, use exchange and routing key
	if driver == "rabbitmq" {
		if op.Exchange != "" && op.RoutingKey != "" {
			return fmt.Sprintf("%s.%s", op.Exchange, op.RoutingKey), nil
		}
		if op.Queue != "" {
			return op.Queue, nil
		}
	}

	// For Kafka, use queue as topic
	if op.Queue != "" {
		return op.Queue, nil
	}

	return "", fmt.Errorf("queue operation '%s' requires exchange/routing_key or queue", op.Name)
}

// formatTCP formats a TCP operation: action identifier
func (r *OperationResolver) formatTCP(op *OperationDef) (string, error) {
	if op.Action == "" {
		return "", fmt.Errorf("TCP operation '%s' requires action", op.Name)
	}
	return op.Action, nil
}

// formatFile formats a file operation: path pattern
func (r *OperationResolver) formatFile(op *OperationDef) (string, error) {
	if op.PathPattern == "" {
		return "", fmt.Errorf("file operation '%s' requires path_pattern", op.Name)
	}
	return op.PathPattern, nil
}

// formatS3 formats an S3 operation: path pattern (key prefix/pattern)
func (r *OperationResolver) formatS3(op *OperationDef) (string, error) {
	if op.PathPattern == "" {
		return "", fmt.Errorf("S3 operation '%s' requires path_pattern", op.Name)
	}
	return op.PathPattern, nil
}

// formatCache formats a cache operation: key pattern
func (r *OperationResolver) formatCache(op *OperationDef) (string, error) {
	if op.KeyPattern == "" {
		return "", fmt.Errorf("cache operation '%s' requires key_pattern", op.Name)
	}
	return op.KeyPattern, nil
}

// formatExec formats an exec operation: command
func (r *OperationResolver) formatExec(op *OperationDef) (string, error) {
	if op.Command == "" {
		return "", fmt.Errorf("exec operation '%s' requires command", op.Name)
	}
	return op.Command, nil
}

// GetOperation retrieves an operation by connector and operation name.
func (r *OperationResolver) GetOperation(connectorName, opName string) *OperationDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ops, ok := r.operations[connectorName]
	if !ok {
		return nil
	}
	return ops[opName]
}

// HasOperation checks if a named operation exists.
func (r *OperationResolver) HasOperation(connectorName, opName string) bool {
	return r.GetOperation(connectorName, opName) != nil
}

// ListOperations returns all operations for a connector.
func (r *OperationResolver) ListOperations(connectorName string) []*OperationDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ops, ok := r.operations[connectorName]
	if !ok {
		return nil
	}

	result := make([]*OperationDef, 0, len(ops))
	for _, op := range ops {
		result = append(result, op)
	}
	return result
}

// GetConnectorConfig returns the stored config for a connector.
func (r *OperationResolver) GetConnectorConfig(connectorName string) *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.configs[connectorName]
}

// ValidateParams validates provided parameters against an operation's param definitions.
func (r *OperationResolver) ValidateParams(connectorName, opName string, params map[string]interface{}) []error {
	op := r.GetOperation(connectorName, opName)
	if op == nil {
		return []error{fmt.Errorf("operation '%s' not found in connector '%s'", opName, connectorName)}
	}

	var errors []error

	// Check required parameters
	for _, p := range op.RequiredParams() {
		if _, ok := params[p.Name]; !ok {
			errors = append(errors, fmt.Errorf("missing required parameter: %s", p.Name))
		}
	}

	// Type checking could be added here

	return errors
}
