package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// What a step does when its call fails.
//
// Three words are implemented and anything else falls through to failing. So
// on_error = "ignore" — what somebody writing from memory reaches for — means
// the opposite of what it says, and takes the whole flow down. `default` with
// nothing to default to does the same, which is a setting that reads as
// handled and behaves as unhandled.

func stepsConfig(steps ...*flow.StepConfig) *parser.Configuration {
	return &parser.Configuration{Flows: []*flow.Config{{Name: "enrich", Steps: steps}}}
}

func TestAWordThisDoesNotImplementIsRefused(t *testing.T) {
	for _, word := range []string{"ignore", "continue", "retry", "silent"} {
		t.Run(word, func(t *testing.T) {
			errs := ValidateStepErrorHandling(stepsConfig(
				&flow.StepConfig{Name: "lookup", OnError: word},
			))
			if len(errs) != 1 {
				t.Fatalf("errors = %v", errs)
			}
			msg := errs[0].Error()
			for _, want := range []string{"enrich", "lookup", word, "skip"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the error does not mention %q: %v", want, msg)
				}
			}
		})
	}
}

func TestTheThreeWordsThatWorkAreAccepted(t *testing.T) {
	for name, step := range map[string]*flow.StepConfig{
		"fail":                   {Name: "s", OnError: "fail"},
		"skip":                   {Name: "s", OnError: "skip"},
		"default, with one":      {Name: "s", OnError: "default", Default: map[string]interface{}{"total": 0}},
		"nothing written at all": {Name: "s"},
	} {
		t.Run(name, func(t *testing.T) {
			if errs := ValidateStepErrorHandling(stepsConfig(step)); len(errs) != 0 {
				t.Errorf("refused a step that works: %v", errs)
			}
		})
	}
}

func TestADefaultWithNothingToDefaultToIsRefused(t *testing.T) {
	// It reads as handled and behaves as unhandled, which is the worse of the
	// two ways to be wrong.
	errs := ValidateStepErrorHandling(stepsConfig(
		&flow.StepConfig{Name: "pricing", OnError: "default"},
	))
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "gives no default") {
		t.Errorf("the error does not say what is missing: %v", errs[0])
	}
}

func TestSkipNeedsNoDefault(t *testing.T) {
	// skip means carry on without a result, and a default is optional there.
	if errs := ValidateStepErrorHandling(stepsConfig(
		&flow.StepConfig{Name: "s", OnError: "skip"},
	)); len(errs) != 0 {
		t.Errorf("skip without a default was refused: %v", errs)
	}
}

func TestEveryBadStepIsReportedAtOnce(t *testing.T) {
	errs := ValidateStepErrorHandling(stepsConfig(
		&flow.StepConfig{Name: "one", OnError: "ignore"},
		&flow.StepConfig{Name: "two", OnError: "default"},
		&flow.StepConfig{Name: "three", OnError: "skip"},
	))
	if len(errs) != 2 {
		t.Errorf("%d errors, want both bad ones: %v", len(errs), errs)
	}
}

func TestNoStepsIsNotAnError(t *testing.T) {
	if errs := ValidateStepErrorHandling(nil); len(errs) != 0 {
		t.Errorf("errors for no configuration: %v", errs)
	}
	if errs := ValidateStepErrorHandling(&parser.Configuration{}); len(errs) != 0 {
		t.Errorf("errors for an empty configuration: %v", errs)
	}
}
