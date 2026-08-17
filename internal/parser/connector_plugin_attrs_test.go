package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A connector whose type arrives with a plugin carries attributes the parser
// has never seen: they are declared in the plugin's own manifest. Those used to
// be rejected by name, so a plugin connector could not be configured at all —
// the type loaded, and the one setting it needed was an "unsupported argument".
//
// The strictness stays for the types this runtime ships, because there the list
// is the only thing between a mistyped setting and a service that starts and
// quietly ignores it.

func parseConnectors(t *testing.T, body string) (*Configuration, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "connectors.mycel")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	return NewHCLParser().Parse(context.Background(), dir)
}

func TestAPluginConnectorCarriesTheAttributesItsPluginDeclared(t *testing.T) {
	config, err := parseConnectors(t, `
connector "stock" {
  type = "inventory_store"

  warehouse  = "auckland"
  batch_size = 50
  strict     = true
}
`)
	if err != nil {
		t.Fatalf("a plugin connector could not be configured: %v", err)
	}

	if len(config.Connectors) != 1 {
		t.Fatalf("%d connectors", len(config.Connectors))
	}
	props := config.Connectors[0].Properties
	if props["warehouse"] != "auckland" {
		t.Errorf("warehouse = %v", props["warehouse"])
	}
	if props["strict"] != true {
		t.Errorf("strict = %v", props["strict"])
	}
	if props["batch_size"] == nil {
		t.Error("batch_size did not reach the connector")
	}
}

func TestAMistypedAttributeOnAConnectorMycelShipsIsStillRefused(t *testing.T) {
	// The check that stops a setting from being silently ignored.
	_, err := parseConnectors(t, `
connector "orders_db" {
  type     = "database"
  driver   = "postgres"
  datbase  = "orders"
}
`)
	if err == nil {
		t.Fatal("a mistyped attribute was accepted")
	}
	if !strings.Contains(err.Error(), "datbase") {
		t.Errorf("error = %q, want it to name the attribute", err)
	}
}

func TestAConnectorWithNoTypeIsStillHeldToTheList(t *testing.T) {
	// A profiled connector has no type of its own — its types live inside its
	// profiles — so there is nothing to say its attributes come from
	// somewhere else.
	_, err := parseConnectors(t, `
connector "store" {
  select  = "env('STORE_PROFILE')"
  default = "local"

  wharehouse = "typo"

  profile "local" {
    type   = "database"
    driver = "sqlite"
  }
}
`)
	if err == nil {
		t.Fatal("a mistyped attribute on a profiled connector was accepted")
	}
}

func TestEveryBuiltInTypeIsStillChecked(t *testing.T) {
	// If a type ever fell out of the list the parser reads, its connectors
	// would silently start accepting anything — the failure would be a
	// loosening nobody notices.
	for _, typeName := range []string{"rest", "database", "mq", "graphql", "grpc", "cache", "s3"} {
		t.Run(typeName, func(t *testing.T) {
			_, err := parseConnectors(t, `
connector "c" {
  type = "`+typeName+`"

  not_a_real_attribute = "x"
}
`)
			if err == nil {
				t.Error("an attribute nobody implements was accepted")
			}
		})
	}
}
