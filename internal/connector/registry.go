package connector

import (
	"context"
	"fmt"
	"sync"
)

// Factory creates connectors based on configuration.
// Open/Closed Principle - new connector types can be added via new factories.
type Factory interface {
	// Create creates a new connector instance from configuration.
	Create(ctx context.Context, config *Config) (Connector, error)

	// Supports returns true if this factory can create the given connector type.
	Supports(connectorType, driver string) bool
}

// Registry manages connector factories and instances.
// Dependency Inversion Principle - core code depends on this abstraction.
type Registry struct {
	mu         sync.RWMutex
	factories  []Factory
	connectors map[string]Connector
}

// NewRegistry creates a new connector registry.
func NewRegistry() *Registry {
	return &Registry{
		factories:  make([]Factory, 0),
		connectors: make(map[string]Connector),
	}
}

// RegisterFactory adds a factory to the registry.
// Factories are checked in order, so register more specific factories first.
func (r *Registry) RegisterFactory(factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories = append(r.factories, factory)
}

// Create creates a connector using the appropriate factory.
func (r *Registry) Create(ctx context.Context, config *Config) (Connector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, factory := range r.factories {
		if factory.Supports(config.Type, config.Driver) {
			conn, err := factory.Create(ctx, config)
			if err != nil {
				return nil, fmt.Errorf("factory failed to create connector %s: %w", config.Name, err)
			}
			return conn, nil
		}
	}

	return nil, fmt.Errorf("no factory found for connector type=%s driver=%s", config.Type, config.Driver)
}

// Register creates and stores a connector instance.
func (r *Registry) Register(ctx context.Context, config *Config) error {
	conn, err := r.Create(ctx, config)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.connectors[config.Name]; exists {
		return fmt.Errorf("connector %s already registered", config.Name)
	}

	r.connectors[config.Name] = conn
	return nil
}

// Get retrieves a registered connector by name.
func (r *Registry) Get(name string) (Connector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conn, ok := r.connectors[name]
	if !ok {
		return nil, fmt.Errorf("connector %s not found", name)
	}
	return conn, nil
}

// GetReader retrieves a connector as a Reader.
func (r *Registry) GetReader(name string) (Reader, error) {
	conn, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	reader, ok := conn.(Reader)
	if !ok {
		return nil, fmt.Errorf("connector %s does not support reading", name)
	}
	return reader, nil
}

// GetWriter retrieves a connector as a Writer.
func (r *Registry) GetWriter(name string) (Writer, error) {
	conn, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	writer, ok := conn.(Writer)
	if !ok {
		return nil, fmt.Errorf("connector %s does not support writing", name)
	}
	return writer, nil
}

// ConnectAll establishes connections to all registered connectors.
func (r *Registry) ConnectAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, conn := range r.connectors {
		if err := conn.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect %s: %w", name, err)
		}
	}
	return nil
}

// CloseAll closes all registered connectors.
func (r *Registry) CloseAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	for name, conn := range r.connectors {
		if err := conn.Close(ctx); err != nil {
			lastErr = fmt.Errorf("failed to close %s: %w", name, err)
		}
	}
	return lastErr
}

// HealthCheckAll performs health checks on all registered connectors.
func (r *Registry) HealthCheckAll(ctx context.Context) map[string]error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]error)
	for name, conn := range r.connectors {
		results[name] = conn.Health(ctx)
	}
	return results
}

// List returns the names of all registered connectors.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}

// Names is an alias for List.
func (r *Registry) Names() []string {
	return r.List()
}

// Replace replaces an existing connector with a new one.
// This is used by the mock system to wrap connectors.
func (r *Registry) Replace(name string, conn Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectors[name] = conn
}
