package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/aspect"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// An aspect that cannot run is worse than one that is missing: it parses, it
// validates, the service starts, and the cross-cutting thing it was there for —
// the audit trail, the cache, the retry — simply never happens. mycel validate
// and startup both run this check, and nothing ran the check itself.

func aspects(configs ...*aspect.Config) *parser.Configuration {
	return &parser.Configuration{Aspects: configs}
}

func TestAnAspectThatCanRunIsAccepted(t *testing.T) {
	err := ValidateAspects(aspects(&aspect.Config{
		Name: "audit", When: aspect.After, On: []string{"create_*", "update_*"},
		Action: &aspect.ActionConfig{Connector: "audit_db", Operation: "INSERT"},
	}))
	if err != nil {
		t.Errorf("an aspect that can run was refused: %v", err)
	}
}

func TestAnAspectWithNothingToDoIsRefused(t *testing.T) {
	// The one that hurts: it matches flows, it runs, and it does nothing at
	// all — so the audit trail somebody believes they configured is empty.
	err := ValidateAspects(aspects(&aspect.Config{
		Name: "audit", When: aspect.After, On: []string{"*"},
	}))
	if err == nil {
		t.Fatal("an aspect with no action, cache, rate limit or response was accepted")
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Errorf("error = %q, want it to name the aspect", err)
	}
}

func TestAnAspectMatchingNothingIsRefused(t *testing.T) {
	// No patterns means it never runs, which is the same as not writing it.
	err := ValidateAspects(aspects(&aspect.Config{
		Name: "audit", When: aspect.After,
		Action: &aspect.ActionConfig{Connector: "audit_db"},
	}))
	if err == nil {
		t.Error("an aspect that matches no flow was accepted")
	}
}

func TestAnAspectWithNoNameIsRefused(t *testing.T) {
	err := ValidateAspects(aspects(&aspect.Config{
		When: aspect.After, On: []string{"*"},
		Action: &aspect.ActionConfig{Connector: "audit_db"},
	}))
	if err == nil {
		t.Error("an aspect with no name was accepted")
	}
}

func TestAnAspectWithNoMomentIsRefused(t *testing.T) {
	// before, after, around, on_error, on_drop — without one there is nothing
	// to hang it on.
	err := ValidateAspects(aspects(&aspect.Config{
		Name: "audit", On: []string{"*"},
		Action: &aspect.ActionConfig{Connector: "audit_db"},
	}))
	if err == nil {
		t.Error("an aspect with no moment to run at was accepted")
	}
}

func TestAResponseCanOnlyBeChangedAfterTheFlowHasRun(t *testing.T) {
	// Before it runs there is no response to change, so writing one there is a
	// mistake with no symptom at runtime.
	err := ValidateAspects(aspects(&aspect.Config{
		Name: "envelope", When: aspect.Before, On: []string{"*"},
		Response: &aspect.ResponseConfig{Headers: map[string]string{"X-Served-By": "mycel"}},
	}))
	if err == nil {
		t.Error("an aspect changing the response before the flow ran was accepted")
	}
}

func TestAroundIsOnlyForThingsThatWrapTheFlow(t *testing.T) {
	// around is usually a cache or a circuit breaker, and an action is allowed
	// there on purpose — the check says so in as many words, so this pins the
	// decision rather than assuming the stricter reading.
	withAction := ValidateAspects(aspects(&aspect.Config{
		Name: "audit", When: aspect.Around, On: []string{"*"},
		Action: &aspect.ActionConfig{Connector: "audit_db"},
	}))
	if withAction != nil {
		t.Errorf("an action around a flow was refused: %v", withAction)
	}

	accepted := ValidateAspects(aspects(&aspect.Config{
		Name: "cached", When: aspect.Around, On: []string{"get_*"},
		Cache: &aspect.CacheConfig{Storage: "redis_cache", TTL: "5m"},
	}))
	if accepted != nil {
		t.Errorf("a cache around a flow was refused: %v", accepted)
	}
}

func TestEveryAspectIsChecked(t *testing.T) {
	// Not just the first: a configuration with one good aspect and one broken
	// one has to be refused.
	err := ValidateAspects(aspects(
		&aspect.Config{
			Name: "audit", When: aspect.After, On: []string{"*"},
			Action: &aspect.ActionConfig{Connector: "audit_db"},
		},
		&aspect.Config{Name: "broken", When: aspect.After, On: []string{"*"}},
	))
	if err == nil {
		t.Error("a configuration whose second aspect cannot run was accepted")
	}
}

func TestNoAspectsIsNothingToCheck(t *testing.T) {
	if err := ValidateAspects(aspects()); err != nil {
		t.Errorf("err = %v", err)
	}
	if err := ValidateAspects(nil); err != nil {
		t.Errorf("err = %v", err)
	}
}
