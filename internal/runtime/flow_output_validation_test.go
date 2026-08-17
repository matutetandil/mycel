package runtime

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/transform"
	"github.com/matutetandil/mycel/v2/internal/validate"
)

// A validate block names a type for what goes in and a type for what comes
// out. Only the first was ever checked — validateOutput was written, complete,
// and called by nothing — so a flow that declared an output contract had it
// enforced nowhere.
//
// The asymmetry is what makes it worse than a missing feature. Someone who
// watches `validate { input = ... }` refuse a bad request has every reason to
// believe the other half of the same block does the same thing.

func validatingHandler(t *testing.T, cfg *flow.Config) *FlowHandler {
	t.Helper()
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	return &FlowHandler{
		Config:      cfg,
		Transformer: tr,
		Connectors:  connector.NewRegistry(),
		Types: map[string]*validate.TypeSchema{
			"customer": {
				Name: "customer",
				Fields: []validate.FieldSchema{
					{Name: "id", Type: "string", Required: true},
					{Name: "email", Type: "string", Required: true},
					{Name: "age", Type: "number"},
				},
			},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func outputContract() *flow.Config {
	return &flow.Config{
		Name:     "get_customer",
		Validate: &flow.ValidateConfig{Output: "customer"},
	}
}

func TestAnAnswerThatMatchesTheContractPasses(t *testing.T) {
	h := validatingHandler(t, outputContract())
	err := h.validateResult(context.Background(), map[string]interface{}{
		"id": "c-1", "email": "someone@example.com", "age": 41,
	})
	if err != nil {
		t.Fatalf("a matching answer was refused: %v", err)
	}
}

func TestAnAnswerMissingARequiredFieldIsRefused(t *testing.T) {
	// The case the contract exists for: a transform that drops a field, or a
	// column renamed in the database, reaching the caller as a record with a
	// hole in it.
	h := validatingHandler(t, outputContract())
	err := h.validateResult(context.Background(), map[string]interface{}{"id": "c-1"})
	if err == nil {
		t.Fatal("an answer missing a required field was let through")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("error = %q, want it to name the field", err)
	}
}

func TestAnAnswerWithTheWrongKindOfValueIsRefused(t *testing.T) {
	h := validatingHandler(t, outputContract())
	err := h.validateResult(context.Background(), map[string]interface{}{
		"id": "c-1", "email": "someone@example.com", "age": "not a number",
	})
	if err == nil {
		t.Fatal("an answer with the wrong kind of value was let through")
	}
	if !strings.Contains(err.Error(), "age") {
		t.Errorf("error = %q, want it to name the field", err)
	}
}

func TestEveryRecordOfAListIsChecked(t *testing.T) {
	// A read answers with rows while the type describes a record, so the
	// contract applies to each one. Checking only the first would let a bad
	// row through in every result but the shortest.
	h := validatingHandler(t, outputContract())

	rows := []map[string]interface{}{
		{"id": "c-1", "email": "someone@example.com"},
		{"id": "c-2", "email": "other@example.com"},
	}
	if err := h.validateResult(context.Background(), rows); err != nil {
		t.Fatalf("matching records were refused: %v", err)
	}

	rows = append(rows, map[string]interface{}{"id": "c-3"})
	err := h.validateResult(context.Background(), rows)
	if err == nil {
		t.Fatal("a bad record in the third position was let through")
	}
	if !strings.Contains(err.Error(), "record 2") {
		t.Errorf("error = %q, want it to say which record", err)
	}
}

func TestAListOfAnythingIsCheckedWhereItCanBe(t *testing.T) {
	h := validatingHandler(t, outputContract())
	err := h.validateResult(context.Background(), []interface{}{
		map[string]interface{}{"id": "c-1", "email": "someone@example.com"},
		map[string]interface{}{"id": "c-2"},
	})
	if err == nil {
		t.Fatal("a bad record was let through")
	}
	if !strings.Contains(err.Error(), "record 1") {
		t.Errorf("error = %q", err)
	}
}

func TestAnAnswerWithNothingToCheckAgainstIsLeftAlone(t *testing.T) {
	// A count, a string, a nothing. There is no record to hold against the
	// type, and refusing them would make the contract unusable on any flow
	// that does not answer with a record.
	h := validatingHandler(t, outputContract())
	for name, answer := range map[string]interface{}{
		"nothing":           nil,
		"a number":          42,
		"a string":          "done",
		"a list of numbers": []interface{}{1, 2, 3},
	} {
		t.Run(name, func(t *testing.T) {
			if err := h.validateResult(context.Background(), answer); err != nil {
				t.Errorf("%v was refused: %v", answer, err)
			}
		})
	}
}

func TestAFlowWithNoContractChecksNothing(t *testing.T) {
	// The default has to stay what it was, or every flow that answers with
	// something other than its input starts failing.
	h := validatingHandler(t, &flow.Config{Name: "get_customer"})
	if err := h.validateResult(context.Background(), map[string]interface{}{"anything": true}); err != nil {
		t.Errorf("a flow with no validate block refused an answer: %v", err)
	}

	h = validatingHandler(t, &flow.Config{
		Name:     "get_customer",
		Validate: &flow.ValidateConfig{Input: "customer"},
	})
	if err := h.validateResult(context.Background(), map[string]interface{}{"anything": true}); err != nil {
		t.Errorf("a flow validating only its input refused an answer: %v", err)
	}
}

func TestAContractNamingATypeThatDoesNotExistIsReported(t *testing.T) {
	// Silently skipping it would leave the flow unchecked with the
	// configuration saying otherwise — the failure this whole change is about.
	h := validatingHandler(t, &flow.Config{
		Name:     "get_customer",
		Validate: &flow.ValidateConfig{Output: "custmoer"},
	})
	err := h.validateResult(context.Background(), map[string]interface{}{"id": "c-1"})
	if err == nil {
		t.Fatal("a contract naming a type that does not exist was ignored")
	}
	if !strings.Contains(err.Error(), "custmoer") {
		t.Errorf("error = %q, want it to name what was written", err)
	}
}

func TestBothHalvesOfTheBlockAreEnforced(t *testing.T) {
	// The point of the change, stated once: the same block, both directions.
	h := validatingHandler(t, &flow.Config{
		Name:     "get_customer",
		Validate: &flow.ValidateConfig{Input: "customer", Output: "customer"},
	})

	badRecord := map[string]interface{}{"id": "c-1"}
	if err := h.validateInput(context.Background(), badRecord); err == nil {
		t.Error("the input half let a bad record through")
	}
	if err := h.validateResult(context.Background(), badRecord); err == nil {
		t.Error("the output half let a bad record through")
	}
}
