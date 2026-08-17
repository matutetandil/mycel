package graphql

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A federation v2 subgraph schema file begins with a declaration of which
// version of the specification it speaks. It is not decoration and it is not
// optional — it is line one of every subgraph schema anyone exports or writes.
// The parser refused it, so someone who pointed the connector at a real
// subgraph schema got a syntax error on a line they had copied from the
// specification, and the service did not start.

const subgraphSchemaFile = `
extend schema
  @link(url: "https://specs.apollo.dev/federation/v2.3",
        import: ["@key", "@shareable", "@external", "@provides"])

directive @key(fields: FieldSet!, resolvable: Boolean = true) repeatable on OBJECT | INTERFACE
directive @tag(name: String!) repeatable on FIELD_DEFINITION | OBJECT
directive @shareable on OBJECT | FIELD_DEFINITION

scalar FieldSet

type Customer @key(fields: "id") {
  id: ID!
  email: String
  "the orders this customer has placed"
  orders: [Order!]
}

type Order @key(fields: "id") @shareable {
  id: ID!
  total: Float
}

type Query {
  customer(id: ID!): Customer
}
`

func TestARealSubgraphSchemaFileLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	if err := os.WriteFile(path, []byte(subgraphSchemaFile), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := NewSchemaBuilder()
	if err := b.LoadSDL(path); err != nil {
		t.Fatalf("a federation v2 subgraph schema file was refused: %v", err)
	}

	// Everything after the declaration has to survive it, or the file has been
	// read as something other than what was written.
	for _, name := range []string{"Customer", "Order"} {
		if b.GetType(name) == nil {
			t.Errorf("type %s was lost", name)
		}
	}
	parsed := b.GetParsedSchema()
	if parsed == nil || parsed.Query == nil {
		t.Fatal("the query type was lost")
	}
	if _, ok := parsed.Query.Fields["customer"]; !ok {
		t.Error("the query field was lost")
	}

	// The description is the check that the stripping stopped where it should:
	// it sits after the declaration, inside a type, in a string.
	if field := parsed.Types["Customer"].Fields["orders"]; field == nil {
		t.Error("the orders field was lost")
	} else if !strings.Contains(field.Description, "orders this customer has placed") {
		t.Errorf("description = %q, want the one written in the file", field.Description)
	}

	// And the file is served back to a gateway as it was written.
	if !strings.Contains(b.GetRawSDL(), "@link") {
		t.Error("the declaration was removed from the schema published to the gateway")
	}
}

func TestTheRepeatableKeywordSurvivesBeingRemoved(t *testing.T) {
	parsed, err := ParseSDLComplete(subgraphSchemaFile)
	if err != nil {
		t.Fatalf("ParseSDLComplete: %v", err)
	}

	for _, name := range []string{"key", "tag"} {
		d, ok := parsed.Directives[name]
		if !ok {
			t.Errorf("directive @%s was lost", name)
			continue
		}
		if !d.IsRepeatable {
			t.Errorf("@%s is not repeatable, though that is how it was declared", name)
		}
	}

	// A directive without the keyword must not acquire it.
	if d, ok := parsed.Directives["shareable"]; !ok {
		t.Error("directive @shareable was lost")
	} else if d.IsRepeatable {
		t.Error("@shareable was made repeatable")
	}

	// The arguments and locations either side of the keyword are still there.
	key := parsed.Directives["key"]
	if _, ok := key.Args["fields"]; !ok {
		t.Errorf("@key lost its fields argument: %+v", key.Args)
	}
	if len(key.Locations) != 2 {
		t.Errorf("@key locations = %v, want OBJECT and INTERFACE", key.Locations)
	}
}

func TestStrippingLeavesAnOrdinarySchemaAlone(t *testing.T) {
	// Most schema files have none of this in them, and they must come through
	// byte for byte — a stray edit here would be a change to every schema
	// anyone has written.
	for _, sdl := range []string{
		`type Query { hello: String }`,
		"type Customer {\n  id: ID!\n}\n\ntype Query {\n  customer: Customer\n}\n",
		`schema { query: Query }` + "\n" + `type Query { hello: String }`,
		// A field, argument and description that merely contain the words.
		"type Query {\n  \"\"\"the schema, extended\"\"\"\n  schema(repeatable: Boolean): String\n}",
		`type Config { schema: String, repeatable: Boolean }`,
	} {
		got, repeatable := stripSchemaExtensions(sdl)
		if got != sdl {
			t.Errorf("an ordinary schema was rewritten:\n got %q\nwant %q", got, sdl)
		}
		if len(repeatable) != 0 {
			t.Errorf("found repeatable directives in a schema with none: %v", repeatable)
		}
		if _, err := ParseSDLComplete(sdl); err != nil {
			t.Errorf("ParseSDLComplete(%q): %v", sdl, err)
		}
	}
}

func TestTheDeclarationGoesWhereverItIsWritten(t *testing.T) {
	// It is conventionally first, but nothing requires that, and a file that
	// puts it elsewhere must not lose the types around it.
	const sdl = `
type Query { hello: String }

extend schema
  @link(url: "https://specs.apollo.dev/federation/v2.0", import: ["@key"])

type Customer { id: ID! }
`
	parsed, err := ParseSDLComplete(sdl)
	if err != nil {
		t.Fatalf("ParseSDLComplete: %v", err)
	}
	if parsed.Query == nil {
		t.Error("the type before the declaration was lost")
	}
	if _, ok := parsed.Types["Customer"]; !ok {
		t.Error("the type after the declaration was lost")
	}
}

func TestASchemaBlockKeepsItsRootTypes(t *testing.T) {
	// Only the directives beside it go: the body is what names the root types,
	// and dropping it would leave a service with no query type at all.
	const sdl = `
schema @link(url: "https://specs.apollo.dev/federation/v2.0") {
  query: RootQuery
}

type RootQuery { hello: String }
`
	parsed, err := ParseSDLComplete(sdl)
	if err != nil {
		t.Fatalf("ParseSDLComplete: %v", err)
	}
	if parsed.Query == nil {
		t.Fatal("the root query named by the schema block was lost")
	}
	if _, ok := parsed.Query.Fields["hello"]; !ok {
		t.Errorf("the root query has fields %v", parsed.Query.Fields)
	}
}

func TestAnExtensionWithARootTypeBodyGoesWhole(t *testing.T) {
	// Leaving the header's body behind would put a stray block in the document
	// and fail the parse a second time, further from the cause.
	const sdl = `
extend schema @link(url: "https://specs.apollo.dev/federation/v2.0") {
  query: Query
}

type Query { hello: String }
`
	cleaned, _ := stripSchemaExtensions(sdl)
	if strings.Contains(cleaned, "query: Query") {
		t.Errorf("the extension's body was left behind:\n%s", cleaned)
	}
	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("ParseSDLComplete: %v", err)
	}
}

func TestTheWordsInStringsAndCommentsAreNotDefinitions(t *testing.T) {
	const sdl = `
# extend schema @link(url: "no")
"""
extend schema
"""
type Query {
  hello(note: String = "extend schema @link"): String
}
`
	parsed, err := ParseSDLComplete(sdl)
	if err != nil {
		t.Fatalf("ParseSDLComplete: %v", err)
	}
	if parsed.Query == nil {
		t.Fatal("the type was removed along with the words that look like a declaration")
	}
	field, ok := parsed.Query.Fields["hello"]
	if !ok {
		t.Fatalf("the field was lost: %v", parsed.Query.Fields)
	}
	if arg, ok := field.Args["note"]; !ok {
		t.Errorf("the argument was lost: %v", field.Args)
	} else if arg.DefaultValue != "extend schema @link" {
		t.Errorf("default = %v, want the string as written", arg.DefaultValue)
	}
}

func TestARenamedRootTypeIsFoundInEitherOrder(t *testing.T) {
	// The block may be written before or after the type it names, and a schema
	// whose root came back empty would expose no fields at all.
	for name, sdl := range map[string]string{
		"block first": "schema { query: RootQuery, mutation: RootMutation }\n" +
			"type RootQuery { hello: String }\ntype RootMutation { go: String }",
		"type first": "type RootQuery { hello: String }\ntype RootMutation { go: String }\n" +
			"schema { query: RootQuery, mutation: RootMutation }",
	} {
		t.Run(name, func(t *testing.T) {
			parsed, err := ParseSDLComplete(sdl)
			if err != nil {
				t.Fatalf("ParseSDLComplete: %v", err)
			}
			if parsed.Query == nil || len(parsed.Query.Fields) == 0 {
				t.Error("the root query has no fields, so the service would answer nothing")
			}
			if parsed.Mutation == nil || len(parsed.Mutation.Fields) == 0 {
				t.Error("the root mutation has no fields")
			}
			// And it is not left behind as an ordinary type as well, which is how
			// a literal `type Query` is already treated.
			if _, ok := parsed.Types["RootQuery"]; ok {
				t.Error("the root type is also listed as an ordinary type")
			}
		})
	}
}

func TestAnUnbackedRootNameIsLeftAlone(t *testing.T) {
	// A block naming a type nobody declared is the author's mistake, and the
	// nearest unrelated type must not be adopted as the root in its place.
	parsed, err := ParseSDLComplete("schema { query: Absent }\ntype Customer { id: ID! }")
	if err != nil {
		t.Fatalf("ParseSDLComplete: %v", err)
	}
	if parsed.Query == nil || parsed.Query.Name != "Absent" {
		t.Fatalf("root query = %+v, want the name that was written", parsed.Query)
	}
	if len(parsed.Query.Fields) != 0 {
		t.Errorf("the root query acquired fields %v", parsed.Query.Fields)
	}
	if _, ok := parsed.Types["Customer"]; !ok {
		t.Error("an unrelated type was consumed as the root")
	}
}
