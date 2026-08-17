package graphql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

// Between a database row and a GraphQL answer there is a rename: columns are
// spelled with underscores and schemas usually are not. The rename is the one
// place a field can come back empty without anything going wrong — a null in
// the response is a legitimate answer, so nothing reports it.

func answering(t *testing.T, sdl string, row interface{}) *graphql.Schema {
	t.Helper()
	b := NewSchemaBuilder()
	if err := b.ParseSDL(sdl); err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	err := b.RegisterHandler("Query.customer", func(context.Context, map[string]interface{}) (interface{}, error) {
		return row, nil
	})
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	schema, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return schema
}

func query(t *testing.T, schema *graphql.Schema, request string) map[string]interface{} {
	t.Helper()
	result := graphql.Do(graphql.Params{Schema: *schema, RequestString: request})
	if len(result.Errors) > 0 {
		t.Fatalf("query: %v", result.Errors)
	}
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	customer, _ := decoded["customer"].(map[string]interface{})
	if customer == nil {
		t.Fatalf("no customer in %s", encoded)
	}
	return customer
}

func TestAColumnAnswersUnderEitherSpelling(t *testing.T) {
	// A schema written to match the database is as reasonable as one written
	// in GraphQL's conventions, and the same row has to answer both. Renaming
	// the key outright left the first kind resolving to null — not an error,
	// just an empty field.
	const sdl = `
type Customer {
  id: ID!
  created_at: String
  createdAt: String
  external_id: String
  externalId: String
}
type Query { customer: Customer }
`
	row := map[string]interface{}{
		"id": "1", "created_at": "2026-01-01", "external_id": "X-1",
	}
	customer := query(t, answering(t, sdl, row),
		`{ customer { id created_at createdAt external_id externalId } }`)

	for field, want := range map[string]string{
		"created_at":  "2026-01-01",
		"createdAt":   "2026-01-01",
		"external_id": "X-1",
		"externalId":  "X-1",
	} {
		if got := customer[field]; got != want {
			t.Errorf("%s = %v, want %q", field, got, want)
		}
	}
}

func TestARowThatAlreadyHasBothKeepsThemApart(t *testing.T) {
	// If the row carries both spellings they mean different things, and the
	// converted one must not overwrite the one that was sent.
	const sdl = `
type Customer {
  id: ID!
  created_at: String
  createdAt: String
}
type Query { customer: Customer }
`
	row := map[string]interface{}{
		"id": "1", "created_at": "from the column", "createdAt": "from the transform",
	}
	customer := query(t, answering(t, sdl, row), `{ customer { created_at createdAt } }`)

	if customer["created_at"] != "from the column" {
		t.Errorf("created_at = %v", customer["created_at"])
	}
	if customer["createdAt"] != "from the transform" {
		t.Errorf("createdAt = %v, want the value that was sent under that name", customer["createdAt"])
	}
}

func TestNestedRowsAreRenamedToo(t *testing.T) {
	const sdl = `
type Address {
  street_name: String
  streetName: String
}
type Customer {
  id: ID!
  home_address: Address
  homeAddress: Address
}
type Query {
  customer: Customer
}
`
	row := map[string]interface{}{
		"id":           "1",
		"home_address": map[string]interface{}{"street_name": "Queen Street"},
	}
	customer := query(t, answering(t, sdl, row),
		`{ customer { home_address { street_name streetName } homeAddress { streetName } } }`)

	nested, _ := customer["home_address"].(map[string]interface{})
	if nested == nil {
		t.Fatalf("customer = %v", customer)
	}
	if nested["street_name"] != "Queen Street" || nested["streetName"] != "Queen Street" {
		t.Errorf("nested = %v", nested)
	}
	camel, _ := customer["homeAddress"].(map[string]interface{})
	if camel == nil || camel["streetName"] != "Queen Street" {
		t.Errorf("homeAddress = %v", camel)
	}
}

func TestASingleRowIsHandedBackAsAnObject(t *testing.T) {
	// A database read returns rows even when the schema promises one object.
	// Handing the list over would fail the query with a type error.
	const sdl = `
type Customer { id: ID! }
type Query { customer: Customer }
`
	rows := []map[string]interface{}{{"id": "1"}}
	customer := query(t, answering(t, sdl, rows), `{ customer { id } }`)
	if customer["id"] != "1" {
		t.Errorf("customer = %v", customer)
	}
}

func TestNoRowsIsNothingRatherThanAnEmptyObject(t *testing.T) {
	b := NewSchemaBuilder()
	if err := b.ParseSDL(`type Customer { id: ID! }
type Query { customer: Customer }`); err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	err := b.RegisterHandler("Query.customer", func(context.Context, map[string]interface{}) (interface{}, error) {
		return []map[string]interface{}{}, nil
	})
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	schema, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result := graphql.Do(graphql.Params{Schema: *schema, RequestString: `{ customer { id } }`})
	if len(result.Errors) > 0 {
		t.Fatalf("query: %v", result.Errors)
	}
	data, _ := result.Data.(map[string]interface{})
	if data["customer"] != nil {
		t.Errorf("customer = %v, want nothing found", data["customer"])
	}
}

func TestTheUnwrappingKeepsSeveralRowsAsAList(t *testing.T) {
	for name, tc := range map[string]struct {
		in     interface{}
		isList bool
	}{
		"one row becomes the row":      {[]map[string]interface{}{{"id": "1"}}, false},
		"two rows stay a list":         {[]map[string]interface{}{{"id": "1"}, {"id": "2"}}, true},
		"one element of anything":      {[]interface{}{"a"}, false},
		"two elements of anything":     {[]interface{}{"a", "b"}, true},
		"something that is not a list": {map[string]interface{}{"id": "1"}, false},
		"a value that is not a map":    {"plain", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := unwrapSingleResult(tc.in)
			_, gotList := got.([]map[string]interface{})
			if !gotList {
				_, gotList = got.([]interface{})
			}
			if gotList != tc.isList {
				t.Errorf("got %#v, list = %v want %v", got, gotList, tc.isList)
			}
		})
	}

	if got := unwrapSingleResult([]map[string]interface{}{}); got != nil {
		t.Errorf("no rows became %#v, want nothing", got)
	}
}

func TestTheRenameHandlesTheAwkwardNames(t *testing.T) {
	for written, want := range map[string]string{
		"created_at":    "createdAt",
		"external_id":   "externalId",
		"a_b_c":         "aBC",
		"id":            "id",
		"alreadyCamel":  "alreadyCamel",
		"":              "",
		"_leading":      "Leading",
		"trailing_":     "trailing",
		"double__under": "doubleUnder",
		"with_9_digit":  "with9Digit",
	} {
		if got := snakeToCamel(written); got != want {
			t.Errorf("snakeToCamel(%q) = %q, want %q", written, got, want)
		}
	}
}

func TestAQueryIsToldWhichFieldsItWasAskedFor(t *testing.T) {
	// The selection is passed into the flow so a step nobody's field needs can
	// be skipped, which is the whole reason a GraphQL destination can be
	// cheaper than a REST one.
	var seen []string
	b := NewSchemaBuilder()
	if err := b.ParseSDL(`
type Customer { id: ID!, email: String, name: String }
type Query { customer: Customer }
`); err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	err := b.RegisterHandler("Query.customer", func(_ context.Context, input map[string]interface{}) (interface{}, error) {
		seen, _ = input["__requested_fields"].([]string)
		return map[string]interface{}{"id": "1", "email": "a@example.com", "name": "Someone"}, nil
	})
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	schema, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	result := graphql.Do(graphql.Params{Schema: *schema, RequestString: `{ customer { id email } }`})
	if len(result.Errors) > 0 {
		t.Fatalf("query: %v", result.Errors)
	}

	asText := make([]string, 0, len(seen))
	for _, f := range seen {
		asText = append(asText, strings.ToLower(f))
	}
	joined := strings.Join(asText, ",")
	for _, want := range []string{"id", "email"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the flow was told %v, want it to include %q", asText, want)
		}
	}
	if strings.Contains(joined, "name") {
		t.Errorf("the flow was told about %q, which was not asked for", "name")
	}
}

func TestTypesThatReferToEachOtherBuildWhateverOrderTheyAreIn(t *testing.T) {
	// The conversion walks a map, and a map is walked in a different order
	// every run. A field naming another object used to resolve to whichever
	// object was in the map at that moment — an empty placeholder, if that type
	// had not been reached yet — which was then thrown away while the field
	// still pointed at it. The same file, the same binary, built or refused
	// depending on the run.
	const sdl = `
type Address {
  street: String
  city:   String
}

type Company {
  name:    String
  address: Address
}

type Customer {
  id:      ID!
  address: Address
  employer: Company
  referrer: Customer
}

type Query {
  customer: Customer
}
`
	// Enough repeats that a random order would have shown itself.
	for i := 0; i < 25; i++ {
		b := NewSchemaBuilder()
		if err := b.ParseSDL(sdl); err != nil {
			t.Fatalf("run %d: ParseSDL: %v", i, err)
		}
		schema, err := b.Build()
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}

		customer := schema.QueryType().Fields()["customer"]
		object, ok := customer.Type.(*graphql.Object)
		if !ok {
			t.Fatalf("run %d: customer is %T", i, customer.Type)
		}
		address, ok := object.Fields()["address"]
		if !ok {
			t.Fatalf("run %d: customer has no address: %v", i, object.Fields())
		}
		// The one that mattered: the referenced type has its own fields rather
		// than being the discarded empty one.
		referenced, ok := address.Type.(*graphql.Object)
		if !ok {
			t.Fatalf("run %d: address is %T", i, address.Type)
		}
		if len(referenced.Fields()) != 2 {
			t.Fatalf("run %d: Address has %d fields, want both", i, len(referenced.Fields()))
		}
	}
}

func TestATypeThatRefersToItselfBuilds(t *testing.T) {
	// A tree, a comment thread, a manager chain. This is what the deferred
	// fields are for in the first place.
	b := NewSchemaBuilder()
	if err := b.ParseSDL(`
type Employee {
  id:      ID!
  manager: Employee
  reports: [Employee!]
}
type Query { employee: Employee }
`); err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	if _, err := b.Build(); err != nil {
		t.Fatalf("a type referring to itself was refused: %v", err)
	}
}
