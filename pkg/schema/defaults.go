package schema

// DefaultRegistry creates a registry pre-populated with the standard root block
// schemas (flow, aspect, service, etc.) but WITHOUT connector-specific schemas.
// For a full registry with all connectors, use connectors.FullRegistry().
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.SetBuiltins(BuiltinRootSchemas())
	return reg
}

// RegisterFunc is a function that registers connector schemas into a registry.
type RegisterFunc func(reg *Registry)

// NewRegistryWith creates a registry using a custom registration function.
func NewRegistryWith(fn RegisterFunc) *Registry {
	reg := DefaultRegistry()
	if fn != nil {
		fn(reg)
	}
	return reg
}
