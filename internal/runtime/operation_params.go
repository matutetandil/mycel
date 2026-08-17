package runtime

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/validate"
)

// applyOperationParams fills in defaults and checks the constraints an
// operation declares for its parameters.
//
// A param block reads like a contract:
//
//	param "limit" { type = "number", default = 100, max = 500 }
//	param "id"    { type = "string", required = true }
//
// Everything in it was parsed and none of it was enforced: `required` was
// checked by a function nothing called, `default` was computed into a value
// nothing read, and the constraints were stored on the definition and never
// looked at, under a comment marking where the check would go. So the block
// documented an intent the runtime did not hold up — the least useful state for
// a validation feature, because it reads as protection that is not there.
//
// Constraints are the ones the `type` block already enforces, from
// internal/validate, rather than a second implementation of the same rules.
func applyOperationParams(op *connector.OperationDef, input map[string]interface{}) error {
	if op == nil || len(op.Params) == 0 || input == nil {
		return nil
	}

	var problems []string

	for _, p := range op.Params {
		value, present := input[p.Name]
		if !present || value == nil || value == "" {
			if p.Default != nil {
				input[p.Name] = p.Default
				continue
			}
			if p.Required {
				problems = append(problems, fmt.Sprintf("%s is required", p.Name))
			}
			continue
		}

		coerced, err := coerceParam(p, value)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", p.Name, err))
			continue
		}
		input[p.Name] = coerced

		for _, c := range paramConstraints(p) {
			if err := c.Validate(coerced); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", p.Name, err))
			}
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("invalid parameters: %s", strings.Join(problems, "; "))
	}
	return nil
}

// coerceParam converts a value to the declared type where that is unambiguous.
//
// Conversion rather than rejection is the whole point on a REST source: path
// and query parameters arrive as strings, always, so `type = "number"` enforced
// strictly would reject every request that uses the feature where it is most
// useful. What a person writing `param "limit" { type = "number" }` wants is a
// number in `input.limit`, and to be told when "abc" arrives instead.
func coerceParam(p *connector.ParamDef, value interface{}) (interface{}, error) {
	switch strings.ToLower(p.Type) {
	case "", "any":
		return value, nil

	case "number", "integer", "int", "float":
		switch v := value.(type) {
		case int:
			return v, nil
		case int64:
			return int(v), nil
		case float64:
			return v, nil
		case string:
			if n, err := strconv.Atoi(v); err == nil {
				return n, nil
			}
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f, nil
			}
			return nil, fmt.Errorf("expected a number, got %q", v)
		}
		return nil, fmt.Errorf("expected a number, got %T", value)

	case "boolean", "bool":
		switch v := value.(type) {
		case bool:
			return v, nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("expected true or false, got %q", v)
			}
			return b, nil
		}
		return nil, fmt.Errorf("expected a boolean, got %T", value)

	case "string":
		switch v := value.(type) {
		case string:
			return v, nil
		case int, int64, float64, bool:
			return fmt.Sprintf("%v", v), nil
		}
		return nil, fmt.Errorf("expected a string, got %T", value)

	case "array", "list":
		if _, ok := value.([]interface{}); !ok {
			return nil, fmt.Errorf("expected an array, got %T", value)
		}
		return value, nil

	case "object", "map":
		if _, ok := value.(map[string]interface{}); !ok {
			return nil, fmt.Errorf("expected an object, got %T", value)
		}
		return value, nil
	}

	// An unrecognised type name is not a reason to reject a request; the value
	// passes through and the constraints still apply.
	return value, nil
}

// paramConstraints turns a parameter definition into the constraints that
// already exist for type blocks.
func paramConstraints(p *connector.ParamDef) []validate.Constraint {
	var cs []validate.Constraint

	if p.Min != nil {
		cs = append(cs, &validate.MinConstraint{Min: *p.Min})
	}
	if p.Max != nil {
		cs = append(cs, &validate.MaxConstraint{Max: *p.Max})
	}
	if p.MinLength != nil {
		cs = append(cs, &validate.MinLengthConstraint{MinLength: *p.MinLength})
	}
	if p.MaxLength != nil {
		cs = append(cs, &validate.MaxLengthConstraint{MaxLength: *p.MaxLength})
	}
	if p.Pattern != "" {
		cs = append(cs, &validate.PatternConstraint{Pattern: p.Pattern})
	}
	if len(p.Enum) > 0 {
		cs = append(cs, &validate.EnumConstraint{Values: p.Enum})
	}

	return cs
}
