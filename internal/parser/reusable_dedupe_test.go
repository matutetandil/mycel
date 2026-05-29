package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNamedDedupeTopLevel verifies that a `dedupe "<name>" { ... }` block at
// top level is parsed and registered under NamedDedupes.
func TestNamedDedupeTopLevel(t *testing.T) {
	hcl := `
dedupe "standard" {
  cache         = "memory_cache"
  key           = "input.id"
  ttl           = "1h"
  on_duplicate  = "ack"
  fingerprint {
    id = "input.id"
  }
}
`
	cfg := mustParse(t, hcl)

	if len(cfg.NamedDedupes) != 1 {
		t.Fatalf("expected 1 named dedupe, got %d", len(cfg.NamedDedupes))
	}
	d := cfg.NamedDedupes[0]
	if d.Name != "standard" {
		t.Errorf("name: want %q, got %q", "standard", d.Name)
	}
	if d.Cache != "memory_cache" {
		t.Errorf("cache: want %q, got %q", "memory_cache", d.Cache)
	}
	if d.Key != "input.id" {
		t.Errorf("key: want %q, got %q", "input.id", d.Key)
	}
	if d.TTL != "1h" {
		t.Errorf("ttl: want %q, got %q", "1h", d.TTL)
	}
	if d.OnDuplicate != "ack" {
		t.Errorf("on_duplicate: want %q, got %q", "ack", d.OnDuplicate)
	}
	if d.Fingerprint["id"] != "input.id" {
		t.Errorf("fingerprint[id]: want %q, got %q", "input.id", d.Fingerprint["id"])
	}
}

// TestFlowDedupeUseResolvesToNamed verifies that a flow's dedupe with
// `use = "dedupe.<name>"` and no other fields ends up fully populated from
// the named block after parse.
func TestFlowDedupeUseResolvesToNamed(t *testing.T) {
	hcl := `
dedupe "standard" {
  cache         = "memory_cache"
  key           = "input.id"
  ttl           = "1h"
  on_duplicate  = "ack"
  fingerprint {
    id   = "input.id"
    name = "input.name"
  }
}

flow "uses_named" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  dedupe {
    use = "dedupe.standard"
  }
}
`
	cfg := mustParseDir(t, hcl)
	f := cfg.Flows[0]
	if f.Dedupe == nil {
		t.Fatal("flow.Dedupe should not be nil after resolve")
	}
	if f.Dedupe.Cache != "memory_cache" {
		t.Errorf("cache: want %q, got %q", "memory_cache", f.Dedupe.Cache)
	}
	if f.Dedupe.Key != "input.id" {
		t.Errorf("key: want %q, got %q", "input.id", f.Dedupe.Key)
	}
	if f.Dedupe.TTL != "1h" {
		t.Errorf("ttl: want %q, got %q", "1h", f.Dedupe.TTL)
	}
	if f.Dedupe.OnDuplicate != "ack" {
		t.Errorf("on_duplicate: want %q, got %q", "ack", f.Dedupe.OnDuplicate)
	}
	if len(f.Dedupe.Fingerprint) != 2 {
		t.Errorf("fingerprint: want 2 entries, got %d", len(f.Dedupe.Fingerprint))
	}
	if f.Dedupe.Use != "standard" {
		t.Errorf("Use should be preserved for tracing: want %q, got %q", "standard", f.Dedupe.Use)
	}
}

// TestFlowDedupeInlineOverridesNamed verifies attribute-level override: inline
// non-zero values win over the named base, but unset inline fields inherit
// from the base. Fingerprint map is merged key-by-key (inline wins per key,
// named-only keys are preserved).
func TestFlowDedupeInlineOverridesNamed(t *testing.T) {
	hcl := `
dedupe "standard" {
  cache         = "memory_cache"
  key           = "input.id"
  ttl           = "1h"
  on_duplicate  = "ack"
  fingerprint {
    id   = "input.id"
    name = "input.name"
  }
}

flow "overrides" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  dedupe {
    use          = "dedupe.standard"
    ttl          = "24h"
    on_duplicate = "reject"
    fingerprint {
      name  = "input.upper_name"
      email = "input.email"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)
	d := cfg.Flows[0].Dedupe

	// Base attributes carry over untouched.
	if d.Cache != "memory_cache" {
		t.Errorf("cache: want %q, got %q (should inherit from base)", "memory_cache", d.Cache)
	}
	if d.Key != "input.id" {
		t.Errorf("key: want %q, got %q (should inherit from base)", "input.id", d.Key)
	}
	// Overridden attributes use the inline value.
	if d.TTL != "24h" {
		t.Errorf("ttl: want %q (override), got %q", "24h", d.TTL)
	}
	if d.OnDuplicate != "reject" {
		t.Errorf("on_duplicate: want %q (override), got %q", "reject", d.OnDuplicate)
	}
	// Fingerprint merges: id (base only) preserved, name (both) wins inline,
	// email (inline only) added.
	if d.Fingerprint["id"] != "input.id" {
		t.Errorf("fingerprint[id]: want %q (preserved from base), got %q", "input.id", d.Fingerprint["id"])
	}
	if d.Fingerprint["name"] != "input.upper_name" {
		t.Errorf("fingerprint[name]: want %q (inline wins), got %q", "input.upper_name", d.Fingerprint["name"])
	}
	if d.Fingerprint["email"] != "input.email" {
		t.Errorf("fingerprint[email]: want %q (inline-only), got %q", "input.email", d.Fingerprint["email"])
	}
}

// TestFlowDedupeUseUnknownNameFails verifies that referencing a non-existent
// named dedupe fails at parse time with a helpful message listing the
// available names — not at runtime when the flow first dispatches.
func TestFlowDedupeUseUnknownNameFails(t *testing.T) {
	hcl := `
dedupe "standard" {
  cache = "memory_cache"
  key   = "input.id"
  fingerprint { id = "input.id" }
}

flow "typo" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  dedupe {
    use = "dedupe.standar"
  }
}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("expected parse error for unknown dedupe reference")
	}
	msg := err.Error()
	if !strings.Contains(msg, "standar") || !strings.Contains(msg, "standard") {
		t.Errorf("error should mention the typo and list the available name; got: %s", msg)
	}
}

// TestInlineDedupeWithoutUseStillSelfContained verifies that the existing
// inline-only behavior (no `use`) is unchanged: a self-contained dedupe
// must still specify cache/key/fingerprint or fail clearly.
func TestInlineDedupeWithoutUseStillSelfContained(t *testing.T) {
	hcl := `
flow "still_inline" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  dedupe {
    cache = "memory_cache"
    key   = "input.id"
    fingerprint { id = "input.id" }
  }
}
`
	cfg := mustParseDir(t, hcl)
	d := cfg.Flows[0].Dedupe
	if d.Cache != "memory_cache" || d.Key != "input.id" || d.Fingerprint["id"] != "input.id" {
		t.Errorf("inline dedupe lost fields: %+v", d)
	}
	if d.Use != "" {
		t.Errorf("inline-only dedupe should have empty Use, got %q", d.Use)
	}
	if d.OnDuplicate != "ack" {
		t.Errorf("inline-only dedupe default OnDuplicate: want %q, got %q", "ack", d.OnDuplicate)
	}
}

// TestInlineDedupeMissingCacheFailsClearly keeps the v2.5 error UX: an
// inline dedupe without a `use` and without `cache` must still fail with a
// helpful message, not silently end up half-configured.
func TestInlineDedupeMissingCacheFailsClearly(t *testing.T) {
	hcl := `
flow "broken" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  dedupe {
    key = "input.id"
    fingerprint { id = "input.id" }
  }
}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("expected error for inline dedupe without cache")
	}
	if !strings.Contains(err.Error(), "cache") {
		t.Errorf("error should mention the missing cache attribute; got: %s", err.Error())
	}
}

// mustParse parses a single-file config from a string and returns it. Used
// for tests that only need to exercise top-level blocks (no flow refs).
func mustParse(t *testing.T, hcl string) *Configuration {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := NewHCLParser().ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cfg
}

// mustParseDir parses a config through the full Parse() pipeline (including
// ValidateUniqueNames + ResolveReferences) so flow-level refs to named
// blocks get resolved. Use whenever the test asserts post-resolve state.
func mustParseDir(t *testing.T, hcl string) *Configuration {
	t.Helper()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cfg
}
