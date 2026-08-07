package main

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/runtime"
	"github.com/matutetandil/mycel/pkg/schema"
)

// The generated skeleton comes from the connector's own schema rather than a
// template. A template would drift the first time a connector changed its
// required attributes — the failure this project keeps finding.
func TestRenderConnector_FromSchema(t *testing.T) {
	reg := runtime.NewSchemaRegistry()
	provider := reg.Lookup("database", "postgres")
	if provider == nil {
		t.Fatal("postgres schema not registered")
	}

	got := renderConnector("orders_db", "database", "postgres", provider.ConnectorSchema())

	for _, want := range []string{
		`connector "orders_db" {`,
		`type   = "database"`,
		`driver = "postgres"`,
		"database =",  // required by postgres, so it must be emitted
		"// Optional:", // the rest listed, not guessed at
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated connector missing %q:\n%s", want, got)
		}
	}

	// Optional attributes are comments, not live config the user has to delete.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "sslmode") && !strings.Contains(line, "//") {
			t.Errorf("optional attribute emitted as live config: %q", line)
		}
	}
}

// Hosts and credentials are the usual required strings, and a committed
// literal is how secrets reach git.
func TestPlaceholderFor_PrefersEnvForStrings(t *testing.T) {
	got := placeholderFor(schema.Attr{Name: "password", Type: schema.TypeString, Required: true})
	if !strings.Contains(got, `env("PASSWORD")`) {
		t.Errorf("string placeholder should use env(), got %q", got)
	}
}

// A real default or an allowed value beats a bare TODO: it is one fewer thing
// to look up, and it is already correct.
func TestPlaceholderFor_PrefersDefaultsAndEnums(t *testing.T) {
	if got := placeholderFor(schema.Attr{Name: "port", Type: schema.TypeNumber, Default: 5432}); got != "5432" {
		t.Errorf("default not used: %q", got)
	}
	got := placeholderFor(schema.Attr{
		Name:   "sslmode",
		Type:   schema.TypeString,
		Values: []string{"disable", "require"},
	})
	if got != `"disable"` {
		t.Errorf("first allowed value not used: %q", got)
	}
}

func TestRenderFlow_WiresTheConnectorsGiven(t *testing.T) {
	got := renderFlow("order_created", "rabbit", "orders_db")

	for _, want := range []string{
		`flow "order_created" {`,
		`connector = "rabbit"`,
		`connector = "orders_db"`,
		"target    =",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated flow missing %q:\n%s", want, got)
		}
	}
}

// Without a destination the skeleton must not invent one — it points at the
// two real options instead.
func TestRenderFlow_NoDestinationExplainsTheChoice(t *testing.T) {
	got := renderFlow("ingest", "rabbit", "")

	// Look for a real block rather than the substring, since the comment that
	// replaces it names both `to { }` and `response { }`.
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "to {") {
			t.Errorf("no destination was requested, so none should be generated:\n%s", got)
		}
	}
	if !strings.Contains(got, "response { }") {
		t.Errorf("the response alternative should be mentioned:\n%s", got)
	}
}

// `operation` means two different things depending on the source, and getting
// it wrong on a stream source silently discards every message. The generated
// comment has to say so.
func TestRenderFlow_ExplainsOperation(t *testing.T) {
	got := renderFlow("ingest", "rabbit", "")

	if !strings.Contains(got, "narrows a subscription") {
		t.Errorf("the skeleton should explain what operation does on a stream source:\n%s", got)
	}
}
