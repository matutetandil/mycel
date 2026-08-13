package parser

import (
	"context"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/validate"
)

// A type block whose constraints parse but never attach validates nothing, and
// says nothing about it — the failure mode is a service that accepts anything
// while its configuration claims otherwise. The constraint machinery had no
// tests at all, so these assert that each constraint arrives and that it is the
// constraint that was asked for.
//
// Note the syntax: `email = string({ format = "email" })`, with the object
// inside parentheses. `string { format = "email" }` — which the documentation
// showed in some eighty places until 2.14.0 — is not a call and does not parse.

func typeNamed(t *testing.T, cfg *Configuration, name string) *validate.TypeSchema {
	t.Helper()
	for _, ty := range cfg.Types {
		if ty.Name == name {
			return ty
		}
	}
	t.Fatalf("no type named %q (have %d)", name, len(cfg.Types))
	return nil
}

func fieldNamed(t *testing.T, schema *validate.TypeSchema, name string) *validate.FieldSchema {
	t.Helper()
	for i := range schema.Fields {
		if schema.Fields[i].Name == name {
			return &schema.Fields[i]
		}
	}
	t.Fatalf("type %q has no field %q", schema.Name, name)
	return nil
}

// constraintOf returns the first constraint of the requested concrete type.
func constraintOf[T validate.Constraint](t *testing.T, f *validate.FieldSchema) T {
	t.Helper()
	for _, c := range f.Constraints {
		if typed, ok := c.(T); ok {
			return typed
		}
	}
	var zero T
	t.Fatalf("field has no %T constraint (has %d constraints: %#v)", zero, len(f.Constraints), f.Constraints)
	return zero
}

func TestTypeConstraintsAttach(t *testing.T) {
	cfg := parseOne(t, `
type "user" {
  email      = string({ format = "email" })
  age        = number({ min = 0, max = 150 })
  username   = string({ min_length = 3, max_length = 32 })
  slug       = string({ pattern = "^[a-z-]+$" })
  status     = string({ enum = ["active", "pending", "banned"] })
  plain      = string
}
`)
	schema := typeNamed(t, cfg, "user")

	t.Run("format", func(t *testing.T) {
		c := constraintOf[*validate.FormatConstraint](t, fieldNamed(t, schema, "email"))
		if c.Format != "email" {
			t.Errorf("format = %q, want email", c.Format)
		}
	})

	t.Run("min and max both attach", func(t *testing.T) {
		f := fieldNamed(t, schema, "age")
		if got := constraintOf[*validate.MinConstraint](t, f).Min; got != 0 {
			t.Errorf("min = %v, want 0", got)
		}
		if got := constraintOf[*validate.MaxConstraint](t, f).Max; got != 150 {
			t.Errorf("max = %v, want 150", got)
		}
		// Two constraints in one object must both survive; keeping only the
		// last would silently drop the lower bound.
		if len(f.Constraints) != 2 {
			t.Errorf("age has %d constraints, want 2", len(f.Constraints))
		}
	})

	t.Run("length bounds", func(t *testing.T) {
		f := fieldNamed(t, schema, "username")
		if got := constraintOf[*validate.MinLengthConstraint](t, f).MinLength; got != 3 {
			t.Errorf("min_length = %v, want 3", got)
		}
		if got := constraintOf[*validate.MaxLengthConstraint](t, f).MaxLength; got != 32 {
			t.Errorf("max_length = %v, want 32", got)
		}
	})

	t.Run("pattern", func(t *testing.T) {
		c := constraintOf[*validate.PatternConstraint](t, fieldNamed(t, schema, "slug"))
		if c.Pattern != "^[a-z-]+$" {
			t.Errorf("pattern = %q", c.Pattern)
		}
	})

	t.Run("enum keeps every value in order", func(t *testing.T) {
		c := constraintOf[*validate.EnumConstraint](t, fieldNamed(t, schema, "status"))
		want := []string{"active", "pending", "banned"}
		if len(c.Values) != len(want) {
			t.Fatalf("enum has %d values, want %d: %#v", len(c.Values), len(want), c.Values)
		}
		for i := range want {
			if c.Values[i] != want[i] {
				t.Errorf("enum[%d] = %q, want %q", i, c.Values[i], want[i])
			}
		}
	})

	t.Run("a bare type carries no constraints", func(t *testing.T) {
		if f := fieldNamed(t, schema, "plain"); len(f.Constraints) != 0 {
			t.Errorf("a bare string picked up %d constraints", len(f.Constraints))
		}
	})
}

func TestConstraintsActuallyReject(t *testing.T) {
	// Parsing a constraint is only half of it — the validator has to enforce
	// it. This is the assertion that would have caught a constraint that
	// attaches to the wrong field or is built with a zero value.
	cfg := parseOne(t, `
type "signup" {
  email = string({ format = "email" })
  age   = number({ min = 18, max = 120 })
  role  = string({ enum = ["admin", "user"] })
}
`)
	schema := typeNamed(t, cfg, "signup")
	v := validate.NewTypeValidator(validate.NewConstraintRegistry())
	ctx := context.Background()

	good := map[string]interface{}{"email": "a@b.com", "age": 30, "role": "user"}
	if res := v.Validate(ctx, good, schema); !res.Valid {
		t.Fatalf("a valid payload was rejected: %#v", res.Errors)
	}

	for _, tc := range []struct {
		name    string
		payload map[string]interface{}
	}{
		{"malformed email", map[string]interface{}{"email": "not-an-email", "age": 30, "role": "user"}},
		{"age below the minimum", map[string]interface{}{"email": "a@b.com", "age": 12, "role": "user"}},
		{"age above the maximum", map[string]interface{}{"email": "a@b.com", "age": 999, "role": "user"}},
		{"a role outside the enum", map[string]interface{}{"email": "a@b.com", "age": 30, "role": "root"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if res := v.Validate(ctx, tc.payload, schema); res.Valid {
				t.Error("the payload was accepted")
			}
		})
	}
}

func TestFederationDirectivesAreNotTreatedAsConstraints(t *testing.T) {
	// external/shareable and friends share the object with real constraints.
	// Mistaking one for the other would either drop the directive or attach a
	// nil constraint.
	cfg := parseOne(t, `
type "product" {
  sku   = string({ external = true, shareable = true })
  price = number({ min = 0, provides = "currency" })
}
`)
	schema := typeNamed(t, cfg, "product")

	sku := fieldNamed(t, schema, "sku")
	if !sku.External {
		t.Error("external was not applied")
	}
	if !sku.Shareable {
		t.Error("shareable was not applied")
	}
	if len(sku.Constraints) != 0 {
		t.Errorf("directives leaked into constraints: %#v", sku.Constraints)
	}

	price := fieldNamed(t, schema, "price")
	if price.Provides != "currency" {
		t.Errorf("provides = %q, want currency", price.Provides)
	}
	// The real constraint alongside the directive must still be there.
	if got := constraintOf[*validate.MinConstraint](t, price).Min; got != 0 {
		t.Errorf("min = %v, want 0", got)
	}
	if len(price.Constraints) != 1 {
		t.Errorf("price has %d constraints, want just the min", len(price.Constraints))
	}
}

func TestAConstraintNameThatIsNotOneIsRefused(t *testing.T) {
	// It used to be dropped, which is the worst of the three options: the
	// field then accepts everything while the configuration says it does not,
	// and validate reports the file as fine. A rule nobody applies is not a
	// rule, and the name it was written under is a typo worth naming.
	//
	// Nothing is ever appended for it either, so a nil cannot reach the
	// validator — the concern this replaces.
	_, err := parseOneErr(t, `
type "odd" {
  field = string({ max_lenght = 5 })
}
`)
	if err == nil {
		t.Fatal("a constraint nobody applies was accepted")
	}
	for _, want := range []string{"field", "max_lenght", "max_length"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

func TestAConstraintGivenTheWrongKindOfValueIsRefused(t *testing.T) {
	// format takes a name, not a number. Dropping it left the field
	// unconstrained in exactly the same silent way.
	_, err := parseOneErr(t, `
type "odd" {
  field = string({ format = 5 })
}
`)
	if err == nil {
		t.Fatal("a constraint given the wrong kind of value was accepted")
	}
	if !strings.Contains(err.Error(), "format") {
		t.Errorf("error = %q, want it to name the constraint", err)
	}
}

func TestToFloat64AndToInt(t *testing.T) {
	// HCL hands numbers over in whichever Go type fits; the converters are what
	// keep `min = 0` from becoming a zero-valued constraint by accident.
	for _, tc := range []struct {
		in   interface{}
		want float64
	}{
		{int(7), 7}, {int64(7), 7}, {float64(7.5), 7.5}, {"nope", 0}, {nil, 0},
	} {
		if got := toFloat64(tc.in); got != tc.want {
			t.Errorf("toFloat64(%#v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct {
		in   interface{}
		want int
	}{
		{int(7), 7}, {int64(7), 7}, {float64(7.9), 7}, {"nope", 0}, {nil, 0},
	} {
		if got := toInt(tc.in); got != tc.want {
			t.Errorf("toInt(%#v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
