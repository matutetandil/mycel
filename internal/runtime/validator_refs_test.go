package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/internal/validate"
	"github.com/matutetandil/mycel/v2/internal/validator"
)

// A validator a type names.
//
// The reference is looked up when the flow runs, and a name that is not in the
// registry is skipped — `if !ok { continue }`. So a typo does not fail, it
// turns the rule off, and a validator exists precisely to refuse input that
// should not get in. Nothing anywhere said the field was going unchecked.

func typesConfig(fields []validate.FieldSchema, validators []string) *parser.Configuration {
	c := &parser.Configuration{
		Types: []*validate.TypeSchema{{Name: "user", Fields: fields}},
	}
	for _, name := range validators {
		c.Validators = append(c.Validators, &validator.Config{Name: name})
	}
	return c
}

func TestAValidatorNobodyDeclaredIsNamed(t *testing.T) {
	errs := ValidateValidatorReferences(typesConfig(
		[]validate.FieldSchema{{Name: "email", ValidatorRef: "corporate_emial"}},
		[]string{"corporate_email"},
	))

	if len(errs) != 1 {
		t.Fatalf("errors = %v, want the misspelt one", errs)
	}
	msg := errs[0].Error()
	for _, want := range []string{"user", "email", "corporate_emial", "corporate_email"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not mention %q: %v", want, msg)
		}
	}
}

func TestAValidatorThatExistsIsFine(t *testing.T) {
	errs := ValidateValidatorReferences(typesConfig(
		[]validate.FieldSchema{
			{Name: "email", ValidatorRef: "corporate_email"},
			{Name: "name"},
		},
		[]string{"corporate_email"},
	))
	if len(errs) != 0 {
		t.Errorf("refused a reference that resolves: %v", errs)
	}
}

func TestAConfigurationWithNoValidatorsSaysSo(t *testing.T) {
	// The likeliest shape of this mistake: the validator block was never
	// written at all, or lives in a directory that was not loaded.
	errs := ValidateValidatorReferences(typesConfig(
		[]validate.FieldSchema{{Name: "email", ValidatorRef: "corporate_email"}},
		nil,
	))
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "declares none") {
		t.Errorf("the error does not say there are none: %v", errs[0])
	}
}

func TestNoValidatorReferencesIsNotAnError(t *testing.T) {
	if errs := ValidateValidatorReferences(nil); len(errs) != 0 {
		t.Errorf("errors for no configuration: %v", errs)
	}
	if errs := ValidateValidatorReferences(&parser.Configuration{}); len(errs) != 0 {
		t.Errorf("errors for an empty configuration: %v", errs)
	}
}
