package graphql

import (
	"testing"
	"time"
)

// The custom scalars sit between every value and every response, and their
// failure is silent by construction: a serialiser that does not recognise a
// type returns nil, so a wrong branch does not error — the field simply comes
// back empty, on every request, for everyone.

func TestDateTimeSerialisesTheShapesAValueArrivesIn(t *testing.T) {
	moment := time.Date(2026, 8, 12, 15, 4, 5, 0, time.UTC)

	for name, value := range map[string]interface{}{
		"a time":           moment,
		"a pointer to one": &moment,
	} {
		t.Run(name, func(t *testing.T) {
			got := DateTimeScalar.Serialize(value)
			if got != "2026-08-12T15:04:05Z" {
				t.Errorf("serialised as %#v", got)
			}
		})
	}

	// A value that already went through a driver as text is passed along
	// rather than dropped.
	if got := DateTimeScalar.Serialize("2026-08-12T15:04:05Z"); got != "2026-08-12T15:04:05Z" {
		t.Errorf("a string was serialised as %#v", got)
	}

	// A nil pointer is absent, not the zero time — 0001-01-01 in a response
	// reads as data rather than as nothing.
	var missing *time.Time
	if got := DateTimeScalar.Serialize(missing); got != nil {
		t.Errorf("a nil time serialised as %#v", got)
	}
}

func TestDateTimeAcceptsWhatDatabasesActuallyReturn(t *testing.T) {
	// A driver hands back whichever of these its column type produces, and a
	// parser that only took RFC 3339 would reject most of them.
	for _, input := range []string{
		"2026-08-12T15:04:05Z",
		"2026-08-12T15:04:05+02:00",
		"2026-08-12T15:04:05",
		"2026-08-12 15:04:05",
		"2026-08-12",
	} {
		parsed := DateTimeScalar.ParseValue(input)
		if _, ok := parsed.(time.Time); !ok {
			t.Errorf("ParseValue(%q) = %#v, want a time", input, parsed)
		}
	}
}

func TestDateTimeRefusesWhatIsNotOne(t *testing.T) {
	for _, input := range []interface{}{"not a date", "", 42, true, nil} {
		if parsed := DateTimeScalar.ParseValue(input); parsed != nil {
			if _, ok := parsed.(time.Time); ok {
				t.Errorf("ParseValue(%#v) produced a time", input)
			}
		}
	}
}

func TestDateAndTimeKeepOnlyTheirHalf(t *testing.T) {
	moment := time.Date(2026, 8, 12, 15, 4, 5, 0, time.UTC)

	if got := DateScalar.Serialize(moment); got != "2026-08-12" {
		t.Errorf("Date serialised as %#v", got)
	}
	if got := TimeScalar.Serialize(moment); got != "15:04:05" {
		t.Errorf("Time serialised as %#v", got)
	}
}

func TestJSONCarriesWhateverItIsGiven(t *testing.T) {
	// The scalar every dynamic field uses, so it has to pass structures
	// through unchanged rather than stringify them.
	value := map[string]interface{}{"nested": []interface{}{1.0, "two"}}
	got, ok := JSONScalar.Serialize(value).(map[string]interface{})
	if !ok {
		t.Fatalf("serialised as %T", JSONScalar.Serialize(value))
	}
	if len(got["nested"].([]interface{})) != 2 {
		t.Errorf("got %#v", got)
	}
}

func TestTheBoundedIntegersRefuseWhatTheyName(t *testing.T) {
	if got := PositiveIntScalar.Serialize(5); got == nil {
		t.Error("a positive integer was refused by PositiveInt")
	}
	if got := PositiveIntScalar.ParseValue(0); got != nil {
		t.Error("zero was accepted as a positive integer")
	}
	if got := PositiveIntScalar.ParseValue(-3); got != nil {
		t.Error("a negative number was accepted as a positive integer")
	}

	if got := NonNegativeIntScalar.ParseValue(0); got == nil {
		t.Error("zero was refused by NonNegativeInt, which is the difference between the two")
	}
	if got := NonNegativeIntScalar.ParseValue(-1); got != nil {
		t.Error("a negative number was accepted as non-negative")
	}
}

func TestGetScalarByName(t *testing.T) {
	for _, name := range []string{"JSON", "DateTime", "Date", "Time"} {
		if GetScalar(name) == nil {
			t.Errorf("GetScalar(%q) returned nothing", name)
		}
	}
	if GetScalar("Widget") != nil {
		t.Error("an unknown scalar name returned something")
	}
}

func TestTheValidatorsBehindTheScalars(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
		fn    func(string) bool
		input string
	}{
		{"an address", true, isValidEmail, "person@example.com"},
		{"no at sign", false, isValidEmail, "person.example.com"},
		{"two at signs", false, isValidEmail, "a@b@example.com"},
		{"nothing before the at", false, isValidEmail, "@example.com"},
		{"no dot in the domain", false, isValidEmail, "person@example"},

		{"an https url", true, isValidURL, "https://example.com"},
		{"an http url", true, isValidURL, "http://example.com/path"},
		{"no scheme", false, isValidURL, "example.com/path"},
		{"a scheme nobody serves", false, isValidURL, "gopher://example.com"},

		{"a uuid", true, isValidUUID, "3d3a1d7f-38bb-4867-9c9d-5c2487d117be"},
		{"too short", false, isValidUUID, "3d3a1d7f-38bb"},
		{"not hexadecimal", false, isValidUUID, "zzzzzzzz-38bb-4867-9c9d-5c2487d117be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.input); got != tc.valid {
				t.Errorf("%q = %v, want %v", tc.input, got, tc.valid)
			}
		})
	}
}
