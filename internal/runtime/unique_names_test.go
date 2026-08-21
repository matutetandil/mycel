package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/saga"
)

// Names that something is keyed by.
//
// Top-level names were checked; the ones inside a flow were not, and each of
// them is a map key at run time. Two steps called the same thing both ran, the
// second overwrote the first, and every reference to that name meant whichever
// came last — so the flow answered, was wrong, and paid for a query whose
// result was thrown away on every request.

func TestTwoStepsCalledTheSameThingAreRefused(t *testing.T) {
	errs := ValidateUniqueInnerNames(&parser.Configuration{Flows: []*flow.Config{{
		Name: "get_profile",
		Steps: []*flow.StepConfig{
			{Name: "detail", Connector: "db"},
			{Name: "detail", Connector: "db"},
		},
	}}})

	if len(errs) != 1 {
		t.Fatalf("errors = %v, want the one repeated name", errs)
	}
	// The message has to name the flow, the step and why it matters, since the
	// symptom is data that is quietly wrong rather than anything failing.
	msg := errs[0].Error()
	for _, want := range []string{"get_profile", "detail", "overwrites"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not mention %q: %v", want, msg)
		}
	}
}

func TestAStepNameRepeatedManyTimesIsCountedOnce(t *testing.T) {
	// One error per name, not one per repeat: three copies of a step is one
	// mistake to fix.
	errs := ValidateUniqueInnerNames(&parser.Configuration{Flows: []*flow.Config{{
		Name: "f",
		Steps: []*flow.StepConfig{
			{Name: "detail"}, {Name: "detail"}, {Name: "detail"},
		},
	}}})
	if len(errs) != 1 {
		t.Fatalf("%d errors, want one: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "3 times") {
		t.Errorf("the error does not say how many: %v", errs[0])
	}
}

func TestDifferentNamesAreFine(t *testing.T) {
	errs := ValidateUniqueInnerNames(&parser.Configuration{Flows: []*flow.Config{{
		Name: "f",
		Steps: []*flow.StepConfig{
			{Name: "user"}, {Name: "orders"}, {Name: "reviews"},
		},
		Enrichments: []*flow.EnrichConfig{{Name: "pricing"}, {Name: "stock"}},
	}}})
	if len(errs) != 0 {
		t.Errorf("refused a flow whose names are all different: %v", errs)
	}
}

func TestTheSameNameInTwoFlowsIsFine(t *testing.T) {
	// Step results do not cross flows, and every flow having a step called
	// "detail" is ordinary.
	errs := ValidateUniqueInnerNames(&parser.Configuration{Flows: []*flow.Config{
		{Name: "one", Steps: []*flow.StepConfig{{Name: "detail"}}},
		{Name: "two", Steps: []*flow.StepConfig{{Name: "detail"}}},
	}})
	if len(errs) != 0 {
		t.Errorf("refused the same step name in two flows: %v", errs)
	}
}

func TestEnrichmentsAreKeyedByNameToo(t *testing.T) {
	errs := ValidateUniqueInnerNames(&parser.Configuration{Flows: []*flow.Config{{
		Name:        "f",
		Enrichments: []*flow.EnrichConfig{{Name: "pricing"}, {Name: "pricing"}},
	}}})
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "enrich") {
		t.Errorf("the error does not say what was repeated: %v", errs[0])
	}
}

func TestASagaStepIsFoundByItsNameWhenItCompensates(t *testing.T) {
	// Which is the one where a repeat is worst: the compensation for a step is
	// found by name, so two steps sharing one share a compensation as well.
	errs := ValidateUniqueInnerNames(&parser.Configuration{Sagas: []*saga.Config{{
		Name: "checkout",
		Steps: []*saga.StepConfig{
			{Name: "reserve"}, {Name: "reserve"},
		},
	}}})
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "checkout") {
		t.Errorf("the error does not name the saga: %v", errs[0])
	}
}

func TestNothingConfiguredHasNoDuplicates(t *testing.T) {
	if errs := ValidateUniqueInnerNames(nil); len(errs) != 0 {
		t.Errorf("errors for no configuration: %v", errs)
	}
	if errs := ValidateUniqueInnerNames(&parser.Configuration{}); len(errs) != 0 {
		t.Errorf("errors for an empty configuration: %v", errs)
	}
}
