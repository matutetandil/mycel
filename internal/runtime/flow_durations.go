package runtime

import (
	"fmt"

	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// Durations that a flow writes and the runtime reads.
//
// Every one of these is read with a parse whose error is discarded: a cache
// TTL that does not parse falls through to "use the connector default", a
// timeout that does not parse falls through to whatever the default is. So
// `ttl = "5 minutes"` — or `ttl = 300`, meaning five minutes and now readable
// as the string "300" — is not a cache that lasts five minutes. It is a cache
// with no TTL at all, and nothing anywhere says so.
//
// Checked at startup, where the answer is a service that does not start rather
// than one that quietly caches for ever.

// durationField is one duration somebody wrote, and where they wrote it.
type durationField struct {
	where string // flow "x": cache.ttl
	value string
}

// ValidateFlowDurations reports every duration in a flow that cannot be read.
func ValidateFlowDurations(config *parser.Configuration) []error {
	if config == nil {
		return nil
	}

	var errs []error
	for _, f := range config.Flows {
		if f == nil {
			continue
		}
		for _, d := range flowDurations(f) {
			if d.value == "" {
				continue
			}
			// flow.ParseDuration, not the standard library's: this
			// configuration language has days and weeks in it, and the
			// examples in this repository use them.
			if _, err := flow.ParseDuration(d.value); err != nil {
				errs = append(errs, fmt.Errorf(
					`flow %q: %s = %q is not a duration; write it with a unit, such as "30s", "5m", "24h" or "30d"`,
					f.Name, d.where, d.value))
			}
		}
	}
	return errs
}

// flowDurations lists the durations a flow config carries.
func flowDurations(f *flow.Config) []durationField {
	var fields []durationField

	add := func(where, value string) {
		fields = append(fields, durationField{where: where, value: value})
	}

	if f.Cache != nil {
		add("cache.ttl", f.Cache.TTL)
	}
	if f.Async != nil {
		add("async.ttl", f.Async.TTL)
	}
	if f.Dedupe != nil {
		add("dedupe.ttl", f.Dedupe.TTL)
	}
	if f.Idempotency != nil {
		add("idempotency.ttl", f.Idempotency.TTL)
	}
	if f.Lock != nil {
		add("lock.timeout", f.Lock.Timeout)
	}
	if f.Semaphore != nil {
		add("semaphore.timeout", f.Semaphore.Timeout)
	}
	if f.Coordinate != nil {
		add("coordinate.timeout", f.Coordinate.Timeout)
	}
	if f.ErrorHandling != nil && f.ErrorHandling.Retry != nil {
		add("error_handling.retry.delay", f.ErrorHandling.Retry.Delay)
		add("error_handling.retry.max_delay", f.ErrorHandling.Retry.MaxDelay)
	}

	return fields
}
