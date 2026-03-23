package schema

import "sync"

// Registry holds connector schema providers indexed by type and driver.
// The parser and IDE engine query this to get connector-specific schemas.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]ConnectorSchemaProvider // key: "type" or "type:driver"
	builtins  []Block                            // non-connector root schemas (flow, aspect, service, etc.)
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]ConnectorSchemaProvider),
	}
}

// Register adds a connector schema provider for the given type and driver.
// If driver is empty, the provider is used for all drivers of that type.
func (r *Registry) Register(connType, driver string, p ConnectorSchemaProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := connType
	if driver != "" {
		key = connType + ":" + driver
	}
	r.providers[key] = p
}

// Lookup returns the schema provider for the given type and driver.
// Falls back to type-only if no type:driver match exists.
func (r *Registry) Lookup(connType, driver string) ConnectorSchemaProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try type:driver first
	if driver != "" {
		if p, ok := r.providers[connType+":"+driver]; ok {
			return p
		}
	}
	// Fall back to type-only
	return r.providers[connType]
}

// SetBuiltins sets the non-connector root block schemas (flow, aspect, service, etc.).
func (r *Registry) SetBuiltins(blocks []Block) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.builtins = blocks
}

// RootSchema returns the complete root schema including all registered connectors.
// This is what the parser and IDE use to validate top-level blocks.
func (r *Registry) RootSchema() []Block {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]Block, len(r.builtins))
	copy(schemas, r.builtins)

	// The connector block schema is the base merged with all known types
	// For the IDE, we return the base connector schema — type-specific
	// enrichment happens dynamically when the connector type is known.
	return schemas
}

// ConnectorSchema returns the full connector block schema for a given type and driver.
// Merges the base connector schema with the type-specific schema from the registry.
func (r *Registry) ConnectorSchema(connType, driver string) Block {
	base := BaseConnectorSchema()
	p := r.Lookup(connType, driver)
	if p == nil {
		return base
	}
	return Merge(base, p.ConnectorSchema())
}

// AllConnectorTypes returns all registered connector type names.
func (r *Registry) AllConnectorTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var types []string
	for key := range r.providers {
		// Extract type from "type" or "type:driver"
		t := key
		for i := 0; i < len(key); i++ {
			if key[i] == ':' {
				t = key[:i]
				break
			}
		}
		if !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}
	return types
}
