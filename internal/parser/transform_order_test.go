package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every block whose attributes are CEL expressions evaluated in sequence has to
// come back out of the parser in the order it was written, because an
// expression may reference a field above it through `output`. hcl.Attributes is
// a map, so the order is not free — it is reconstructed from source positions.

func parseOne(t *testing.T, hcl string) *Configuration {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "flows.mycel")
	if err := os.WriteFile(path, []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := NewHCLParser().ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cfg
}

// parseOneErr is parseOne for the cases where the refusal is the point.
func parseOneErr(t *testing.T, hcl string) (*Configuration, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "flows.mycel")
	if err := os.WriteFile(path, []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return NewHCLParser().ParseFile(context.Background(), path)
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestTransformBlockRecordsDeclarationOrder(t *testing.T) {
	// Deliberately not alphabetical: sorted-by-name would pass by accident.
	cfg := parseOne(t, `
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  transform {
    subtotal = "sum(pluck(input.items, 'price'))"
    tax      = "output.subtotal * 0.21"
    total    = "output.subtotal + output.tax"
    id       = "uuid()"
  }
  to {
    connector = "db"
    target    = "orders"
  }
}
`)
	assertOrder(t, cfg.Flows[0].Transform.Order,
		[]string{"subtotal", "tax", "total", "id"})
}

func TestTransformOrderExcludesUseMarker(t *testing.T) {
	cfg := parseOne(t, `
transform "base" {
  email = "lower(input.email)"
}

flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  transform {
    use = "transform.base"
    id  = "uuid()"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`)
	assertOrder(t, cfg.Flows[0].Transform.Order, []string{"id"})
	assertOrder(t, cfg.Transforms[0].Order, []string{"email"})
}

func TestResponseBlockRecordsDeclarationOrder(t *testing.T) {
	cfg := parseOne(t, `
flow "get_order" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }
  to {
    connector = "db"
    target    = "orders"
  }
  response {
    subtotal = "output.amount"
    tax      = "output.subtotal * 0.21"
    total    = "output.subtotal + output.tax"
  }
}
`)
	assertOrder(t, cfg.Flows[0].ResponseOrder,
		[]string{"subtotal", "tax", "total"})
}

func TestNamedResponseOrderMergesUnderInlineOverrides(t *testing.T) {
	cfg := parseOne(t, `
response "envelope" {
  status = "'ok'"
  data   = "output"
}

flow "get_order" {
  from {
    connector = "api"
    operation = "GET /orders/:id"
  }
  to {
    connector = "db"
    target    = "orders"
  }
  response {
    use       = "response.envelope"
    data      = "output.rows"
    timestamp = "now()"
  }
}
`)
	if err := cfg.ResolveReferences(); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// `data` is overridden inline but keeps the named block's position;
	// `timestamp` is new and follows.
	assertOrder(t, cfg.Flows[0].ResponseOrder,
		[]string{"status", "data", "timestamp"})
	if got := cfg.Flows[0].Response["data"]; got != "output.rows" {
		t.Fatalf("inline override lost: data = %q", got)
	}
}

func TestDestinationTransformRecordsDeclarationOrder(t *testing.T) {
	cfg := parseOne(t, `
flow "fan_out" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  to {
    connector = "db"
    target    = "orders"
    transform {
      order_id = "output.id"
      audit    = "'order:' + output.id"
    }
  }
}
`)
	assertOrder(t, cfg.Flows[0].To.TransformOrder,
		[]string{"order_id", "audit"})
}

func TestErrorResponseBodyRecordsDeclarationOrder(t *testing.T) {
	cfg := parseOne(t, `
flow "create_payment" {
  from {
    connector = "api"
    operation = "POST /payments"
  }
  to {
    connector = "db"
    target    = "payments"
  }
  error_handling {
    error_response {
      status = 503
      body {
        code    = "'service_unavailable'"
        message = "'Payment service is temporarily unavailable.'"
        detail  = "output.code"
      }
    }
  }
}
`)
	assertOrder(t, cfg.Flows[0].ErrorHandling.ErrorResponse.BodyOrder,
		[]string{"code", "message", "detail"})
}

func TestFallbackTransformRecordsDeclarationOrder(t *testing.T) {
	cfg := parseOne(t, `
flow "create_payment" {
  from {
    connector = "api"
    operation = "POST /payments"
  }
  to {
    connector = "db"
    target    = "payments"
  }
  error_handling {
    fallback {
      connector = "rabbit"
      target    = "dead_letters"
      transform {
        reason  = "error.message"
        payload = "input"
        summary = "output.reason"
      }
    }
  }
}
`)
	assertOrder(t, cfg.Flows[0].ErrorHandling.Fallback.TransformOrder,
		[]string{"reason", "payload", "summary"})
}

// A mapping value that is not a string used to reach cty's AsString and panic
// the whole binary with a Go stack trace. A bare number or boolean is
// unambiguous and is kept; anything else has to say so.

func TestBareNumberAndBooleanMappingsAreKept(t *testing.T) {
	cfg := parseOne(t, `
flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
  transform {
    count  = 5
    ratio  = 0.25
    active = true
    off    = false
  }
  to {
    connector = "db"
    target    = "t"
  }
}
`)
	m := cfg.Flows[0].Transform.Mappings
	for field, want := range map[string]string{
		"count": "5", "ratio": "0.25", "active": "true", "off": "false",
	} {
		if m[field] != want {
			t.Errorf("%s = %q, want %q", field, m[field], want)
		}
	}
}

func TestObjectMappingReportsTheFieldInsteadOfPanicking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flows.mycel")
	if err := os.WriteFile(path, []byte(`
flow "f" {
  from {
    connector = "api"
    operation = "POST /x"
  }
  transform {
    body = { message = "ok" }
  }
}
`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := NewHCLParser().ParseFile(context.Background(), path)
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	for _, want := range []string{`"body"`, "quotes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
