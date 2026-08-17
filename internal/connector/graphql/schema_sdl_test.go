package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/validate"
)

// A federated service describes itself: the gateway asks it for its schema and
// composes that answer with every other subgraph's. The schema is assembled by
// hand here, as text, which means a missing brace or a type written twice is
// not caught by anything in this process — it is caught by the gateway, at
// composition time, as a failure of the whole graph rather than of this
// service. Parsing what we generate is the check that never happens otherwise.

func federatedBuilder(t *testing.T) *SchemaBuilder {
	t.Helper()
	b := NewSchemaBuilder()
	b.EnableFederation(2)
	return b
}

func serviceSDL(t *testing.T, b *SchemaBuilder) string {
	t.Helper()
	if _, err := b.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	sdl := b.federation.GetSDL()
	if sdl == "" {
		t.Fatal("the service describes itself with an empty schema")
	}
	return sdl
}

func TestTheSchemaAFederationGatewayReadsParses(t *testing.T) {
	b := federatedBuilder(t)
	handler := func(context.Context, map[string]interface{}) (interface{}, error) { return nil, nil }

	if err := b.LoadHCLTypes(map[string]*validate.TypeSchema{
		"customer": {
			Name: "customer",
			Fields: []validate.FieldSchema{
				{Name: "id", Type: "string", Required: true},
				{Name: "email", Type: "string"},
				{Name: "orders", Type: "array"},
			},
		},
	}); err != nil {
		t.Fatalf("LoadHCLTypes: %v", err)
	}

	for _, op := range []string{"Query.customer", "Query.customers", "Mutation.createCustomer"} {
		if err := b.RegisterHandlerWithArgs(op, handler, "customer", []*ArgDef{
			{Name: "id", Type: "id", Required: true},
			{Name: "limit", Type: "int"},
		}); err != nil {
			t.Fatalf("register %s: %v", op, err)
		}
	}
	b.RegisterEntity("Customer", []EntityKey{{Fields: "id", Resolvable: true}}, nil)

	sdl := serviceSDL(t, b)
	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("the schema this service publishes is not parseable, so a gateway would refuse it: %v\n%s", err, sdl)
	}

	// The two fields the federation protocol adds are the gateway's own; a
	// subgraph that advertised them as ordinary fields would have the gateway
	// try to compose them.
	for _, internal := range []string{"_service", "_entities"} {
		if strings.Contains(sdl, internal) {
			t.Errorf("the published schema advertises %s, which belongs to the protocol", internal)
		}
	}

	for _, want := range []string{"createCustomer", "@key(fields: \"id\")", "scalar JSON"} {
		if !strings.Contains(sdl, want) {
			t.Errorf("the published schema is missing %q:\n%s", want, sdl)
		}
	}
}

func TestAnEntityIsDescribedOnlyOnce(t *testing.T) {
	// The entity types are written first, with their key directive, and the
	// types from configuration are written after. A type that is both would
	// otherwise appear twice, and a schema that declares a type twice does not
	// compose.
	b := federatedBuilder(t)
	if err := b.LoadHCLTypes(map[string]*validate.TypeSchema{
		"Customer": {
			Name:   "Customer",
			Fields: []validate.FieldSchema{{Name: "id", Type: "string"}},
		},
	}); err != nil {
		t.Fatalf("LoadHCLTypes: %v", err)
	}
	b.RegisterEntity("Customer", []EntityKey{{Fields: "id", Resolvable: true}}, nil)

	sdl := serviceSDL(t, b)
	if n := strings.Count(sdl, "type Customer "); n != 1 {
		t.Errorf("Customer is declared %d times:\n%s", n, sdl)
	}
	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("the published schema does not parse: %v\n%s", err, sdl)
	}
}

func TestASchemaFileIsPublishedAsWritten(t *testing.T) {
	// Someone who brought their own schema file gets that file back, not a
	// reconstruction of it: the directives and descriptions they wrote are the
	// point of having written it.
	b := federatedBuilder(t)
	const sdl = `
type Customer @key(fields: "id") {
  "the customer's own identifier"
  id: ID!
  email: String
}

type Query {
  customer(id: ID!): Customer
}
`
	if err := b.ParseSDL(sdl); err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}

	published := serviceSDL(t, b)
	if !strings.Contains(published, "the customer's own identifier") {
		t.Error("the description written in the schema file was lost")
	}
	if _, err := ParseSDLComplete(published); err != nil {
		t.Fatalf("the published schema does not parse: %v\n%s", err, published)
	}
}

func TestASubscriptionAppearsInThePublishedSchema(t *testing.T) {
	b := federatedBuilder(t)
	handler := func(context.Context, map[string]interface{}) (interface{}, error) { return nil, nil }

	if err := b.RegisterHandlerWithArgs("Query.ping", handler, "", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := b.RegisterHandlerWithArgs("Subscription.orderPlaced", handler, "", nil); err != nil {
		t.Fatalf("register subscription: %v", err)
	}

	sdl := serviceSDL(t, b)
	if !strings.Contains(sdl, "type Subscription") || !strings.Contains(sdl, "orderPlaced") {
		t.Errorf("the subscription is not in the published schema:\n%s", sdl)
	}
	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("the published schema does not parse: %v\n%s", err, sdl)
	}
}

func TestAServiceWithNoFieldsStillPublishesAParseableSchema(t *testing.T) {
	// The empty case is the one a half-written configuration produces, and it
	// still has to answer the gateway with something it can read.
	b := federatedBuilder(t)
	sdl := serviceSDL(t, b)
	if _, err := ParseSDLComplete(sdl); err != nil {
		t.Fatalf("a service with no fields publishes an unparseable schema: %v\n%s", err, sdl)
	}
}
