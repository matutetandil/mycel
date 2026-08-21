package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/aspect"
	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// A connector named by a block other than from and to.
//
// Those two were checked when the flow was registered. The twenty-odd others
// behaved three different ways, which is the worst possible answer: a dedupe
// pointing at a connector nobody declared was refused at parse time, an
// idempotency block started and failed on the first request that carried a key,
// and a cache block started and cached nothing for ever without a word —
// getCacheConnector looks the name up, gets an error, returns nil, and every
// caller reads nil as "no cache configured".
//
// The steps example in this repository referenced eight connectors it never
// declared. It validated.

func refConfig(connectors []*connector.Config, flows []*flow.Config) *parser.Configuration {
	return &parser.Configuration{Connectors: connectors, Flows: flows}
}

func declared(name, kind string) *connector.Config {
	return &connector.Config{Name: name, Type: kind}
}

func TestAConnectorNobodyDeclaredIsNamed(t *testing.T) {
	for name, tc := range map[string]struct {
		flow  *flow.Config
		where string
	}{
		"a cache that caches nowhere": {
			&flow.Config{Name: "f", Cache: &flow.CacheConfig{Storage: "typo", TTL: "5m"}},
			"cache.storage",
		},
		"an idempotency store": {
			&flow.Config{Name: "f", Idempotency: &flow.IdempotencyConfig{Storage: "typo", Key: "input.id"}},
			"idempotency.storage",
		},
		"a dedupe": {
			&flow.Config{Name: "f", Dedupe: &flow.DedupeConfig{Cache: "typo"}},
			"dedupe.cache",
		},
		"an async store": {
			&flow.Config{Name: "f", Async: &flow.AsyncConfig{Storage: "typo"}},
			"async.storage",
		},
		"a step": {
			&flow.Config{Name: "f", Steps: []*flow.StepConfig{{Name: "lookup", Connector: "typo"}}},
			`step "lookup".connector`,
		},
		"an enrichment": {
			&flow.Config{Name: "f", Enrichments: []*flow.EnrichConfig{{Connector: "typo"}}},
			"enrich[0].connector",
		},
		"a fallback": {
			&flow.Config{Name: "f", ErrorHandling: &flow.ErrorHandlingConfig{
				Fallback: &flow.FallbackConfig{Connector: "typo"},
			}},
			"error_handling.fallback.connector",
		},
		"one destination of several": {
			&flow.Config{Name: "f", MultiTo: []*flow.ToConfig{{Connector: "cache_conn"}, {Connector: "typo"}}},
			"to[1].connector",
		},
	} {
		t.Run(name, func(t *testing.T) {
			errs := ValidateConnectorReferences(refConfig(
				[]*connector.Config{declared("cache_conn", "cache")}, []*flow.Config{tc.flow}))
			if len(errs) == 0 {
				t.Fatal("accepted a name nothing declares")
			}
			if !strings.Contains(errs[0].Error(), tc.where) {
				t.Errorf("the error does not say where it was written: %v", errs[0])
			}
			if !strings.Contains(errs[0].Error(), "typo") {
				t.Errorf("the error does not name the connector: %v", errs[0])
			}
		})
	}
}

func TestAConnectorThatCannotDoTheJobIsNamed(t *testing.T) {
	// A cache that caches into a database does nothing either, and the
	// silence is identical.
	errs := ValidateConnectorReferences(refConfig(
		[]*connector.Config{declared("orders_db", "database")},
		[]*flow.Config{{Name: "f", Cache: &flow.CacheConfig{Storage: "orders_db"}}}))

	if len(errs) == 0 {
		t.Fatal("a cache pointing at a database was accepted")
	}
	if !strings.Contains(errs[0].Error(), "database") || !strings.Contains(errs[0].Error(), "cache") {
		t.Errorf("the error does not say what it is and what it should be: %v", errs[0])
	}
}

func TestTheSuggestionPointsAtWhatWasMeant(t *testing.T) {
	// A name off by a letter is what this catches most of the time.
	errs := ValidateConnectorReferences(refConfig(
		[]*connector.Config{declared("redis_cache", "cache")},
		[]*flow.Config{{Name: "f", Cache: &flow.CacheConfig{Storage: "redis_cahce"}}}))

	if len(errs) == 0 {
		t.Fatal("accepted")
	}
	if !strings.Contains(errs[0].Error(), `did you mean "redis_cache"`) {
		t.Errorf("no suggestion: %v", errs[0])
	}
}

func TestEveryDeclaredNameIsOfferedWhenNothingIsClose(t *testing.T) {
	errs := ValidateConnectorReferences(refConfig(
		[]*connector.Config{declared("memcache", "cache"), declared("api", "rest")},
		[]*flow.Config{{Name: "f", Cache: &flow.CacheConfig{Storage: "zzz"}}}))

	if len(errs) == 0 {
		t.Fatal("accepted")
	}
	if !strings.Contains(errs[0].Error(), "declared: api, memcache") {
		t.Errorf("the error offers nothing to choose from: %v", errs[0])
	}
}

func TestAConfigurationThatNamesWhatItDeclaredIsFine(t *testing.T) {
	errs := ValidateConnectorReferences(refConfig(
		[]*connector.Config{
			declared("api", "rest"),
			declared("db", "database"),
			declared("memcache", "cache"),
		},
		[]*flow.Config{{
			Name:        "f",
			From:        &flow.FromConfig{Connector: "api"},
			To:          &flow.ToConfig{Connector: "db"},
			Cache:       &flow.CacheConfig{Storage: "memcache"},
			Steps:       []*flow.StepConfig{{Name: "lookup", Connector: "db"}},
			Idempotency: &flow.IdempotencyConfig{Storage: "memcache", Key: "input.id"},
		}},
	))
	if len(errs) != 0 {
		t.Errorf("refused a configuration whose names all exist: %v", errs)
	}
}

func TestAnAspectsConnectorIsCheckedToo(t *testing.T) {
	// An aspect that audits into a name nobody declared audits nothing, and an
	// audit trail is discovered to be missing during an investigation.
	config := refConfig([]*connector.Config{declared("api", "rest")}, nil)
	config.Aspects = []*aspect.Config{{
		Name:   "audit",
		On:     []string{"create_*"},
		Action: &aspect.ActionConfig{Connector: "audit_db"},
	}}

	errs := ValidateConnectorReferences(config)
	if len(errs) == 0 {
		t.Fatal("an aspect writing to a connector nobody declared was accepted")
	}
	if !strings.Contains(errs[0].Error(), `aspect "audit"`) {
		t.Errorf("the error does not name the aspect: %v", errs[0])
	}
}

func TestANameBuiltAtRuntimeIsLeftAlone(t *testing.T) {
	// A reference carrying an expression is resolved when the flow runs, and
	// refusing it here would refuse a configuration that works.
	errs := ValidateConnectorReferences(refConfig(
		[]*connector.Config{declared("api", "rest")},
		[]*flow.Config{{Name: "f", Steps: []*flow.StepConfig{
			{Name: "lookup", Connector: "${input.tenant}_db"},
		}}}))
	if len(errs) != 0 {
		t.Errorf("refused a name built at runtime: %v", errs)
	}
}

func TestNoConfigurationHasNoReferences(t *testing.T) {
	if errs := ValidateConnectorReferences(nil); len(errs) != 0 {
		t.Errorf("errors for no configuration: %v", errs)
	}
}
