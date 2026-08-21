package graphql

import (
	"strings"
	"testing"
	"time"

	"github.com/graphql-go/graphql/language/ast"
)

// The scalars a schema can declare, exercised in all three directions.
//
// A scalar has three jobs — serialise what a flow produced, parse what a
// client sent as a variable, and parse what a client wrote inline in the query
// — and about half of this file had never been asked to do any of them. They
// are the boundary where a Go value becomes something a client reads: a
// timestamp that serialises to nothing is a null field in an answer, and a
// PositiveInt that lets a zero through is a contract the schema no longer
// keeps.

func TestATimestampCrossesTheBoundaryBothWays(t *testing.T) {
	when := time.Date(2026, 8, 21, 10, 30, 0, 0, time.UTC)

	if got := DateTimeScalar.Serialize(when); got != "2026-08-21T10:30:00Z" {
		t.Errorf("a time serialised to %#v", got)
	}
	if got := DateTimeScalar.Serialize(&when); got != "2026-08-21T10:30:00Z" {
		t.Errorf("a time pointer serialised to %#v", got)
	}
	var absent *time.Time
	if got := DateTimeScalar.Serialize(absent); got != nil {
		t.Errorf("a nil time serialised to %#v, want nothing", got)
	}
	// A string is already what the wire wants.
	if got := DateTimeScalar.Serialize("2026-08-21T10:30:00Z"); got != "2026-08-21T10:30:00Z" {
		t.Errorf("a string serialised to %#v", got)
	}
	if got := DateTimeScalar.Serialize(42); got != nil {
		t.Errorf("something that is not a time serialised to %#v", got)
	}

	// Coming the other way, the forms a client actually sends.
	for _, sent := range []string{
		"2026-08-21T10:30:00Z",
		"2026-08-21T10:30:00",
		"2026-08-21 10:30:00",
		"2026-08-21",
	} {
		parsed, ok := DateTimeScalar.ParseValue(sent).(time.Time)
		if !ok {
			t.Errorf("%q parsed to nothing", sent)
			continue
		}
		if parsed.Year() != 2026 || parsed.Month() != time.August || parsed.Day() != 21 {
			t.Errorf("%q parsed to %v", sent, parsed)
		}
	}
	if got := DateTimeScalar.ParseValue("the day before yesterday"); got != nil {
		t.Errorf("nonsense parsed to %#v", got)
	}
	if got := DateTimeScalar.ParseValue(when); got != when {
		t.Errorf("a time parsed to %#v", got)
	}

	// And written inline in the query rather than sent as a variable.
	inline := DateTimeScalar.ParseLiteral(&ast.StringValue{Value: "2026-08-21T10:30:00Z"})
	if _, ok := inline.(time.Time); !ok {
		t.Errorf("an inline timestamp parsed to %#v", inline)
	}
	if got := DateTimeScalar.ParseLiteral(&ast.IntValue{Value: "42"}); got != nil {
		t.Errorf("an inline int parsed as a timestamp: %#v", got)
	}
	if got := DateTimeScalar.ParseLiteral(&ast.StringValue{Value: "nonsense"}); got != nil {
		t.Errorf("inline nonsense parsed to %#v", got)
	}
}

func TestADateAndATimeKeepTheirOwnShape(t *testing.T) {
	when := time.Date(2026, 8, 21, 10, 30, 0, 0, time.UTC)

	if got := DateScalar.Serialize(when); got != "2026-08-21" {
		t.Errorf("a date serialised to %#v", got)
	}
	if got := TimeScalar.Serialize(when); !strings.HasPrefix(got.(string), "10:30") {
		t.Errorf("a time-of-day serialised to %#v", got)
	}

	parsed, ok := DateScalar.ParseValue("2026-08-21").(time.Time)
	if !ok || parsed.Day() != 21 {
		t.Errorf("a date parsed to %#v", parsed)
	}
	// A Date takes a date and nothing else: a full timestamp is refused, which
	// is what makes the field mean what it says.
	if got := DateScalar.ParseValue("2026-08-21T10:30:00Z"); got != nil {
		t.Errorf("a full timestamp was accepted as a Date: %#v", got)
	}
	if got := DateScalar.ParseValue(42); got != nil {
		t.Errorf("a number parsed as a date: %#v", got)
	}
	if got := DateScalar.ParseLiteral(&ast.StringValue{Value: "2026-08-21"}); got == nil {
		t.Error("an inline date parsed to nothing")
	}
	if got := TimeScalar.ParseLiteral(&ast.StringValue{Value: "10:30:00"}); got == nil {
		t.Error("an inline time parsed to nothing")
	}
}

// A number that has to be positive, in every form a client can send it.
func TestANumberWithAContractKeepsIt(t *testing.T) {
	for _, value := range []interface{}{5, int64(5), float64(5)} {
		if got := PositiveIntScalar.Serialize(value); got != 5 {
			t.Errorf("%T(5) serialised to %#v", value, got)
		}
		if got := PositiveIntScalar.ParseValue(value); got != 5 {
			t.Errorf("%T(5) parsed to %#v", value, got)
		}
	}
	for _, value := range []interface{}{0, int64(0), float64(0), -1, "5"} {
		if got := PositiveIntScalar.Serialize(value); got != nil {
			t.Errorf("%#v serialised as a positive integer: %#v", value, got)
		}
		if got := PositiveIntScalar.ParseValue(value); got != nil {
			t.Errorf("%#v parsed as a positive integer: %#v", value, got)
		}
	}
	if got := PositiveIntScalar.ParseLiteral(&ast.IntValue{Value: "7"}); got != 7 {
		t.Errorf("an inline 7 parsed to %#v", got)
	}
	if got := PositiveIntScalar.ParseLiteral(&ast.IntValue{Value: "0"}); got != nil {
		t.Errorf("an inline 0 parsed as a positive integer: %#v", got)
	}

	// Zero is allowed here and below it is not.
	for _, value := range []interface{}{0, int64(0), float64(0)} {
		if got := NonNegativeIntScalar.Serialize(value); got != 0 {
			t.Errorf("%T(0) serialised to %#v", value, got)
		}
		if got := NonNegativeIntScalar.ParseValue(value); got != 0 {
			t.Errorf("%T(0) parsed to %#v", value, got)
		}
	}
	if got := NonNegativeIntScalar.Serialize(-1); got != nil {
		t.Errorf("-1 serialised as non-negative: %#v", got)
	}
	if got := NonNegativeIntScalar.ParseLiteral(&ast.IntValue{Value: "0"}); got != 0 {
		t.Errorf("an inline 0 parsed to %#v", got)
	}
	if got := NonNegativeIntScalar.ParseLiteral(&ast.IntValue{Value: "-2"}); got != nil {
		t.Errorf("an inline -2 parsed as non-negative: %#v", got)
	}
}

// The three scalars that check the shape of a string.
func TestTheScalarsThatCheckAString(t *testing.T) {
	for _, c := range []struct {
		scalar string
		good   []string
		bad    []string
	}{
		{
			scalar: "Email",
			good:   []string{"ada@example.com", "a.b+c@sub.example.co.uk"},
			bad:    []string{"", "ada", "ada@", "@example.com", "a@b@c.com", "ada@example"},
		},
		{
			scalar: "URL",
			good:   []string{"http://example.com", "https://example.com/path"},
			bad:    []string{"", "example.com", "ftp://x", "http://"},
		},
		{
			scalar: "UUID",
			good:   []string{"550e8400-e29b-41d4-a716-446655440000", "550E8400-E29B-41D4-A716-446655440000"},
			bad:    []string{"", "550e8400", "550e8400-e29b-41d4-a716-44665544000", "550e8400xe29b-41d4-a716-446655440000"},
		},
	} {
		t.Run(c.scalar, func(t *testing.T) {
			scalar := GetScalar(c.scalar)
			if scalar == nil {
				t.Fatalf("%s is not in the map, so a schema declaring it has nothing to use", c.scalar)
			}
			for _, value := range c.good {
				if got := scalar.ParseValue(value); got == nil {
					t.Errorf("%q was refused", value)
				}
				if got := scalar.Serialize(value); got == nil {
					t.Errorf("%q serialised to nothing", value)
				}
				if got := scalar.ParseLiteral(&ast.StringValue{Value: value}); got == nil {
					t.Errorf("%q written inline was refused", value)
				}
			}
			for _, value := range c.bad {
				if got := scalar.ParseValue(value); got != nil {
					t.Errorf("%q was accepted as %s: %#v", value, c.scalar, got)
				}
				if got := scalar.ParseLiteral(&ast.StringValue{Value: value}); got != nil {
					t.Errorf("%q written inline was accepted as %s", value, c.scalar)
				}
			}
		})
	}
}

// JSON carries whatever it was given, in both directions.
func TestJSONCarriesWhateverItHolds(t *testing.T) {
	value := map[string]interface{}{"a": 1, "nested": map[string]interface{}{"b": true}}

	if got := JSONScalar.Serialize(value); got == nil {
		t.Error("a map serialised to nothing")
	}
	if got := JSONScalar.ParseValue(value); got == nil {
		t.Error("a map parsed to nothing")
	}

	// And the helpers beside it.
	text := SerializeToJSON(value)
	if !strings.Contains(text, `"a"`) {
		t.Errorf("serialised to %q", text)
	}
	back, err := ParseFromJSON(text)
	if err != nil {
		t.Fatalf("parsing back: %v", err)
	}
	row, ok := back.(map[string]interface{})
	if !ok || row["a"] == nil {
		t.Errorf("parsed back to %#v", back)
	}
	if _, err := ParseFromJSON("{not json"); err == nil {
		t.Error("nonsense parsed as JSON")
	}
	// Something that cannot be JSON comes back as an empty object rather than
	// as a panic — the answer still has to be sent.
	if got := SerializeToJSON(func() {}); got != "{}" && got != "" && got != "null" {
		t.Errorf("something unserialisable came back as %q", got)
	}
}

// Every scalar the map offers is one a schema can use, and a name nobody has
// comes back as nothing rather than as a nil that is used later.
func TestEveryScalarInTheMapIsUsable(t *testing.T) {
	if len(ScalarMap) == 0 {
		t.Fatal("no scalars are registered")
	}
	for name, scalar := range ScalarMap {
		if scalar == nil {
			t.Errorf("%s maps to nothing", name)
			continue
		}
		if GetScalar(name) != scalar {
			t.Errorf("GetScalar(%q) does not hand back the same scalar", name)
		}
	}
	if GetScalar("Sharpness") != nil {
		t.Error("a scalar nobody declared came back as something")
	}
}

// An inline literal of each kind, which is the path a query that writes its
// arguments out rather than sending variables takes.
func TestALiteralOfEachKind(t *testing.T) {
	if got := parseLiteralValue(&ast.StringValue{Value: "x"}); got != "x" {
		t.Errorf("string = %#v", got)
	}
	if got := parseLiteralValue(&ast.BooleanValue{Value: true}); got != true {
		t.Errorf("boolean = %#v", got)
	}
	if got := parseLiteralValue(&ast.IntValue{Value: "3"}); got == nil {
		t.Errorf("int = %#v", got)
	}
	if got := parseLiteralValue(&ast.FloatValue{Value: "3.5"}); got == nil {
		t.Errorf("float = %#v", got)
	}
	list := parseLiteralValue(&ast.ListValue{Values: []ast.Value{
		&ast.StringValue{Value: "a"},
		&ast.StringValue{Value: "b"},
	}})
	if items, ok := list.([]interface{}); !ok || len(items) != 2 {
		t.Errorf("list = %#v", list)
	}
	object := parseLiteralValue(&ast.ObjectValue{Fields: []*ast.ObjectField{
		{Name: &ast.Name{Value: "a"}, Value: &ast.StringValue{Value: "1"}},
	}})
	if fields, ok := object.(map[string]interface{}); !ok || fields["a"] != "1" {
		t.Errorf("object = %#v", object)
	}
}
