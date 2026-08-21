package connectors

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// These schemas are the description of Mycel's configuration language.
//
// Completions, `mycel add`, the exported documentation and the checks
// `mycel validate` performs are all built from them, so a schema that is
// wrong is not a wrong schema — it is a wrong completion, a generated file
// that does not run, and documentation describing something that does not
// exist. The parity tests elsewhere compare them against the parser; this one
// checks they are well formed at all, which is what nothing did: the package
// had no test of its own.

func everySchema(t *testing.T) map[string]schema.ConnectorSchemaProvider {
	t.Helper()

	reg := schema.NewRegistry()
	RegisterAll(reg)

	// Every registration, drivers included. Walking the types alone and
	// looking each one up reads the default driver of each — one of the four
	// database schemas, one of the three queue ones — and leaves the rest
	// checked by nothing.
	found := map[string]schema.ConnectorSchemaProvider{}
	for _, registration := range reg.AllRegistrations() {
		provider := reg.Lookup(registration.Type, registration.Driver)
		if provider == nil {
			t.Errorf("%s/%s is registered and answers with nothing", registration.Type, registration.Driver)
			continue
		}
		name := registration.Type
		if registration.Driver != "" {
			name += "/" + registration.Driver
		}
		found[name] = provider
	}
	if len(found) == 0 {
		t.Fatal("the registry holds nothing; this test is checking nothing")
	}
	return found
}

func TestEverySchemaIsWellFormed(t *testing.T) {
	for connType, provider := range everySchema(t) {
		t.Run(connType, func(t *testing.T) {
			checkBlock(t, connType, provider.ConnectorSchema())

			// A source schema says what a flow may write in a from block, and
			// a target schema what it may write in a to or step block. Either
			// may be absent — a connector that cannot be read from has no
			// source schema — but one that exists has to be usable.
			if src := provider.SourceSchema(); src != nil {
				checkBlock(t, connType+" from", *src)
			}
			if dst := provider.TargetSchema(); dst != nil {
				checkBlock(t, connType+" to", *dst)
			}
		})
	}
}

// checkBlock walks a block and everything under it.
func checkBlock(t *testing.T, where string, block schema.Block) {
	t.Helper()

	seen := map[string]bool{}
	for _, attr := range block.Attrs {
		switch {
		case attr.Name == "":
			t.Errorf("%s has an attribute with no name", where)
		case seen[attr.Name]:
			// Declared twice, and which one wins is whichever the reader
			// happens to reach first: completions show one, validation uses
			// the other.
			t.Errorf("%s declares %q twice", where, attr.Name)
		case attr.Type == "":
			// Without a type, a completion cannot offer a value and
			// validation cannot tell a number from the word for one.
			t.Errorf("%s: %q has no type", where, attr.Name)
		case attr.Doc == "":
			// The doc is the hover text and the comment `mycel add` writes,
			// so an attribute without one is offered with nothing said about
			// it.
			t.Errorf("%s: %q has no documentation", where, attr.Name)
		}
		seen[attr.Name] = true

		// A list of allowed words is only useful if the words are there, and
		// a default that is not one of them is a default that fails
		// validation the moment somebody writes it out.
		for _, value := range attr.Values {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: %q allows an empty value", where, attr.Name)
			}
		}
		if def, ok := attr.Default.(string); ok && len(attr.Values) > 0 && def != "" {
			if !containsValue(attr.Values, def) {
				t.Errorf("%s: %q defaults to %q, which is not one of %v",
					where, attr.Name, def, attr.Values)
			}
		}
	}

	children := map[string]bool{}
	for _, child := range block.Children {
		if child.Type == "" {
			t.Errorf("%s has a child block with no type", where)
			continue
		}
		if children[child.Type] {
			t.Errorf("%s declares the %s block twice", where, child.Type)
		}
		children[child.Type] = true

		checkBlock(t, where+"."+child.Type, child)
	}
}

func TestABlockWithNothingInItSaysWhy(t *testing.T) {
	// A block that declares no attributes and no children is either a block
	// that takes anything — which it has to say, or completions offer nothing
	// and validation refuses everything — or a block nobody finished.
	for connType, provider := range everySchema(t) {
		t.Run(connType, func(t *testing.T) {
			for _, child := range provider.ConnectorSchema().Children {
				if len(child.Attrs) == 0 && len(child.Children) == 0 && !child.Open {
					t.Errorf("the %s block accepts nothing and is not open, so it can only be written empty", child.Type)
				}
			}
		})
	}
}

func TestARequiredAttributeIsNotAlsoDefaulted(t *testing.T) {
	// The two say opposite things: one that it must be written, the other
	// that it need not be. `mycel add` reads both, and which it believes
	// decides whether the generated file has a TODO in it.
	for connType, provider := range everySchema(t) {
		t.Run(connType, func(t *testing.T) {
			walkAttrs(provider.ConnectorSchema(), func(where string, attr schema.Attr) {
				if attr.Required && attr.Default != nil {
					t.Errorf("%s: %q is required and has a default of %v", where, attr.Name, attr.Default)
				}
			})
		})
	}
}

func TestTheRegistryHoldsWhatItWasGiven(t *testing.T) {
	reg := schema.NewRegistry()
	RegisterAll(reg)

	// A driver-specific schema has to be found by its driver, and the type on
	// its own has to answer with something: a connector written without a
	// driver still needs completions.
	for _, tc := range []struct{ connType, driver string }{
		{"database", "postgres"},
		{"database", "mysql"},
		{"database", "sqlite"},
		{"database", "mongodb"},
		{"mq", "rabbitmq"},
		{"mq", "kafka"},
		{"mq", "redis"},
	} {
		if reg.Lookup(tc.connType, tc.driver) == nil {
			t.Errorf("no schema for %s/%s", tc.connType, tc.driver)
		}
		if reg.Lookup(tc.connType, "") == nil {
			t.Errorf("no schema for %s with no driver named", tc.connType)
		}
	}

	// Each driver describes its own system: two drivers answering with the
	// same block means one of them was registered under the wrong name.
	postgres := reg.ConnectorSchema("database", "postgres")
	sqlite := reg.ConnectorSchema("database", "sqlite")
	if len(postgres.Attrs) == len(sqlite.Attrs) && attrNames(postgres) == attrNames(sqlite) {
		t.Error("postgres and sqlite describe themselves identically")
	}

	// A type nobody registered answers with nothing rather than with a
	// neighbour's schema.
	if reg.Lookup("salesforce", "") != nil {
		t.Error("a connector type nobody registered was answered for")
	}
}

func TestTheFullRegistryHasTheBlocksAsWellAsTheConnectors(t *testing.T) {
	// What external tooling reads: the connector schemas and the block
	// schemas — flow, aspect, type — in one registry.
	reg := FullRegistry()

	if reg.Lookup("rest", "") == nil {
		t.Error("the full registry has no connectors in it")
	}
	if len(reg.AllConnectorTypes()) < 20 {
		t.Errorf("the full registry holds %d connector types", len(reg.AllConnectorTypes()))
	}
	var hasFlow bool
	for _, block := range reg.RootSchema() {
		if block.Type == "flow" {
			hasFlow = true
		}
	}
	if !hasFlow {
		t.Error("the full registry has no flow block, so nothing outside can complete one")
	}
}

func walkAttrs(block schema.Block, visit func(where string, attr schema.Attr)) {
	where := block.Type
	if where == "" {
		where = "connector"
	}
	for _, attr := range block.Attrs {
		visit(where, attr)
	}
	for _, child := range block.Children {
		walkAttrs(child, visit)
	}
}

func attrNames(block schema.Block) string {
	var names []string
	for _, a := range block.Attrs {
		names = append(names, a.Name)
	}
	return strings.Join(names, ",")
}

func containsValue(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
