package connector

import (
	"errors"
	"fmt"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/sanitize"
)

// invalidInput stands in for the runtime's validation error: the flow declared
// a contract for its input and the request did not meet it.
type invalidInput struct{ fields []string }

func (e *invalidInput) Error() string              { return "validation error on 'email': field is required" }
func (e *invalidInput) ValidationFields() []string { return e.fields }

func TestWhoseFaultAFailureWasIsNotReadOffTheMessage(t *testing.T) {
	// The name of a response field ends up in the message of any error raised
	// while evaluating it. `invalidated` contains `invalid`, and that was
	// enough to have every such failure reported as the caller's doing — a
	// 400, and with it the licence to quote the text back in production.
	//
	// The name is not contrived: examples/cache ships a flow whose response
	// is `invalidated = "size(step.affected ?? [])"`.
	named := fmt.Errorf("response transform error: failed to evaluate expression for " +
		"'invalidated': got 'types.String', expected iterable type")
	if ClientFault(named) {
		t.Error("a failure was blamed on the caller because a field the flow author named contained 'invalid'")
	}

	// The same failure with the field called something else. The two must
	// agree: nothing about which of them happened is the caller's doing.
	other := fmt.Errorf("response transform error: failed to evaluate expression for " +
		"'count': got 'types.String', expected iterable type")
	if ClientFault(named) != ClientFault(other) {
		t.Error("two identical failures were classified differently because of a field name")
	}

	// Driver text is the other half of the same problem: a connection error
	// reading `invalid connection` would have been handed to the caller, and
	// database errors are the ones that carry fragments of a query.
	if ClientFault(errors.New("pq: invalid connection to host db-primary.internal")) {
		t.Error("a driver error was blamed on the caller")
	}
}

func TestWhatIsRecognisedAsTheCallersOwnDoing(t *testing.T) {
	// Input that failed the contract the flow declares, recognised through
	// the interface in pkg/errors rather than through its text.
	if !ClientFault(&invalidInput{fields: []string{"email"}}) {
		t.Error("a validation failure was not recognised as the caller's")
	}

	// Still recognised through a wrap, because that is how it reaches the
	// connector.
	if !ClientFault(fmt.Errorf("flow \"create\": %w", &invalidInput{})) {
		t.Error("a wrapped validation failure was not recognised")
	}

	// Input the sanitizer turned away. Reported as a 500 it read as Mycel
	// breaking, and 5xx is the retryable class — so a client posting an
	// oversized field retried it forever.
	if !ClientFault(fmt.Errorf("field too large: %w", sanitize.ErrRejected)) {
		t.Error("a sanitizer rejection was not recognised as the caller's")
	}

	// Anything unrecognised is ours. That keeps the work retryable and keeps
	// the text unpublished.
	if ClientFault(errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")) {
		t.Error("an unrecognised failure was blamed on the caller")
	}
	if ClientFault(nil) {
		t.Error("no error at all was blamed on the caller")
	}
}

func TestHowMuchOfAFailureIsQuotedBack(t *testing.T) {
	internal := errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")

	// In production the caller learns the kind of failure and nothing else.
	if got := FailureMessage(internal, "production", "Internal Server Error"); got != "Internal Server Error" {
		t.Errorf("an internal failure was described to a production caller: %q", got)
	}
	// "prod" is the same deployment by a shorter name.
	if got := FailureMessage(internal, "prod", "Internal Server Error"); got != "Internal Server Error" {
		t.Errorf("the short spelling of production was not recognised: %q", got)
	}

	// Its own request is a different matter: it cannot be fixed without
	// being told what was wrong with it.
	if got := FailureMessage(&invalidInput{}, "production", "Internal Server Error"); got == "Internal Server Error" {
		t.Error("a caller was not told what was wrong with its own request")
	}

	// Outside production everything is shown, which is the point of
	// development.
	if got := FailureMessage(internal, "development", "Internal Server Error"); got != internal.Error() {
		t.Errorf("a developer was not told what actually failed: %q", got)
	}
}
