package optimizer

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/flow"
)

// Step skipping matches the fields a GraphQL query asked for against the names
// of the transform's mappings. That holds while the field returns an object
// whose fields are those mappings. It does not hold when the field returns a
// list: the client then asks for the fields of the element — `name`, `image` —
// and never for the name of the mapping that holds the list. Nothing matched,
// no step was marked as needed, every step was skipped, and the transform
// evaluated against nulls. The answer was an empty list, HTTP 200, nothing in
// the log.
//
// An unmatched set means the requested names are not in the mappings'
// namespace, so it says "cannot tell", not "nothing is needed".

func steps(names ...string) []*flow.StepConfig {
	out := make([]*flow.StepConfig, 0, len(names))
	for _, n := range names {
		out = append(out, &flow.StepConfig{Name: n})
	}
	return out
}

func TestAnUnmatchedFieldSetRunsEveryStep(t *testing.T) {
	for name, c := range map[string]struct {
		mappings map[string]string
		asked    []string
	}{
		"a list field: the element's fields are asked for, never the mapping's name": {
			map[string]string{"items": "as_list(step.rows).map(r, {'name': r.name})"},
			[]string{"name"},
		},
		"the same, with a second mapping beside it": {
			map[string]string{
				"items": "as_list(step.rows).map(r, {'name': r.name})",
				"count": "size(as_list(step.rows))",
			},
			[]string{"name", "image"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			o := NewStepOptimizer(steps("rows"), ExtractTransformExpressions(c.mappings), c.asked)
			if needed := o.AnalyzeDependencies(); !needed["rows"] {
				t.Errorf("the step was skipped: %v", needed)
			}
			if skippable := o.GetSkippableSteps(); len(skippable) != 0 {
				t.Errorf("skippable = %v, want none", skippable)
			}
		})
	}
}

// The optimisation is kept for the shape it was written for: an object field,
// where the requested names and the mapping names are the same namespace.
func TestTheOptimisationSurvivesForAnObjectField(t *testing.T) {
	mappings := map[string]string{
		"profile": "step.user.name",
		"orders":  "as_list(step.orders)",
	}

	o := NewStepOptimizer(steps("user", "orders"), ExtractTransformExpressions(mappings), []string{"profile"})
	needed := o.AnalyzeDependencies()
	if !needed["user"] {
		t.Error("the step the requested field depends on was skipped")
	}
	if needed["orders"] {
		t.Error("a step nothing asked for was executed; the optimisation is gone")
	}

	// A nested request names its top field, which is the mapping.
	o = NewStepOptimizer(steps("user", "orders"), ExtractTransformExpressions(mappings), []string{"orders.total"})
	needed = o.AnalyzeDependencies()
	if !needed["orders"] || needed["user"] {
		t.Errorf("needed = %v, want only the orders step", needed)
	}
}

// A mapping that reads no step is still a matched name, so a flow whose steps
// nothing reads keeps skipping them exactly as before.
func TestAMappingThatReadsNoStepStillCounts(t *testing.T) {
	mappings := map[string]string{"greeting": "'hello ' + input.name"}
	o := NewStepOptimizer(steps("unused"), ExtractTransformExpressions(mappings), []string{"greeting"})
	if needed := o.AnalyzeDependencies(); needed["unused"] {
		t.Errorf("a step no mapping reads was executed: %v", needed)
	}
}
