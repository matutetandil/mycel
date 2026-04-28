// Package mysql provides a MySQL database connector factory.
package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
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

	// Get port (optional, default 3306). Accepts numeric and string values
	// so port = env("DB_PORT", "3306") works.
	port := connector.IntFromProps(cfg.Properties, "port", 3306)

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

	// Parse read replicas configuration
	if replicas, ok := cfg.Properties["replicas"].([]interface{}); ok {
		for _, r := range replicas {
			if replicaMap, ok := r.(map[string]interface{}); ok {
				replica := parseReplicaConfig(replicaMap)
				conn.AddReplica(replica)
			}
		}
	}

	// Check use_replicas setting (default: true if replicas are configured)
	if useReplicas, ok := cfg.Properties["use_replicas"].(bool); ok {
		conn.SetUseReplicas(useReplicas)
	}

	return conn, nil
}

// parseReplicaConfig extracts replica configuration from a map.
func parseReplicaConfig(m map[string]interface{}) ReplicaConfig {
	replica := ReplicaConfig{
		Port:   3306,
		Weight: 1,
	}

	if host, ok := m["host"].(string); ok {
		replica.Host = host
	}
	if port, ok := connector.IntFromPropsStrict(m, "port"); ok {
		replica.Port = port
	}
	if weight, ok := connector.IntFromPropsStrict(m, "weight"); ok {
		replica.Weight = weight
	}
	if maxConns, ok := connector.IntFromPropsStrict(m, "max_connections"); ok {
		replica.MaxConns = maxConns
	}

	return replica
}
