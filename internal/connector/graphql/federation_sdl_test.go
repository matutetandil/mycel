package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/matutetandil/mycel/v2/internal/graphql/analyzer"
)

// What a federated service publishes about itself, and what a resolver is
// handed. A router reads the SDL to work out which service owns which type; a
// directive missing from it is a type the router will not route.

func TestAFederatedSchemaCarriesTheDirectivesARouterNeeds(t *testing.T) {
	f := NewFederationSupport(2)

	sdl := f.GenerateFederatedSDL(`
type Order @key(fields: "id") {
  id: ID!
}
`)

	// The definitions have to be in the document the router composes from, or
	// composition fails on a directive it does not know.
	for _, want := range []string{"directive @key", "type Order"} {
		if !strings.Contains(sdl, want) {
			t.Errorf("the published schema does not carry %q:\n%s", want, sdl)
		}
	}
}

func TestAFederationVersionDecidesWhatIsPublished(t *testing.T) {
	// A v1 router does not know the v2 directives and refuses a schema
	// carrying them, so the version is not cosmetic.
	one := NewFederationSupport(1).GenerateFederatedSDL("type Order { id: ID! }")
	two := NewFederationSupport(2).GenerateFederatedSDL("type Order { id: ID! }")

	if one == two {
		t.Error("both versions publish the same directives")
	}
	if !strings.Contains(one, "directive @key") || !strings.Contains(two, "directive @key") {
		t.Error("a version published no @key at all")
	}

	// Nothing said is version 2, which is what a current router speaks.
	if NewFederationSupport(0).GenerateFederatedSDL("type Order { id: ID! }") != two {
		t.Error("the default is not the current version")
	}
}

func TestAServiceSaysWhichEntitiesItOwns(t *testing.T) {
	f := NewFederationSupport(2)

	if f.HasEntities() {
		t.Error("a service with nothing registered claims entities")
	}

	f.RegisterEntity("Order", []EntityKey{{Fields: "id", Resolvable: true}}, nil, nil)

	if !f.HasEntities() {
		t.Fatal("a registered entity is not there")
	}
	names := f.GetEntityNames()
	if len(names) != 1 || names[0] != "Order" {
		t.Errorf("entities = %v", names)
	}
}

// --- What a resolver is handed ----------------------------------------------

func TestAResolverKnowsWhichFieldsWereAskedFor(t *testing.T) {
	// The whole point of the optimization: a query asking for two fields
	// should not make the flow fetch forty.
	tree := analyzer.NewFieldTree()
	tree.AddField(analyzer.NewFieldNode("id"))
	tree.AddField(analyzer.NewFieldNode("reference"))
	fields := analyzer.NewRequestedFields(tree)

	ctx := WithRequestedFields(context.Background(), fields)
	got := GetRequestedFields(ctx)
	if got == nil {
		t.Fatal("the fields the query asked for did not reach the flow")
	}
	if !got.Has("id") || !got.Has("reference") {
		t.Errorf("fields = %v, want the ones the query asked for", got.List())
	}
	if got.Has("a_field_nobody_asked_for") {
		t.Error("a field the query did not ask for is reported as requested")
	}

	// And a context that carries none answers nothing rather than an empty
	// list, which a handler would read as "fetch nothing".
	if GetRequestedFields(context.Background()) != nil {
		t.Error("a context carrying no fields answered with some")
	}
}

func TestAnAnswerIsWrappedTheWayAClientExpects(t *testing.T) {
	// A GraphQL client reads data and errors, and nothing else. A failure put
	// anywhere but errors is one the client reports as success.
	answer := BuildDataResponse(map[string]interface{}{"orders": []interface{}{}})
	if answer.Data == nil {
		t.Error("the answer carries no data")
	}
	if len(answer.Errors) != 0 {
		t.Error("a successful answer carries errors")
	}

	failure := BuildErrorResponse(context.DeadlineExceeded)
	if len(failure.Errors) != 1 {
		t.Fatalf("errors = %v, want the one that happened", failure.Errors)
	}
	if failure.Errors[0].Message == "" {
		t.Error("the failure says nothing")
	}
	if failure.Data != nil {
		t.Error("a failed answer carries data")
	}
}

func TestAResolverAnswersWithWhatTheFlowProduced(t *testing.T) {
	// The wiring between a flow and a GraphQL field: whatever the handler
	// returns is what the client reads.
	resolve := CreateResolver(func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "1", "reference": input["reference"]}, nil
	})

	answer, err := resolved(t, resolve, graphql.ResolveParams{
		Context: context.Background(),
		Args:    map[string]interface{}{"reference": "ORD-1"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	row, ok := answer.(map[string]interface{})
	if !ok {
		t.Fatalf("answer = %#v", answer)
	}
	// The arguments reached the flow, which is how a query says what it wants.
	if row["reference"] != "ORD-1" {
		t.Errorf("row = %v, want the argument carried through", row)
	}
}

func TestAFailingFlowFailsTheField(t *testing.T) {
	// Rather than answering null and leaving the client to guess.
	resolve := CreateResolver(func(context.Context, map[string]interface{}) (interface{}, error) {
		return nil, context.DeadlineExceeded
	})

	if _, err := resolved(t, resolve, graphql.ResolveParams{Context: context.Background()}); err == nil {
		t.Error("a flow that failed answered the field successfully")
	}
}

func TestOneRowCanBeAnsweredAsAnObject(t *testing.T) {
	// A query for a single order gets an object; the flow answers with the
	// rows a database returned, which is a list of one.
	resolve := CreateResolverWithOptions(func(context.Context, map[string]interface{}) (interface{}, error) {
		return []map[string]interface{}{{"id": "1"}}, nil
	}, ResolverOptions{UnwrapSingleResult: true})

	answer, err := resolved(t, resolve, graphql.ResolveParams{Context: context.Background()})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, ok := answer.(map[string]interface{}); !ok {
		t.Errorf("answer = %#v, want a single object", answer)
	}
}

// resolved runs a resolver and settles the thunk graphql-go resolvers return,
// which is how the library defers work until it needs the value.
func resolved(t *testing.T, resolve graphql.FieldResolveFn, p graphql.ResolveParams) (interface{}, error) {
	t.Helper()
	answer, err := resolve(p)
	if err != nil {
		return nil, err
	}
	thunk, ok := answer.(func() (interface{}, error))
	if !ok {
		return answer, nil
	}
	return thunk()
}
