package main

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/runtime"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// Every connector `mycel add` can generate has to parse.
//
// The round-trip test above covers sagas, state machines, validators and
// transforms, and connectors — the thing people generate most — were not in
// it. Two of them did not parse: a `database` or `mq` connector generated
// without a driver was accepted by validate and failed at start-up with
// "no factory found", and a profiled connector could not be generated at all
// because the only user-facing connector type with no schema was that one.

// driverFor returns a driver for types that cannot be built without one.
func driverFor(reg *schema.Registry, connType string) string {
	block := reg.ConnectorSchema(connType, "")
	for _, a := range block.Attrs {
		if a.Name == "driver" && a.Required && len(a.Values) > 0 {
			return a.Values[0]
		}
	}
	return ""
}

func TestEveryGeneratedConnectorParses(t *testing.T) {
	reg := runtime.NewSchemaRegistry()

	for _, connType := range reg.AllConnectorTypes() {
		t.Run(connType, func(t *testing.T) {
			driver := driverFor(reg, connType)

			provider := reg.Lookup(connType, driver)
			if provider == nil {
				t.Fatalf("no schema for %s/%s", connType, driver)
			}

			body := renderConnector("generated", connType, driver, provider.ConnectorSchema())
			if err := parseGenerated(t, body); err != nil {
				t.Fatalf("`mycel add connector --type %s` generated something that does not parse: %v\n\n%s",
					connType, err, body)
			}
		})
	}
}

func TestAGeneratedProfiledConnectorHasSomethingToResolveTo(t *testing.T) {
	// A profiled connector is nothing but its profiles: one name that becomes
	// a different backend per environment. Two rules the parser enforces are
	// not "this attribute is required" — there must be a profile, and one of
	// select or default must name which — and generating a file without them
	// puts somebody back where the command exists to save them from.
	reg := runtime.NewSchemaRegistry()
	provider := reg.Lookup("profiled", "")
	if provider == nil {
		t.Fatal("`mycel add connector --type profiled` cannot generate anything: the type has no schema")
	}

	body := renderConnector("pricing", "profiled", "", provider.ConnectorSchema())

	if !strings.Contains(body, `profile "primary"`) {
		t.Errorf("no profile was written, so there is nothing to resolve to:\n%s", body)
	}
	if !strings.Contains(body, `default = "primary"`) {
		t.Errorf("nothing says which profile to use:\n%s", body)
	}
	// The alternative is offered rather than hidden, and neither is written
	// twice.
	if !strings.Contains(body, "select") {
		t.Errorf("the other way of naming a profile is not mentioned:\n%s", body)
	}
	if strings.Count(body, "default =") != 1 {
		t.Errorf("default appears more than once:\n%s", body)
	}
	if err := parseGenerated(t, body); err != nil {
		t.Fatalf("the generated profiled connector does not parse: %v\n\n%s", err, body)
	}
}

func TestAConnectorTypeThatNeedsADriverIsRefused(t *testing.T) {
	// Generated without one, the file parses and fails at start-up with
	// "no factory found for connector type=database driver=". Refusing here
	// puts the fix one flag away instead.
	reg := runtime.NewSchemaRegistry()

	err := requireDriver("database", "", reg.ConnectorSchema("database", ""))
	if err == nil {
		t.Fatal("a database connector was generated with no driver")
	}
	for _, want := range []string{"--driver", "postgres", "mysql", "sqlite", "mongodb"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}

	// A misspelt one is refused with the right spelling in the message.
	err = requireDriver("mq", "rabitmq", reg.ConnectorSchema("mq", ""))
	if err == nil {
		t.Fatal("a misspelt broker driver was accepted")
	}
	if !strings.Contains(err.Error(), "rabbitmq") {
		t.Errorf("the refusal does not offer the right spelling: %v", err)
	}

	// A driver that is right goes through, and so does a type that has none.
	if err := requireDriver("database", "postgres", reg.ConnectorSchema("database", "postgres")); err != nil {
		t.Errorf("a correct database connector was refused: %v", err)
	}
	if err := requireDriver("rest", "", reg.ConnectorSchema("rest", "")); err != nil {
		t.Errorf("a connector type with no driver was asked for one: %v", err)
	}
}
