package validate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Built-in constraints for type validation.

// FormatConstraint validates string formats.
type FormatConstraint struct {
	Format string
}

func (c *FormatConstraint) Name() string { return "format" }
func (c *FormatConstraint) Validate(value interface{}) error {
	s, ok := value.(string)
	if !ok {
		return nil // Type checking is done separately
	}

	switch c.Format {
	case "email":
		if !strings.Contains(s, "@") || !strings.Contains(s, ".") {
			return fmt.Errorf("invalid email format")
		}
	case "url":
		if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
			return fmt.Errorf("invalid URL format")
		}
	case "uuid":
		// Checked as a UUID rather than as a string of the right length: any
		// thirty-six characters used to pass, so a truncated identifier or a
		// line of text went through as one.
		if !uuidPattern.MatchString(s) {
			return fmt.Errorf("invalid UUID format")
		}
	case "date":
		// Parsed rather than counted. Counting the dashes accepted
		// 9999-99-99 and 0000-00-00, which reach a database as a date it
		// refuses — much further from the field that was wrong.
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("invalid date format (expected YYYY-MM-DD)")
		}
	case "datetime":
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("invalid datetime format (expected RFC 3339, e.g. 2026-08-15T09:30:00Z)")
		}
	}
	return nil
}

// MinConstraint validates minimum numeric values.
type MinConstraint struct {
	Min float64
}

func (c *MinConstraint) Name() string { return "min" }
func (c *MinConstraint) Validate(value interface{}) error {
	n, ok := asNumber(value)
	if !ok {
		return nil // whether it is a number at all is the type check's business
	}
	if n < c.Min {
		return fmt.Errorf("value must be at least %v", c.Min)
	}
	return nil
}

// MaxConstraint validates maximum numeric values.
type MaxConstraint struct {
	Max float64
}

func (c *MaxConstraint) Name() string { return "max" }
func (c *MaxConstraint) Validate(value interface{}) error {
	n, ok := asNumber(value)
	if !ok {
		return nil
	}
	if n > c.Max {
		return fmt.Errorf("value must be at most %v", c.Max)
	}
	return nil
}

// MinLengthConstraint validates minimum string length.
type MinLengthConstraint struct {
	MinLength int
}

func (c *MinLengthConstraint) Name() string { return "min_length" }
func (c *MinLengthConstraint) Validate(value interface{}) error {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	if len(s) < c.MinLength {
		return fmt.Errorf("string must be at least %d characters", c.MinLength)
	}
	return nil
}

// MaxLengthConstraint validates maximum string length.
type MaxLengthConstraint struct {
	MaxLength int
}

func (c *MaxLengthConstraint) Name() string { return "max_length" }
func (c *MaxLengthConstraint) Validate(value interface{}) error {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	if len(s) > c.MaxLength {
		return fmt.Errorf("string must be at most %d characters", c.MaxLength)
	}
	return nil
}

// PatternConstraint validates string patterns using regex.
type PatternConstraint struct {
	Pattern string
	regex   *regexp.Regexp
}

func (c *PatternConstraint) Name() string { return "pattern" }
func (c *PatternConstraint) Validate(value interface{}) error {
	s, ok := value.(string)
	if !ok {
		return nil
	}

	// Compile regex lazily
	if c.regex == nil {
		var err error
		c.regex, err = regexp.Compile(c.Pattern)
		if err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}
	}

	if !c.regex.MatchString(s) {
		return fmt.Errorf("string does not match pattern: %s", c.Pattern)
	}
	return nil
}

// EnumConstraint validates that a value is one of allowed values.
type EnumConstraint struct {
	Values []string
}

func (c *EnumConstraint) Name() string { return "enum" }
func (c *EnumConstraint) Validate(value interface{}) error {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	for _, allowed := range c.Values {
		if s == allowed {
			return nil
		}
	}
	return fmt.Errorf("value must be one of: %v", c.Values)
}

// CustomValidatorConstraint wraps a custom validator function.
// This allows custom validators to be used as constraints in type schemas.
type CustomValidatorConstraint struct {
	ValidatorName string
	ValidateFn    func(value interface{}) error
}

func (c *CustomValidatorConstraint) Name() string { return "custom:" + c.ValidatorName }
func (c *CustomValidatorConstraint) Validate(value interface{}) error {
	if c.ValidateFn == nil {
		return fmt.Errorf("validator %q not found", c.ValidatorName)
	}
	return c.ValidateFn(value)
}

// Helper functions

// uuidPattern is the canonical 8-4-4-4-12 hexadecimal form.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// asNumber reads a number of any width, and says whether it was one.
//
// The type check admits every integer and float width Go has, and this used to
// know four of them — everything else came back as zero. So a quantity of 50
// arriving as an int32, which is what a Postgres int4 column produces, failed
// `min = 1` with "value must be at least 1", and a quantity of 5000 passed
// `max = 100` because it too was read as zero. The two widths that happen to
// be missing are the ones database drivers use.
//
// Returning whether it converted also means a value that is not a number is
// left to the type check, the way the string constraints already leave a
// non-string alone, instead of being compared as zero.
func asNumber(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	case float32:
		return float64(val), true
	case float64:
		return val, true
	case json.Number:
		n, err := val.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}
