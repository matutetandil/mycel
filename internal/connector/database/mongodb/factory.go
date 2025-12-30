// Package mongodb provides a MongoDB database connector factory.
package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

// Factory creates MongoDB connectors.
type Factory struct{}

// NewFactory creates a new MongoDB connector factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Type returns the connector type this factory handles.
func (f *Factory) Type() string {
	return "database"
}

// Supports returns true if this factory can create the given connector type.
func (f *Factory) Supports(connType, driver string) bool {
	return connType == "database" && (driver == "mongodb" || driver == "mongo")
}

// Create creates a new MongoDB connector from config.
func (f *Factory) Create(ctx context.Context, cfg *connector.Config) (connector.Connector, error) {
	// Get URI (preferred method)
	uri, _ := cfg.Properties["uri"].(string)

	// If no URI, build from components
	if uri == "" {
		host, _ := cfg.Properties["host"].(string)
		if host == "" {
			host = "localhost"
		}

		port := 27017
		if p, ok := cfg.Properties["port"].(int); ok {
			port = p
		}

		user, _ := cfg.Properties["user"].(string)
		password, _ := cfg.Properties["password"].(string)

		if user != "" && password != "" {
			uri = fmt.Sprintf("mongodb://%s:%s@%s:%d", user, password, host, port)
		} else {
			uri = fmt.Sprintf("mongodb://%s:%d", host, port)
		}
	}

	// Get database name (required)
	database, ok := cfg.Properties["database"].(string)
	if !ok || database == "" {
		return nil, fmt.Errorf("mongodb connector requires database name")
	}

	// Create connector
	conn := New(cfg.Name, uri, database)

	// Apply pool configuration if present
	if pool, ok := cfg.Properties["pool"].(map[string]interface{}); ok {
		var maxPool, minPool uint64
		var connectTimeout time.Duration

		if m, ok := pool["max"].(int); ok {
			maxPool = uint64(m)
		}
		if m, ok := pool["min"].(int); ok {
			minPool = uint64(m)
		}
		if m, ok := pool["connect_timeout"].(int); ok {
			connectTimeout = time.Duration(m) * time.Second
		}

		conn.SetPoolConfig(maxPool, minPool, connectTimeout)
	}

	return conn, nil
}
