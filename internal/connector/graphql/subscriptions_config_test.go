package graphql

import (
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// The subscriptions block has exactly four attributes and every one of them
// reaches the server. The two timeouts are the ones that decide when an idle
// client is dropped, so a value that parses and is then ignored would show up
// as connections dying at 60s no matter what the config said.
func TestSubscriptionsConfigReachesTheServer(t *testing.T) {
	cfg := &connector.Config{
		Name: "gql",
		Type: "graphql",
		Properties: map[string]interface{}{
			"driver":   "server",
			"port":     4000,
			"endpoint": "/graphql",
			"subscriptions": map[string]interface{}{
				"enabled":             true,
				"path":                "/graphql/ws",
				"keep_alive_interval": "10s",
				"connection_timeout":  "45s",
			},
		},
	}

	conn, err := (&Factory{}).Create(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	server, ok := conn.(*ServerConnector)
	if !ok {
		t.Fatalf("Create returned %T", conn)
	}
	subs := server.config.Subscriptions
	if subs == nil {
		t.Fatal("the subscriptions block did not reach the connector")
	}
	if !subs.Enabled {
		t.Error("enabled did not apply")
	}
	if subs.Path != "/graphql/ws" {
		t.Errorf("path = %q", subs.Path)
	}
	if subs.KeepAliveInterval != 10*time.Second {
		t.Errorf("keep_alive_interval = %v, want 10s", subs.KeepAliveInterval)
	}
	if subs.ConnectionTimeout != 45*time.Second {
		t.Errorf("connection_timeout = %v, want 45s", subs.ConnectionTimeout)
	}
}

func TestSubscriptionsDefaults(t *testing.T) {
	cfg := &connector.Config{
		Name: "gql",
		Type: "graphql",
		Properties: map[string]interface{}{
			"driver":        "server",
			"port":          4001,
			"endpoint":      "/graphql",
			"subscriptions": map[string]interface{}{"enabled": true},
		},
	}
	conn, err := (&Factory{}).Create(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	subs := conn.(*ServerConnector).config.Subscriptions
	// These are the values the documentation now states.
	if subs.Path != "/subscriptions" {
		t.Errorf("default path = %q, want /subscriptions", subs.Path)
	}
	if subs.KeepAliveInterval != 30*time.Second {
		t.Errorf("default keep_alive_interval = %v, want 30s", subs.KeepAliveInterval)
	}
	if subs.ConnectionTimeout != 60*time.Second {
		t.Errorf("default connection_timeout = %v, want 60s", subs.ConnectionTimeout)
	}
}
