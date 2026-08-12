package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// A cache key has one hard requirement: the same request must produce the same
// key every time. Break that and nothing errors — the entry is written under
// one key, looked up under another, and the cache silently never hits while
// still paying to store, leaving a duplicate behind on every call.

func handlerWithCache(name string, cache *flow.CacheConfig) *FlowHandler {
	return &FlowHandler{Config: &flow.Config{Name: name, Cache: cache}}
}

func TestDefaultCacheKeyIsStable(t *testing.T) {
	h := handlerWithCache("get_product", &flow.CacheConfig{})
	input := map[string]interface{}{"id": 7, "lang": "es", "region": "ar", "v": 2}

	first := h.buildCacheKey(input)
	for i := 0; i < 200; i++ {
		if got := h.buildCacheKey(input); got != first {
			t.Fatalf("the same input produced two keys:\n  %s\n  %s", first, got)
		}
	}

	// Every field has to be in there, or two different requests collide onto
	// one entry and start serving each other's responses.
	for _, want := range []string{"get_product", "id=7", "lang=es", "region=ar", "v=2"} {
		if !strings.Contains(first, want) {
			t.Errorf("key %q does not carry %q", first, want)
		}
	}
}

func TestDefaultCacheKeyDistinguishesDifferentInputs(t *testing.T) {
	h := handlerWithCache("get_product", &flow.CacheConfig{})

	a := h.buildCacheKey(map[string]interface{}{"id": 1})
	b := h.buildCacheKey(map[string]interface{}{"id": 2})
	if a == b {
		t.Errorf("two different ids produced the same key: %q", a)
	}

	// An extra field must change the key too; otherwise a narrower request
	// serves a broader one's cached answer.
	c := h.buildCacheKey(map[string]interface{}{"id": 1, "lang": "es"})
	if a == c {
		t.Errorf("adding a field did not change the key: %q", a)
	}
}

func TestBuildCacheKeyWithoutACacheBlock(t *testing.T) {
	h := handlerWithCache("no_cache", nil)
	if got := h.buildCacheKey(map[string]interface{}{"id": 1}); got != "" {
		t.Errorf("a flow with no cache block produced the key %q", got)
	}
}

func TestBuildCacheKeyFromAnExplicitTemplate(t *testing.T) {
	h := handlerWithCache("get_product", &flow.CacheConfig{Key: "product:${input.id}"})
	if got := h.buildCacheKey(map[string]interface{}{"id": 42}); got != "product:42" {
		t.Errorf("key = %q, want product:42", got)
	}
}

func TestInterpolateKey(t *testing.T) {
	h := handlerWithCache("f", &flow.CacheConfig{})
	input := map[string]interface{}{
		"id":   7,
		"lang": "es",
		"user": map[string]interface{}{"tier": "gold"},
	}

	for _, tc := range []struct{ name, template, want string }{
		{"no placeholders", "static", "static"},
		{"one placeholder", "p:${input.id}", "p:7"},
		{"two placeholders", "${input.id}/${input.lang}", "7/es"},
		{"a nested path", "t:${input.user.tier}", "t:gold"},
		{"the input prefix is optional", "p:${id}", "p:7"},
		// A missing field resolves to empty rather than to the literal
		// placeholder — two different missing fields must not produce keys
		// that look like templates.
		{"a missing field", "p:${input.nope}", "p:"},
		{"an unterminated placeholder is left alone", "p:${input.id", "p:${input.id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.interpolateKey(tc.template, input); got != tc.want {
				t.Errorf("interpolateKey(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}
}

func TestResolveInputPath(t *testing.T) {
	h := handlerWithCache("f", &flow.CacheConfig{})
	input := map[string]interface{}{
		"id":      7,
		"headers": map[string]string{"x-tenant": "acme"},
		"user":    map[string]interface{}{"profile": map[string]interface{}{"tier": "gold"}},
	}

	for _, tc := range []struct {
		path string
		want interface{}
	}{
		{"id", 7},
		{"input.id", 7},               // the prefix is stripped
		{"user.profile.tier", "gold"}, // arbitrarily deep
		{"headers.x-tenant", "acme"},  // map[string]string too
		{"missing", ""},               // absent
		{"user.missing.deeper", ""},   // absent halfway down
		{"id.deeper", ""},             // descending into a scalar
	} {
		t.Run(tc.path, func(t *testing.T) {
			if got := h.resolveInputPath(tc.path, input); got != tc.want {
				t.Errorf("resolveInputPath(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

func TestCacheKeyFromANamedCache(t *testing.T) {
	// A named cache contributes its prefix, so two flows sharing one store do
	// not collide on the flow name alone.
	h := handlerWithCache("get_product", &flow.CacheConfig{Use: "shared"})
	h.NamedCaches = map[string]*flow.NamedCacheConfig{
		"shared": {Prefix: "catalog"},
	}
	got := h.buildCacheKey(map[string]interface{}{"id": 1})
	if !strings.HasPrefix(got, "catalog:get_product") {
		t.Errorf("key = %q, want it to start with catalog:get_product", got)
	}

	// Without a prefix the flow name alone is the template.
	h2 := handlerWithCache("get_product", &flow.CacheConfig{Use: "plain"})
	h2.NamedCaches = map[string]*flow.NamedCacheConfig{"plain": {}}
	if got := h2.buildCacheKey(map[string]interface{}{"id": 1}); got != "get_product" {
		t.Errorf("key = %q, want get_product", got)
	}
}
