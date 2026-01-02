package mock

import (
	"fmt"
	"log/slog"

	"github.com/matutetandil/mycel/internal/connector"
)

// Manager manages mock configuration and connector wrapping.
type Manager struct {
	config *Config
	loader *Loader
	logger *slog.Logger
}

// NewManager creates a new mock manager.
func NewManager(config *Config) *Manager {
	var loader *Loader
	if config != nil && config.Path != "" {
		loader = NewLoader(config.Path)
	}

	return &Manager{
		config: config,
		loader: loader,
		logger: slog.Default().With("component", "mock-manager"),
	}
}

// IsEnabled returns true if mocking is enabled.
func (m *Manager) IsEnabled() bool {
	return m.config != nil && m.config.Enabled
}

// ShouldMock returns true if the given connector should be mocked.
func (m *Manager) ShouldMock(connectorName string) bool {
	if !m.IsEnabled() {
		return false
	}

	// Check per-connector config
	if connConfig, ok := m.config.Connectors[connectorName]; ok {
		if connConfig.Enabled != nil && !*connConfig.Enabled {
			return false
		}
	}

	// Check no-mock list
	for _, name := range m.config.NoMock {
		if name == connectorName {
			return false
		}
	}

	// Check mock-only list (if specified, only those are mocked)
	if len(m.config.MockOnly) > 0 {
		for _, name := range m.config.MockOnly {
			if name == connectorName {
				return true
			}
		}
		return false
	}

	// Default: mock if enabled
	return true
}

// WrapConnector wraps a connector with mock capabilities if appropriate.
func (m *Manager) WrapConnector(name string, conn connector.Connector) (connector.Connector, error) {
	if !m.ShouldMock(name) {
		return conn, nil
	}

	if m.loader == nil {
		return conn, nil
	}

	// Get per-connector config
	var connConfig *ConnectorMockConfig
	if m.config.Connectors != nil {
		connConfig = m.config.Connectors[name]
	}

	// Check if mock files exist for this connector
	mockOnly := m.loader.Exists(name)

	wrapped, err := NewConnector(name, conn, m.loader, connConfig, mockOnly)
	if err != nil {
		return nil, fmt.Errorf("wrapping connector %s with mock: %w", name, err)
	}

	m.logger.Info("connector wrapped with mock",
		"name", name,
		"mock_only", mockOnly)

	return wrapped, nil
}

// WrapRegistry wraps all connectors in a registry with mock capabilities.
func (m *Manager) WrapRegistry(registry *connector.Registry) error {
	if !m.IsEnabled() {
		return nil
	}

	// Get all connector names
	names := registry.Names()

	for _, name := range names {
		conn, err := registry.Get(name)
		if err != nil {
			continue
		}

		wrapped, err := m.WrapConnector(name, conn)
		if err != nil {
			return err
		}

		if wrapped != conn {
			// Replace with wrapped version
			registry.Replace(name, wrapped)
		}
	}

	return nil
}

// ClearCache clears the mock file cache.
func (m *Manager) ClearCache() {
	if m.loader != nil {
		m.loader.ClearCache()
	}
}

// GetConfig returns the mock configuration.
func (m *Manager) GetConfig() *Config {
	return m.config
}
