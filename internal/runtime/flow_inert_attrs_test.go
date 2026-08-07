package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

func TestInertFlowAttrs_ParamsOnToBlock(t *testing.T) {
	cfg := &parser.Configuration{
		Flows: []*flow.Config{{
			Name: "write_order",
			To: &flow.ToConfig{
				Connector: "db",
				ConnectorParams: map[string]interface{}{
					"target": "orders",
					"params": map[string]interface{}{"id": "input.id"},
				},
			},
		}},
	}

	warnings := InertFlowAttrs(cfg)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	for _, want := range []string{"write_order", "params", "transform"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning missing %q: %s", want, warnings[0])
		}
	}
}

func TestInertFlowAttrs_ParamsOnMultiTo(t *testing.T) {
	cfg := &parser.Configuration{
		Flows: []*flow.Config{{
			Name: "fanout",
			MultiTo: []*flow.ToConfig{
				{Connector: "db", ConnectorParams: map[string]interface{}{"target": "orders"}},
				{Connector: "api", ConnectorParams: map[string]interface{}{
					"params": map[string]interface{}{"id": "input.id"},
				}},
			},
		}},
	}

	warnings := InertFlowAttrs(cfg)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	// The message must point at the offending destination, not just the flow.
	for _, want := range []string{"fanout", "#2", `"api"`} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning missing %q: %s", want, warnings[0])
		}
	}
}

// params is a real attribute on step and inside transaction { exec }; only the
// to block ignores it. Nothing else should trip the warning.
func TestInertFlowAttrs_CleanFlowIsSilent(t *testing.T) {
	cfg := &parser.Configuration{
		Flows: []*flow.Config{{
			Name: "clean",
			From: &flow.FromConfig{Connector: "queue"},
			Steps: []*flow.StepConfig{{
				Connector: "db",
				ConnectorParams: map[string]interface{}{
					"query":  "SELECT * FROM t WHERE id = :id",
					"params": map[string]interface{}{"id": "input.id"},
				},
			}},
			To: &flow.ToConfig{
				Connector:       "db",
				ConnectorParams: map[string]interface{}{"target": "orders"},
			},
		}},
	}

	if warnings := InertFlowAttrs(cfg); len(warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}

func TestInertFlowAttrs_NilSafe(t *testing.T) {
	if warnings := InertFlowAttrs(nil); warnings != nil {
		t.Fatalf("expected nil for nil config, got: %v", warnings)
	}

	cfg := &parser.Configuration{Flows: []*flow.Config{nil, {Name: "no_to"}}}
	if warnings := InertFlowAttrs(cfg); len(warnings) != 0 {
		t.Fatalf("expected no warnings, got: %v", warnings)
	}
}
