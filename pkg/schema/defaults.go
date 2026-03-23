package schema

// DefaultRegistry creates a registry pre-populated with all built-in connector schemas
// and the standard root block schemas (flow, aspect, service, etc.).
//
// This is the main entry point for consumers (IDE, parser) that want the complete
// Mycel schema without manually registering each connector.
//
// NOTE: This function registers schemas using the ConnectorSchemaProvider interface.
// Each connector package has a ConnectorSchemaDef struct that implements it.
// The registration is done here (in pkg/) rather than in internal/ because
// pkg/schema has no access to internal/connector/ packages.
// When the connector packages are wired in (Phase D), they'll register themselves
// via init() or explicit registration from the runtime.
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.SetBuiltins(BuiltinRootSchemas())
	return reg
}
