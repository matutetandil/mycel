package graphql

import (
	"strings"
	"testing"
)

// Reading a schema file somebody wrote.
//
// A `schema_file` is how a service exposes a GraphQL API it did not generate:
// the types come from the file and the flows fill them in. What the parser
// understands is therefore the contract with whoever wrote it — and a
// construct it silently drops is a field that exists in the file, is missing
// from the running service, and produces "cannot query field" against a schema
// the author is looking at.

const shopCatalogueSDL = `
"A customer of the shop."
type Customer implements Node & Timestamped {
  id: ID!
  email: String!
  orders(first: Int = 10, status: OrderStatus): [Order!]!
  tags: [String]
}

type Order {
  id: ID!
  total: Float!
  status: OrderStatus!
  customer: Customer
}

interface Node {
  id: ID!
}

interface Timestamped {
  createdAt: String!
}

enum OrderStatus {
  PENDING
  PAID
  SHIPPED
}

input OrderFilter {
  status: OrderStatus
  minimum: Float = 0.0
}

scalar DateTime

union SearchResult = Customer | Order

type Query {
  customer(id: ID!): Customer
  orders(filter: OrderFilter): [Order!]!
  search(term: String!): [SearchResult!]!
}

type Mutation {
  createOrder(input: OrderFilter!): Order!
}

type Subscription {
  orderPlaced: Order!
}
`

func parsedCatalogue(t *testing.T) *ParsedSchema {
	t.Helper()
	schema, err := ParseSDLComplete(shopCatalogueSDL)
	if err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	return schema
}

func TestEveryKindOfThingASchemaCanDeclare(t *testing.T) {
	schema := parsedCatalogue(t)

	// The three roots. A missing one is an operation the service cannot
	// answer at all.
	if schema.Query == nil || schema.Mutation == nil || schema.Subscription == nil {
		t.Fatalf("roots: query=%v mutation=%v subscription=%v",
			schema.Query != nil, schema.Mutation != nil, schema.Subscription != nil)
	}

	for _, name := range []string{"Customer", "Order"} {
		if schema.Types[name] == nil {
			t.Errorf("%s is in the file and not in the schema", name)
		}
	}
	// An input type is what a mutation takes, and it is kept apart from the
	// output types because the two cannot be used in each other's place.
	if schema.Inputs["OrderFilter"] == nil {
		t.Errorf("inputs = %v", schema.Inputs)
	}
	if len(schema.Enums["OrderStatus"].Values) != 3 {
		t.Errorf("the enum's values = %v", schema.Enums["OrderStatus"].Values)
	}
	// A scalar the author declared is theirs to serialise; dropped, every
	// field using it fails to build.
	var declaredScalar bool
	for _, name := range schema.Scalars {
		if name == "DateTime" {
			declaredScalar = true
		}
	}
	if !declaredScalar {
		t.Errorf("scalars = %v", schema.Scalars)
	}
	if union, ok := schema.Unions["SearchResult"]; !ok || len(union.Types) != 2 {
		t.Errorf("unions = %v", schema.Unions)
	}
	if len(schema.Interfaces) != 2 {
		t.Errorf("interfaces = %v", schema.Interfaces)
	}
}

func TestWhatAFieldCarries(t *testing.T) {
	schema := parsedCatalogue(t)

	// Reached by path, which is how a flow names what it answers:
	// `operation = "Query.customer"`.
	field := schema.GetFieldByPath("Query.customer")
	if field == nil {
		t.Fatal("Query.customer was not found by path")
	}
	if _, ok := field.Args["id"]; !ok || len(field.Args) != 1 {
		t.Errorf("arguments = %v", field.Args)
	}

	orders := schema.GetFieldByPath("Customer.orders")
	if orders == nil {
		t.Fatal("Customer.orders was not found")
	}
	// A list of non-null Order, itself non-null: the difference between
	// [Order!]! and [Order] decides whether a null in the list is an error.
	if !orders.Type.IsList || !orders.Type.ElementNonNull || !orders.Type.ListNonNull {
		t.Errorf("type = %+v", orders.Type)
	}
	if got := orders.Type.String(); got != "[Order!]!" {
		t.Errorf("rendered as %q", got)
	}
	// A default on an argument is what makes it optional to the caller.
	first, ok := orders.Args["first"]
	if !ok || first.DefaultValue == nil {
		t.Errorf("the argument's default was lost: %+v", orders.Args)
	}

	// A path to something that is not there, and a path that is not a path.
	for _, path := range []string{"Query.nothing", "Nothing.field", "notapath", ""} {
		if schema.GetFieldByPath(path) != nil {
			t.Errorf("%q resolved to a field", path)
		}
		if schema.HasField(path) {
			t.Errorf("%q was reported as present", path)
		}
	}
	if !schema.HasField("Mutation.createOrder") {
		t.Error("a field that is there was reported as missing")
	}
}

func TestWhatTheServiceCanBeAsked(t *testing.T) {
	schema := parsedCatalogue(t)

	queries := schema.GetAllQueryFields()
	if len(queries) != 3 {
		t.Errorf("query fields = %v", queries)
	}
	mutations := schema.GetAllMutationFields()
	if len(mutations) != 1 {
		t.Errorf("mutation fields = %v", mutations)
	}

	// A schema with no mutations at all answers with nothing rather than
	// panicking: a read-only API is an ordinary thing to write.
	readOnly, err := ParseSDLComplete(`type Query { ping: String }`)
	if err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	if got := readOnly.GetAllMutationFields(); got != nil {
		t.Errorf("a schema with no mutations answered %v", got)
	}
	if got := readOnly.GetAllQueryFields(); len(got) != 1 {
		t.Errorf("query fields = %v", got)
	}
}

func TestATypeDeclaredInTwoPlaces(t *testing.T) {
	// `extend type` is how federation and a schema split across files add to
	// a type. The fields have to end up on one type, or the ones in the
	// extension are missing from the running service.
	schema, err := ParseSDLComplete(`
type Customer {
  id: ID!
}

extend type Customer {
  email: String!
  loyaltyPoints: Int
}

type Query {
  customer(id: ID!): Customer
}
`)
	if err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}

	customer := schema.Types["Customer"]
	if customer == nil {
		t.Fatal("Customer is not in the schema")
	}
	for _, field := range []string{"id", "email", "loyaltyPoints"} {
		if _, ok := customer.Fields[field]; !ok {
			t.Errorf("%s was declared and is missing: %v", field, customer.Fields)
		}
	}
}

func TestWhatATypeImplements(t *testing.T) {
	// Interfaces are what a union or a `Node` lookup resolves through, and
	// `implements A & B` is the form the specification uses.
	customer := parsedCatalogue(t).Types["Customer"]
	if customer == nil {
		t.Fatal("Customer is not in the schema")
	}
	if len(customer.Implements) != 2 {
		t.Errorf("implements = %v, want both interfaces", customer.Implements)
	}
}

func TestADescriptionSurvives(t *testing.T) {
	// Descriptions are what a client's schema explorer shows; dropped, an API
	// that documents itself stops doing so.
	customer := parsedCatalogue(t).Types["Customer"]
	if customer.Description == "" {
		t.Errorf("the type's description was dropped")
	}
	if !strings.Contains(customer.Description, "customer of the shop") {
		t.Errorf("description = %q", customer.Description)
	}
}

func TestASchemaThatDoesNotParse(t *testing.T) {
	// Refused at start-up rather than at the first query: a service that
	// starts with half a schema answers "cannot query field" for the rest.
	for name, body := range map[string]string{
		"a brace that never closes": `type Query { ping: String`,
		"nothing at all":            ``,
	} {
		t.Run(name, func(t *testing.T) {
			schema, err := ParseSDLComplete(body)
			if err == nil && schema != nil && schema.Query != nil && len(schema.Query.Fields) > 0 {
				t.Errorf("a schema that should not parse produced %v", schema.Query.Fields)
			}
		})
	}
}
