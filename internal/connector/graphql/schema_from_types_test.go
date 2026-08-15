package graphql

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/validate"
)

// A type declared in HCL becomes the schema a client sees. Everything about it
// is a promise: a field that comes out String when the data is a number is a
// client that cannot read the answer, and an id typed as a string is one that
// cannot be looked up.

func typeSchema(name string, fields ...validate.FieldSchema) *validate.TypeSchema {
	return &validate.TypeSchema{Name: name, Fields: fields}
}

func field(name, fieldType string, required bool, constraints ...validate.Constraint) validate.FieldSchema {
	return validate.FieldSchema{
		Name: name, Type: fieldType, Required: required, Constraints: constraints,
	}
}

func converterWith(t *testing.T, schemas ...*validate.TypeSchema) *HCLConverter {
	t.Helper()
	c := NewHCLConverter()
	byName := make(map[string]*validate.TypeSchema, len(schemas))
	for _, s := range schemas {
		byName[s.Name] = s
	}
	c.LoadTypeSchemas(byName)
	if err := c.Convert(); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return c
}

func TestWhatAFieldBecomesInTheSchema(t *testing.T) {
	// The mapping a client depends on. A number is Float unless something says
	// it counts in whole units — a quantity typed as Float is one a client
	// renders as 3.0.
	c := converterWith(t, typeSchema("Order",
		field("id", "id", true),
		field("reference", "string", true),
		field("total", "number", true),
		field("quantity", "number", true, &validate.MinConstraint{Min: 1}),
		field("paid", "boolean", false),
		field("tags", "array", false),
		field("metadata", "object", false),
		field("email", "string", false, &validate.FormatConstraint{Format: "email"}),
		field("website", "string", false, &validate.FormatConstraint{Format: "url"}),
		field("placed_at", "string", false, &validate.FormatConstraint{Format: "datetime"}),
	))

	sdl := c.GenerateSDL()

	for _, want := range []string{
		"id: ID!",
		"reference: String!",
		"total: Float!",
		"quantity: Int!",
		"paid: Boolean",
		"email: Email",
		"website: URL",
		"placed_at: DateTime",
	} {
		if !strings.Contains(sdl, want) {
			t.Errorf("the schema does not carry %q:\n%s", want, sdl)
		}
	}
}

func TestARequiredFieldIsRequiredInTheSchema(t *testing.T) {
	// The exclamation mark is the contract: without it a client accepts null
	// where the service promised a value, and finds out at render time.
	c := converterWith(t, typeSchema("Order",
		field("id", "id", true),
		field("note", "string", false),
	))

	sdl := c.GenerateSDL()
	if !strings.Contains(sdl, "id: ID!") {
		t.Errorf("a required field is optional in the schema:\n%s", sdl)
	}
	if strings.Contains(sdl, "note: String!") {
		t.Errorf("an optional field is required in the schema:\n%s", sdl)
	}
}

func TestAFieldWithAFixedSetOfValuesBecomesAnEnum(t *testing.T) {
	// Which is what stops a client sending "shiped" and finding out from a
	// database constraint.
	c := converterWith(t, typeSchema("Order",
		field("id", "id", true),
		field("status", "string", true, &validate.EnumConstraint{
			Values: []string{"pending", "paid", "shipped"},
		}),
	))

	enums := c.AllEnums()
	if len(enums) != 1 {
		t.Fatalf("enums = %v, want the one declared", enums)
	}

	// GraphQL enum values are written in capitals by convention, and every
	// client generator assumes it — emitting them as declared would produce a
	// schema that reads as valid and that no generated client can use.
	sdl := c.GenerateSDL()
	for _, want := range []string{"enum StatusEnum", "PENDING", "PAID", "SHIPPED", "status: StatusEnum!"} {
		if !strings.Contains(sdl, want) {
			t.Errorf("the schema does not carry %q:\n%s", want, sdl)
		}
	}
}

func TestATypeCanBeLookedUpByName(t *testing.T) {
	// What a resolver is wired to, and what a federated service resolves an
	// entity into.
	c := converterWith(t, typeSchema("Order", field("id", "id", true)))

	if c.GetType("Order") == nil {
		t.Error("a type that was declared cannot be found")
	}
	if c.GetType("ATypeNobodyDeclared") != nil {
		t.Error("a type nobody declared was found")
	}
	if c.AllTypes()["Order"] == nil {
		t.Error("the type is not among all of them")
	}
}

func TestOneTypeCanReferToAnother(t *testing.T) {
	// An order has a customer, and the schema has to say which type that is
	// rather than falling back to JSON.
	c := converterWith(t,
		typeSchema("Customer", field("id", "id", true), field("name", "string", true)),
		typeSchema("Order", field("id", "id", true), field("customer", "Customer", true)),
	)

	sdl := c.GenerateSDL()
	if !strings.Contains(sdl, "customer: Customer!") {
		t.Errorf("a field referring to another type lost it:\n%s", sdl)
	}
}

func TestAFieldOfATypeNobodyDeclaredFallsBackToSomethingUsable(t *testing.T) {
	// Rather than emitting a schema naming a type that does not exist, which
	// no client can build against at all.
	c := converterWith(t, typeSchema("Order",
		field("id", "id", true),
		field("extras", "SomethingNobodyDeclared", false),
	))

	sdl := c.GenerateSDL()
	if strings.Contains(sdl, "SomethingNobodyDeclared") {
		t.Errorf("the schema names a type that does not exist:\n%s", sdl)
	}
}

func TestAnHTTPFailureSaysWhetherItIsWorthRetrying(t *testing.T) {
	// A 4xx cannot be fixed by sending it again, and retrying one is how a
	// consumer spends its retries on a request that will never succeed.
	for name, tc := range map[string]struct {
		status    int
		permanent bool
	}{
		"a request the server refused": {400, true},
		"credentials it rejected":      {401, true},
		"something not found":          {404, true},
		"the server having a problem":  {500, false},
		"a gateway that is down":       {502, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := &HTTPError{StatusCode: tc.status, Body: "the body"}
			if err.IsPermanent() != tc.permanent {
				t.Errorf("permanent = %v, want %v", err.IsPermanent(), tc.permanent)
			}
			if !strings.Contains(err.Error(), "the body") {
				t.Errorf("error = %q, want it to carry what the server said", err)
			}
		})
	}
}
