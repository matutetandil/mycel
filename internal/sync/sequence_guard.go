// Package sync — sequence guard primitive.
//
// SequenceGuard prevents out-of-order delivery from regressing per-resource
// state by remembering the last monotonic sequence number processed for a
// given key. The classic use case is RabbitMQ retries / fan-out where an
// older message could be re-delivered after a newer one has already been
// applied: the guard rejects the older one without re-processing it.
//
// Atomicity is the caller's responsibility — wrap the same key in an
// outer Lock to make the read-decide-write pattern safe across concurrent
// workers. Without the outer lock, two workers can read the same stored
// value concurrently and both decide to proceed.
package sync

import (
	"context"
	"errors"
	"time"
)

// SequenceGuard is the storage interface for monotonic sequence dedup.
type SequenceGuard interface {
	// Read returns the last stored sequence for key, or (0, false) if not set.
	// An unset key is not an error — it means "no message has been processed
	// for this resource yet" and the caller should proceed.
	Read(ctx context.Context, key string) (int64, bool, error)

	// Write stores sequence for key with the given TTL. A zero TTL means no
	// expiry. Overwrites any existing value — callers must compare and only
	// call Write when the new sequence is strictly greater than the stored.
	Write(ctx context.Context, key string, sequence int64, ttl time.Duration) error

	// Close cleans up resources.
	Close() error
}

// SequenceGuardOnOlder defines what the runtime should do when the current
// sequence is not strictly greater than the stored one.
type SequenceGuardOnOlder string

const (
	// OnOlderAck silently ack the delivery. The default — appropriate when
	// older messages are by definition superseded.
	OnOlderAck SequenceGuardOnOlder = "ack"
	// OnOlderReject route the delivery to the DLQ.
	OnOlderReject SequenceGuardOnOlder = "reject"
	// OnOlderRequeue put the delivery back on the queue. Rarely useful for
	// sequence dedup but supported for symmetry with the filter block.
	OnOlderRequeue SequenceGuardOnOlder = "requeue"
)

// ParseOnOlder normalizes an OnOlder string. Unknown values fall back to
// OnOlderAck — the safe default.
func ParseOnOlder(s string) SequenceGuardOnOlder {
	switch s {
	case "ack":
		return OnOlderAck
	case "reject":
		return OnOlderReject
	case "requeue":
		return OnOlderRequeue
	default:
		return OnOlderAck
	}
}

// FlowSequenceGuardConfig is the flow-level config for a sequence guard.
// Mirrors flow.SequenceGuardConfig to avoid an import cycle.
type FlowSequenceGuardConfig struct {
	Storage  *SyncStorageConfig
	Key      string
	Sequence string
	OnOlder  string
	TTL      string
}

// SequenceGuardSkippedError is returned when the current sequence is not
// strictly greater than the stored one. The caller is expected to translate
// this into an MQ ack/reject/requeue based on the configured OnOlder policy.
type SequenceGuardSkippedError struct {
	Key            string
	StoredSequence int64
	CurrentSequence int64
	Policy         SequenceGuardOnOlder
}

func (e *SequenceGuardSkippedError) Error() string {
	return "sequence guard: current sequence not greater than stored"
}

// IsSequenceGuardSkipped is a sentinel-style helper for the runtime to
// detect skips without depending on errors.As at every call site.
func IsSequenceGuardSkipped(err error) (*SequenceGuardSkippedError, bool) {
	var s *SequenceGuardSkippedError
	if errors.As(err, &s) {
		return s, true
	}
	return nil, false
}
