package graphql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

// A schema-first service writes its schema in SDL and Mycel turns it into
// something that can answer queries. Interfaces, unions and input objects are
// ordinary GraphQL, and none of them had ever been converted in a test — so
// the way to check the conversion is to use it: build the schema and run a
// query through it.

const shopSDL = `
enum Status {
  DRAFT
  PUBLISHED
}

interface Node {
  id: ID!
}

type Product implements Node {
  id: ID!
  name: String!
  status: Status!
  price: Float
  tags: [String!]
}

type Service implements Node {
  id: ID!
  name: String!
  hours: Int
}

union SearchResult = Product | Service

input PriceRange {
  min: Float
  max: Float = 1000.0
}

input ProductFilter {
  name: String
  status: Status
  price: PriceRange
  tags: [String!]
}

type Query {
  product(id: ID!): Product
  products(filter: ProductFilter): [Product!]
  search(term: String!): [SearchResult!]
  node(id: ID!): Node
}
`

func shopSchema(t *testing.T) (*SchemaBuilder, *SDLConverter) {
	t.Helper()
	b := NewSchemaBuilder()
	if err := b.ParseSDL(shopSDL); err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	if b.sdlConverter == nil {
		t.Fatal("nothing was converted")
	}
	return b, b.sdlConverter
}

func TestEveryKindOfTypeInTheSchemaIsConverted(t *testing.T) {
	_, converter := shopSchema(t)

	if converter.GetType("Product") == nil {
		t.Error("an object type was not converted")
	}
	if converter.GetEnum("Status") == nil {
		t.Error("an enum was not converted")
	}
	if converter.GetInterface("Node") == nil {
		t.Error("an interface was not converted")
	}
	if converter.GetUnion("SearchResult") == nil {
		t.Error("a union was not converted")
	}
	if converter.GetInput("ProductFilter") == nil {
		t.Error("an input type was not converted")
	}
}

func TestATypeSaysWhichInterfaceItImplements(t *testing.T) {
	// A client asking for a Node and selecting fields on Product depends on
	// this; without it the query is rejected as a fragment on an unrelated
	// type.
	_, converter := shopSchema(t)

	product := converter.GetType("Product")
	if product == nil {
		t.Fatal("Product was not converted")
	}
	interfaces := product.Interfaces()
	if len(interfaces) != 1 || interfaces[0].Name() != "Node" {
		t.Errorf("Product implements %v, want Node", interfaces)
	}
}

func TestAUnionCarriesEveryTypeItIsMadeOf(t *testing.T) {
	_, converter := shopSchema(t)

	union := converter.GetUnion("SearchResult")
	names := make([]string, 0, 2)
	for _, member := range union.Types() {
		names = append(names, member.Name())
	}
	if len(names) != 2 {
		t.Fatalf("the union is made of %v", names)
	}
	if !strings.Contains(strings.Join(names, ","), "Product") ||
		!strings.Contains(strings.Join(names, ","), "Service") {
		t.Errorf("the union is made of %v, want Product and Service", names)
	}
}

func TestAnInputTypeKeepsItsFieldsAndTheirDefaults(t *testing.T) {
	// A default declared in the schema is what a client gets when it leaves
	// the field out, so losing it changes the answer rather than the shape.
	_, converter := shopSchema(t)

	priceRange := converter.GetInput("PriceRange")
	fields := priceRange.Fields()
	if fields["min"] == nil || fields["max"] == nil {
		t.Fatalf("the input has fields %v", fields)
	}
	if fields["max"].DefaultValue == nil {
		t.Error("a default declared in the schema was dropped")
	}
}

func TestAnInputCanHoldAnotherInputAndAList(t *testing.T) {
	// Filters nest, and a filter whose nested input came back as a plain
	// string would be rejected the first time somebody used it.
	_, converter := shopSchema(t)

	filter := converter.GetInput("ProductFilter").Fields()
	nested, ok := filter["price"].Type.(*graphql.InputObject)
	if !ok {
		t.Errorf("the nested input came back as %T", filter["price"].Type)
	} else if nested.Name() != "PriceRange" {
		t.Errorf("the nested input is %q", nested.Name())
	}

	list, ok := filter["tags"].Type.(*graphql.List)
	if !ok {
		t.Fatalf("a list input came back as %T", filter["tags"].Type)
	}
	if _, ok := list.OfType.(*graphql.NonNull); !ok {
		t.Errorf("[String!] lost the ! on its items: %T", list.OfType)
	}
}

func TestAnEnumInAnInputIsStillTheEnum(t *testing.T) {
	_, converter := shopSchema(t)

	filter := converter.GetInput("ProductFilter").Fields()
	if _, ok := filter["status"].Type.(*graphql.Enum); !ok {
		t.Errorf("an enum used as an input field came back as %T", filter["status"].Type)
	}
}

func TestWhatIsRequiredStaysRequired(t *testing.T) {
	// The ! is the contract: a field that loses it starts accepting null, and
	// the failure turns up in whatever reads the answer.
	_, converter := shopSchema(t)

	fields := converter.GetType("Product").Fields()

	if _, ok := fields["id"].Type.(*graphql.NonNull); !ok {
		t.Errorf("ID! came back as %T", fields["id"].Type)
	}
	if _, ok := fields["price"].Type.(*graphql.NonNull); ok {
		t.Error("Float became required although the schema did not say so")
	}
}

func TestTheConvertedSchemaAnswersAQuery(t *testing.T) {
	// The conversion is only worth anything if what comes out can be used, so
	// this runs a real query — with an input object, a nested input and an
	// enum — through the schema that was built.
	builder, _ := shopSchema(t)

	// A flow is what answers a field, and this is the door the runtime uses.
	err := builder.RegisterHandler("Query.products",
		func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			filter, _ := input["filter"].(map[string]interface{})
			if filter == nil {
				return nil, nil
			}
			price, _ := filter["price"].(map[string]interface{})
			return []interface{}{
				map[string]interface{}{
					"id": "p-1", "name": filter["name"], "status": "PUBLISHED",
					"price": price["max"], "tags": []interface{}{"new"},
				},
			}, nil
		})
	if err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}

	schema, buildErr := builder.Build()
	if buildErr != nil {
		t.Fatalf("Build: %v", buildErr)
	}

	result := graphql.Do(graphql.Params{
		Schema: *schema,
		RequestString: `{
			products(filter: {name: "lamp", status: PUBLISHED, price: {min: 10.0}}) {
				id name status price tags
			}
		}`,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("the query was refused: %v", result.Errors)
	}

	encoded, _ := json.Marshal(result.Data)
	answer := string(encoded)
	if !strings.Contains(answer, `"name":"lamp"`) {
		t.Errorf("answer = %s, want the argument to have reached the resolver", answer)
	}
	// The default declared on the nested input, which the client did not send.
	if !strings.Contains(answer, `"price":1000`) {
		t.Errorf("answer = %s, want the default from the schema", answer)
	}
}

func TestAScalarNobodyDefinedIsStillUsable(t *testing.T) {
	// A schema naming a scalar Mycel does not know — DateTime is built in, but
	// someone writes scalar Money — must still build, or the service will not
	// start over a type it does not need to understand.
	b := NewSchemaBuilder()
	err := b.ParseSDL(`
scalar Money

type Query {
  total: Money
}
`)
	if err != nil {
		t.Fatalf("ParseSDL: %v", err)
	}
	if b.sdlConverter.GetScalar("Money") == nil {
		t.Error("a scalar the schema declared was not made available")
	}
}
