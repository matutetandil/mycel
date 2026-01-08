package connector

// OperationDef defines a named operation on a connector.
// Operations encapsulate connector-specific logic (queries, endpoints, etc.)
// and allow flows to reference them by name.
type OperationDef struct {
	// Name is the operation identifier.
	Name string

	// Description provides documentation for this operation.
	Description string

	// REST-specific attributes
	Method string // GET, POST, PUT, DELETE, PATCH
	Path   string // /users, /users/{id}

	// Database-specific attributes
	Query string // SELECT * FROM users WHERE id = $1
	Table string // users

	// GraphQL-specific attributes
	OperationType string // Query, Mutation, Subscription
	Field         string // users, createUser

	// gRPC-specific attributes
	Service string // UserService
	RPC     string // GetUser

	// MQ-specific attributes
	Exchange   string // events
	RoutingKey string // user.created
	Queue      string // user_events

	// TCP-specific attributes
	Protocol string // json, msgpack, nestjs
	Action   string // get_user, create_user

	// File/S3-specific attributes
	PathPattern string // /data/{date}/*.json

	// Cache-specific attributes
	KeyPattern string // user:{id}
	TTL        int    // seconds

	// Exec-specific attributes
	Command string   // /usr/bin/script.sh
	Args    []string // ["--flag", "value"]

	// Common attributes
	Input   string      // Type reference for input validation
	Output  string      // Type reference for output validation
	Timeout int         // Operation timeout in milliseconds
	Params  []*ParamDef // Operation parameters
}

// ParamDef defines an operation parameter.
type ParamDef struct {
	// Name is the parameter identifier.
	Name string

	// Type is the parameter type (string, number, boolean, array, object).
	Type string

	// Required indicates if the parameter is mandatory.
	Required bool

	// Default is the default value if not provided.
	Default interface{}

	// Description provides documentation for this parameter.
	Description string

	// In specifies where the parameter comes from (path, query, header, body).
	// Used primarily for REST operations.
	In string

	// Validation constraints
	Min       *float64 // Minimum value for numbers
	Max       *float64 // Maximum value for numbers
	MinLength *int     // Minimum length for strings
	MaxLength *int     // Maximum length for strings
	Pattern   string   // Regex pattern for strings
	Enum      []string // Allowed values
}

// GetParam finds a parameter by name.
func (o *OperationDef) GetParam(name string) *ParamDef {
	for _, p := range o.Params {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// HasParam checks if a parameter exists.
func (o *OperationDef) HasParam(name string) bool {
	return o.GetParam(name) != nil
}

// RequiredParams returns all required parameters.
func (o *OperationDef) RequiredParams() []*ParamDef {
	var required []*ParamDef
	for _, p := range o.Params {
		if p.Required {
			required = append(required, p)
		}
	}
	return required
}

// DefaultValues returns a map of parameter names to their default values.
func (o *OperationDef) DefaultValues() map[string]interface{} {
	defaults := make(map[string]interface{})
	for _, p := range o.Params {
		if p.Default != nil {
			defaults[p.Name] = p.Default
		}
	}
	return defaults
}

// OperationProvider interface for connectors that support named operations.
type OperationProvider interface {
	// GetOperation returns an operation by name.
	GetOperation(name string) *OperationDef

	// ListOperations returns all defined operations.
	ListOperations() []*OperationDef

	// HasOperation checks if an operation exists.
	HasOperation(name string) bool
}
