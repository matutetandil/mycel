package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/flow"
)

// These two kinds were added through the reusableKinds registry alone (no
// edits to rootSchema / ParseFile / ValidateUniqueNames / ResolveReferences),
// so the tests double as proof that the table-driven abstraction holds.

// --- semaphore ---

func TestNamedSemaphoreTopLevel(t *testing.T) {
	hcl := `
semaphore "external_api" {
  storage {
    driver = "memory"
  }
  key         = "'external_api'"
  max_permits = 5
  timeout     = "30s"
  lease       = "60s"
}
`
	cfg := mustParse(t, hcl)
	if len(cfg.NamedSemaphores) != 1 {
		t.Fatalf("expected 1 named semaphore, got %d", len(cfg.NamedSemaphores))
	}
	s := cfg.NamedSemaphores[0]
	if s.Name != "external_api" || s.Key != "'external_api'" || s.MaxPermits != 5 || s.Timeout != "30s" || s.Lease != "60s" {
		t.Errorf("named semaphore parsed wrong: %+v", s)
	}
}

func TestFlowSemaphoreUseResolvesAndOverrides(t *testing.T) {
	hcl := `
semaphore "external_api" {
  storage {
    driver = "redis"
    url    = "redis://base:6379"
  }
  key         = "'external_api'"
  max_permits = 5
  timeout     = "30s"
}

flow "resolves" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  semaphore {
    use = "semaphore.external_api"
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
  semaphore {
    use         = "semaphore.external_api"
    max_permits = 10
    storage {
      driver = "memory"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)

	resolved := findFlow(t, cfg, "resolves").Semaphore
	if resolved.MaxPermits != 5 || resolved.Key != "'external_api'" || resolved.Timeout != "30s" {
		t.Errorf("resolved semaphore should inherit base: %+v", resolved)
	}
	if resolved.Storage == nil || resolved.Storage.URL != "redis://base:6379" {
		t.Errorf("resolved semaphore should inherit base storage: %+v", resolved.Storage)
	}

	ov := findFlow(t, cfg, "overrides").Semaphore
	if ov.MaxPermits != 10 {
		t.Errorf("max_permits override: want 10, got %d", ov.MaxPermits)
	}
	// Storage replaced wholesale: base URL must not leak.
	if ov.Storage == nil || ov.Storage.Driver != "memory" || ov.Storage.URL != "" {
		t.Errorf("storage should be replaced wholesale: %+v", ov.Storage)
	}
}

func TestFlowSemaphoreUnknownNameFails(t *testing.T) {
	hcl := `
semaphore "external_api" {
  storage {
    driver = "memory"
  }
  key         = "'x'"
  max_permits = 1
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
  semaphore {
    use = "semaphore.external_apii"
  }
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "external_apii") || !strings.Contains(err.Error(), "external_api") {
		t.Errorf("expected unknown-name error listing available; got: %v", err)
	}
}

func TestNamedSemaphoreMissingMaxPermitsFails(t *testing.T) {
	hcl := `
semaphore "broken" {
  storage {
    driver = "memory"
  }
  key = "'x'"
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "max_permits") {
		t.Errorf("expected max_permits error; got: %v", err)
	}
}

// --- sequence_guard ---

func TestNamedSequenceGuardTopLevel(t *testing.T) {
	hcl := `
sequence_guard "sku_seq" {
  storage {
    driver = "memory"
  }
  key      = "'sku:' + input.sku"
  sequence = "input.jobId"
  on_older = "ack"
  ttl      = "30d"
}
`
	cfg := mustParse(t, hcl)
	if len(cfg.NamedSequenceGuards) != 1 {
		t.Fatalf("expected 1 named sequence_guard, got %d", len(cfg.NamedSequenceGuards))
	}
	sg := cfg.NamedSequenceGuards[0]
	if sg.Name != "sku_seq" || sg.Key != "'sku:' + input.sku" || sg.Sequence != "input.jobId" || sg.OnOlder != "ack" || sg.TTL != "30d" {
		t.Errorf("named sequence_guard parsed wrong: %+v", sg)
	}
}

func TestFlowSequenceGuardUseResolvesAndOverrides(t *testing.T) {
	hcl := `
sequence_guard "sku_seq" {
  storage {
    driver = "redis"
    url    = "redis://base:6379"
  }
  key      = "'sku:' + input.sku"
  sequence = "input.jobId"
  on_older = "ack"
  ttl      = "30d"
}

flow "resolves" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  sequence_guard {
    use = "sequence_guard.sku_seq"
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
  sequence_guard {
    use      = "sequence_guard.sku_seq"
    on_older = "reject"
    ttl      = "7d"
  }
}
`
	cfg := mustParseDir(t, hcl)

	r := findFlow(t, cfg, "resolves").SequenceGuard
	if r.Key != "'sku:' + input.sku" || r.Sequence != "input.jobId" || r.OnOlder != "ack" || r.TTL != "30d" {
		t.Errorf("resolved sequence_guard should inherit base: %+v", r)
	}
	if r.Storage == nil || r.Storage.URL != "redis://base:6379" {
		t.Errorf("resolved sequence_guard should inherit base storage: %+v", r.Storage)
	}

	ov := findFlow(t, cfg, "overrides").SequenceGuard
	if ov.OnOlder != "reject" || ov.TTL != "7d" {
		t.Errorf("override failed: %+v", ov)
	}
	// Untouched fields inherit.
	if ov.Key != "'sku:' + input.sku" || ov.Sequence != "input.jobId" {
		t.Errorf("untouched fields should inherit base: %+v", ov)
	}
}

func TestFlowSequenceGuardUnknownNameFails(t *testing.T) {
	hcl := `
sequence_guard "sku_seq" {
  storage {
    driver = "memory"
  }
  key      = "'x'"
  sequence = "input.n"
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
  sequence_guard {
    use = "sequence_guard.sku_seqq"
  }
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "sku_seqq") || !strings.Contains(err.Error(), "sku_seq") {
		t.Errorf("expected unknown-name error listing available; got: %v", err)
	}
}

func TestInlineSequenceGuardWithoutUseStillSelfContained(t *testing.T) {
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
  sequence_guard {
    storage {
      driver = "memory"
    }
    key      = "'sku:' + input.sku"
    sequence = "input.jobId"
  }
}
`
	cfg := mustParseDir(t, hcl)
	sg := cfg.Flows[0].SequenceGuard
	if sg.Key == "" || sg.Sequence == "" || sg.Storage == nil {
		t.Errorf("inline sequence_guard lost fields: %+v", sg)
	}
	if sg.Use != "" {
		t.Errorf("inline-only sequence_guard should have empty Use, got %q", sg.Use)
	}
}

// --- helpers ---

func findFlow(t *testing.T, cfg *Configuration, name string) *flow.Config {
	t.Helper()
	for _, f := range cfg.Flows {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("flow %q not found", name)
	return nil
}

func parseDirErr(t *testing.T, hcl string) error {
	t.Helper()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	return err
}
