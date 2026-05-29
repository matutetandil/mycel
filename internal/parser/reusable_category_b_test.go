package parser

import (
	"strings"
	"testing"
)

// Category B: blocks with sub-blocks (coordinate, transaction, error_handling).
// All added through the reusableKinds registry. Override is attribute-level for
// scalars and wholesale for sub-blocks (no deep merge), matching the v2.6 plan.

// --- coordinate ---

func TestNamedCoordinateResolvesAndOverrides(t *testing.T) {
	hcl := `
coordinate "ordering" {
  storage {
    driver = "redis"
    url    = "redis://base:6379"
  }
  timeout     = "60s"
  on_timeout  = "fail"
  max_retries = 3
  wait {
    when = "input.type == 'child'"
    for  = "'parent:' + input.parent_id"
  }
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
  coordinate {
    use = "coordinate.ordering"
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
  coordinate {
    use        = "coordinate.ordering"
    timeout    = "10s"
    storage {
      driver = "memory"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)

	r := findFlow(t, cfg, "resolves").Coordinate
	if r.Timeout != "60s" || r.OnTimeout != "fail" || r.MaxRetries != 3 {
		t.Errorf("resolved coordinate should inherit base scalars: %+v", r)
	}
	if r.Wait == nil || r.Wait.When != "input.type == 'child'" {
		t.Errorf("resolved coordinate should inherit base wait: %+v", r.Wait)
	}
	if r.Storage == nil || r.Storage.URL != "redis://base:6379" {
		t.Errorf("resolved coordinate should inherit base storage: %+v", r.Storage)
	}

	ov := findFlow(t, cfg, "overrides").Coordinate
	if ov.Timeout != "10s" {
		t.Errorf("timeout override: want 10s, got %q", ov.Timeout)
	}
	// Untouched scalar inherits.
	if ov.OnTimeout != "fail" || ov.MaxRetries != 3 {
		t.Errorf("untouched coordinate scalars should inherit: %+v", ov)
	}
	// Storage replaced wholesale: base URL must not leak.
	if ov.Storage == nil || ov.Storage.Driver != "memory" || ov.Storage.URL != "" {
		t.Errorf("storage should be replaced wholesale: %+v", ov.Storage)
	}
	// Wait sub-block not overridden inline → inherited from base.
	if ov.Wait == nil || ov.Wait.When == "" {
		t.Errorf("wait should inherit from base when not overridden: %+v", ov.Wait)
	}
}

func TestNamedCoordinateMissingStorageFails(t *testing.T) {
	hcl := `
coordinate "broken" {
  timeout = "30s"
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "storage") {
		t.Errorf("expected storage error for named coordinate; got: %v", err)
	}
}

func TestFlowCoordinateUnknownNameFails(t *testing.T) {
	hcl := `
coordinate "ordering" {
  storage {
    driver = "memory"
  }
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
  coordinate {
    use = "coordinate.orderingg"
  }
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "orderingg") || !strings.Contains(err.Error(), "ordering") {
		t.Errorf("expected unknown-name error listing available; got: %v", err)
	}
}

// --- transaction ---

func TestNamedTransactionResolves(t *testing.T) {
	hcl := `
connector "db" {
  type   = "database"
  driver = "sqlite"
  path   = "./t.db"
}

transaction "persist_order" {
  exec {
    query   = "INSERT INTO orders (sku) VALUES (:sku)"
    params  = { sku = "input.sku" }
    capture = "order_id"
  }
  exec {
    query  = "INSERT INTO order_log (order_id) VALUES (:oid)"
    params = { oid = "captured.order_id" }
  }
}

flow "save" {
  from {
    connector = "x"
    operation = "POST /orders"
  }
  to {
    connector = "db"
    transaction {
      use = "transaction.persist_order"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)
	tx := findFlow(t, cfg, "save").To.Transaction
	if tx == nil {
		t.Fatal("transaction should not be nil after resolve")
	}
	if len(tx.Statements) != 2 {
		t.Fatalf("expected 2 statements pulled from named transaction, got %d", len(tx.Statements))
	}
	if tx.Statements[0].Exec == nil || !strings.Contains(tx.Statements[0].Exec.Query, "INSERT INTO orders") {
		t.Errorf("first statement not resolved from named base: %+v", tx.Statements[0])
	}
	if tx.Use != "persist_order" {
		t.Errorf("Use should be preserved for tracing: got %q", tx.Use)
	}
}

func TestNamedTransactionEmptyFails(t *testing.T) {
	hcl := `
transaction "empty" {
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "at least one exec or each") {
		t.Errorf("expected empty-transaction error; got: %v", err)
	}
}

func TestFlowTransactionUnknownNameFails(t *testing.T) {
	hcl := `
connector "db" {
  type   = "database"
  driver = "sqlite"
  path   = "./t.db"
}

transaction "persist" {
  exec {
    query  = "INSERT INTO t (a) VALUES (:a)"
    params = { a = "input.a" }
  }
}

flow "typo" {
  from {
    connector = "x"
    operation = "POST /t"
  }
  to {
    connector = "db"
    transaction {
      use = "transaction.persistt"
    }
  }
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "persistt") || !strings.Contains(err.Error(), "persist") {
		t.Errorf("expected unknown-name error listing available; got: %v", err)
	}
}

// --- error_handling ---

func TestNamedErrorHandlingResolvesAndOverrides(t *testing.T) {
	hcl := `
error_handling "standard" {
  retry {
    attempts = 3
    delay    = "1s"
    backoff  = "exponential"
  }
  on_timeout {
    action = "ack"
  }
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
  error_handling {
    use = "error_handling.standard"
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
  error_handling {
    use = "error_handling.standard"
    retry {
      attempts = 5
      backoff  = "linear"
    }
  }
}
`
	cfg := mustParseDir(t, hcl)

	r := findFlow(t, cfg, "resolves").ErrorHandling
	if r.Retry == nil || r.Retry.Attempts != 3 || r.Retry.Backoff != "exponential" {
		t.Errorf("resolved error_handling should inherit base retry: %+v", r.Retry)
	}
	if r.OnTimeout == nil || r.OnTimeout.Action != "ack" {
		t.Errorf("resolved error_handling should inherit base on_timeout: %+v", r.OnTimeout)
	}

	ov := findFlow(t, cfg, "overrides").ErrorHandling
	// retry sub-block replaced wholesale by inline.
	if ov.Retry == nil || ov.Retry.Attempts != 5 || ov.Retry.Backoff != "linear" {
		t.Errorf("retry should be replaced wholesale by inline: %+v", ov.Retry)
	}
	// on_timeout not overridden → inherited from base.
	if ov.OnTimeout == nil || ov.OnTimeout.Action != "ack" {
		t.Errorf("on_timeout should inherit from base when not overridden: %+v", ov.OnTimeout)
	}
}

// TestNamedErrorHandlingWithNestedRetryUse is the cross-cutting case: a named
// error_handling whose retry itself references a named retry. The error_handling
// pass materializes the retry onto the flow, then the retry pass (which runs
// after) folds the retry reference. Proves the registry ordering constraint.
func TestNamedErrorHandlingWithNestedRetryUse(t *testing.T) {
	hcl := `
retry "aggressive" {
  attempts  = 7
  delay     = "2s"
  max_delay = "1m"
  backoff   = "exponential"
}

error_handling "standard" {
  retry {
    use = "retry.aggressive"
  }
}

flow "uses_both" {
  from {
    connector = "x"
    operation = "y"
  }
  to {
    connector = "x"
    target    = "y"
  }
  error_handling {
    use = "error_handling.standard"
  }
}
`
	cfg := mustParseDir(t, hcl)
	r := findFlow(t, cfg, "uses_both").ErrorHandling.Retry
	if r == nil {
		t.Fatal("retry should be present after resolving error_handling + nested retry")
	}
	if r.Attempts != 7 || r.Delay != "2s" || r.MaxDelay != "1m" || r.Backoff != "exponential" {
		t.Errorf("nested retry reference not resolved from named retry: %+v", r)
	}
}

func TestNamedErrorHandlingEmptyFails(t *testing.T) {
	hcl := `
error_handling "empty" {
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected non-empty error for named error_handling; got: %v", err)
	}
}

func TestFlowErrorHandlingUnknownNameFails(t *testing.T) {
	hcl := `
error_handling "standard" {
  retry {
    attempts = 3
  }
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
    use = "error_handling.standardd"
  }
}
`
	err := parseDirErr(t, hcl)
	if err == nil || !strings.Contains(err.Error(), "standardd") || !strings.Contains(err.Error(), "standard") {
		t.Errorf("expected unknown-name error listing available; got: %v", err)
	}
}
