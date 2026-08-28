package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/metrics"
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

// invalidateErrors reads the counter for one cache and attribute.
func invalidateErrors(t *testing.T, cache, attr string) float64 {
	t.Helper()
	return testutil.ToFloat64(metrics.Default().CacheInvalidateErrors.WithLabelValues(cache, attr))
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

// An invalidation that did not happen used to be indistinguishable from one
// that did: the error reached a call site that discarded it, so the flow
// answered 200, the caller believed the entries were gone, and they were still
// there.
//
// The two kinds want different answers, so they are asserted separately.

// A keys_from that cannot be evaluated is not transient. It fails identically
// on every message for as long as the flow is deployed, invalidating nothing,
// and `mycel validate` does not evaluate CEL so it is not caught beforehand
// either. Failing loudly on the first message beats invalidating nothing for a
// month.
func TestInvalidate_AnExpressionThatCannotRunFailsTheRequest(t *testing.T) {
	for _, tc := range []struct{ name, expr string }{
		{"does not evaluate", "step.nope.map(x, x)"},
		{"yields a non-list", "step.count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &bytes.Buffer{}
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prev)

			h, _, done := newInvalidateHandler(t, &flow.InvalidateConfig{
				Storage: "redis", KeysFrom: tc.expr,
			})
			defer done()

			before := invalidateErrors(t, "redis", "keys_from")
			ctx := ctxWithSteps(map[string]interface{}{"count": 3}, nil)

			err := h.executeInvalidation(ctx, map[string]interface{}{}, nil)
			if err == nil {
				t.Fatal("a permanently broken expression must fail the request, not pass in silence")
			}
			if !strings.Contains(err.Error(), "after.invalidate.keys_from") {
				t.Errorf("the error does not name the attribute: %v", err)
			}
			if got := invalidateErrors(t, "redis", "keys_from"); got != before+1 {
				t.Errorf("mycel_cache_invalidate_errors_total went from %v to %v, want +1", before, got)
			}
			if !strings.Contains(logs.String(), "cache invalidation did not happen") {
				t.Errorf("nothing was logged:\n%s", logs.String())
			}
		})
	}
}

// A cache that could not be reached is transient, and the flow's own work is
// already done and committed — failing the request afterwards would be wrong.
// But an operator has to see it: the cache is now serving what that write made
// stale and nothing will correct it.
func TestInvalidate_AnUnreachableCacheIsReportedButNotFatal(t *testing.T) {
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	h, _, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage: "redis", Keys: []string{"product:1"},
	})
	defer done()

	// A cache that answers every command with a failure, which is what an
	// unreachable one looks like from here.
	unreachable := &failingCache{err: errors.New("dial tcp: connection refused")}
	h.Connectors.Replace("redis", unreachable)
	h.CacheConnector = unreachable

	before := invalidateErrors(t, "redis", "keys")
	err := h.executeInvalidation(ctxWithSteps(nil, nil), map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("a write that is already committed must not fail on the invalidation afterwards: %v", err)
	}
	if got := invalidateErrors(t, "redis", "keys"); got != before+1 {
		t.Errorf("mycel_cache_invalidate_errors_total went from %v to %v, want +1", before, got)
	}
	if !strings.Contains(logs.String(), "cache invalidation did not happen") {
		t.Errorf("an unreachable cache passed in silence:\n%s", logs.String())
	}
}

// A working invalidation neither counts nor logs.
func TestInvalidate_SuccessIsQuiet(t *testing.T) {
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	h, cache, done := newInvalidateHandler(t, &flow.InvalidateConfig{
		Storage: "redis", KeysFrom: "step.ids.map(i, 'k:' + i)",
	})
	defer done()

	_ = cache.Set(context.Background(), "k:1", []byte("x"), 0)
	before := invalidateErrors(t, "redis", "keys_from")

	ctx := ctxWithSteps(map[string]interface{}{"ids": []interface{}{"1"}}, nil)
	if err := h.executeInvalidation(ctx, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("executeInvalidation: %v", err)
	}
	if got := invalidateErrors(t, "redis", "keys_from"); got != before {
		t.Errorf("a working invalidation counted an error")
	}
	if strings.Contains(logs.String(), "did not happen") {
		t.Errorf("a working invalidation logged a failure:\n%s", logs.String())
	}
	if left := remaining(t, cache, "k:1"); len(left) != 0 {
		t.Errorf("k:1 survived")
	}
}

// failingCache answers every command with the same error.
type failingCache struct{ err error }

func (c *failingCache) Name() string                                             { return "redis" }
func (c *failingCache) Type() string                                             { return "cache" }
func (c *failingCache) Connect(context.Context) error                            { return nil }
func (c *failingCache) Close(context.Context) error                              { return nil }
func (c *failingCache) Health(context.Context) error                             { return c.err }
func (c *failingCache) Get(context.Context, string) ([]byte, bool, error)        { return nil, false, c.err }
func (c *failingCache) Set(context.Context, string, []byte, time.Duration) error { return c.err }
func (c *failingCache) Delete(context.Context, ...string) error                  { return c.err }
func (c *failingCache) DeletePattern(context.Context, string) error              { return c.err }
func (c *failingCache) Exists(context.Context, string) (bool, error)             { return false, c.err }
func (c *failingCache) TTL(context.Context, string) (time.Duration, error)       { return 0, c.err }
