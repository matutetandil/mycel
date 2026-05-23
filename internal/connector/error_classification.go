package connector

import (
	"context"
	"errors"
	"strings"
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
