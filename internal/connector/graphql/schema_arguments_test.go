package graphql

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/matutetandil/mycel/v3/internal/validate"
)

// A GraphQL argument is where the schema stops being decoration and starts
// refusing things: a field declared to take an Int rejects a string before any
// handler runs. When the mapping from a written type name to a GraphQL type
// falls through, the argument becomes JSON, everything is accepted, and the
// refusal that was supposed to happen at the edge happens somewhere inside the
// flow instead — or not at all.

func TestEveryWrittenTypeNameReachesItsGraphQLType(t *testing.T) {
	// Both spellings of each type exist because both are written: HCL types say
	// "number" and "boolean", while people coming from GraphQL write "Int" and
	// "Boolean". A spelling that misses lands on JSON, which accepts anything.
	b := NewSchemaBuilder()

	for _, tc := range []struct {
		written string
		want    graphql.Input
	}{
		{"string", graphql.String},
		{"String", graphql.String},
		{"number", graphql.Float},
		{"float", graphql.Float},
		{"Float", graphql.Float},
		{"int", graphql.Int},
		{"integer", graphql.Int},
		{"Int", graphql.Int},
		{"boolean", graphql.Boolean},
		{"bool", graphql.Boolean},
		{"Boolean", graphql.Boolean},
		{"id", graphql.ID},
		{"ID", graphql.ID},
	} {
		if got := b.mapArgType(tc.written); got != tc.want {
			t.Errorf("%q maps to %v, want %v", tc.written, got, tc.want)
		}
	}
}

func TestAnUnknownArgumentTypeBecomesJSON(t *testing.T) {
	// The fallback is deliberate — a name nobody declared still has to produce a
	// usable field rather than failing the whole schema — but it means a typo in
	// a type name silently turns validation off for that argument.
	b := NewSchemaBuilder()
	if got := b.mapArgType("Custmoer"); got != JSONScalar {
		t.Errorf("an undeclared type mapped to %v, want the JSON fallback", got)
	}
}

func TestAnArgumentCanNameATypeFromTheConfiguration(t *testing.T) {
	// This is what makes a written type usable as an argument: a mutation taking
	// an "order" is meant to get the input object built from the order type, not
	// an opaque blob.
	b := NewSchemaBuilder()
	err := b.LoadHCLTypes(map[string]*validate.TypeSchema{
		"order": {
			Name: "order",
			Fields: []validate.FieldSchema{
				{Name: "id", Type: "string", Required: true},
				{Name: "total", Type: "number"},
			},
		},
	})
	if err != nil {
		t.Fatalf("LoadHCLTypes: %v", err)
	}

	// Named either way: the input object is registered under a suffixed name,
	// and someone writing the argument uses the name they wrote in the type.
	for _, written := range []string{"order", "orderInput"} {
		got := b.mapArgType(written)
		if got == JSONScalar {
			t.Errorf("%q fell through to JSON, so the declared shape is not enforced", written)
			continue
		}
		if _, ok := got.(*graphql.InputObject); !ok {
			t.Errorf("%q mapped to %T, want an input object", written, got)
		}
	}
}

func TestAFieldWithNoDeclaredArgumentsTakesJSON(t *testing.T) {
	// Most fields are registered without an argument list, and they still have
	// to accept the payload a flow sends. Dropping this would make every such
	// field argument-less and every call to one an error.
	b := NewSchemaBuilder()
	args := b.buildArgs(nil)

	input, ok := args["input"]
	if !ok {
		t.Fatalf("no generic input argument was built, got %v", args)
	}
	if input.Type != JSONScalar {
		t.Errorf("input is %v, want the JSON scalar", input.Type)
	}
}

func TestADeclaredArgumentKeepsItsTypeRequirednessAndDefault(t *testing.T) {
	b := NewSchemaBuilder()
	args := b.buildArgs([]*ArgDef{
		{Name: "id", Type: "id", Required: true, Description: "the order"},
		{Name: "limit", Type: "int", Default: 25},
	})

	if len(args) != 2 {
		t.Fatalf("got %d arguments, want 2", len(args))
	}

	// Required means non-null in the schema, which is what makes the server
	// refuse the call rather than handing the handler a missing id.
	nonNull, ok := args["id"].Type.(*graphql.NonNull)
	if !ok {
		t.Fatalf("id is %v, want it non-null because it was declared required", args["id"].Type)
	}
	if nonNull.OfType != graphql.ID {
		t.Errorf("id wraps %v, want ID", nonNull.OfType)
	}
	if args["id"].Description != "the order" {
		t.Errorf("description = %q, want the one that was written", args["id"].Description)
	}

	if args["limit"].Type != graphql.Int {
		t.Errorf("limit is %v, want Int", args["limit"].Type)
	}
	if args["limit"].DefaultValue != 25 {
		t.Errorf("default = %v, want 25 so the argument can be omitted", args["limit"].DefaultValue)
	}
	if _, isNonNull := args["limit"].Type.(*graphql.NonNull); isNonNull {
		t.Error("limit was made non-null although it has a default")
	}
}

func TestRegisteringAFieldWithArgumentsPutsThemOnTheField(t *testing.T) {
	// The path someone actually takes: the arguments travel from the written
	// operation through to the schema the server serves.
	b := NewSchemaBuilder()
	handler := func(_ context.Context, input map[string]interface{}) (interface{}, error) { return input, nil }

	err := b.RegisterHandlerWithArgs("Query.order", handler, "", []*ArgDef{
		{Name: "id", Type: "id", Required: true},
	})
	if err != nil {
		t.Fatalf("RegisterHandlerWithArgs: %v", err)
	}

	schema, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	field, ok := schema.QueryType().Fields()["order"]
	if !ok {
		t.Fatal("the field was not registered")
	}
	if len(field.Args) != 1 || field.Args[0].Name() != "id" {
		t.Fatalf("field arguments = %v, want the declared id", field.Args)
	}

	// And the schema refuses a call that omits it, which is the whole point of
	// declaring the argument.
	result := graphql.Do(graphql.Params{Schema: *schema, RequestString: `{ order }`})
	if len(result.Errors) == 0 {
		t.Error("a query omitting a required argument was accepted")
	}
}

func TestAnOperationNameWithoutATypeIsRefused(t *testing.T) {
	b := NewSchemaBuilder()
	handler := func(context.Context, map[string]interface{}) (interface{}, error) { return nil, nil }

	if err := b.RegisterHandlerWithArgs("order", handler, "", nil); err == nil {
		t.Error("an operation with no type before the dot was accepted")
	}
	if err := b.RegisterHandlerWithArgs("Nonsense.order", handler, "", nil); err == nil {
		t.Error("an operation on a type that is not Query, Mutation or Subscription was accepted")
	}
}

// Loading a schema is what decides which of the several ways of describing one
// is in force, and the mode is not written anywhere — it is inferred from what
// was loaded. Getting that inference wrong means a service that answers with a
// schema nobody asked for.

func TestLoadingTypesFromConfigurationSelectsThatMode(t *testing.T) {
	b := NewSchemaBuilder()
	if err := b.LoadHCLTypes(map[string]*validate.TypeSchema{
		"customer": {Name: "customer", Fields: []validate.FieldSchema{{Name: "id", Type: "string"}}},
	}); err != nil {
		t.Fatalf("LoadHCLTypes: %v", err)
	}

	if b.mode != SchemaModeHCL {
		t.Errorf("mode = %q, want %q", b.mode, SchemaModeHCL)
	}
	if b.GetType("Customer") == nil && b.GetType("customer") == nil {
		t.Error("the declared type is not registered under either spelling")
	}
	if b.GetHCLConverter() == nil {
		t.Error("no converter was kept, so the types cannot be referred to later")
	}
}

func TestLoadingBothDescriptionsIsHybrid(t *testing.T) {
	// Someone with a schema file who also declares types in configuration gets
	// both, rather than one silently replacing the other.
	b := NewSchemaBuilder()
	if err := b.ParseSDL(`type Query { hello: String }`); err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	if b.mode != SchemaModeSDL {
		t.Fatalf("mode after a schema file = %q, want %q", b.mode, SchemaModeSDL)
	}

	if err := b.LoadHCLTypes(map[string]*validate.TypeSchema{
		"customer": {Name: "customer", Fields: []validate.FieldSchema{{Name: "id", Type: "string"}}},
	}); err != nil {
		t.Fatalf("LoadHCLTypes: %v", err)
	}
	if b.mode != SchemaModeHybrid {
		t.Errorf("mode = %q, want %q", b.mode, SchemaModeHybrid)
	}
}

func TestAnExplicitModeIsNotOverwritten(t *testing.T) {
	b := NewSchemaBuilder()
	b.SetMode(SchemaModeDynamic)

	if err := b.LoadHCLTypes(map[string]*validate.TypeSchema{
		"customer": {Name: "customer", Fields: []validate.FieldSchema{{Name: "id", Type: "string"}}},
	}); err != nil {
		t.Fatalf("LoadHCLTypes: %v", err)
	}
	if b.mode != SchemaModeDynamic {
		t.Errorf("mode = %q, want the one that was set explicitly", b.mode)
	}
}

func TestNoTypesAtAllChangesNothing(t *testing.T) {
	b := NewSchemaBuilder()
	if err := b.LoadHCLTypes(nil); err != nil {
		t.Fatalf("LoadHCLTypes: %v", err)
	}
	if b.mode != SchemaModeAuto {
		t.Errorf("mode = %q, want it left alone", b.mode)
	}
	if b.GetHCLConverter() != nil {
		t.Error("a converter was built for no types")
	}
}

func TestASchemaFileIsReadFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	const sdl = `
type Customer {
  id: ID!
  email: String
}

type Query {
  customer(id: ID!): Customer
}
`
	if err := os.WriteFile(path, []byte(sdl), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := NewSchemaBuilder()
	if err := b.LoadSDL(path); err != nil {
		t.Fatalf("LoadSDL: %v", err)
	}

	if b.GetRawSDL() != sdl {
		t.Error("the file contents were not kept, so the schema cannot be served back")
	}
	if b.GetParsedSchema() == nil {
		t.Fatal("nothing was parsed")
	}
	if b.GetSDLConverter() == nil {
		t.Error("no converter was kept for the parsed schema")
	}
	if b.GetType("Customer") == nil {
		t.Error("the type declared in the file is not registered")
	}
}

func TestASchemaFileThatIsNotThereIsReported(t *testing.T) {
	// A path with a typo in it must name the path, since the alternative is a
	// service that starts with an empty schema and answers nothing.
	b := NewSchemaBuilder()
	err := b.LoadSDL(filepath.Join(t.TempDir(), "absent.graphql"))
	if err == nil {
		t.Fatal("a missing schema file was accepted")
	}
	if !strings.Contains(err.Error(), "absent.graphql") {
		t.Errorf("error = %q, want it to name the file", err)
	}
}

func TestASchemaFileThatIsNotASchemaIsReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	if err := os.WriteFile(path, []byte("this is not a schema {{{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b := NewSchemaBuilder()
	if err := b.LoadSDL(path); err == nil {
		t.Error("a file that is not a schema was accepted")
	}
}

func TestSetRawSDLReplacesWhatIsServedBack(t *testing.T) {
	b := NewSchemaBuilder()
	b.SetRawSDL("type Query { hello: String }")
	if got := b.GetRawSDL(); got != "type Query { hello: String }" {
		t.Errorf("raw schema = %q", got)
	}
}
