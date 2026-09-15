package graphql

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

// A Subscription field declared in SDL is the field that runs.
//
// Nothing read the Subscription type a schema declares: ParseSDL built the
// Query and Mutation fields from it and skipped Subscription, and the field a
// flow registered was built from scratch and stored over whatever was there.
// So a field declared to return an object arrived as JSON — a client selecting
// subfields got "must not have a sub selection" — and any argument it declared
// was gone. Meanwhile `_service { sdl }` kept publishing the declaration, so
// the contract a gateway composed on and the schema that answered were two
// different schemas.

const declaredSubscriptionSDL = `
type Order {
  id: ID!
  total: Float
}

type Query { ping: String }

type Subscription {
  "an order, as it is placed"
  orderPlaced(store: String! = "main"): Order
  raw: JSON
}

scalar JSON
`

func subscriptionSchema(t *testing.T, returns string) *graphql.Schema {
	t.Helper()

	path := filepath.Join(t.TempDir(), "schema.graphql")
	if err := os.WriteFile(path, []byte(declaredSubscriptionSDL), 0o644); err != nil {
		t.Fatal(err)
	}

	b := NewSchemaBuilder()
	if err := b.LoadSDL(path); err != nil {
		t.Fatalf("LoadSDL: %v", err)
	}
	pass := func(_ context.Context, input map[string]interface{}) (interface{}, error) { return input, nil }
	if err := b.RegisterHandlerWithReturnType("Subscription.orderPlaced", pass, returns); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterHandler("Query.ping",
		func(context.Context, map[string]interface{}) (interface{}, error) { return "pong", nil }); err != nil {
		t.Fatal(err)
	}
	schema, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return schema
}

func TestASubscriptionFieldKeepsWhatTheSDLDeclares(t *testing.T) {
	// The flow declares no `returns`, which is the ordinary case in SDL mode:
	// the type is written once, in the schema.
	schema := subscriptionSchema(t, "")

	field := schema.SubscriptionType().Fields()["orderPlaced"]
	if field == nil {
		t.Fatal("the declared field is not in the schema")
	}
	if named, ok := graphql.GetNamed(field.Type).(graphql.Type); !ok || named.Name() != "Order" {
		t.Errorf("type = %v, want the declared Order — a client cannot select subfields on JSON", field.Type)
	}
	if len(field.Args) != 1 || field.Args[0].Name() != "store" {
		t.Errorf("args = %v, want the declared store argument", field.Args)
	}
	if field.Args[0].DefaultValue != "main" {
		t.Errorf("the argument's default = %#v, want \"main\"", field.Args[0].DefaultValue)
	}
	if field.Description != "an order, as it is placed" {
		t.Errorf("description = %q, want the one written in the schema", field.Description)
	}
	// It is still a subscription: the two functions that make it one are there.
	if field.Subscribe == nil || field.Resolve == nil {
		t.Error("the field lost its subscribe or resolve function")
	}
}

func TestAFlowsReturnTypeStillDecidesTheSubscriptionField(t *testing.T) {
	// Same rule as a query field: a flow that names a return type gets it.
	schema := subscriptionSchema(t, "Order[]")

	field := schema.SubscriptionType().Fields()["orderPlaced"]
	if _, isList := graphql.GetNullable(field.Type).(*graphql.List); !isList {
		t.Errorf("type = %v, want the list the flow asked for", field.Type)
	}
	// The declared argument survives a flow that names a return type.
	if len(field.Args) != 1 {
		t.Errorf("args = %v, want the declared argument kept", field.Args)
	}
}

func TestASubscriptionFieldNobodyDeclaredIsStillJSON(t *testing.T) {
	// Without SDL there is nothing to keep, and the old behaviour is the only
	// one available.
	b := NewSchemaBuilder()
	pass := func(_ context.Context, input map[string]interface{}) (interface{}, error) { return input, nil }
	if err := b.RegisterHandler("Subscription.anything", pass); err != nil {
		t.Fatal(err)
	}
	if err := b.RegisterHandler("Query.ping",
		func(context.Context, map[string]interface{}) (interface{}, error) { return "pong", nil }); err != nil {
		t.Fatal(err)
	}
	schema, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if got := schema.SubscriptionType().Fields()["anything"].Type; got != JSONScalar {
		t.Errorf("type = %v, want JSON", got)
	}
}

func TestADeclaredSubscriptionArgumentCanBeOmittedWhenItHasADefault(t *testing.T) {
	// The two fixes meet here: the argument now exists, and it has a default,
	// so a client may leave it out.
	schema := subscriptionSchema(t, "")
	if !newDefaultFiller(schema).needed {
		t.Fatal("the declared default is not visible to the filler")
	}

	filled, _ := newDefaultFiller(schema).apply(`subscription { orderPlaced { id } }`, nil, "")
	result := graphql.Do(graphql.Params{Schema: *schema, RequestString: filled})
	for _, err := range result.Errors {
		if err.Message == `Field "orderPlaced" argument "store" of type "String!" is required but not provided.` {
			t.Errorf("the subscription was refused for omitting an argument with a default")
		}
	}
}

func TestADeclaredSubscriptionFieldNoFlowServesIsReportedNotCrashed(t *testing.T) {
	// The SDL above declares two fields and only one flow serves them, which is
	// the state a schema is in while it is being built out. The unserved field
	// is now in the schema — the SDL says it is — so what it does when someone
	// subscribes to it has to be an answer rather than a panic.
	schema := subscriptionSchema(t, "")

	results := graphql.Subscribe(graphql.Params{
		Schema:        *schema,
		RequestString: `subscription { raw }`,
		Context:       context.Background(),
	})
	result := <-results
	if len(result.Errors) == 0 {
		t.Fatal("subscribing to a field no flow serves was accepted")
	}
	if !strings.Contains(result.Errors[0].Message, "raw") {
		t.Errorf("the error does not name the field: %v", result.Errors[0].Message)
	}
}
