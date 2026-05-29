package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNamedRetryTopLevel verifies that a `retry "<name>" { ... }` block at top
// level is parsed and registered under NamedRetries.
func TestNamedRetryTopLevel(t *testing.T) {
	hcl := `
retry "aggressive" {
  attempts  = 5
  delay     = "1s"
  max_delay = "30s"
  backoff   = "exponential"
}
`
	cfg := mustParse(t, hcl)

	if len(cfg.NamedRetries) != 1 {
		t.Fatalf("expected 1 named retry, got %d", len(cfg.NamedRetries))
	}
	r := cfg.NamedRetries[0]
	if r.Name != "aggressive" {
		t.Errorf("name: want %q, got %q", "aggressive", r.Name)
	}
	if r.Attempts != 5 {
		t.Errorf("attempts: want %d, got %d", 5, r.Attempts)
	}
	if r.Delay != "1s" {
		t.Errorf("delay: want %q, got %q", "1s", r.Delay)
	}
	if r.MaxDelay != "30s" {
		t.Errorf("max_delay: want %q, got %q", "30s", r.MaxDelay)
	}
	if r.Backoff != "exponential" {
		t.Errorf("backoff: want %q, got %q", "exponential", r.Backoff)
	}
}

// TestFlowRetryUseResolvesToNamed verifies that an error_handling.retry block
// with `use = "retry.<name>"` and no other fields ends up fully populated from
// the named block after parse.
func TestFlowRetryUseResolvesToNamed(t *testing.T) {
	hcl := `
retry "aggressive" {
  attempts  = 5
  delay     = "1s"
  max_delay = "30s"
  backoff   = "exponential"
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
  error_handling {
    retry {
      use = "retry.aggressive"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)
	f := cfg.Flows[0]
	if f.ErrorHandling == nil || f.ErrorHandling.Retry == nil {
		t.Fatal("flow.ErrorHandling.Retry should not be nil after resolve")
	}
	r := f.ErrorHandling.Retry
	if r.Attempts != 5 {
		t.Errorf("attempts: want %d, got %d", 5, r.Attempts)
	}
	if r.Delay != "1s" {
		t.Errorf("delay: want %q, got %q", "1s", r.Delay)
	}
	if r.MaxDelay != "30s" {
		t.Errorf("max_delay: want %q, got %q", "30s", r.MaxDelay)
	}
	if r.Backoff != "exponential" {
		t.Errorf("backoff: want %q, got %q", "exponential", r.Backoff)
	}
	if r.Use != "aggressive" {
		t.Errorf("Use should be preserved for tracing: want %q, got %q", "aggressive", r.Use)
	}
}

// TestFlowRetryInlineOverridesNamed verifies attribute-level override: inline
// non-zero values win over the named base, but unset inline fields inherit
// from the base.
func TestFlowRetryInlineOverridesNamed(t *testing.T) {
	hcl := `
retry "aggressive" {
  attempts  = 5
  delay     = "1s"
  max_delay = "30s"
  backoff   = "exponential"
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
  error_handling {
    retry {
      use      = "retry.aggressive"
      attempts = 2
      backoff  = "linear"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)
	r := cfg.Flows[0].ErrorHandling.Retry

	// Overridden attributes use the inline value.
	if r.Attempts != 2 {
		t.Errorf("attempts: want %d (override), got %d", 2, r.Attempts)
	}
	if r.Backoff != "linear" {
		t.Errorf("backoff: want %q (override), got %q", "linear", r.Backoff)
	}
	// Untouched attributes carry over from the base.
	if r.Delay != "1s" {
		t.Errorf("delay: want %q (inherit from base), got %q", "1s", r.Delay)
	}
	if r.MaxDelay != "30s" {
		t.Errorf("max_delay: want %q (inherit from base), got %q", "30s", r.MaxDelay)
	}
}

// TestFlowRetryUseUnknownNameFails verifies that referencing a non-existent
// named retry fails at parse time with a helpful message listing the available
// names — not at runtime when the flow first dispatches.
func TestFlowRetryUseUnknownNameFails(t *testing.T) {
	hcl := `
retry "aggressive" {
  attempts = 5
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
  error_handling {
    retry {
      use = "retry.agressive"
    }
  }
}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("expected parse error for unknown retry reference")
	}
	msg := err.Error()
	if !strings.Contains(msg, "agressive") || !strings.Contains(msg, "aggressive") {
		t.Errorf("error should mention the typo and list the available name; got: %s", msg)
	}
}

// TestInlineRetryWithoutUseStillSelfContained verifies that the existing
// inline-only behavior (no `use`) is unchanged: an inline retry inside
// error_handling keeps its fields and has an empty Use.
func TestInlineRetryWithoutUseStillSelfContained(t *testing.T) {
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
  error_handling {
    retry {
      attempts  = 3
      delay     = "500ms"
      backoff   = "constant"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)
	r := cfg.Flows[0].ErrorHandling.Retry
	if r.Attempts != 3 || r.Delay != "500ms" || r.Backoff != "constant" {
		t.Errorf("inline retry lost fields: %+v", r)
	}
	if r.Use != "" {
		t.Errorf("inline-only retry should have empty Use, got %q", r.Use)
	}
}

// TestNamedRetryMissingAttemptsFailsClearly verifies that a top-level retry
// with no positive attempts fails clearly — a named retry that retries nothing
// is almost certainly a mistake, and we catch it at parse time.
func TestNamedRetryMissingAttemptsFailsClearly(t *testing.T) {
	hcl := `
retry "broken" {
  delay = "1s"
}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("expected error for named retry without attempts")
	}
	if !strings.Contains(err.Error(), "attempts") {
		t.Errorf("error should mention the missing attempts; got: %s", err.Error())
	}
}
