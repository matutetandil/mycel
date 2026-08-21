package validate

import (
	"context"
	"testing"
)

// An identifier is a type of its own.
//
// GraphQL publishes it as ID, which accepts a number or a string, and the
// converter has mapped it since it was written. Validation refused it as an
// unknown type and the list of field types never named it — so a type using it
// built a schema and could not be validated against, and nothing offered it.

func TestAnIdentifierIsAcceptedEitherWay(t *testing.T) {
	schema := &TypeSchema{Name: "User", Fields: []FieldSchema{
		{Name: "id", Type: "id", Required: true},
		{Name: "name", Type: "string"},
	}}

	validator := NewTypeValidator(NewConstraintRegistry())
	for _, id := range []interface{}{"abc-123", 42, int64(42), 42.0} {
		result := validator.Validate(context.Background(),
			map[string]interface{}{"id": id, "name": "Ada"}, schema)
		if !result.Valid {
			t.Errorf("an id written as %#v was refused: %v", id, result.Errors)
		}
	}
}

func TestSomethingThatIsNotAnIdentifier(t *testing.T) {
	schema := &TypeSchema{Name: "User", Fields: []FieldSchema{
		{Name: "id", Type: "id", Required: true},
	}}

	validator := NewTypeValidator(NewConstraintRegistry())
	result := validator.Validate(context.Background(),
		map[string]interface{}{"id": map[string]interface{}{"nested": true}}, schema)
	if result.Valid {
		t.Error("an object was accepted as an id")
	}
}
