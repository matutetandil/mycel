package schema

// DefaultRegistry creates a registry pre-populated with the standard root block
// schemas (flow, aspect, service, etc.) but WITHOUT connector-specific schemas.
// Use this as a base and register connector schemas via RegisterFunc.
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.SetBuiltins(BuiltinRootSchemas())
	return reg
}

// RegisterFunc is a function that registers connector schemas into a registry.
// The runtime provides one (registerBuiltinSchemas) that registers all built-in connectors.
type RegisterFunc func(reg *Registry)

// NewRegistryWith creates a fully-populated registry using the provided registration function.
// This allows pkg/ide (which can't import internal/) to receive a fully-populated registry
// from the caller (CLI, Studio, or runtime) which CAN import internal/.
//
// Usage from Studio or CLI:
//
//	import "github.com/matutetandil/mycel/internal/runtime"
//	reg := schema.NewRegistryWith(runtime.RegisterBuiltinSchemas)
//	engine := ide.NewEngine(dir, ide.WithRegistry(reg))
func NewRegistryWith(fn RegisterFunc) *Registry {
	reg := DefaultRegistry()
	if fn != nil {
		fn(reg)
	}
	return reg
}
