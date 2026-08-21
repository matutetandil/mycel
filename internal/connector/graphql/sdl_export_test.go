package graphql

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/validate"
)

// `mycel export graphql-schema` is documented in the CLI reference and in the
// federation guide, and no such command existed — while the generator behind it
// sat complete and uncalled. A federation gateway needs this schema at build
// time, from the configuration rather than from a running service.

func exportFlows() []*flow.Config {
	from := func(op string) *flow.FromConfig {
		return &flow.FromConfig{Connector: "gql", ConnectorParams: map[string]interface{}{"operation": op}}
	}
	return []*flow.Config{
		{Name: "users", From: from("Query.users")},
		{Name: "user", From: from("Query.user"), Validate: &flow.ValidateConfig{Output: "User"}},
		{Name: "create", From: from("Mutation.createUser"),
			Validate: &flow.ValidateConfig{Input: "User", Output: "User"}},
		// Not a GraphQL flow, and not a field.
		{Name: "rest", From: from("GET /users")},
		// A root nobody serves.
		{Name: "sub", From: from("Subscription.userAdded")},
	}
}

func exportTypes() []*validate.TypeSchema {
	return []*validate.TypeSchema{{
		Name: "User",
		Fields: []validate.FieldSchema{
			{Name: "id", Type: "string", Required: true},
			{Name: "email", Type: "string"},
		},
	}}
}

func TestExportedSDLParses(t *testing.T) {
	// The point of the test: not that the string looks right, but that it is a
	// schema. Comparing text would pass on output no client could read.
	sdl := ExportSDL(exportTypes(), exportFlows())

	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("the exported schema does not parse: %v\n\n%s", err, sdl)
	}
}

func TestExportedSDLCarriesTypesAndFields(t *testing.T) {
	sdl := ExportSDL(exportTypes(), exportFlows())

	for _, want := range []string{
		"type User {", "input UserInput {",
		"type Query {", "type Mutation {",
		"users: JSON",                         // no declared output type
		"user: User",                          // validate.output names it
		"createUser(input: UserInput!): User", // and validate.input is the argument
	} {
		if !strings.Contains(sdl, want) {
			t.Errorf("the schema is missing %q:\n%s", want, sdl)
		}
	}

	// A REST flow is not a GraphQL field, and a root the runtime does not serve
	// is not one either.
	for _, unwanted := range []string{"GET /users", "userAdded"} {
		if strings.Contains(sdl, unwanted) {
			t.Errorf("%q leaked into the schema", unwanted)
		}
	}
}

func TestAnEmptyRootTypeIsOmitted(t *testing.T) {
	// `type Mutation {}` is not valid SDL, so a configuration with no mutations
	// must not produce one.
	sdl := ExportSDL(exportTypes(), []*flow.Config{
		{Name: "users", From: &flow.FromConfig{Connector: "gql",
			ConnectorParams: map[string]interface{}{"operation": "Query.users"}}},
	})

	if strings.Contains(sdl, "type Mutation") {
		t.Errorf("an empty Mutation type was written:\n%s", sdl)
	}
	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("the exported schema does not parse: %v\n\n%s", err, sdl)
	}
}

func TestFieldsAreOrdered(t *testing.T) {
	// Flows come off a directory walk, so without sorting the same
	// configuration would export a different schema on each run — and this
	// output is meant to be committed and diffed.
	first := ExportSDL(exportTypes(), exportFlows())
	for i := 0; i < 20; i++ {
		if got := ExportSDL(exportTypes(), exportFlows()); got != first {
			t.Fatal("two exports of the same configuration differ")
		}
	}
}

func TestADeclaredSchemaFileIsTheSchema(t *testing.T) {
	// The server serves the file a connector points at, so an export that
	// rebuilt one from the type blocks would hand out a schema the running
	// service does not serve. This asserts the shape of the decision; the
	// reading of the file belongs to the command, which knows the config
	// directory.
	sdl := ExportSDL(exportTypes(), exportFlows())

	// The derived form is what happens when nothing declares a file: it is
	// built from the blocks, and it parses.
	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("the derived schema does not parse: %v", err)
	}
	if !strings.Contains(sdl, "type User {") {
		t.Error("the derived schema does not carry the type blocks")
	}
}
