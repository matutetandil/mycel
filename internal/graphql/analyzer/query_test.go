package analyzer

import (
	"sort"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
)

// Working out what a GraphQL client actually asked for.
//
// This is what decides which columns a query selects and which nested
// resolvers run: a field dropped here comes back null, and a field invented
// here is work nobody asked for. It reads a real AST, and until now it was
// only tested through the tree it builds, never from a query — so fragments,
// aliases and arguments, which are where the reading gets hard, went
// unchecked.

// resolveParams parses a query the way the server receives it and hands back
// what a resolver for the top-level field would be given.
func resolveParams(t *testing.T, query string) graphql.ResolveParams {
	t.Helper()

	doc, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{Body: []byte(query)})})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	fragments := map[string]ast.Definition{}
	var fields []*ast.Field

	for _, definition := range doc.Definitions {
		switch def := definition.(type) {
		case *ast.OperationDefinition:
			for _, selection := range def.SelectionSet.Selections {
				if field, ok := selection.(*ast.Field); ok {
					fields = append(fields, field)
				}
			}
		case *ast.FragmentDefinition:
			fragments[def.Name.Value] = def
		}
	}

	if len(fields) == 0 {
		t.Fatalf("no top-level field in %s", query)
	}
	return graphql.ResolveParams{
		Info: graphql.ResolveInfo{FieldASTs: fields, Fragments: fragments},
	}
}

func TestTheFieldsAQueryAsksFor(t *testing.T) {
	p := resolveParams(t, `
		query {
			user {
				id
				name
				orders {
					id
					total
				}
			}
		}
	`)

	fields := ExtractFields(p)

	if !fields.Has("id") || !fields.Has("name") {
		t.Errorf("the plain fields were not seen: %v", fields.ListFlat())
	}
	// Nested paths are what tell a resolver whether to go and fetch the
	// orders at all.
	if !fields.Has("orders.total") {
		t.Errorf("nested fields were not seen: %v", fields.ListFlat())
	}
	if fields.Has("email") {
		t.Error("a field nobody asked for was reported as requested")
	}
	if fields.IsEmpty() {
		t.Error("a query with fields in it came back empty")
	}
}

func TestFieldsBehindAFragment(t *testing.T) {
	// Fragments are how a client shares a selection between queries, and to
	// this code they are indirection: unresolved, every field inside one goes
	// missing and the response comes back with nulls where data was asked for.
	p := resolveParams(t, `
		query {
			user {
				id
				...contactDetails
				... on Person {
					nickname
				}
			}
		}

		fragment contactDetails on User {
			email
			phone
		}
	`)

	fields := ExtractFields(p)

	for _, want := range []string{"id", "email", "phone", "nickname"} {
		if !fields.Has(want) {
			t.Errorf("%s was asked for and not seen: %v", want, fields.ListFlat())
		}
	}
}

func TestAFragmentThatIsNotThere(t *testing.T) {
	// A query naming a fragment the document does not define is invalid, and
	// the server rejects it — but this runs before that, so it must not take
	// the process with it.
	p := graphql.ResolveParams{
		Info: graphql.ResolveInfo{
			FieldASTs: resolveParams(t, `query { user { id ...missing } }`).Info.FieldASTs,
			Fragments: map[string]ast.Definition{},
		},
	}

	fields := ExtractFields(p)
	if !fields.Has("id") {
		t.Errorf("the fields that were there went missing too: %v", fields.ListFlat())
	}
}

func TestAFieldAskedForUnderAnotherName(t *testing.T) {
	// An alias is the name the answer must come back under, and a client may
	// ask for the same field twice under two names.
	p := resolveParams(t, `
		query {
			user {
				userName: name
				contact: email
			}
		}
	`)

	fields := ExtractFields(p)

	if !fields.Has("userName") || !fields.Has("contact") {
		t.Errorf("aliased fields were not seen under their alias: %v", fields.ListFlat())
	}
	node := fields.Get("userName")
	if node == nil || node.Name != "name" {
		t.Errorf("the alias lost the field it stands for: %+v", node)
	}
}

func TestTheArgumentsAFieldWasGiven(t *testing.T) {
	// These become the filter, the limit and the page a resolver applies, so
	// a value read as the wrong shape is a query for the wrong rows.
	p := resolveParams(t, `
		query {
			user {
				orders(
					first: 10
					minimum: 12.5
					status: "paid"
					includeCancelled: false
					sort: DESC
					tags: ["urgent", "nz"]
					filter: { country: "NZ", open: true }
					after: $cursor
				) {
					id
				}
			}
		}
	`)

	args := ExtractFields(p).Get("orders").Arguments

	if args["first"] != "10" || args["minimum"] != "12.5" {
		t.Errorf("numbers = %v/%v", args["first"], args["minimum"])
	}
	if args["status"] != "paid" {
		t.Errorf("string = %v", args["status"])
	}
	// Booleans arrive as booleans while numbers arrive as the text they were
	// written as — that is what the AST holds, and worth pinning down here
	// because a caller comparing an argument has to know which it is getting.
	if args["includeCancelled"] != false {
		t.Errorf("boolean = %#v, want a real false", args["includeCancelled"])
	}
	// An enum arrives as the word itself, not quoted.
	if args["sort"] != "DESC" {
		t.Errorf("enum = %v", args["sort"])
	}
	list, ok := args["tags"].([]interface{})
	if !ok || len(list) != 2 || list[0] != "urgent" {
		t.Errorf("list = %v", args["tags"])
	}
	nested, ok := args["filter"].(map[string]interface{})
	if !ok || nested["country"] != "NZ" || nested["open"] != true {
		t.Errorf("record = %v", args["filter"])
	}
	// A variable is not resolved here — it is named, so a resolver can see
	// that the value comes from the request's variables.
	if args["after"] != "$cursor" {
		t.Errorf("variable = %v, want it named", args["after"])
	}
}

func TestIntrospectionIsNotAField(t *testing.T) {
	// __typename is answered by the server itself. Passed on, a resolver
	// selects a column of that name and the database refuses the query.
	p := resolveParams(t, `query { user { __typename id } }`)

	fields := ExtractFields(p)
	if fields.Has("__typename") {
		t.Errorf("introspection was passed on as a field: %v", fields.ListFlat())
	}
	if !fields.Has("id") {
		t.Error("the real field went missing with it")
	}
}

func TestReadingTheSameQueryFromTheResolveInfo(t *testing.T) {
	// The other entry point, used where only the info is to hand. It must
	// answer the same as the one that takes the whole params.
	p := resolveParams(t, `query { user { id orders { total } } }`)

	fromParams := ExtractFields(p).ListFlat()
	fromInfo := ExtractFieldsFromInfo(p.Info).ListFlat()

	sort.Strings(fromParams)
	sort.Strings(fromInfo)
	if len(fromParams) != len(fromInfo) {
		t.Fatalf("params saw %v, info saw %v", fromParams, fromInfo)
	}
	for i := range fromParams {
		if fromParams[i] != fromInfo[i] {
			t.Errorf("params saw %v, info saw %v", fromParams, fromInfo)
		}
	}

	// And the plain list of names, which is what the older callers read.
	names := ExtractFieldNames(p)
	if len(names) == 0 {
		t.Error("no field names came back")
	}
}

func TestHowMuchAQueryIsAskingFor(t *testing.T) {
	// Depth and count are what a limit is applied to: a client asking for
	// orders inside items inside products is a query that can cost far more
	// than it looks.
	shallow := AnalyzeQuery(resolveParams(t, `query { user { id name } }`))
	if shallow.HasNestedFields {
		t.Errorf("a flat query was reported as nested: %+v", shallow)
	}
	if shallow.FieldCount != 2 || shallow.MaxDepth != 1 {
		t.Errorf("count/depth = %d/%d, want 2/1", shallow.FieldCount, shallow.MaxDepth)
	}

	deep := AnalyzeQuery(resolveParams(t, `
		query {
			user {
				id
				orders {
					id
					items {
						sku
					}
				}
			}
		}
	`))
	if !deep.HasNestedFields {
		t.Error("a nested query was reported as flat")
	}
	if deep.MaxDepth != 3 {
		t.Errorf("depth = %d, want three levels", deep.MaxDepth)
	}
	if deep.RequestedFields == nil || deep.RequestedFields.Tree() == nil {
		t.Error("the analysis carries no fields")
	}

	// The level a field sits at, which is how a resolver decides what to fetch
	// in one go and what to leave to a nested resolver.
	top := deep.RequestedFields.ListAtDepth(0)
	sort.Strings(top)
	if len(top) != 2 || top[0] != "id" || top[1] != "orders" {
		t.Errorf("top level = %v", top)
	}
	if nested := deep.RequestedFields.ListAtDepth(1); len(nested) != 2 {
		t.Errorf("second level = %v", nested)
	}
}

func TestAQueryThatAsksForNothingNested(t *testing.T) {
	// A leaf field with no selection: nothing to walk into, and it must not
	// be reported as having children.
	fields := ExtractFields(resolveParams(t, `query { version }`))

	if !fields.IsEmpty() {
		t.Errorf("a query selecting nothing under its field came back with %v", fields.ListFlat())
	}
	if sub := fields.SubFields("nothing"); !sub.IsEmpty() {
		t.Errorf("a path that is not there came back with %v", sub.ListFlat())
	}

	analysis := AnalyzeQuery(resolveParams(t, `query { version }`))
	if analysis.FieldCount != 0 || analysis.MaxDepth != 0 {
		t.Errorf("count/depth = %d/%d, want nothing", analysis.FieldCount, analysis.MaxDepth)
	}
}
