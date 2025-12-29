package validate

import (
	"context"
	"testing"
)

func TestTypeValidator_ValidateRequired(t *testing.T) {
	validator := NewTypeValidator(NewConstraintRegistry())

	schema := &TypeSchema{
		Name: "user",
		Fields: []FieldSchema{
			{Name: "name", Type: "string", Required: true},
			{Name: "email", Type: "string", Required: true},
			{Name: "age", Type: "number", Required: false},
		},
	}

	tests := []struct {
		name    string
		data    map[string]interface{}
		valid   bool
		errCode string
	}{
		{
			name:  "valid with all fields",
			data:  map[string]interface{}{"name": "John", "email": "john@example.com", "age": 30},
			valid: true,
		},
		{
			name:  "valid with optional missing",
			data:  map[string]interface{}{"name": "John", "email": "john@example.com"},
			valid: true,
		},
		{
			name:    "missing required field",
			data:    map[string]interface{}{"name": "John"},
			valid:   false,
			errCode: "required",
		},
		{
			name:    "missing all required",
			data:    map[string]interface{}{"age": 30},
			valid:   false,
			errCode: "required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(context.Background(), tt.data, schema)
			if result.Valid != tt.valid {
				t.Errorf("expected valid=%v, got %v", tt.valid, result.Valid)
				for _, e := range result.Errors {
					t.Logf("  error: %s", e.Error())
				}
			}
			if !tt.valid && tt.errCode != "" {
				found := false
				for _, e := range result.Errors {
					if e.Code == tt.errCode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error code %s not found", tt.errCode)
				}
			}
		})
	}
}

func TestTypeValidator_ValidateTypes(t *testing.T) {
	validator := NewTypeValidator(NewConstraintRegistry())

	tests := []struct {
		name      string
		fieldType string
		value     interface{}
		valid     bool
	}{
		{"string valid", "string", "hello", true},
		{"string invalid", "string", 123, false},
		{"number int", "number", 42, true},
		{"number float", "number", 3.14, true},
		{"number invalid", "number", "42", false},
		{"boolean true", "boolean", true, true},
		{"boolean false", "boolean", false, true},
		{"boolean invalid", "boolean", "true", false},
		{"array valid", "array", []interface{}{"a", "b"}, true},
		{"array invalid", "array", "not an array", false},
		{"object valid", "object", map[string]interface{}{"key": "value"}, true},
		{"object invalid", "object", []string{"a"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &TypeSchema{
				Name: "test",
				Fields: []FieldSchema{
					{Name: "field", Type: tt.fieldType, Required: true},
				},
			}
			data := map[string]interface{}{"field": tt.value}
			result := validator.Validate(context.Background(), data, schema)
			if result.Valid != tt.valid {
				t.Errorf("expected valid=%v, got %v", tt.valid, result.Valid)
			}
		})
	}
}

func TestConstraints(t *testing.T) {
	t.Run("MinConstraint", func(t *testing.T) {
		c := &MinConstraint{Min: 10}
		if err := c.Validate(15); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
		if err := c.Validate(5); err == nil {
			t.Error("expected error for value below min")
		}
	})

	t.Run("MaxConstraint", func(t *testing.T) {
		c := &MaxConstraint{Max: 100}
		if err := c.Validate(50); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
		if err := c.Validate(150); err == nil {
			t.Error("expected error for value above max")
		}
	})

	t.Run("MinLengthConstraint", func(t *testing.T) {
		c := &MinLengthConstraint{MinLength: 5}
		if err := c.Validate("hello world"); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
		if err := c.Validate("hi"); err == nil {
			t.Error("expected error for string too short")
		}
	})

	t.Run("MaxLengthConstraint", func(t *testing.T) {
		c := &MaxLengthConstraint{MaxLength: 10}
		if err := c.Validate("hello"); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
		if err := c.Validate("hello world!"); err == nil {
			t.Error("expected error for string too long")
		}
	})

	t.Run("FormatConstraint email", func(t *testing.T) {
		c := &FormatConstraint{Format: "email"}
		if err := c.Validate("test@example.com"); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
		if err := c.Validate("invalid-email"); err == nil {
			t.Error("expected error for invalid email")
		}
	})

	t.Run("EnumConstraint", func(t *testing.T) {
		c := &EnumConstraint{Values: []string{"active", "inactive", "pending"}}
		if err := c.Validate("active"); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
		if err := c.Validate("unknown"); err == nil {
			t.Error("expected error for value not in enum")
		}
	})
}

func TestValidatorWithConstraints(t *testing.T) {
	validator := NewTypeValidator(NewConstraintRegistry())

	schema := &TypeSchema{
		Name: "user",
		Fields: []FieldSchema{
			{
				Name:     "age",
				Type:     "number",
				Required: true,
				Constraints: []Constraint{
					&MinConstraint{Min: 0},
					&MaxConstraint{Max: 150},
				},
			},
			{
				Name:     "email",
				Type:     "string",
				Required: true,
				Constraints: []Constraint{
					&FormatConstraint{Format: "email"},
				},
			},
		},
	}

	tests := []struct {
		name  string
		data  map[string]interface{}
		valid bool
	}{
		{
			name:  "valid data",
			data:  map[string]interface{}{"age": 25, "email": "test@example.com"},
			valid: true,
		},
		{
			name:  "age below min",
			data:  map[string]interface{}{"age": -5, "email": "test@example.com"},
			valid: false,
		},
		{
			name:  "age above max",
			data:  map[string]interface{}{"age": 200, "email": "test@example.com"},
			valid: false,
		},
		{
			name:  "invalid email",
			data:  map[string]interface{}{"age": 25, "email": "not-an-email"},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(context.Background(), tt.data, schema)
			if result.Valid != tt.valid {
				t.Errorf("expected valid=%v, got %v", tt.valid, result.Valid)
				for _, e := range result.Errors {
					t.Logf("  error: %s", e.Error())
				}
			}
		})
	}
}
