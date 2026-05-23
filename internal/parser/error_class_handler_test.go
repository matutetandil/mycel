package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseFlowSource(t *testing.T, hcl string) (*Configuration, error) {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return NewHCLParser().ParseFile(context.Background(), tmpFile)
}

// TestParseOnTimeoutAndOnError: on_timeout / on_error blocks parse into the
// ErrorHandlingConfig with their action.
func TestParseOnTimeoutAndOnError(t *testing.T) {
	hcl := `
flow "process_orders" {
  from {
    connector = "rabbit"
    operation = "orders.new"
  }

  error_handling {
    retry {
      attempts = 3
      delay    = "2s"
    }
    on_timeout {
      action = "ack"
    }
    on_error {
      action = "requeue"
    }
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
`
	cfg, err := parseFlowSource(t, hcl)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(cfg.Flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(cfg.Flows))
	}
	eh := cfg.Flows[0].ErrorHandling
	if eh == nil {
		t.Fatal("expected error_handling block")
	}
	if eh.OnTimeout == nil || eh.OnTimeout.Action != "ack" {
		t.Errorf("expected on_timeout action 'ack', got %+v", eh.OnTimeout)
	}
	if eh.OnError == nil || eh.OnError.Action != "requeue" {
		t.Errorf("expected on_error action 'requeue', got %+v", eh.OnError)
	}
	// retry {} must still parse unchanged alongside the new blocks.
	if eh.Retry == nil || eh.Retry.Attempts != 3 {
		t.Errorf("expected retry attempts 3, got %+v", eh.Retry)
	}
}

// TestParseOnTimeoutInvalidAction: an unknown action value is a parse error.
func TestParseOnTimeoutInvalidAction(t *testing.T) {
	hcl := `
flow "process_orders" {
  from {
    connector = "rabbit"
    operation = "orders.new"
  }

  error_handling {
    on_timeout {
      action = "drop"
    }
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
`
	_, err := parseFlowSource(t, hcl)
	if err == nil {
		t.Fatal("expected a parse error for invalid action 'drop'")
	}
	if !strings.Contains(err.Error(), "invalid action") {
		t.Errorf("expected 'invalid action' in error, got: %v", err)
	}
}
