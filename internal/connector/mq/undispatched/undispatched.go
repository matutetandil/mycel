// Package undispatched reports messages that reached a consumer and matched no
// flow handler.
//
// Every message queue driver has the same hole: the broker routed a message
// here, so something upstream expects it to be handled, but no flow claims it.
// The message is then dropped — nacked without requeue on RabbitMQ, offset
// committed on Kafka, discarded on Redis pub/sub — and until now that happened
// with a WARN line and no metric, so the queue showed deliveries with no acks
// and nothing else moved.
//
// The usual cause is a `from { operation = "..." }` that matches no key
// actually delivered: on a message queue source `operation` reads like an
// operation *name* but is a subscription *pattern*, and an invented value
// matches nothing.
//
// It lives in its own package because the drivers cannot share code through
// their parent: internal/connector/mq imports each driver to build them, so a
// driver importing internal/connector/mq would close the cycle.
package undispatched

import (
	"log/slog"
	"sort"
	"sync"

	"github.com/matutetandil/mycel/internal/metrics"
)

// Event describes one dropped message.
type Event struct {
	// Connector is the connector name from the HCL config.
	Connector string

	// Driver is "rabbitmq", "kafka" or "redis".
	Driver string

	// Target is where the message arrived: queue, topic or channel.
	Target string

	// Key is what was matched against the registered handler patterns: the
	// routing key on RabbitMQ, the topic on Kafka, the channel on Redis.
	Key string

	// Patterns are the handler patterns flows registered. The difference
	// between these and Key is the diagnosis, so they are logged alongside.
	Patterns []string

	// Consequence spells out what the driver just did with the message,
	// which differs enough between brokers to be worth stating outright
	// (e.g. Kafka commits the offset, so it will not be redelivered).
	Consequence string
}

// Reporter logs and counts dropped messages, at most one log line per key.
//
// A misconfigured consumer drops *every* message with that key, so logging each
// one buries the rest of the log while adding nothing: the first line already
// carries the full diagnosis. Repeats only move the counter. Safe for
// concurrent use; embed it by value in a connector.
type Reporter struct {
	reported sync.Map
}

// Report records ev against the undispatched counter and, the first time a
// given key is seen, logs it at error level.
//
// Error rather than warning: a message nobody handles is a misconfiguration
// with data loss attached, not a routine event.
func (r *Reporter) Report(logger *slog.Logger, ev Event) {
	metrics.Default().RecordUndispatchedMessage(ev.Connector, ev.Driver, ev.Target, ev.Key)

	if _, seen := r.reported.LoadOrStore(ev.Key, struct{}{}); seen {
		return
	}

	if logger == nil {
		logger = slog.Default()
	}

	attrs := []interface{}{
		"connector", ev.Connector,
		"driver", ev.Driver,
		"target", ev.Target,
		"key", ev.Key,
		"registered_patterns", ev.Patterns,
	}
	if ev.Consequence != "" {
		attrs = append(attrs, "consequence", ev.Consequence)
	}
	attrs = append(attrs, "hint",
		"a flow's from{} operation must match the key; omit it to accept every message")

	logger.Error("message dropped: no flow handles this key", attrs...)
}

// SortedPatterns returns the keys of a handler map, sorted so diagnostics are
// stable across runs. Callers hold their own lock.
func SortedPatterns[T any](handlers map[string]T) []string {
	patterns := make([]string, 0, len(handlers))
	for pattern := range handlers {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

// ReportNoHandlers logs that a consumer is starting with nothing registered to
// receive its messages, so every delivery will be dropped at lookup.
//
// Called from each driver's start path rather than from configuration
// analysis: declaring a source schema only means a connector *can* be a
// source, and a database used purely as a write target or an MQ connector with
// only a publisher block are indistinguishable from an unused consumer until
// one actually starts consuming.
func ReportNoHandlers(logger *slog.Logger, connectorName, driver, target string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("consumer has no flow handlers: every message will be dropped",
		"connector", connectorName,
		"driver", driver,
		"target", target,
		"hint", "a flow needs from { connector = \""+connectorName+"\" } to receive these messages",
	)
}
