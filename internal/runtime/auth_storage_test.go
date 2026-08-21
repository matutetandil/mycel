package runtime

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/auth"
	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// The auth storage configuration was read by nobody, and the manager falls back
// to memory for every store it is not given — so an auth block kept its users
// in the process and lost them on restart, while a storage block naming a
// database sat there looking like it had been applied.
//
// These cover the reading of that configuration. Whether a given store then
// speaks correctly to Postgres or MySQL is for the integration suite; what
// matters here is that the configuration is obeyed and that a combination with
// no store behind it is refused rather than quietly ignored.

func storageRuntime(t *testing.T, connectors []*connector.Config) *Runtime {
	t.Helper()
	return &Runtime{
		config: &parser.Configuration{Connectors: connectors},
		logger: slog.Default(),
	}
}

func TestNoStorageBlockLeavesTheDefaults(t *testing.T) {
	r := storageRuntime(t, nil)

	for _, cfg := range []*auth.Config{
		nil,
		{},
		{Storage: &auth.StorageConfig{Driver: "memory"}},
		{Storage: &auth.StorageConfig{Driver: ""}},
	} {
		opts, err := r.buildAuthStores(cfg)
		if err != nil {
			t.Errorf("buildAuthStores: %v", err)
		}
		if len(opts) != 0 {
			t.Errorf("got %d store options, want the manager's own defaults", len(opts))
		}
	}
}

func TestAnUnknownStorageDriverIsRefused(t *testing.T) {
	// Silently falling back to memory is what this replaces: someone who writes
	// a driver name with a typo would otherwise get an in-process user store
	// and no indication of it.
	r := storageRuntime(t, nil)

	_, err := r.buildAuthStores(&auth.Config{Storage: &auth.StorageConfig{Driver: "postgres"}})
	if err == nil {
		t.Fatal("an unknown driver was accepted")
	}
	if !strings.Contains(err.Error(), "memory, redis or database") {
		t.Errorf("error = %q, want it to name the drivers that exist", err)
	}
}

func TestADatabaseDriverNeedsAConnector(t *testing.T) {
	r := storageRuntime(t, nil)

	_, err := r.buildAuthStores(&auth.Config{
		Storage: &auth.StorageConfig{Driver: "database"},
	})
	if err == nil {
		t.Fatal("a database storage driver with no connector was accepted")
	}
	if !strings.Contains(err.Error(), "no connector was named") {
		t.Errorf("error = %q", err)
	}
}

func TestAMissingConnectorIsNamed(t *testing.T) {
	r := storageRuntime(t, nil)
	r.connectors = connector.NewRegistry()

	_, err := r.buildAuthStores(&auth.Config{
		Storage: &auth.StorageConfig{Driver: "database", Connector: "nowhere"},
	})
	if err == nil {
		t.Fatal("a storage connector that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("error = %q, want it to name the connector", err)
	}
}

func TestADriverWithNoAuthStoresIsRefusedAtStartup(t *testing.T) {
	// SQLite is the case someone hits first, since it is what the examples use.
	// Starting anyway would mean an auth system whose accounts vanish on
	// restart, with a storage block in the file saying otherwise.
	r := storageRuntime(t, []*connector.Config{
		{Name: "db", Type: "database", Driver: "sqlite", Properties: map[string]interface{}{
			"database": ":memory:",
		}},
	})
	reg := connector.NewRegistry()
	r.connectors = reg

	_, err := r.buildAuthStores(&auth.Config{
		Storage: &auth.StorageConfig{Driver: "database", Connector: "db"},
	})
	if err == nil {
		t.Fatal("a driver with no auth stores behind it was accepted")
	}
	// Either message is right depending on whether the connector was registered;
	// what matters is that it names the connector rather than failing vaguely.
	if !strings.Contains(err.Error(), "db") {
		t.Errorf("error = %q, want it to name the connector", err)
	}
}

func TestInitAuthWithoutAnAuthBlockDoesNothing(t *testing.T) {
	r := storageRuntime(t, nil)
	if err := r.initAuth(context.Background()); err != nil {
		t.Fatalf("initAuth: %v", err)
	}
	if r.authManager != nil || r.authHandler != nil {
		t.Error("an auth system was built for a configuration with no auth block")
	}
}

func TestInitAuthBuildsTheManagerAndHandler(t *testing.T) {
	r := storageRuntime(t, nil)
	r.config.Auth = &auth.Config{
		Preset: "development",
		JWT:    &auth.JWTConfig{Algorithm: "HS256", Secret: "a-secret-long-enough-to-be-plausible"},
	}

	if err := r.initAuth(context.Background()); err != nil {
		t.Fatalf("initAuth: %v", err)
	}
	if r.authManager == nil {
		t.Error("no auth manager was built")
	}
	if r.authHandler == nil {
		t.Error("no auth handler was built, so no endpoint could be mounted")
	}

	// Building the manager starts the cleanup loop, which would otherwise
	// outlive the test.
	if r.authCleanup == nil {
		t.Error("no cleanup loop was started, so expired sessions would accumulate")
	} else if err := r.authCleanup.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// A source whose type is not on this list has its payload dropped without a
// word — the failure mode that bit websocket, sse and tcp in 2.1.1. An inbound
// webhook is a source like any other.
func TestWebhookCountsAsAnEventDrivenSource(t *testing.T) {
	for _, connType := range []string{"mq", "mqtt", "cdc", "file", "websocket", "sse", "tcp", "webhook"} {
		if !isEventDrivenSource(connType) {
			t.Errorf("%q is not treated as event-driven, so its payload would be discarded", connType)
		}
	}
	for _, connType := range []string{"rest", "http", "database", "graphql"} {
		if isEventDrivenSource(connType) {
			t.Errorf("%q was treated as event-driven", connType)
		}
	}
}
