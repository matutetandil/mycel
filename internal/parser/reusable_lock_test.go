package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNamedLockTopLevel verifies that a `lock "<name>" { ... }` block at top
// level is parsed and registered under NamedLocks.
func TestNamedLockTopLevel(t *testing.T) {
	hcl := `
lock "user_mutex" {
  storage {
    driver = "redis"
    url    = "redis://localhost:6379"
  }
  key     = "'user:' + input.id"
  timeout = "30s"
  wait    = true
  retry   = "100ms"
}
`
	cfg := mustParse(t, hcl)

	if len(cfg.NamedLocks) != 1 {
		t.Fatalf("expected 1 named lock, got %d", len(cfg.NamedLocks))
	}
	l := cfg.NamedLocks[0]
	if l.Name != "user_mutex" {
		t.Errorf("name: want %q, got %q", "user_mutex", l.Name)
	}
	if l.Key != "'user:' + input.id" {
		t.Errorf("key: want %q, got %q", "'user:' + input.id", l.Key)
	}
	if l.Timeout != "30s" {
		t.Errorf("timeout: want %q, got %q", "30s", l.Timeout)
	}
	if !l.Wait {
		t.Errorf("wait: want true, got false")
	}
	if l.Retry != "100ms" {
		t.Errorf("retry: want %q, got %q", "100ms", l.Retry)
	}
	if l.Storage == nil || l.Storage.Driver != "redis" || l.Storage.URL != "redis://localhost:6379" {
		t.Errorf("storage not parsed correctly: %+v", l.Storage)
	}
}

// TestFlowLockUseResolvesToNamed verifies that a flow's lock with
// `use = "lock.<name>"` and no other fields ends up fully populated from the
// named block after parse, including the storage sub-block.
func TestFlowLockUseResolvesToNamed(t *testing.T) {
	hcl := `
lock "user_mutex" {
  storage {
    driver = "redis"
    url    = "redis://localhost:6379"
  }
  key     = "'user:' + input.id"
  timeout = "30s"
  wait    = true
  retry   = "100ms"
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
  lock {
    use = "lock.user_mutex"
  }
}
`
	cfg := mustParseDir(t, hcl)
	l := cfg.Flows[0].Lock
	if l == nil {
		t.Fatal("flow.Lock should not be nil after resolve")
	}
	if l.Key != "'user:' + input.id" {
		t.Errorf("key: want %q, got %q", "'user:' + input.id", l.Key)
	}
	if l.Timeout != "30s" {
		t.Errorf("timeout: want %q, got %q", "30s", l.Timeout)
	}
	if !l.Wait {
		t.Errorf("wait: want true (from base), got false")
	}
	if l.Retry != "100ms" {
		t.Errorf("retry: want %q, got %q", "100ms", l.Retry)
	}
	if l.Storage == nil || l.Storage.Driver != "redis" {
		t.Errorf("storage should inherit from base: %+v", l.Storage)
	}
	if l.Use != "user_mutex" {
		t.Errorf("Use should be preserved for tracing: want %q, got %q", "user_mutex", l.Use)
	}
}

// TestFlowLockInlineOverridesNamed verifies attribute-level override: inline
// non-zero scalar values win, and an inline storage block replaces the named
// base's storage block wholesale.
func TestFlowLockInlineOverridesNamed(t *testing.T) {
	hcl := `
lock "user_mutex" {
  storage {
    driver = "redis"
    url    = "redis://base:6379"
  }
  key     = "'user:' + input.id"
  timeout = "30s"
  retry   = "100ms"
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
  lock {
    use     = "lock.user_mutex"
    key     = "'order:' + input.order_id"
    timeout = "5s"
    storage {
      driver = "memory"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)
	l := cfg.Flows[0].Lock

	// Overridden scalars use the inline value.
	if l.Key != "'order:' + input.order_id" {
		t.Errorf("key: want override, got %q", l.Key)
	}
	if l.Timeout != "5s" {
		t.Errorf("timeout: want %q (override), got %q", "5s", l.Timeout)
	}
	// Untouched scalar inherits from base.
	if l.Retry != "100ms" {
		t.Errorf("retry: want %q (inherit from base), got %q", "100ms", l.Retry)
	}
	// Storage sub-block is replaced wholesale, not deep-merged: the base URL
	// must NOT survive when the inline storage omits it.
	if l.Storage == nil || l.Storage.Driver != "memory" {
		t.Errorf("storage driver: want %q (inline replace), got %+v", "memory", l.Storage)
	}
	if l.Storage.URL != "" {
		t.Errorf("storage URL should not leak from base after wholesale replace, got %q", l.Storage.URL)
	}
}

// TestFlowLockUseUnknownNameFails verifies that referencing a non-existent
// named lock fails at parse time with a helpful message listing the available
// names.
func TestFlowLockUseUnknownNameFails(t *testing.T) {
	hcl := `
lock "user_mutex" {
  key = "'user:' + input.id"
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
  lock {
    use = "lock.user_mutexx"
  }
}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("expected parse error for unknown lock reference")
	}
	msg := err.Error()
	if !strings.Contains(msg, "user_mutexx") || !strings.Contains(msg, "user_mutex") {
		t.Errorf("error should mention the typo and list the available name; got: %s", msg)
	}
}

// TestInlineLockWithoutUseStillSelfContained verifies that the existing
// inline-only behavior (no `use`) is unchanged.
func TestInlineLockWithoutUseStillSelfContained(t *testing.T) {
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
  lock {
    storage {
      driver = "memory"
    }
    key     = "'user:' + input.id"
    timeout = "10s"
  }
}
`
	cfg := mustParseDir(t, hcl)
	l := cfg.Flows[0].Lock
	if l.Key != "'user:' + input.id" || l.Timeout != "10s" {
		t.Errorf("inline lock lost fields: %+v", l)
	}
	if l.Storage == nil || l.Storage.Driver != "memory" {
		t.Errorf("inline lock storage missing: %+v", l.Storage)
	}
	if l.Use != "" {
		t.Errorf("inline-only lock should have empty Use, got %q", l.Use)
	}
}

// TestInlineLockMissingKeyFailsClearly keeps the existing error UX: an inline
// lock without a `use` and without a `key` must still fail with a helpful
// message, not silently end up half-configured.
func TestInlineLockMissingKeyFailsClearly(t *testing.T) {
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
  lock {
    timeout = "10s"
  }
}
`
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().Parse(context.Background(), tmpDir)
	if err == nil {
		t.Fatal("expected error for inline lock without key")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("error should mention the missing key; got: %s", err.Error())
	}
}
