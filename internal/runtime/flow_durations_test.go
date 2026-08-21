package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// A duration nobody can read.
//
// Every one of these is parsed at the point of use with the error discarded,
// so a cache TTL that does not parse means "no TTL, use the connector default"
// and a timeout that does not parse means "the default timeout". The flow that
// meant to cache for five minutes caches for however long the connector feels
// like, and nothing says so — not at startup, not in a log, not ever.

func flowWith(f *flow.Config) *parser.Configuration {
	f.Name = "the_flow"
	return &parser.Configuration{Flows: []*flow.Config{f}}
}

func TestADurationNobodyCanReadIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		config *flow.Config
		names  string // what the message must mention
	}{
		"five minutes, written as a number of seconds": {
			&flow.Config{Cache: &flow.CacheConfig{Storage: "redis", TTL: "300"}},
			"cache.ttl",
		},
		"five minutes, written in words": {
			&flow.Config{Cache: &flow.CacheConfig{Storage: "redis", TTL: "5 minutes"}},
			"cache.ttl",
		},
		"a lock timeout": {
			&flow.Config{Lock: &flow.LockConfig{Key: "input.id", Timeout: "30 sec"}},
			"lock.timeout",
		},
		"a dedupe window": {
			&flow.Config{Dedupe: &flow.DedupeConfig{TTL: "1 day"}},
			"dedupe.ttl",
		},
		"a retry delay": {
			&flow.Config{ErrorHandling: &flow.ErrorHandlingConfig{
				Retry: &flow.RetryConfig{Delay: "1 second"},
			}},
			"error_handling.retry.delay",
		},
	} {
		t.Run(name, func(t *testing.T) {
			errs := ValidateFlowDurations(flowWith(tc.config))
			if len(errs) == 0 {
				t.Fatal("accepted, so it will be discarded at the point of use instead")
			}
			// The message has to say which one and where, or somebody is
			// reading a whole config wondering which duration it means.
			if !strings.Contains(errs[0].Error(), tc.names) {
				t.Errorf("the error does not name the attribute: %v", errs[0])
			}
			if !strings.Contains(errs[0].Error(), "the_flow") {
				t.Errorf("the error does not name the flow: %v", errs[0])
			}
		})
	}
}

func TestADurationThatReadsIsAccepted(t *testing.T) {
	for name, cfg := range map[string]*flow.Config{
		"seconds":    {Cache: &flow.CacheConfig{TTL: "30s"}},
		"minutes":    {Cache: &flow.CacheConfig{TTL: "5m"}},
		"hours":      {Cache: &flow.CacheConfig{TTL: "24h"}},
		"a compound": {Cache: &flow.CacheConfig{TTL: "1h30m"}},
		// This language has days and weeks, and the examples in this
		// repository use them. A validator built on the standard library's
		// parser would have called every one of them invalid.
		"days":                   {Cache: &flow.CacheConfig{TTL: "30d"}},
		"weeks":                  {Dedupe: &flow.DedupeConfig{TTL: "2w"}},
		"milliseconds":           {Coordinate: &flow.CoordinateConfig{Timeout: "500ms"}},
		"nothing written at all": {Cache: &flow.CacheConfig{Storage: "redis"}},
		"no blocks at all":       {},
	} {
		t.Run(name, func(t *testing.T) {
			if errs := ValidateFlowDurations(flowWith(cfg)); len(errs) > 0 {
				t.Errorf("refused a duration that reads: %v", errs)
			}
		})
	}
}

func TestEveryBadDurationIsReportedAtOnce(t *testing.T) {
	// Not one per run: somebody fixing a config should see all of them.
	errs := ValidateFlowDurations(flowWith(&flow.Config{
		Cache:      &flow.CacheConfig{TTL: "300"},
		Dedupe:     &flow.DedupeConfig{TTL: "one day"},
		Coordinate: &flow.CoordinateConfig{Timeout: "soon"},
	}))
	if len(errs) != 3 {
		t.Errorf("%d errors, want all three: %v", len(errs), errs)
	}
}

func TestNoConfigurationIsNotAnError(t *testing.T) {
	if errs := ValidateFlowDurations(nil); len(errs) != 0 {
		t.Errorf("errors for no configuration: %v", errs)
	}
}
