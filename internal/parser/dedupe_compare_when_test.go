package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const compareWhenFlowPrefix = `
flow "gated" {
  from {
    connector = "rabbit"
    operation = "q"
  }
  dedupe {
    cache = "memory_cache"
    key   = "'sku:' + input.body.sku"
    ttl   = "30d"
`

const compareWhenFlowSuffix = `
    fingerprint {
      name = "output.name"
    }
  }
  to {
    connector = "api"
    operation = "POST"
    target    = "/styles"
  }
}
`

func parseGatedFlow(t *testing.T, compareWhenLine string) *Configuration {
	t.Helper()
	return mustParse(t, compareWhenFlowPrefix+compareWhenLine+compareWhenFlowSuffix)
}

// TestDedupeCompareWhenQuoted: the ordinary spelling, a quoted CEL string.
func TestDedupeCompareWhenQuoted(t *testing.T) {
	cfg := parseGatedFlow(t, `    compare_when = "output.style_exists == 1"`)

	d := cfg.Flows[0].Dedupe
	if d.CompareWhen != "output.style_exists == 1" {
		t.Errorf("compare_when: want %q, got %q", "output.style_exists == 1", d.CompareWhen)
	}
	// It must not have leaked into the thing it is deliberately NOT part of.
	if _, ok := d.Fingerprint["style_exists"]; ok {
		t.Error("compare_when must not add anything to the fingerprint projection")
	}
}

// TestDedupeCompareWhenUnquoted: written bare, HCL cannot evaluate it against
// its own context, so the raw source text is what has to survive — the same
// path `key` and the fingerprint expressions take.
func TestDedupeCompareWhenUnquoted(t *testing.T) {
	cfg := parseGatedFlow(t, `    compare_when = output.style_exists == 1`)

	if got := cfg.Flows[0].Dedupe.CompareWhen; got != "output.style_exists == 1" {
		t.Errorf("compare_when: want %q, got %q", "output.style_exists == 1", got)
	}
}

// TestDedupeCompareWhenAbsentIsEmpty: absent means "always compare", and the
// runtime distinguishes that from a configured predicate by the empty string.
func TestDedupeCompareWhenAbsentIsEmpty(t *testing.T) {
	cfg := parseGatedFlow(t, "")

	if got := cfg.Flows[0].Dedupe.CompareWhen; got != "" {
		t.Errorf("compare_when should be empty when unset, got %q", got)
	}
}

// TestDedupeCompareWhenEmptyStringRejected: an empty predicate is never what
// anyone means. Silently treating it as "always compare" would make a typo
// (or an env() that resolved to nothing) look like a working gate.
func TestDedupeCompareWhenEmptyStringRejected(t *testing.T) {
	tmpDir := t.TempDir()
	src := compareWhenFlowPrefix + `    compare_when = ""` + compareWhenFlowSuffix
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(src), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("an empty compare_when must be rejected, not silently ignored")
	}
	if !strings.Contains(err.Error(), "compare_when") {
		t.Errorf("the error must name the attribute; got: %v", err)
	}
}

// TestNamedDedupeCarriesCompareWhen: a reusable block can carry the gate, so
// several flows with the same "can this record vanish" story share one
// definition.
func TestNamedDedupeCarriesCompareWhen(t *testing.T) {
	cfg := mustParseDir(t, `
dedupe "gated_by_existence" {
  cache        = "memory_cache"
  key          = "'sku:' + input.body.sku"
  ttl          = "30d"
  on_duplicate = "ack"
  compare_when = "output.style_exists == 1"
  fingerprint {
    name = "output.name"
  }
}

flow "uses_named" {
  from {
    connector = "rabbit"
    operation = "q"
  }
  dedupe {
    use = "dedupe.gated_by_existence"
  }
  to {
    connector = "api"
    operation = "POST"
    target    = "/styles"
  }
}
`)

	if len(cfg.NamedDedupes) != 1 || cfg.NamedDedupes[0].CompareWhen != "output.style_exists == 1" {
		t.Fatalf("named dedupe did not keep compare_when: %+v", cfg.NamedDedupes)
	}
	if got := cfg.Flows[0].Dedupe.CompareWhen; got != "output.style_exists == 1" {
		t.Errorf("a flow referencing the named block must inherit compare_when, got %q", got)
	}
}

// TestFlowDedupeOverridesCompareWhen: the same attribute-level override every
// other dedupe attribute already supports. A flow whose existence check has a
// different shape reuses the base and replaces just the gate.
func TestFlowDedupeOverridesCompareWhen(t *testing.T) {
	cfg := mustParseDir(t, `
dedupe "base" {
  cache        = "memory_cache"
  key          = "'sku:' + input.body.sku"
  ttl          = "30d"
  on_duplicate = "ack"
  compare_when = "output.style_exists == 1"
  fingerprint {
    name = "output.name"
  }
}

flow "narrower" {
  from {
    connector = "rabbit"
    operation = "q"
  }
  dedupe {
    use          = "dedupe.base"
    compare_when = "output.row_id > 0"
  }
  to {
    connector = "api"
    operation = "POST"
    target    = "/items"
  }
}
`)

	d := cfg.Flows[0].Dedupe
	if d.CompareWhen != "output.row_id > 0" {
		t.Errorf("inline compare_when must win, got %q", d.CompareWhen)
	}
	// And the rest of the base survived the override.
	if d.Cache != "memory_cache" || d.TTL != "30d" || d.Fingerprint["name"] != "output.name" {
		t.Errorf("overriding compare_when must not disturb the rest of the base: %+v", d)
	}
}

// TestNamedDedupeWithoutCompareWhenKeepsBase: an inline block that does NOT
// set the gate inherits the base's, rather than clearing it. Getting this
// backwards would silently disable the gate on every flow that reuses a
// gated block without restating it.
func TestNamedDedupeWithoutCompareWhenKeepsBase(t *testing.T) {
	cfg := mustParseDir(t, `
dedupe "base" {
  cache        = "memory_cache"
  key          = "'sku:' + input.body.sku"
  ttl          = "30d"
  on_duplicate = "ack"
  compare_when = "output.style_exists == 1"
  fingerprint {
    name = "output.name"
  }
}

flow "inherits" {
  from {
    connector = "rabbit"
    operation = "q"
  }
  dedupe {
    use = "dedupe.base"
    ttl = "7d"
  }
  to {
    connector = "api"
    operation = "POST"
    target    = "/items"
  }
}
`)

	d := cfg.Flows[0].Dedupe
	if d.CompareWhen != "output.style_exists == 1" {
		t.Errorf("an override that does not mention compare_when must keep the base's, got %q", d.CompareWhen)
	}
	if d.TTL != "7d" {
		t.Errorf("ttl override: want 7d, got %q", d.TTL)
	}
}
