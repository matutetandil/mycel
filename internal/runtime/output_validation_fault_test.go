package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/validate"
	myerrors "github.com/matutetandil/mycel/v3/pkg/errors"
)

// Input and output validation used to share one error type. They read alike —
// the same checker, the same field messages — but they say opposite things
// about who erred, and everything a caller is told follows from that.
//
// Sharing the type meant a flow whose own answer broke the contract it
// declares answered 400: the caller was told its request was at fault when no
// request could have satisfied it, told not to retry something a retry might
// well have fixed, and handed the names of the fields of an internal record,
// because a 400 was taken as licence to quote the text.
func TestAnAnswerThatBrokeItsContractIsNotBlamedOnTheCaller(t *testing.T) {
	failed := []validate.Error{{Field: "ssn", Message: "field is required", Code: "required"}}

	// Input: the caller's, and recognised as such through the interface the
	// connectors consult.
	var caller myerrors.Validation
	if !errors.As(error(&ValidationError{Errors: failed}), &caller) {
		t.Error("a failed request was not recognised as the caller's doing")
	}

	// Output: ours, and deliberately not recognised as the caller's.
	var ours myerrors.Validation
	if errors.As(error(&OutputValidationError{Errors: failed}), &ours) {
		t.Error("the service breaking its own output contract was blamed on the caller")
	}
}

func TestAnOutputFailureStillSaysWhatBroke(t *testing.T) {
	// Not being the caller's fault is not a reason to be silent: a developer
	// and a log still need the field.
	err := &OutputValidationError{Errors: []validate.Error{
		{Field: "ssn", Message: "field is required", Code: "required"},
	}}

	if got := err.Error(); got == "" {
		t.Error("an output failure said nothing at all")
	} else if !strings.Contains(got, "ssn") || !strings.Contains(got, "output") {
		t.Errorf("an output failure does not name the field or say which side it was on: %q", got)
	}

	// And one with nothing in it still reads as an output failure.
	if got := (&OutputValidationError{}).Error(); !strings.Contains(got, "output") {
		t.Errorf("an empty output failure = %q", got)
	}
}
