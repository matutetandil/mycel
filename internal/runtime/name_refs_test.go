package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/aspect"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/validate"
)

// The last two namespaces a configuration points into by name.
//
// A type named by validate { input = "user" } is looked up when a request
// arrives, so a typo is a 500 on the first one through the door. A flow named
// by an aspect's action is worse: an after aspect whose action fails is logged
// at warning level and the flow carries on, so an audit aspect pointing at a
// misspelt flow writes nothing for ever while producing a line per message
// that nobody reads.

func TestATypeNobodyDeclaredIsNamed(t *testing.T) {
	for name, v := range map[string]*flow.ValidateConfig{
		"the input type":  {Input: "usr"},
		"the output type": {Output: "usr"},
	} {
		t.Run(name, func(t *testing.T) {
			errs := ValidateTypeReferences(&parser.Configuration{
				Types: []*validate.TypeSchema{{Name: "user"}},
				Flows: []*flow.Config{{Name: "create_user", Validate: v}},
			})
			if len(errs) != 1 {
				t.Fatalf("errors = %v", errs)
			}
			msg := errs[0].Error()
			for _, want := range []string{"create_user", "usr", "user"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the error does not mention %q: %v", want, msg)
				}
			}
		})
	}
}

func TestBothTypesAreReportedTogether(t *testing.T) {
	// One run, both mistakes: fixing a config one error at a time is the
	// experience this avoids.
	errs := ValidateTypeReferences(&parser.Configuration{
		Types: []*validate.TypeSchema{{Name: "user"}},
		Flows: []*flow.Config{{
			Name:     "create_user",
			Validate: &flow.ValidateConfig{Input: "usr", Output: "reslt"},
		}},
	})
	if len(errs) != 2 {
		t.Fatalf("%d errors, want both: %v", len(errs), errs)
	}
	// And in a stable order, or the list reads differently every run.
	if !strings.Contains(errs[0].Error(), "validate.input") {
		t.Errorf("the input error is not first: %v", errs)
	}
}

func TestATypeThatExistsIsFine(t *testing.T) {
	errs := ValidateTypeReferences(&parser.Configuration{
		Types: []*validate.TypeSchema{{Name: "user"}, {Name: "result"}},
		Flows: []*flow.Config{{
			Name:     "create_user",
			Validate: &flow.ValidateConfig{Input: "user", Output: "result"},
		}},
	})
	if len(errs) != 0 {
		t.Errorf("refused a reference that resolves: %v", errs)
	}
}

func TestAFlowAnAspectInvokesHasToExist(t *testing.T) {
	errs := ValidateAspectFlowReferences(&parser.Configuration{
		Flows:   []*flow.Config{{Name: "create_user"}},
		Aspects: []*aspect.Config{{Name: "audit", Action: &aspect.ActionConfig{Flow: "write_audti"}}},
	})
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	msg := errs[0].Error()
	for _, want := range []string{"audit", "write_audti", "create_user"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not mention %q: %v", want, msg)
		}
	}
}

func TestAnAspectWritingToAConnectorIsNotAFlowReference(t *testing.T) {
	// An action names one or the other, never both, and the connector half is
	// checked elsewhere.
	errs := ValidateAspectFlowReferences(&parser.Configuration{
		Flows:   []*flow.Config{{Name: "create_user"}},
		Aspects: []*aspect.Config{{Name: "audit", Action: &aspect.ActionConfig{Connector: "audit_db"}}},
	})
	if len(errs) != 0 {
		t.Errorf("errors for an aspect that names no flow: %v", errs)
	}
}

func TestAConfigurationWithNoFlowsSaysSo(t *testing.T) {
	errs := ValidateAspectFlowReferences(&parser.Configuration{
		Aspects: []*aspect.Config{{Name: "audit", Action: &aspect.ActionConfig{Flow: "write_audit"}}},
	})
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "declares no flows") {
		t.Errorf("the error does not say there are none: %v", errs[0])
	}
}

func TestNothingNamedIsNotAnError(t *testing.T) {
	for name, config := range map[string]*parser.Configuration{
		"no configuration": nil,
		"an empty one":     {},
		"a flow that validates nothing": {
			Flows: []*flow.Config{{Name: "f"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if errs := ValidateTypeReferences(config); len(errs) != 0 {
				t.Errorf("type errors: %v", errs)
			}
			if errs := ValidateAspectFlowReferences(config); len(errs) != 0 {
				t.Errorf("flow errors: %v", errs)
			}
		})
	}
}
