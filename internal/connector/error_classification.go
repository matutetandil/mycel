package connector

import (
	"context"
	"errors"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/sanitize"
	myerrors "github.com/matutetandil/mycel/v3/pkg/errors"
)

// PermanentError is implemented by error types that the runtime should not
// retry. Concrete error types (HTTP 4xx, validation errors, etc.) opt in
// by exposing IsPermanent() bool. Defining the interface here avoids an
// import cycle: any subpackage already depending on `connector` can have
// its error type satisfy this interface without `connector` needing to
// import the subpackage.
type PermanentError interface {
	error
	IsPermanent() bool
}

// IsPermanent returns true when err (or any error wrapped inside it via
// fmt.Errorf("...: %w", ...) ) implements PermanentError and reports
// IsPermanent() == true.
//
// Used by:
//   - the flow-level retry budget, to break out early on errors that
//     replaying cannot fix (HTTP 4xx)
//   - MQ consumers, to decide ack-and-drop vs nack-with-requeue when the
//     flow ultimately fails — without this distinction a 4xx triggers an
//     infinite redelivery loop because the broker is told "try again"
//     while the payload itself is what the destination rejected.
func IsPermanent(err error) bool {
	if err == nil {
		return false
	}
	var p PermanentError
	if errors.As(err, &p) {
		return p.IsPermanent()
	}
	return false
}

// IsTimeoutError reports whether err represents a timeout / context deadline
// exceeded. It is used to route a timed-out request to an on_timeout handler.
// It matches both the typed context.DeadlineExceeded (what net/http surfaces
// when the client Timeout fires) and the string forms other clients produce
// ("deadline exceeded", "timeout").
//
// A timeout is a special case: the local request was abandoned, but the
// remote side may still be processing it. Blindly retrying can trigger a
// concurrent duplicate operation on the same resource — which is exactly why
// a flow may want to ack-and-drop on timeout instead of retrying.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout")
}

// ClientFault reports whether err says the caller's own request was at fault.
//
// Two things a caller depends on follow from the answer: the status it is
// given — 4xx tells it not to retry, 5xx that the failure was ours and the
// work may still be worth repeating — and, in production, whether the error
// text is quoted back to it or withheld.
//
// It is answered by type. It used to be answered, in the REST connector, by
// searching the error's message for "validation", "required" or "invalid",
// and that message carries names the author of the flow chose. A response
// field called `invalidated` contains `invalid`, so every failure raised while
// evaluating that mapping was reported as the caller's doing, whatever had
// actually gone wrong — and its text, believed safe because it had been
// labelled 400, was disclosed in production. Renaming a field was enough to
// turn an opaque error into a published one.
//
// Anything unrecognised is not the caller's fault. That is the safe default in
// both directions: the work stays retryable, and nothing internal is quoted.
func ClientFault(err error) bool {
	if err == nil {
		return false
	}

	// Input that failed the type or validator contract the flow declares.
	// The interface lives in pkg/errors because the runtime produces these
	// and cannot be imported from here.
	var invalid myerrors.Validation
	if errors.As(err, &invalid) {
		return true
	}

	// Input the sanitizer turned away is the sender's to fix. Reported as a
	// 500 it read as Mycel breaking, and 5xx is the retryable class — so a
	// client posting an oversized field retried it forever.
	return errors.Is(err, sanitize.ErrRejected)
}

// FailureMessage returns the text a caller is told about err.
//
// Outside production it is the error itself, which is the point of
// development. In production it is the error only when the caller's own
// request is what failed — otherwise the caller learns the kind of failure and
// nothing else, because an internal error message is a map of the inside of a
// service: table names, hosts, driver output, fragments of a query.
//
// The generic text is supplied by the caller of this function because each
// protocol words it differently — an HTTP status text, a SOAP faultstring, the
// error field of a socket frame.
func FailureMessage(err error, environment, generic string) string {
	if err == nil {
		return generic
	}
	if !IsProduction(environment) || ClientFault(err) {
		return err.Error()
	}
	return generic
}

// IsProduction reports whether environment names a production deployment.
//
// It is the one place the spelling is decided. The REST connector accepted
// both "production" and "prod" and was the only connector to ask at all.
func IsProduction(environment string) bool {
	return environment == "production" || environment == "prod"
}
