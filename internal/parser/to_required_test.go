package parser

import (
	"strings"
	"testing"
)

// `required` on a destination is what makes one write decide the outcome of a
// flow with several. It is read as a boolean and nothing else: a quoted
// "true" is not true to HCL, and reading it as false would leave the main
// write unprotected while the file says otherwise.
func TestARequiredDestinationIsParsed(t *testing.T) {
	config, err := parseString(t, `
flow "item_update" {
  from {
    connector = "rabbit"
    operation = "items"
  }
  to {
    connector = "db"
    target    = "items"
    required  = true
  }
  to {
    connector = "products_api"
    operation = "POST /cache/invalidate"
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dests := config.Flows[0].MultiTo
	if len(dests) != 2 {
		t.Fatalf("%d destinations", len(dests))
	}
	if !dests[0].Required || dests[1].Required {
		t.Errorf("required = %v, %v; want true, false", dests[0].Required, dests[1].Required)
	}
	if _, swept := dests[0].ConnectorParams["required"]; swept {
		t.Error("required was swept into the connector params")
	}
}

func TestRequiredMustBeABoolean(t *testing.T) {
	_, err := parseString(t, `
flow "item_update" {
  from {
    connector = "rabbit"
    operation = "items"
  }
  to {
    connector = "db"
    target    = "items"
    required  = "yes"
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v, want required refused", err)
	}
}
