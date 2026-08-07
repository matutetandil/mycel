package main

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/aspect"
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
		"database =",   // required by postgres, so it must be emitted
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

// The `when` values come from the schema, not a literal in the generator.
// That is what makes the skeleton unable to drift — and it is how on_drop,
// which the runtime supported while the schema did not, reaches users.
func TestRenderAspect_WhenValuesComeFromTheSchema(t *testing.T) {
	got := renderAspect("audit", "create_*", "", schema.AspectSchema())

	for _, want := range []string{"before", "after", "around", "on_error", "on_drop"} {
		if !strings.Contains(got, want) {
			t.Errorf("the when hint should offer %q:\n%s", want, got)
		}
	}
}

// The schema's allowed values must match the runtime's, which is the authority.
// They drifted once: on_drop shipped without reaching the schema, so neither
// completions nor this generator offered it.
func TestAspectSchema_MatchesRuntimeWhenValues(t *testing.T) {
	var declared []string
	for _, a := range schema.AspectSchema().Attrs {
		if a.Name == "when" {
			declared = a.Values
		}
	}

	runtimeValues := []string{
		string(aspect.Before), string(aspect.After), string(aspect.Around),
		string(aspect.OnError), string(aspect.OnDrop),
	}
	if len(declared) != len(runtimeValues) {
		t.Fatalf("schema declares %v, runtime supports %v", declared, runtimeValues)
	}
	for _, want := range runtimeValues {
		found := false
		for _, d := range declared {
			if d == want {
				found = true
			}
		}
		if !found {
			t.Errorf("schema is missing the runtime value %q", want)
		}
	}
}

// An aspect with no action does nothing at all, so one is always generated —
// unlike the optional blocks, which are only mentioned.
func TestRenderAspect_AlwaysGeneratesAnAction(t *testing.T) {
	got := renderAspect("audit", "", "", schema.AspectSchema())

	if !strings.Contains(got, "action {") {
		t.Errorf("an action block must be generated:\n%s", got)
	}
	for _, optional := range []string{"cache { }", "invalidate { }"} {
		if !strings.Contains(got, optional) {
			t.Errorf("optional block %q should be mentioned:\n%s", optional, got)
		}
	}
}

func TestValidateAgainstSchema_RejectsUnknownWhen(t *testing.T) {
	err := validateAgainstSchema(schema.AspectSchema(), "when", "whenever")
	if err == nil {
		t.Fatal("expected an unknown when value to be rejected")
	}
	if !strings.Contains(err.Error(), "on_drop") {
		t.Errorf("the error should list the valid values, got: %v", err)
	}
	if err := validateAgainstSchema(schema.AspectSchema(), "when", "on_drop"); err != nil {
		t.Errorf("on_drop is valid, got: %v", err)
	}
}
