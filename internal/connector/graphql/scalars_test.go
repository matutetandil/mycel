package graphql

import (
	"testing"
	"time"

	"github.com/graphql-go/graphql/language/ast"
)

// The scalars a schema uses for the things GraphQL has no type for. Each one is
// a gate: a value that fails it is refused at the edge, before a flow ever sees
// it — and a scalar that accepts anything is a field whose type says nothing.

func TestAnAddressThatIsNotOneIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		value    interface{}
		accepted bool
	}{
		"an ordinary address":  {"ada@example.com", true},
		"one with a subdomain": {"ada@mail.example.co.nz", true},
		"no at sign":           {"ada.example.com", false},
		"nothing after the at": {"ada@", false},
		"nothing before it":    {"@example.com", false},
		"a sentence":           {"who knows", false},
		"not a string at all":  {42, false},
		"empty":                {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := EmailScalar.ParseValue(tc.value)
			if (got != nil) != tc.accepted {
				t.Errorf("parsed = %v, want accepted = %v", got, tc.accepted)
			}
		})
	}
}

func TestSomethingThatIsNotAURLIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		value    interface{}
		accepted bool
	}{
		"https":               {"https://example.com/orders", true},
		"http":                {"http://example.com", true},
		"no scheme":           {"example.com", false},
		"a sentence":          {"see the website", false},
		"not a string at all": {42, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := URLScalar.ParseValue(tc.value)
			if (got != nil) != tc.accepted {
				t.Errorf("parsed = %v, want accepted = %v", got, tc.accepted)
			}
		})
	}
}

func TestSomethingThatIsNotAUUIDIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		value    interface{}
		accepted bool
	}{
		"a uuid":              {"3f2504e0-4f89-41d3-9a0c-0305e82c3301", true},
		"in capitals":         {"3F2504E0-4F89-41D3-9A0C-0305E82C3301", true},
		"missing a group":     {"3f2504e0-4f89-41d3-9a0c", false},
		"the right length":    {"3f2504e04f8941d39a0c0305e82c3301", false},
		"not a string at all": {42, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := UUIDScalar.ParseValue(tc.value)
			if (got != nil) != tc.accepted {
				t.Errorf("parsed = %v, want accepted = %v", got, tc.accepted)
			}
		})
	}
}

func TestATimestampGoesOutInAFormAClientCanRead(t *testing.T) {
	// RFC 3339, which is what every client library parses. A time serialised
	// as Go's default layout is one a browser reads as a string.
	moment := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)

	serialised, ok := DateTimeScalar.Serialize(moment).(string)
	if !ok {
		t.Fatalf("serialised = %#v, want a string", DateTimeScalar.Serialize(moment))
	}
	if _, err := time.Parse(time.RFC3339, serialised); err != nil {
		t.Errorf("serialised = %q, which no client can parse: %v", serialised, err)
	}

	// And back in.
	parsed := DateTimeScalar.ParseValue("2026-08-15T09:30:00Z")
	if parsed == nil {
		t.Error("a timestamp in the form the schema publishes was refused")
	}
	if DateTimeScalar.ParseValue("the fifteenth of August") != nil {
		t.Error("something that is not a timestamp was accepted")
	}
}

func TestACountCannotBeNegative(t *testing.T) {
	// The point of the scalar: a quantity of -1 is refused at the edge rather
	// than by a database constraint three calls later.
	if PositiveIntScalar.ParseValue(3) == nil {
		t.Error("a positive count was refused")
	}
	if PositiveIntScalar.ParseValue(0) != nil {
		t.Error("zero was accepted as a positive count")
	}
	if PositiveIntScalar.ParseValue(-1) != nil {
		t.Error("a negative count was accepted")
	}

	if NonNegativeIntScalar.ParseValue(0) == nil {
		t.Error("zero was refused as a non-negative count")
	}
	if NonNegativeIntScalar.ParseValue(-1) != nil {
		t.Error("a negative count was accepted")
	}
}

func TestAnythingShapedCanTravelAsJSON(t *testing.T) {
	// The escape hatch for a payload whose shape the schema does not describe.
	value := map[string]interface{}{"sku": "W-1", "tags": []interface{}{"a", "b"}}

	if JSONScalar.Serialize(value) == nil {
		t.Error("a map could not be serialised as JSON")
	}
	if JSONScalar.ParseValue(value) == nil {
		t.Error("a map was refused as JSON")
	}
}

func TestAValueWrittenIntoTheQueryItselfIsRead(t *testing.T) {
	// A literal, rather than a variable: { orders(filter: {status: "paid"}) }.
	// A literal nobody parses is an argument the resolver never sees.
	for name, tc := range map[string]struct {
		literal ast.Value
		want    interface{}
	}{
		"a string":  {&ast.StringValue{Value: "paid"}, "paid"},
		"a boolean": {&ast.BooleanValue{Value: true}, true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := parseLiteralValue(tc.literal); got != tc.want {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}

	// A list and an object, which is what a filter argument usually is.
	list := parseLiteralValue(&ast.ListValue{Values: []ast.Value{
		&ast.StringValue{Value: "paid"},
		&ast.StringValue{Value: "shipped"},
	}})
	if values, ok := list.([]interface{}); !ok || len(values) != 2 {
		t.Errorf("list = %#v, want both values", list)
	}

	object := parseLiteralValue(&ast.ObjectValue{Fields: []*ast.ObjectField{
		{Name: &ast.Name{Value: "status"}, Value: &ast.StringValue{Value: "paid"}},
	}})
	if fields, ok := object.(map[string]interface{}); !ok || fields["status"] != "paid" {
		t.Errorf("object = %#v", object)
	}
}

func TestTheScalarsAreReachableByName(t *testing.T) {
	// A schema names a scalar as a string, so this lookup is what turns a
	// declared format into the gate that enforces it.
	for _, name := range []string{"JSON", "DateTime", "Date", "Time", "Email", "URL", "UUID"} {
		if GetScalar(name) == nil {
			t.Errorf("the schema cannot use %s", name)
		}
	}
	if GetScalar("AScalarNobodyDefined") != nil {
		t.Error("a scalar nobody defined was found")
	}
}

func TestRoundTrippingThroughJSON(t *testing.T) {
	encoded := SerializeToJSON(map[string]interface{}{"sku": "W-1"})
	decoded, err := ParseFromJSON(encoded)
	if err != nil {
		t.Fatalf("ParseFromJSON: %v", err)
	}
	row, ok := decoded.(map[string]interface{})
	if !ok || row["sku"] != "W-1" {
		t.Errorf("decoded = %#v", decoded)
	}

	// Something that cannot be encoded answers with an empty object rather
	// than a broken document.
	if got := SerializeToJSON(func() {}); got != "{}" {
		t.Errorf("got %q, want an empty object", got)
	}
	if _, err := ParseFromJSON("not json"); err == nil {
		t.Error("something that is not JSON was parsed")
	}
}
