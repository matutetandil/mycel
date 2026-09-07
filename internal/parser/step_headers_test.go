package parser

import "testing"

// `headers` on a step or a destination, as an attribute or as a block, is
// kept where the runtime reads it.
func TestHeadersOnAStepAndADestinationAreKept(t *testing.T) {
	cfg := mustParse(t, `
flow "page" {
  from {
    connector = "api"
    operation = "GET /page"
  }

  step "as_attribute" {
    connector = "backend"
    operation = "POST /graphql"
    headers   = { Store = "input.store" }
    body      = { query = "'{ page { title } }'" }
  }

  step "as_block" {
    connector = "backend"
    operation = "POST /graphql"
    headers {
      Store = "input.store"
    }
  }

  to {
    connector = "backend"
    operation = "POST /products"
    headers   = { Store = "input.store", "X-Fixed" = "yes" }
  }
}
`)

	f := cfg.Flows[0]
	for _, step := range f.Steps {
		hdrs := step.GetHeaders()
		if hdrs["Store"] != "input.store" {
			t.Errorf("step %s: headers = %#v", step.Name, hdrs)
		}
	}
	if hdrs := f.To.GetHeaders(); hdrs["Store"] != "input.store" || hdrs["X-Fixed"] != "yes" {
		t.Errorf("to: headers = %#v", hdrs)
	}
}

func TestHeadersOnAnEnrichmentAreKept(t *testing.T) {
	cfg := mustParse(t, `
flow "page" {
  from {
    connector = "api"
    operation = "GET /page"
  }

  enrich "as_attribute" {
    connector = "backend"
    operation = "GET /page"
    headers   = { Store = "input.store" }
  }

  enrich "as_block" {
    connector = "backend"
    operation = "GET /page"
    headers {
      Store = "input.store"
    }
    params {
      id = "input.id"
    }
  }

  to {
    connector = "backend"
    operation = "POST /products"
  }
}
`)

	f := cfg.Flows[0]
	if len(f.Enrichments) != 2 {
		t.Fatalf("enrichments = %d", len(f.Enrichments))
	}
	for _, e := range f.Enrichments {
		if hdrs := e.GetHeaders(); hdrs["Store"] != "input.store" {
			t.Errorf("enrich %s: headers = %#v", e.Name, hdrs)
		}
	}
	if f.Enrichments[1].Params["id"] != "input.id" {
		t.Errorf("the params block beside the headers block was lost: %#v", f.Enrichments[1].Params)
	}
}

func TestHeadersOnANamedTransformsEnrichmentAreKept(t *testing.T) {
	cfg := mustParse(t, `
transform "with_page" {
  enrich "as_attribute" {
    connector = "backend"
    operation = "GET /page"
    headers   = { Store = "input.store" }
  }
  enrich "as_block" {
    connector = "backend"
    operation = "GET /page"
    headers {
      Store = "input.store"
    }
  }
  title = "enriched.as_attribute.title"
}
`)
	tr := cfg.Transforms[0]
	if len(tr.Enrichments) != 2 {
		t.Fatalf("enrichments = %d", len(tr.Enrichments))
	}
	for _, e := range tr.Enrichments {
		if e.Headers["Store"] != "input.store" {
			t.Errorf("enrich %s: headers = %#v", e.Name, e.Headers)
		}
	}
}
