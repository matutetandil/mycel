// Package mysql provides a MySQL database connector factory.
package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

// Factory creates MySQL connectors.
type Factory struct{}

// NewFactory creates a new MySQL connector factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "database"
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "database" && driver == "mysql"
}

// Create creates a new MySQL connector from config.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	// Get host (required)
	host, _ := cfg.Properties["host"].(string)
	if host == "" {
		host = "localhost"
	}

	// Get port (optional, default 3306)
	port := 3306
	if p, ok := cfg.Properties["port"].(int); ok {
		port = p
	}

	// Get database name (required)
	database, ok := cfg.Properties["database"].(string)
	if !ok || database == "" {
		return nil, fmt.Errorf("mysql connector requires database name")
	}

	// Get user (required)
	user, ok := cfg.Properties["user"].(string)
	if !ok || user == "" {
		return nil, fmt.Errorf("mysql connector requires user")
	}

	// Get password (optional)
	password, _ := cfg.Properties["password"].(string)

	// Get charset (optional, default utf8mb4)
	charset, _ := cfg.Properties["charset"].(string)

	// Create connector
	conn := New(cfg.Name, host, port, database, user, password, charset)

	// Apply pool configuration if present
	if pool, ok := cfg.Properties["pool"].(map[string]interface{}); ok {
		maxOpen := 0
		maxIdle := 0
		var maxLifetime time.Duration

		if m, ok := pool["max"].(int); ok {
			maxOpen = m
		}
		if m, ok := pool["min"].(int); ok {
			maxIdle = m
		}
		if m, ok := pool["max_lifetime"].(int); ok {
			maxLifetime = time.Duration(m) * time.Second
		}

		conn.SetPoolConfig(maxOpen, maxIdle, maxLifetime)
	}

	return conn, nil
}
