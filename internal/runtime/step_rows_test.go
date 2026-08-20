package runtime

import (
	"reflect"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// What a step leaves behind when its lookup matches nothing.
//
// It used to leave the empty list, so every later reference to
// `step.user.name` indexed a list with a string, and that is what came back to
// the caller: "unsupported index type 'string' in list" — naming neither the
// step nor the fact that its query found no row. A lookup that finds nothing
// is an ordinary thing for a flow to have to handle, and it read like a
// mistake in the expression.
func TestAStepThatFindsNothingLeavesNothing(t *testing.T) {
	step := &flow.StepConfig{Name: "user"}

	if got := stepRows(step, nil); got != nil {
		t.Errorf("no rows became %#v, want nothing", got)
	}
	if got := stepRows(step, []map[string]interface{}{}); got != nil {
		t.Errorf("an empty result became %#v, want nothing", got)
	}
}

// And a step that declares a default gets it, which is what a default is for:
// the row is missing, and the flow says what to use instead.
func TestAStepThatFindsNothingFallsBackToItsDefault(t *testing.T) {
	step := &flow.StepConfig{
		Name:    "pricing",
		Default: map[string]interface{}{"price": 0, "currency": "USD"},
	}

	got := stepRows(step, nil)
	if !reflect.DeepEqual(got, step.Default) {
		t.Errorf("no rows became %#v, want the declared default", got)
	}
}

// One row is that row, so a field of it can be read; several stay a list.
func TestOneRowIsTheRowAndSeveralAreAList(t *testing.T) {
	step := &flow.StepConfig{Name: "user"}

	one := []map[string]interface{}{{"id": 1, "name": "Ada"}}
	if got := stepRows(step, one); !reflect.DeepEqual(got, one[0]) {
		t.Errorf("a single row became %#v, want the row itself", got)
	}

	several := []map[string]interface{}{{"id": 1}, {"id": 2}}
	if got := stepRows(step, several); !reflect.DeepEqual(got, several) {
		t.Errorf("several rows became %#v, want the list", got)
	}
}
