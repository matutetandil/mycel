package connector

import "errors"

// Disposition is the broker-level outcome a failed flow asks its source
// consumer to apply. It is chosen declaratively by error_handling's per-class
// handlers (on_timeout / on_error), as opposed to being inferred from
// IsPermanent.
type Disposition string

const (
	// DispositionAck acknowledges (drops) the message without retrying or
	// requeueing. Used when the work is idempotent and an upstream will
	// redeliver duplicates — so dropping loses nothing and avoids replaying
	// a request whose remote side may still be in flight.
	DispositionAck Disposition = "ack"

	// DispositionRequeue nacks with requeue so the broker redelivers later.
	DispositionRequeue Disposition = "requeue"

	// DispositionReject nacks without requeue, routing the message to a dead
	// letter queue if one is configured.
	DispositionReject Disposition = "reject"
)

// DispositionError wraps a flow error with an explicit broker disposition.
// MQ consumers detect it (via GetDisposition) and ack / requeue / reject
// accordingly, instead of inferring the outcome from IsPermanent.
//
// It also satisfies PermanentError so that the retry budget and any consumer
// path that only understands permanent-vs-transient still behaves correctly:
// ack and reject are terminal (stop redelivery), requeue is not.
type DispositionError struct {
	// Err is the underlying flow error.
	Err error
	// Disposition is the broker outcome to apply.
	Disposition Disposition
}

func (e *DispositionError) Error() string { return e.Err.Error() }

func (e *DispositionError) Unwrap() error { return e.Err }

// IsPermanent reports ack and reject as permanent (terminal) and requeue as
// transient. This keeps the flow retry budget and any unmodified consumer
// correct even before they learn to read the explicit disposition.
func (e *DispositionError) IsPermanent() bool {
	return e.Disposition == DispositionAck || e.Disposition == DispositionReject
}

// NewDispositionError wraps err with the given broker disposition.
func NewDispositionError(err error, d Disposition) *DispositionError {
	return &DispositionError{Err: err, Disposition: d}
}

// GetDisposition returns the broker disposition carried by err (or by any
// error it wraps via %w), if any. The second return value reports whether a
// disposition was found.
func GetDisposition(err error) (Disposition, bool) {
	var d *DispositionError
	if errors.As(err, &d) {
		return d.Disposition, true
	}
	return "", false
}
