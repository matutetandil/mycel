package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

// A subscription field is shaped like a query field. The published payload
// went to the client as it was, so a scalar subscription field fed a map
// would have arrived as `map[value:3]`, the same symptom a scalar Query field
// had.
func TestAScalarSubscriptionFieldIsAnsweredWithTheValue(t *testing.T) {
	builder := NewSchemaBuilder()
	handler := func(ctx context.Context, input map[string]interface{}) (interface{}, error) { return input, nil }
	if err := builder.RegisterHandlerWithArgs("Subscription.counter", handler, "Int!", nil); err != nil {
		t.Fatal(err)
	}
	if err := builder.RegisterHandlerWithArgs("Subscription.items", handler, "[JSON!]!", nil); err != nil {
		t.Fatal(err)
	}
	schema, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	fields := schema.SubscriptionType().Fields()

	counter := fields["counter"]
	got, err := counter.Resolve(graphql.ResolveParams{
		Source: map[string]interface{}{"value": 3},
		Info:   graphql.ResolveInfo{FieldName: "counter", ReturnType: counter.Type},
	})
	if err != nil || got != 3 {
		t.Errorf("counter = %#v, err = %v", got, err)
	}

	// A list field fed a single object is a list of one.
	items := fields["items"]
	got, err = items.Resolve(graphql.ResolveParams{
		Source: map[string]interface{}{"id": 1},
		Info:   graphql.ResolveInfo{FieldName: "items", ReturnType: items.Type},
	})
	if list, ok := got.([]interface{}); err != nil || !ok || len(list) != 1 {
		t.Errorf("items = %#v, err = %v", got, err)
	}

	// Several keys for a scalar is an error that names the field.
	_, err = counter.Resolve(graphql.ResolveParams{
		Source: map[string]interface{}{"a": 1, "b": 2},
		Info:   graphql.ResolveInfo{FieldName: "counter", ReturnType: counter.Type},
	})
	if err == nil || !strings.Contains(err.Error(), "counter") {
		t.Errorf("a two-key payload to Int!: %v", err)
	}
}
