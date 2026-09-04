package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/aspect"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// A cache key is a template, not CEL. A key written as CEL is used verbatim
// and nothing fails: every request shares one entry, and the first record
// cached is what everyone gets back for the life of the TTL. The guide showed
// the CEL form for a long time, so this is the mistake people copy.

func cachedFlow(key string) *parser.Configuration {
	return &parser.Configuration{Flows: []*flow.Config{{
		Name:  "get_product",
		Cache: &flow.CacheConfig{Storage: "redis", Key: key},
	}}}
}

func TestACacheKeyWrittenAsCELIsRefused(t *testing.T) {
	for _, key := range []string{
		"'product:' + input.id",
		`"product:" + input.id`,
		"'users:' + input.id + ':orders:' + input.status",
		"input.id",
		"product:input.id",
	} {
		t.Run(key, func(t *testing.T) {
			errs := ValidateCacheKeys(cachedFlow(key))
			if len(errs) != 1 {
				t.Fatalf("errors = %v, want exactly one for the key", errs)
			}
			msg := errs[0].Error()
			for _, want := range []string{"get_product", key, "template", "${"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not mention %q", msg, want)
				}
			}
		})
	}
}

func TestTheRefusalSaysWhatToWriteInstead(t *testing.T) {
	errs := ValidateCacheKeys(cachedFlow("'users:' + input.id + ':orders:' + input.status"))
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	if !strings.Contains(errs[0].Error(), "`users:${input.id}:orders:${input.status}`") {
		t.Errorf("error %q does not show the template form of the key", errs[0])
	}
}

func TestATemplateOrAConstantKeyPasses(t *testing.T) {
	for _, key := range []string{
		"",
		"product:${input.id}",
		"users:${input.id}:orders:${input.status}",
		"products:list",
		"${input.body.sku}",
	} {
		t.Run(key, func(t *testing.T) {
			if errs := ValidateCacheKeys(cachedFlow(key)); len(errs) != 0 {
				t.Errorf("a template key was refused: %v", errs)
			}
		})
	}
}

func TestAnAspectCacheKeyIsHeldToTheSameRule(t *testing.T) {
	config := &parser.Configuration{Aspects: []*aspect.Config{{
		Name:  "cache_reads",
		Cache: &aspect.CacheConfig{Storage: "redis", Key: "'products:list'"},
	}}}
	errs := ValidateCacheKeys(config)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "cache_reads") {
		t.Fatalf("errors = %v, want one naming the aspect", errs)
	}
	if !strings.Contains(errs[0].Error(), "`products:list`") {
		t.Errorf("error %q does not show the constant without its quotes", errs[0])
	}
}

func TestNoCacheMeansNothingToCheck(t *testing.T) {
	config := &parser.Configuration{Flows: []*flow.Config{{Name: "plain"}, nil}}
	if errs := ValidateCacheKeys(config); len(errs) != 0 {
		t.Errorf("errors = %v", errs)
	}
	if errs := ValidateCacheKeys(nil); len(errs) != 0 {
		t.Errorf("errors = %v", errs)
	}
}
