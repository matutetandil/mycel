package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
	"github.com/matutetandil/mycel/v3/internal/flow"
	msync "github.com/matutetandil/mycel/v3/internal/sync"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// A message can carry work of different weights. A product's data and its
// images arrive together, the images take minutes because the far side
// downloads them, and a whole-message fingerprint re-sends both because a name
// changed — which is what makes the cheap half wait for the expensive one.
//
// Facets track the parts separately. What has to hold: only the parts that
// changed are written, nothing is written when none did, and a facet is
// committed only when its own destinations succeeded — so a partial failure
// re-sends the part that did not land and only that part.

func facetHandler(t *testing.T, destinations []*flow.ToConfig) (*FlowHandler, func()) {
	t.Helper()

	memCache := memory.New("fp_cache", &types.Config{Driver: "memory"})
	if err := memCache.Connect(context.Background()); err != nil {
		t.Fatalf("cache connect: %v", err)
	}
	mgr := msync.NewManager()
	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	h := &FlowHandler{
		Config: &flow.Config{
			Name:    "split",
			From:    &flow.FromConfig{Connector: "rabbit"},
			MultiTo: destinations,
			Dedupe: &flow.DedupeConfig{
				Cache:       "fp_cache",
				Key:         "'sku:' + input.sku",
				OnDuplicate: "ack",
				TTL:         "1h",
				Facets: []flow.DedupeFacet{
					{Name: "data", Fingerprint: map[string]string{"name": "output.name"}},
					{Name: "assets", Fingerprint: map[string]string{"image": "output.image"}},
				},
			},
		},
		DedupeCache: memCache,
		SyncManager: mgr,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, func() {
		_ = memCache.Close(context.Background())
		_ = mgr.Close()
	}
}

func twoDestinations() (*flow.ToConfig, *flow.ToConfig) {
	return &flow.ToConfig{Connector: "magento", Facet: "data"},
		&flow.ToConfig{Connector: "rabbit", Facet: "assets"}
}

// runFacets drives one message through dedupe, reporting which destinations
// the decision let through and which facets it asked to fail.
func runFacets(t *testing.T, h *FlowHandler, name, image string, failing map[string]bool) (ran map[string]bool, dropped bool) {
	t.Helper()

	input := map[string]interface{}{"sku": "S1", "name": name, "image": image}
	payload := map[string]interface{}{"name": name, "image": image}
	ran = map[string]bool{}

	result, err := h.dedupeAwareWrite(context.Background(), input, payload,
		func(ctx context.Context) (interface{}, error) {
			report := &MultiDestResult{
				Results: map[string]interface{}{},
				Errors:  map[string]string{},
				Success: true,
			}
			labels := destinationLabels(h.Config.MultiTo)
			for _, dest := range h.Config.MultiTo {
				if !facetChanged(ctx, dest.Facet) {
					continue
				}
				ran[dest.Facet] = true
				if failing[dest.Facet] {
					report.Errors[labels[dest]] = "destination refused"
					report.Success = false
					continue
				}
				report.Results[labels[dest]] = "ok"
			}
			if len(report.Errors) > 0 && len(report.Results) == 0 {
				return report, errors.New("all destination writes failed")
			}
			return report, nil
		})

	if _, isDrop := result.(*flow.FilteredResultWithPolicy); isDrop {
		return ran, true
	}
	if err != nil && len(ran) == 0 {
		t.Fatalf("dedupe failed before reaching any destination: %v", err)
	}
	return ran, false
}

func TestOnlyTheFacetsThatChangedAreWritten(t *testing.T) {
	data, assets := twoDestinations()
	h, cleanup := facetHandler(t, []*flow.ToConfig{data, assets})
	defer cleanup()

	// First message: nothing has been seen, so both parts are new.
	ran, dropped := runFacets(t, h, "Widget", "img1.jpg", nil)
	if dropped || !ran["data"] || !ran["assets"] {
		t.Fatalf("first message: ran=%v dropped=%v, want both", ran, dropped)
	}

	// The same message again changes nothing, so it is dropped whole — the
	// behaviour a bare fingerprint has always had.
	ran, dropped = runFacets(t, h, "Widget", "img1.jpg", nil)
	if !dropped || len(ran) != 0 {
		t.Errorf("an unchanged message was not dropped: ran=%v dropped=%v", ran, dropped)
	}

	// Only the image changed: the expensive half runs and Magento is left
	// alone. This is the whole point of the primitive.
	ran, dropped = runFacets(t, h, "Widget", "img2.jpg", nil)
	if dropped || ran["data"] || !ran["assets"] {
		t.Errorf("image-only change: ran=%v dropped=%v, want assets only", ran, dropped)
	}

	// Only the name changed: no image work is queued at all.
	ran, dropped = runFacets(t, h, "Gadget", "img2.jpg", nil)
	if dropped || !ran["data"] || ran["assets"] {
		t.Errorf("data-only change: ran=%v dropped=%v, want data only", ran, dropped)
	}

	// Both changed.
	ran, dropped = runFacets(t, h, "Gizmo", "img3.jpg", nil)
	if dropped || !ran["data"] || !ran["assets"] {
		t.Errorf("both changed: ran=%v dropped=%v, want both", ran, dropped)
	}
}

func TestAFacetIsCommittedOnlyWhenItsOwnDestinationsSucceeded(t *testing.T) {
	data, assets := twoDestinations()
	h, cleanup := facetHandler(t, []*flow.ToConfig{data, assets})
	defer cleanup()

	// The data lands and the asset enqueue fails — the broker is down, the
	// queue is missing. Committing both here would lose the assets for as
	// long as the entry lives, because the next identical message would find
	// them unchanged and never send them again.
	ran, _ := runFacets(t, h, "Widget", "img1.jpg", map[string]bool{"assets": true})
	if !ran["data"] || !ran["assets"] {
		t.Fatalf("both destinations should have been attempted: %v", ran)
	}

	// The retry: the same message, with the broker back. Data is not written
	// a second time, and the assets are.
	ran, dropped := runFacets(t, h, "Widget", "img1.jpg", nil)
	if dropped {
		t.Fatal("the retry was dropped as a duplicate, losing the assets for good")
	}
	if ran["data"] {
		t.Error("the data was written a second time even though it had landed")
	}
	if !ran["assets"] {
		t.Error("the assets were not re-sent, so the failed half was lost")
	}

	// And now that both have landed, an identical message is dropped.
	_, dropped = runFacets(t, h, "Widget", "img1.jpg", nil)
	if !dropped {
		t.Error("a message identical to one fully applied was not dropped")
	}
}

func TestCompareWhenRunsEveryFacet(t *testing.T) {
	// compare_when says the stored fingerprints describe something that is no
	// longer there. Comparing any facet against them could drop a message the
	// downstream needs — an assets-only message against a record that no
	// longer exists would never re-create it — so nothing is compared and
	// every facet runs.
	data, assets := twoDestinations()
	h, cleanup := facetHandler(t, []*flow.ToConfig{data, assets})
	defer cleanup()

	if _, dropped := runFacets(t, h, "Widget", "img1.jpg", nil); dropped {
		t.Fatal("the first message was dropped")
	}

	h.Config.Dedupe.CompareWhen = "false"
	ran, dropped := runFacets(t, h, "Widget", "img1.jpg", nil)
	if dropped {
		t.Fatal("a message was dropped while compare_when said not to compare")
	}
	if !ran["data"] || !ran["assets"] {
		t.Errorf("compare_when false must run every facet: %v", ran)
	}
}

func TestADestinationWithNoFacetAlwaysRuns(t *testing.T) {
	// Every flow written before facets is this flow: destinations that are not
	// tied to any facet must keep running whenever the message is not dropped.
	data, assets := twoDestinations()
	audit := &flow.ToConfig{Connector: "audit"}
	h, cleanup := facetHandler(t, []*flow.ToConfig{data, assets, audit})
	defer cleanup()

	if _, dropped := runFacets(t, h, "Widget", "img1.jpg", nil); dropped {
		t.Fatal("the first message was dropped")
	}

	ran, dropped := runFacets(t, h, "Gadget", "img1.jpg", nil)
	if dropped {
		t.Fatal("a changed message was dropped")
	}
	if !ran[""] {
		t.Error("a destination with no facet was skipped")
	}
	if ran["assets"] {
		t.Error("an unchanged facet's destination ran")
	}
}

// tallyWriter is a destination that records what it was asked to write.
type tallyWriter struct {
	name   string
	writes int
}

func (c *tallyWriter) Name() string                  { return c.name }
func (c *tallyWriter) Type() string                  { return "test" }
func (c *tallyWriter) Connect(context.Context) error { return nil }
func (c *tallyWriter) Close(context.Context) error   { return nil }
func (c *tallyWriter) Health(context.Context) error  { return nil }
func (c *tallyWriter) Write(_ context.Context, _ *connector.Data) (*connector.Result, error) {
	c.writes++
	return &connector.Result{Affected: 1}, nil
}

// A dedupe block on a flow with more than one destination was parsed,
// validated and never consulted: handleMultiDestWrite did not call
// dedupeAwareWrite at all. Three identical messages wrote three rows to every
// destination while the configuration said they would be deduplicated.
//
// The test goes through handleMultiDestWrite rather than calling
// dedupeAwareWrite directly, because the defect was the wiring between them —
// every test that called the primitive itself passed throughout.
func TestDedupeRunsOnAFlowWithSeveralDestinations(t *testing.T) {
	memCache := memory.New("fp_cache", &types.Config{Driver: "memory"})
	if err := memCache.Connect(context.Background()); err != nil {
		t.Fatalf("cache connect: %v", err)
	}
	defer memCache.Close(context.Background())

	mgr := msync.NewManager()
	defer mgr.Close()

	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("transformer: %v", err)
	}

	first := &tallyWriter{name: "first"}
	second := &tallyWriter{name: "second"}
	registry := connector.NewRegistry()
	registry.Replace("first", first)
	registry.Replace("second", second)

	h := &FlowHandler{
		Config: &flow.Config{
			Name: "two_destinations",
			From: &flow.FromConfig{Connector: "rabbit"},
			MultiTo: []*flow.ToConfig{
				{Connector: "first"},
				{Connector: "second"},
			},
			Transform: &flow.TransformConfig{Mappings: map[string]string{"name": "input.name"}},
			Dedupe: &flow.DedupeConfig{
				Cache:       "fp_cache",
				Key:         "'sku:' + input.sku",
				OnDuplicate: "ack",
				TTL:         "1h",
				Fingerprint: map[string]string{"name": "output.name"},
			},
		},
		Connectors:  registry,
		DedupeCache: memCache,
		SyncManager: mgr,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	input := map[string]interface{}{"sku": "S1", "name": "Widget"}
	for i := 0; i < 3; i++ {
		if _, err := h.handleMultiDestWrite(context.Background(), input, Operation{}); err != nil {
			t.Fatalf("message %d: %v", i+1, err)
		}
	}

	if first.writes != 1 || second.writes != 1 {
		t.Errorf("three identical messages wrote %d and %d times; dedupe never ran",
			first.writes, second.writes)
	}
}
