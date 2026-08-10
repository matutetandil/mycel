package parser

import "testing"

// A named transform may hold enrich blocks: the runtime reads them when a flow
// references the transform, and the documentation shows one. The parser
// accepted the block and then rejected the document, so no such file ever
// parsed — JustAttributes refuses a body containing any block, including the
// ones PartialContent had already taken.
func TestNamedTransform_AcceptsEnrichAlongsideMappings(t *testing.T) {
	cfg := mustParse(t, `
transform "with_pricing" {
  enrich "pricing" {
    connector = "pricing_service"
    operation = "getPrice"
    params {
      product_id = "input.id"
    }
  }

  id    = "input.id"
  price = "enriched.pricing.price"
}
`)

	if len(cfg.Transforms) != 1 {
		t.Fatalf("expected 1 named transform, got %d", len(cfg.Transforms))
	}
	tr := cfg.Transforms[0]

	if len(tr.Enrichments) != 1 {
		t.Fatalf("expected the enrich block to be kept, got %d", len(tr.Enrichments))
	}
	e := tr.Enrichments[0]
	if e.Name != "pricing" || e.Connector != "pricing_service" || e.Operation != "getPrice" {
		t.Errorf("enrichment lost its wiring: %+v", e)
	}
	if e.Params["product_id"] != "input.id" {
		t.Errorf("enrich params: want product_id=input.id, got %v", e.Params)
	}

	// The mappings beside the block must survive it, which is the half that
	// JustAttributes was there to do.
	if tr.Mappings["price"] != "enriched.pricing.price" {
		t.Errorf("mappings lost: %v", tr.Mappings)
	}
	if len(tr.Mappings) != 2 {
		t.Errorf("expected 2 mappings, got %d: %v", len(tr.Mappings), tr.Mappings)
	}
}
