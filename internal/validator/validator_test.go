package validator

import (
	"testing"
)

func TestRegexValidator(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		value    string
		valid    bool
		message  string
	}{
		{
			name:    "valid email",
			pattern: `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			value:   "test@example.com",
			valid:   true,
		},
		{
			name:    "invalid email",
			pattern: `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			value:   "invalid-email",
			valid:   false,
			message: "Invalid email",
		},
		{
			name:    "valid phone AR",
			pattern: `^\+54[0-9]{10,11}$`,
			value:   "+5491123456789",
			valid:   true,
		},
		{
			name:    "invalid phone AR - no prefix",
			pattern: `^\+54[0-9]{10,11}$`,
			value:   "1123456789",
			valid:   false,
		},
		{
			name:    "valid UUID",
			pattern: `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			value:   "123e4567-e89b-12d3-a456-426614174000",
			valid:   true,
		},
		{
			name:    "invalid UUID",
			pattern: `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			value:   "not-a-uuid",
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := NewRegexValidator("test", tt.pattern, tt.message)
			if err != nil {
				t.Fatalf("failed to create validator: %v", err)
			}

			err = v.Validate(tt.value)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected invalid, got no error")
			}
		})
	}
}

func TestRegexValidator_InvalidPattern(t *testing.T) {
	_, err := NewRegexValidator("test", "[invalid", "message")
	if err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestRegexValidator_NonStringValue(t *testing.T) {
	v, err := NewRegexValidator("test", ".*", "message")
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	err = v.Validate(123) // not a string
	if err == nil {
		t.Error("expected error for non-string value")
	}
}

func TestCELValidator(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		value   interface{}
		valid   bool
		message string
	}{
		{
			name:  "adult age - valid",
			expr:  "value >= 18 && value <= 120",
			value: int64(25),
			valid: true,
		},
		{
			name:    "adult age - too young",
			expr:    "value >= 18 && value <= 120",
			value:   int64(15),
			valid:   false,
			message: "Must be 18 or older",
		},
		{
			name:    "adult age - too old",
			expr:    "value >= 18 && value <= 120",
			value:   int64(150),
			valid:   false,
			message: "Must be 18 or older",
		},
		{
			name:  "string length - valid",
			expr:  "size(value) >= 3 && size(value) <= 50",
			value: "hello",
			valid: true,
		},
		{
			name:  "string length - too short",
			expr:  "size(value) >= 3 && size(value) <= 50",
			value: "ab",
			valid: false,
		},
		{
			name:  "enum check - valid",
			expr:  "value in ['pending', 'active', 'completed']",
			value: "active",
			valid: true,
		},
		{
			name:  "enum check - invalid",
			expr:  "value in ['pending', 'active', 'completed']",
			value: "unknown",
			valid: false,
		},
		{
			name:  "contains uppercase - valid",
			expr:  "value.matches('[A-Z]')",
			value: "Hello",
			valid: true,
		},
		{
			name:  "contains uppercase - invalid",
			expr:  "value.matches('[A-Z]')",
			value: "hello",
			valid: false,
		},
		{
			name:  "positive number",
			expr:  "value > 0",
			value: 10.5,
			valid: true,
		},
		{
			name:  "negative number fails positive check",
			expr:  "value > 0",
			value: -5.0,
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := NewCELValidator("test", tt.expr, tt.message)
			if err != nil {
				t.Fatalf("failed to create validator: %v", err)
			}

			err = v.Validate(tt.value)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected invalid, got no error")
			}
		})
	}
}

func TestCELValidator_StrongPassword(t *testing.T) {
	expr := `size(value) >= 8 && value.matches("[A-Z]") && value.matches("[a-z]") && value.matches("[0-9]") && value.matches("[!@#$%^&*]")`

	v, err := NewCELValidator("strong_password", expr, "Password too weak")
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		password string
		valid    bool
	}{
		{"Abc123!@", true},
		{"StrongP@ss1", true},
		{"weakpass", false},     // no uppercase, number, or special
		{"ALLCAPS1!", false},    // no lowercase
		{"alllower1!", false},   // no uppercase
		{"NoSpecial1", false},   // no special char
		{"Short1!", false},      // too short
	}

	for _, tt := range tests {
		t.Run(tt.password, func(t *testing.T) {
			err := v.Validate(tt.password)
			if tt.valid && err != nil {
				t.Errorf("expected valid for %q, got error: %v", tt.password, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("expected invalid for %q, got no error", tt.password)
			}
		})
	}
}

func TestCELValidator_InvalidExpression(t *testing.T) {
	_, err := NewCELValidator("test", "invalid expression !!!", "message")
	if err == nil {
		t.Error("expected error for invalid CEL expression")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()

	// Create validators
	v1, _ := NewRegexValidator("email", `^.+@.+$`, "Invalid email")
	v2, _ := NewCELValidator("adult", "value >= 18", "Must be adult")

	// Register
	if err := r.Register(v1); err != nil {
		t.Errorf("failed to register v1: %v", err)
	}
	if err := r.Register(v2); err != nil {
		t.Errorf("failed to register v2: %v", err)
	}

	// Get
	got, ok := r.Get("email")
	if !ok {
		t.Error("failed to get 'email' validator")
	}
	if got.Name() != "email" {
		t.Errorf("expected name 'email', got %q", got.Name())
	}

	// Get non-existent
	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("expected false for non-existent validator")
	}

	// List
	names := r.List()
	if len(names) != 2 {
		t.Errorf("expected 2 validators, got %d", len(names))
	}

	// Duplicate registration should fail
	if err := r.Register(v1); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestCreateValidator(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "regex validator",
			cfg: Config{
				Name:    "email",
				Type:    ValidatorTypeRegex,
				Pattern: `^.+@.+$`,
				Message: "Invalid email",
			},
			wantErr: false,
		},
		{
			name: "CEL validator",
			cfg: Config{
				Name:    "adult",
				Type:    ValidatorTypeCEL,
				Expr:    "value >= 18",
				Message: "Must be adult",
			},
			wantErr: false,
		},
		{
			name: "WASM validator - not implemented",
			cfg: Config{
				Name: "custom",
				Type: ValidatorTypeWASM,
				WASM: "./custom.wasm",
			},
			wantErr: true,
		},
		{
			name: "unknown type",
			cfg: Config{
				Name: "unknown",
				Type: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CreateValidator(tt.cfg)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidatorTypes(t *testing.T) {
	regex, _ := NewRegexValidator("test", ".*", "")
	if regex.Type() != ValidatorTypeRegex {
		t.Errorf("expected regex type, got %v", regex.Type())
	}

	cel, _ := NewCELValidator("test", "true", "")
	if cel.Type() != ValidatorTypeCEL {
		t.Errorf("expected CEL type, got %v", cel.Type())
	}
}
