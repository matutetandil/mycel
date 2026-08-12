// Package mongodb provides a MongoDB database connector factory.
package mongodb

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
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

		port := connector.IntFromProps(cfg.Properties, "port", 27017)

		user, _ := cfg.Properties["user"].(string)
		password, _ := cfg.Properties["password"].(string)

		// A seed-list host resolved through SRV carries no port: the records
		// supply them, and mongodb+srv:// rejects one.
		scheme, hostPart := "mongodb://", fmt.Sprintf("%s:%d", host, port)
		if useSRV, _ := cfg.Properties["srv"].(bool); useSRV {
			scheme, hostPart = "mongodb+srv://", host
		}

		if user != "" && password != "" {
			uri = fmt.Sprintf("%s%s:%s@%s", scheme, url.QueryEscape(user), url.QueryEscape(password), hostPart)
		} else {
			uri = scheme + hostPart
		}

		uri = appendMongoOptions(uri, cfg.Properties)
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

// appendMongoOptions carries the connection settings that are URI options
// rather than parts of the address.
//
// The parser accepted every one of these and nothing read them, so a replica
// set name, an authentication database or a read concern could be written and
// silently had no effect. They are only applied to a URI built from parts: a
// uri given whole is the author's, and rewriting its query string would be
// surprising.
func appendMongoOptions(uri string, props map[string]interface{}) string {
	opts := url.Values{}

	// auth_db is the shorter spelling of auth_source; both were accepted.
	for _, key := range []string{"auth_source", "auth_db"} {
		if v, ok := props[key].(string); ok && v != "" {
			opts.Set("authSource", v)
			break
		}
	}
	if v, ok := props["replica_set"].(string); ok && v != "" {
		opts.Set("replicaSet", v)
	}
	if v, ok := props["read_concern"].(string); ok && v != "" {
		opts.Set("readConcernLevel", v)
	}
	if v, ok := props["direct"].(bool); ok && v {
		opts.Set("directConnection", "true")
	}

	if len(opts) == 0 {
		return uri
	}
	separator := "?"
	if strings.Contains(uri, "?") {
		separator = "&"
	}
	return uri + separator + opts.Encode()
}
