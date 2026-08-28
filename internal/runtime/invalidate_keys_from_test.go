package runtime

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// newInvalidateHandler wires the after block against an in-memory cache, with
// the step-results slot the write handlers fill.
func newInvalidateHandler(t *testing.T, inv *flow.InvalidateConfig) (*FlowHandler, *memory.Connector, func()) {
	t.Helper()

	memCache := memory.New("redis", &types.Config{Driver: "memory"})
	if err := memCache.Connect(context.Background()); err != nil {
		t.Fatalf("cache connect: %v", err)
	}
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	registry := connector.NewRegistry()
	registry.Replace("redis", memCache)

	h := &FlowHandler{
		Config: &flow.Config{
			Name:  "url_rewrite_refresh",
			After: &flow.AfterConfig{Invalidate: inv},
		},
		Connectors:     registry,
		CacheConnector: memCache,
		Transformer:    tr,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, memCache, func() { _ = memCache.Close(context.Background()) }
}

// seed puts entries in the cache and returns the keys that survive.
func remaining(t *testing.T, c *memory.Connector, keys ...string) []string {
	t.Helper()
	var left []string
	for _, k := range keys {
		if _, found, err := c.Get(context.Background(), k); err == nil && found {
			left = append(left, k)
		}
	}
	sort.Strings(left)
	return left
}

// ctxWithSteps is what the write path builds: the slots the after block reads.
func ctxWithSteps(steps map[string]interface{}, output map[string]interface{}) context.Context {
	ctx := context.Background()
	out := &flow.OutputSlot{}
	out.Set(output)
	ctx = flow.WithOutputCapture(ctx, out)
	st := &flow.StepSlot{}
	st.Set(steps)
	return flow.WithStepCapture(ctx, st)
}

// The case from the report: the set of keys to drop is what a query returned,
// so its size is a function of the data. `keys` is one key out per template
// in, fixed when the configuration is parsed, so this could not be written.
func TestInvalidate_KeysFromAQueryTheFlowJustRan(t *testing.T) {
	h, cache, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage:  "redis",
		KeysFrom: "step.affected_paths.map(r, 'url-rewrite-' + r.store_code + '-' + r.path)",
	})
	defer done()

	all := []string{
		"url-rewrite-us-widget",
		"url-rewrite-us-widget-old",
		"url-rewrite-fr-widget-fr",
		"url-rewrite-us-unrelated",
	}
	for _, k := range all {
		if err := cache.Set(context.Background(), k, []byte("x"), 0); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Three rows, a number nothing in the configuration could have known.
	ctx := ctxWithSteps(map[string]interface{}{
		"affected_paths": []interface{}{
			map[string]interface{}{"store_code": "us", "path": "widget"},
			map[string]interface{}{"store_code": "us", "path": "widget-old"},
			map[string]interface{}{"store_code": "fr", "path": "widget-fr"},
		},
	}, nil)

	if err := h.executeInvalidation(ctx, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}

	left := remaining(t, cache, all...)
	if len(left) != 1 || left[0] != "url-rewrite-us-unrelated" {
		t.Errorf("remaining = %v, want only the unrelated entry — a wildcard broad enough to catch the three also takes this one", left)
	}
}

// The static form keeps working, and the two are unioned so a fixed key and a
// computed set can be named together.
func TestInvalidate_StaticAndComputedAreUnioned(t *testing.T) {
	h, cache, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage:  "redis",
		Keys:     []string{"product:${input.sku}"},
		KeysFrom: "step.variants.map(v, 'product:' + v)",
	})
	defer done()

	all := []string{"product:ABC-1", "product:ABC-1-S", "product:ABC-1-M", "product:OTHER"}
	for _, k := range all {
		_ = cache.Set(context.Background(), k, []byte("x"), 0)
	}

	ctx := ctxWithSteps(map[string]interface{}{
		"variants": []interface{}{"ABC-1-S", "ABC-1-M"},
	}, nil)

	if err := h.executeInvalidation(ctx, map[string]interface{}{"sku": "ABC-1"}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}
	if left := remaining(t, cache, all...); len(left) != 1 || left[0] != "product:OTHER" {
		t.Errorf("remaining = %v, want only product:OTHER", left)
	}
}

// input and output are in scope too, not just step.
func TestInvalidate_KeysFromSeesInputAndOutput(t *testing.T) {
	h, cache, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage:  "redis",
		KeysFrom: "[ 'in:' + input.sku, 'out:' + output.slug ]",
	})
	defer done()

	all := []string{"in:ABC-1", "out:widget", "other"}
	for _, k := range all {
		_ = cache.Set(context.Background(), k, []byte("x"), 0)
	}

	ctx := ctxWithSteps(nil, map[string]interface{}{"slug": "widget"})
	if err := h.executeInvalidation(ctx, map[string]interface{}{"sku": "ABC-1"}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}
	if left := remaining(t, cache, all...); len(left) != 1 || left[0] != "other" {
		t.Errorf("remaining = %v", left)
	}
}

// patterns_from is the same for wildcards.
func TestInvalidate_PatternsFrom(t *testing.T) {
	h, cache, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage:      "redis",
		PatternsFrom: "step.stores.map(s, 'catalog:' + s + ':*')",
	})
	defer done()

	all := []string{"catalog:us:1", "catalog:us:2", "catalog:fr:1", "catalog:de:1"}
	for _, k := range all {
		_ = cache.Set(context.Background(), k, []byte("x"), 0)
	}

	ctx := ctxWithSteps(map[string]interface{}{"stores": []interface{}{"us", "fr"}}, nil)
	if err := h.executeInvalidation(ctx, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}
	if left := remaining(t, cache, all...); len(left) != 1 || left[0] != "catalog:de:1" {
		t.Errorf("remaining = %v, want only catalog:de:1", left)
	}
}

// An empty list drops nothing rather than deleting something surprising.
func TestInvalidate_EmptyListIsANoOp(t *testing.T) {
	h, cache, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage:  "redis",
		KeysFrom: "step.nothing.map(x, 'k:' + x)",
	})
	defer done()

	_ = cache.Set(context.Background(), "k:1", []byte("x"), 0)
	ctx := ctxWithSteps(map[string]interface{}{"nothing": []interface{}{}}, nil)

	if err := h.executeInvalidation(ctx, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}
	if left := remaining(t, cache, "k:1"); len(left) != 1 {
		t.Error("an empty computed list deleted something")
	}
}

// An expression that does not produce strings is named rather than deleting a
// key spelled with Go syntax in it and reporting success.
func TestInvalidate_NonStringListIsAnError(t *testing.T) {
	for _, tc := range []struct{ name, expr string }{
		{"not a list", "step.count"},
		{"list of maps", "step.rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, done := newInvalidateHandler(t, &flow.InvalidateConfig{
				Storage: "redis", KeysFrom: tc.expr,
			})
			defer done()

			ctx := ctxWithSteps(map[string]interface{}{
				"count": 3,
				"rows":  []interface{}{map[string]interface{}{"a": 1}},
			}, nil)

			err := h.executeInvalidation(ctx, map[string]interface{}{}, nil)
			if err == nil {
				t.Fatal("expected an error naming the attribute")
			}
			if !strings.Contains(err.Error(), "keys_from") {
				t.Errorf("error does not name the attribute: %v", err)
			}
		})
	}
}

// A ${} template aimed at a list cannot fan out — it renders Go syntax into
// the key. That stays what it was, and now says so, pointing at the attribute
// that does the job.
func TestInvalidate_TemplateAimedAtAListWarns(t *testing.T) {
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	h, _, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage: "redis",
		Keys:    []string{"url-rewrite-${input.paths}"},
	})
	defer done()

	ctx := ctxWithSteps(nil, nil)
	input := map[string]interface{}{"paths": []interface{}{"a", "b", "c"}}
	if err := h.executeInvalidation(ctx, input, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}

	out := logs.String()
	if !strings.Contains(out, "non-scalar") {
		t.Errorf("a template resolving to a list passed in silence:\n%s", out)
	}
	if !strings.Contains(out, "keys_from") {
		t.Errorf("the warning does not point at the attribute that does this:\n%s", out)
	}
}
