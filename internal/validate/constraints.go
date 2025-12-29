package validate

import (
	"fmt"
	"regexp"
	"strings"
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
		// Simple UUID format check
		if len(s) != 36 {
			return fmt.Errorf("invalid UUID format")
		}
	case "date":
		// Basic date format check (YYYY-MM-DD)
		if len(s) != 10 || s[4] != '-' || s[7] != '-' {
			return fmt.Errorf("invalid date format (expected YYYY-MM-DD)")
		}
	case "datetime":
		// Basic datetime format check
		if !strings.Contains(s, "T") {
			return fmt.Errorf("invalid datetime format")
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
	n := toFloat64(value)
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
	n := toFloat64(value)
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

// Helper functions

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float64:
		return val
	case float32:
		return float64(val)
	default:
		return 0
	}
}
