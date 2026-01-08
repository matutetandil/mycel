// Package validate defines the validation system for Mycel.
// It validates data against type schemas defined in HCL.
package validate

import (
	"context"
	"fmt"
)

// Validator validates data against a type schema.
type Validator interface {
	// Validate validates data against the given schema.
	Validate(ctx context.Context, data map[string]interface{}, schema *TypeSchema) *Result
}

// TypeSchema represents a type definition from HCL.
type TypeSchema struct {
	// Name is the type identifier.
	Name string

	// Fields are the schema fields.
	Fields []FieldSchema

	// Federation directives (GraphQL Federation v2 support)

	// Keys defines the @key directive fields for federated entities.
	// Each key string represents fields for entity resolution (e.g., "id", "id name").
	Keys []string

	// Shareable marks the type with @shareable directive (Federation v2).
	// Allows multiple subgraphs to resolve this type.
	Shareable bool

	// Inaccessible marks the type with @inaccessible directive (Federation v2).
	// Hides the type from the composed supergraph schema.
	Inaccessible bool

	// InterfaceNames lists the interfaces this type implements.
	InterfaceNames []string

	// Description provides documentation for the type.
	Description string
}

// FieldSchema represents a field in a type schema.
type FieldSchema struct {
	// Name is the field name.
	Name string

	// Type is the field type (string, number, boolean, array, object).
	Type string

	// Required indicates if the field is required.
	Required bool

	// Constraints are validation constraints for this field.
	Constraints []Constraint

	// ValidatorRef is a reference to a custom validator (e.g., "validator.phone_ar").
	ValidatorRef string

	// Federation field-level directives (GraphQL Federation v2 support)

	// External marks the field with @external directive.
	// Indicates this field is defined in another subgraph.
	External bool

	// Provides is the @provides directive value.
	// Lists fields from the return type that this resolver provides.
	Provides string

	// Requires is the @requires directive value.
	// Lists fields that must be fetched from other subgraphs before resolving this field.
	Requires string

	// Shareable marks the field with @shareable directive.
	Shareable bool

	// Inaccessible marks the field with @inaccessible directive.
	Inaccessible bool

	// Override is the @override directive value (subgraph name).
	// Indicates this subgraph is taking ownership of this field.
	Override string

	// Description provides documentation for the field.
	Description string
}

// Constraint represents a validation constraint.
// Open/Closed Principle - new constraints can be added without modifying existing code.
type Constraint interface {
	// Name returns the constraint identifier.
	Name() string

	// Validate validates the value against this constraint.
	Validate(value interface{}) error
}

// Result holds validation results.
type Result struct {
	// Valid is true if validation passed.
	Valid bool

	// Errors contains all validation errors.
	Errors []Error
}

// NewResult creates a new valid result.
func NewResult() *Result {
	return &Result{
		Valid:  true,
		Errors: make([]Error, 0),
	}
}

// AddError adds a validation error and marks result as invalid.
func (r *Result) AddError(field, message, code string) {
	r.Valid = false
	r.Errors = append(r.Errors, Error{
		Field:   field,
		Message: message,
		Code:    code,
	})
}

// Error represents a single validation error.
type Error struct {
	// Field is the field that failed validation.
	Field string

	// Message is a human-readable error message.
	Message string

	// Code is a machine-readable error code.
	Code string
}

func (e Error) Error() string {
	return fmt.Sprintf("validation error on '%s': %s", e.Field, e.Message)
}

// ConstraintFactory creates constraints from configuration.
// Open/Closed Principle - new constraint types can be added via new factories.
type ConstraintFactory interface {
	// Create creates a constraint from the given parameters.
	Create(constraintType string, params map[string]interface{}) (Constraint, error)

	// Supports returns true if this factory can create the given constraint type.
	Supports(constraintType string) bool
}

// ConstraintRegistry manages constraint factories.
type ConstraintRegistry struct {
	factories []ConstraintFactory
}

// NewConstraintRegistry creates a new constraint registry.
func NewConstraintRegistry() *ConstraintRegistry {
	return &ConstraintRegistry{
		factories: make([]ConstraintFactory, 0),
	}
}

// Register adds a factory to the registry.
func (r *ConstraintRegistry) Register(factory ConstraintFactory) {
	r.factories = append(r.factories, factory)
}

// Create creates a constraint using the appropriate factory.
func (r *ConstraintRegistry) Create(constraintType string, params map[string]interface{}) (Constraint, error) {
	for _, factory := range r.factories {
		if factory.Supports(constraintType) {
			return factory.Create(constraintType, params)
		}
	}
	return nil, fmt.Errorf("no factory found for constraint type: %s", constraintType)
}

// TypeValidator implements Validator using type schemas.
type TypeValidator struct {
	registry *ConstraintRegistry
}

// NewTypeValidator creates a new type validator.
func NewTypeValidator(registry *ConstraintRegistry) *TypeValidator {
	return &TypeValidator{registry: registry}
}

// Validate validates data against the given schema.
func (v *TypeValidator) Validate(ctx context.Context, data map[string]interface{}, schema *TypeSchema) *Result {
	result := NewResult()

	for _, field := range schema.Fields {
		value, exists := data[field.Name]

		// Check required
		if field.Required && !exists {
			result.AddError(field.Name, "field is required", "required")
			continue
		}

		// Skip validation if field is not present and not required
		if !exists {
			continue
		}

		// Validate type
		if err := v.validateType(value, field.Type); err != nil {
			result.AddError(field.Name, err.Error(), "type_mismatch")
			continue
		}

		// Validate constraints
		for _, constraint := range field.Constraints {
			if err := constraint.Validate(value); err != nil {
				result.AddError(field.Name, err.Error(), constraint.Name())
			}
		}
	}

	return result
}

// validateType validates that a value matches the expected type.
func (v *TypeValidator) validateType(value interface{}, expectedType string) error {
	if value == nil {
		return nil // nil values are handled by required check
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "number":
		switch value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			// Valid number types
		default:
			return fmt.Errorf("expected number, got %T", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "array":
		switch value.(type) {
		case []interface{}, []string, []int, []float64:
			// Valid array types
		default:
			return fmt.Errorf("expected array, got %T", value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	default:
		return fmt.Errorf("unknown type: %s", expectedType)
	}

	return nil
}
