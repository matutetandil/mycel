package validate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The checks a type block asks for.
//
// A type is the contract at the edge of a service: what it will accept and
// what it refuses. Both directions cost — a check that is too loose lets bad
// data through to a database that refuses it much later, and one that is too
// tight rejects a request that was fine, which is the failure nobody
// reproduces because the data looked right.

var errWrongLength = errors.New("a barcode of that length is not a barcode")

func validateField(t *testing.T, field FieldSchema, value interface{}) *Result {
	t.Helper()
	return NewTypeValidator(NewConstraintRegistry()).Validate(
		context.Background(),
		map[string]interface{}{field.Name: value},
		&TypeSchema{Name: "test", Fields: []FieldSchema{field}},
	)
}

func TestANumberIsANumberWhateverWidthItArrivesIn(t *testing.T) {
	// The type check admits every width Go has, and the constraints knew
	// four. Everything else read as zero — so a quantity of 50 arriving as an
	// int32, which is what a Postgres int4 column produces, failed "at least
	// 1", and 5000 passed "at most 100" for the same reason.
	field := FieldSchema{
		Name: "quantity", Type: "number", Required: true,
		Constraints: []Constraint{&MinConstraint{Min: 1}, &MaxConstraint{Max: 100}},
	}

	for name, value := range map[string]interface{}{
		"a plain int":              50,
		"an int8":                  int8(50),
		"an int16":                 int16(50),
		"an int32, as pgx returns": int32(50),
		"an int64":                 int64(50),
		"an unsigned int":          uint(50),
		"a uint8":                  uint8(50),
		"a uint32":                 uint32(50),
		"a uint64":                 uint64(50),
		"a float32":                float32(50),
		"a float64, as JSON gives": float64(50),
	} {
		t.Run(name, func(t *testing.T) {
			if result := validateField(t, field, value); !result.Valid {
				t.Errorf("a quantity of 50 was refused: %v", result.Errors)
			}
		})
	}

	// A json.Number is read as a number too, for a decoder configured to
	// produce them — the type check does not admit one today, so this is
	// asserted on the constraint itself rather than through a schema.
	if n, ok := asNumber(json.Number("50")); !ok || n != 50 {
		t.Errorf("a JSON number read as %v, %v", n, ok)
	}

	// And out of range in every width, refused for the right reason.
	for name, value := range map[string]interface{}{
		"an int32":  int32(5000),
		"a uint64":  uint64(5000),
		"a float64": float64(5000),
	} {
		t.Run("too many, as "+name, func(t *testing.T) {
			result := validateField(t, field, value)
			if result.Valid {
				t.Fatal("a quantity of 5000 was accepted against a limit of 100")
			}
			if !strings.Contains(result.Errors[0].Error(), "at most") {
				t.Errorf("refused for the wrong reason: %v", result.Errors[0])
			}
		})
	}
}

func TestSomethingThatIsNotANumberIsTheTypeChecksBusiness(t *testing.T) {
	// Compared as zero, a word would pass `max` and fail `min` — two answers
	// about a value that is not a number at all. The type check says so
	// once, plainly.
	result := validateField(t,
		FieldSchema{Name: "quantity", Type: "number", Required: true,
			Constraints: []Constraint{&MinConstraint{Min: 1}, &MaxConstraint{Max: 100}}},
		"fifty")

	if result.Valid {
		t.Fatal("a word was accepted as a quantity")
	}
	if len(result.Errors) != 1 {
		t.Errorf("errors = %v, want one about the type", result.Errors)
	}
	if !strings.Contains(result.Errors[0].Error(), "number") {
		t.Errorf("the error does not say what was expected: %v", result.Errors[0])
	}
}

func TestTheFormatsAFieldCanBeHeldTo(t *testing.T) {
	for name, tc := range map[string]struct {
		format string
		value  string
		valid  bool
	}{
		"an email address":            {"email", "someone@example.test", true},
		"something with no domain":    {"email", "someone@example", false},
		"something with no recipient": {"email", "example.test", false},

		"an address":            {"url", "https://example.test/orders", true},
		"one over plain http":   {"url", "http://example.test", true},
		"something that is not": {"url", "example.test", false},

		// Any thirty-six characters used to pass, so a truncated identifier
		// or a line of text went through as an identifier.
		"an identifier":               {"uuid", "3f2504e0-4f89-11d3-9a0c-0305e82c3301", true},
		"one in capitals":             {"uuid", "3F2504E0-4F89-11D3-9A0C-0305E82C3301", true},
		"thirty-six other characters": {"uuid", "not-a-uuid-but-exactly-36-chars-long", false},
		"one cut short":               {"uuid", "3f2504e0-4f89-11d3-9a0c", false},

		// Counting dashes accepted a date no calendar has, which reaches a
		// database as a date it refuses — much further from the field that
		// was wrong.
		"a date":                  {"date", "2026-08-15", true},
		"the thirty-second":       {"date", "2026-08-32", false},
		"the ninety-ninth":        {"date", "9999-99-99", false},
		"none at all":             {"date", "0000-00-00", false},
		"one written another way": {"date", "15/08/2026", false},

		"a timestamp":            {"datetime", "2026-08-15T09:30:00Z", true},
		"one with an offset":     {"datetime", "2026-08-15T09:30:00+12:00", true},
		"one with no time":       {"datetime", "2026-08-15", false},
		"one that is not a time": {"datetime", "yesterdayTnow", false},
	} {
		t.Run(name, func(t *testing.T) {
			err := (&FormatConstraint{Format: tc.format}).Validate(tc.value)
			if tc.valid && err != nil {
				t.Errorf("%q was refused as a %s: %v", tc.value, tc.format, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("%q was accepted as a %s", tc.value, tc.format)
			}
		})
	}

	// A format nobody implements does not refuse everything: a type naming
	// one would otherwise reject every request rather than being ignored.
	if err := (&FormatConstraint{Format: "iban"}).Validate("NZ12 3456"); err != nil {
		t.Errorf("an unimplemented format refused a value: %v", err)
	}
	// And a value that is not text is the type check's business.
	if err := (&FormatConstraint{Format: "email"}).Validate(42); err != nil {
		t.Errorf("a number was judged as an email address: %v", err)
	}
}

func TestHowLongAValueMayBe(t *testing.T) {
	for name, tc := range map[string]struct {
		constraint Constraint
		value      interface{}
		valid      bool
	}{
		"long enough":     {&MinLengthConstraint{MinLength: 3}, "abc", true},
		"too short":       {&MinLengthConstraint{MinLength: 3}, "ab", false},
		"short enough":    {&MaxLengthConstraint{MaxLength: 5}, "abcde", true},
		"too long":        {&MaxLengthConstraint{MaxLength: 5}, "abcdef", false},
		"not text at all": {&MinLengthConstraint{MinLength: 3}, 42, true},
		"nor for the max": {&MaxLengthConstraint{MaxLength: 1}, 42, true},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.constraint.Validate(tc.value)
			if tc.valid != (err == nil) {
				t.Errorf("%v against %s = %v", tc.value, tc.constraint.Name(), err)
			}
		})
	}
}

func TestAValueThatMustMatchAPattern(t *testing.T) {
	constraint := &PatternConstraint{Pattern: `^[A-Z]{2}-[0-9]{4}$`}

	if err := constraint.Validate("NZ-1234"); err != nil {
		t.Errorf("a matching value was refused: %v", err)
	}
	// Compiled once and reused: the second call must behave like the first.
	if err := constraint.Validate("NZ-1234"); err != nil {
		t.Errorf("the second call refused what the first accepted: %v", err)
	}
	err := constraint.Validate("nz-1234")
	if err == nil {
		t.Error("a value that does not match was accepted")
	}
	// The message carries the pattern, because the field name alone does not
	// tell somebody what shape was wanted.
	if !strings.Contains(err.Error(), "[A-Z]") {
		t.Errorf("the error does not say what was expected: %v", err)
	}

	// A pattern that is not a pattern fails loudly rather than matching
	// everything.
	broken := &PatternConstraint{Pattern: `([A-Z]`}
	if err := broken.Validate("anything"); err == nil {
		t.Error("a pattern that does not compile accepted everything")
	}

	if err := constraint.Validate(42); err != nil {
		t.Errorf("a number was judged against a string pattern: %v", err)
	}
}

func TestAValueFromAFixedSet(t *testing.T) {
	constraint := &EnumConstraint{Values: []string{"pending", "paid", "shipped"}}

	if err := constraint.Validate("paid"); err != nil {
		t.Errorf("an allowed value was refused: %v", err)
	}
	err := constraint.Validate("PAID")
	if err == nil {
		t.Error("a value in the wrong case was accepted")
	}
	// The message lists what is allowed: a status rejected without the list
	// sends somebody to the configuration file.
	if !strings.Contains(err.Error(), "pending") {
		t.Errorf("the error does not say what is allowed: %v", err)
	}
	if err := constraint.Validate(42); err != nil {
		t.Errorf("a number was judged against a set of words: %v", err)
	}
}

func TestAFieldCheckedByAValidatorSomebodyWrote(t *testing.T) {
	// A named validator — a regex, a CEL expression or a WASM module — used
	// inside a type.
	called := false
	constraint := &CustomValidatorConstraint{
		ValidatorName: "nz_ird",
		ValidateFn: func(value interface{}) error {
			called = true
			return nil
		},
	}

	if err := constraint.Validate("123-456-789"); err != nil {
		t.Errorf("the validator refused: %v", err)
	}
	if !called {
		t.Error("the validator was never called")
	}
	// Its name appears in the error, so a failure says which rule failed.
	if !strings.Contains(constraint.Name(), "nz_ird") {
		t.Errorf("name = %q", constraint.Name())
	}

	// One that was named and never registered has to fail rather than pass:
	// a rule nobody can find is not a rule that holds.
	missing := &CustomValidatorConstraint{ValidatorName: "nz_ird"}
	err := missing.Validate("anything")
	if err == nil {
		t.Fatal("a validator that does not exist accepted the value")
	}
	if !strings.Contains(err.Error(), "nz_ird") {
		t.Errorf("the error does not name it: %v", err)
	}
}

func TestEveryConstraintSaysWhatItIs(t *testing.T) {
	// The name is what appears against a failure, so a caller can tell a
	// length problem from a format one without reading the message.
	for _, tc := range []struct {
		constraint Constraint
		want       string
	}{
		{&FormatConstraint{}, "format"},
		{&MinConstraint{}, "min"},
		{&MaxConstraint{}, "max"},
		{&MinLengthConstraint{}, "min_length"},
		{&MaxLengthConstraint{}, "max_length"},
		{&PatternConstraint{}, "pattern"},
		{&EnumConstraint{}, "enum"},
	} {
		if got := tc.constraint.Name(); got != tc.want {
			t.Errorf("name = %q, want %q", got, tc.want)
		}
	}
}

func TestConstraintsBroughtByAPlugin(t *testing.T) {
	// A plugin can add a kind of check the runtime does not ship — an IRD
	// number, a GTIN — and the registry is what a type block's constraint
	// name is resolved through.
	registry := NewConstraintRegistry()

	if _, err := registry.Create("gtin", nil); err == nil {
		t.Error("a constraint nobody registered was created")
	}

	registry.Register(gtinFactory{})

	built, err := registry.Create("gtin", map[string]interface{}{"length": 13})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(built.Name(), "gtin") {
		t.Errorf("name = %q", built.Name())
	}
	if err := built.Validate("9421023610000"); err != nil {
		t.Errorf("a valid barcode was refused: %v", err)
	}
	if err := built.Validate("123"); err == nil {
		t.Error("a barcode of the wrong length was accepted")
	}

	// A name the registered factory does not answer for is still refused,
	// rather than handed to whichever factory is first.
	if _, err := registry.Create("iban", nil); err == nil {
		t.Error("a constraint nobody implements was created")
	}
}

// gtinFactory is a stand-in for what a plugin registers.
type gtinFactory struct{}

func (gtinFactory) Supports(constraintType string) bool { return constraintType == "gtin" }
func (gtinFactory) Create(constraintType string, params map[string]interface{}) (Constraint, error) {
	length := 13
	if n, ok := params["length"].(int); ok {
		length = n
	}
	return &CustomValidatorConstraint{
		ValidatorName: "gtin",
		ValidateFn: func(value interface{}) error {
			s, ok := value.(string)
			if !ok {
				return nil
			}
			if len(s) != length {
				return errWrongLength
			}
			return nil
		},
	}, nil
}
