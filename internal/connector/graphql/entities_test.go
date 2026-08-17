package graphql

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

// _entities is the field a federation gateway calls to fetch one of this
// service's entities by its key, and it is how a field of a type owned here
// ends up in a query answered by another subgraph. Everything about a federated
// graph that involves more than one service goes through it, and it was barely
// covered — so a representation the gateway sends and this service mishandles
// shows up as a null in somebody's query with no error to explain it.

func federationWith(t *testing.T, resolver EntityResolver) *FederationSupport {
	t.Helper()
	f := NewFederationSupport(2)

	product := graphql.NewObject(graphql.ObjectConfig{
		Name: "Product",
		Fields: graphql.Fields{
			"id":   &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
			"name": &graphql.Field{Type: graphql.String},
		},
	})
	f.RegisterEntity("Product", []EntityKey{{Fields: "id"}}, resolver, product)
	return f
}

// resolveEntities runs the field's resolver the way the gateway's query would.
func resolveEntities(t *testing.T, f *FederationSupport, representations []interface{}) (interface{}, error) {
	t.Helper()
	field := f.CreateEntitiesField()
	if field.Resolve == nil {
		t.Fatal("the _entities field has no resolver")
	}
	return field.Resolve(graphql.ResolveParams{
		Context: context.Background(),
		Args:    map[string]interface{}{"representations": representations},
	})
}

func TestTheGatewayGetsBackTheEntityItAskedFor(t *testing.T) {
	f := federationWith(t, func(_ context.Context, rep map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": rep["id"], "name": "Lamp"}, nil
	})

	answer, err := resolveEntities(t, f, []interface{}{
		map[string]interface{}{"__typename": "Product", "id": "p-1"},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	entities, ok := answer.([]interface{})
	if !ok || len(entities) != 1 {
		t.Fatalf("answer = %#v", answer)
	}
	row, _ := entities[0].(map[string]interface{})
	if row["name"] != "Lamp" || row["id"] != "p-1" {
		t.Errorf("entity = %v", row)
	}
}

func TestEveryRepresentationIsAnsweredInOrder(t *testing.T) {
	// The gateway matches answers to its representations by position, so an
	// answer in the wrong slot attaches one product's fields to another.
	f := federationWith(t, func(_ context.Context, rep map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": rep["id"], "name": "name of " + rep["id"].(string)}, nil
	})

	answer, err := resolveEntities(t, f, []interface{}{
		map[string]interface{}{"__typename": "Product", "id": "p-1"},
		map[string]interface{}{"__typename": "Product", "id": "p-2"},
		map[string]interface{}{"__typename": "Product", "id": "p-3"},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	entities, _ := answer.([]interface{})
	if len(entities) != 3 {
		t.Fatalf("%d answers for three representations", len(entities))
	}
	for i, want := range []string{"p-1", "p-2", "p-3"} {
		row, _ := entities[i].(map[string]interface{})
		if row["id"] != want {
			t.Errorf("answer %d is for %v, want %s", i, row["id"], want)
		}
	}
}

func TestATypeThisServiceDoesNotOwnComesBackEmpty(t *testing.T) {
	// The gateway asks each subgraph only for what it owns, but a graph being
	// recomposed can ask for something this one has not registered. A null in
	// that slot is the answer; a panic or a shifted list is not.
	f := federationWith(t, func(context.Context, map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "p-1"}, nil
	})

	answer, err := resolveEntities(t, f, []interface{}{
		map[string]interface{}{"__typename": "Order", "id": "o-1"},
		map[string]interface{}{"__typename": "Product", "id": "p-1"},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	entities, _ := answer.([]interface{})
	if len(entities) != 2 {
		t.Fatalf("%d answers for two representations", len(entities))
	}
	if entities[0] != nil {
		t.Errorf("a type this service does not own answered %v", entities[0])
	}
	if entities[1] == nil {
		t.Error("the one it does own was dropped with it")
	}
}

func TestARepresentationThatIsNotOneIsSkipped(t *testing.T) {
	f := federationWith(t, func(context.Context, map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": "p-1"}, nil
	})

	answer, err := resolveEntities(t, f, []interface{}{
		"not a representation",
		map[string]interface{}{"id": "p-1"}, // no __typename
		map[string]interface{}{"__typename": "Product", "id": "p-1"},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	entities, _ := answer.([]interface{})
	if len(entities) != 3 {
		t.Fatalf("%d answers for three representations", len(entities))
	}
	if entities[0] != nil || entities[1] != nil {
		t.Errorf("something that is not a representation was resolved: %v", entities[:2])
	}
	if entities[2] == nil {
		t.Error("the well-formed representation was dropped")
	}
}

func TestAResolverThatFailsSaysSo(t *testing.T) {
	// The database behind an entity being down is not the same as the entity
	// not existing, and a gateway showing null for both hides an outage.
	f := federationWith(t, func(context.Context, map[string]interface{}) (interface{}, error) {
		return nil, errors.New("the catalogue is unreachable")
	})

	_, err := resolveEntities(t, f, []interface{}{
		map[string]interface{}{"__typename": "Product", "id": "p-1"},
	})
	if err == nil {
		t.Fatal("a failing resolver answered as though the entity did not exist")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %q, want what went wrong underneath", err)
	}
}

func TestAskingForNoEntitiesIsNotAFailure(t *testing.T) {
	f := federationWith(t, func(context.Context, map[string]interface{}) (interface{}, error) {
		return nil, errors.New("should not be called")
	})

	answer, err := resolveEntities(t, f, []interface{}{})
	if err != nil {
		t.Fatalf("resolving nothing: %v", err)
	}
	if entities, _ := answer.([]interface{}); len(entities) != 0 {
		t.Errorf("answer = %v, want nothing", answer)
	}
}

func TestArgumentsThatAreNotRepresentationsAreRefused(t *testing.T) {
	f := federationWith(t, func(context.Context, map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	field := f.CreateEntitiesField()

	_, err := field.Resolve(graphql.ResolveParams{
		Context: context.Background(),
		Args:    map[string]interface{}{"representations": "not a list"},
	})
	if err == nil {
		t.Error("something that is not a list of representations was accepted")
	}
}
