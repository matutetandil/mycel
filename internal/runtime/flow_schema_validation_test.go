package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// newConfig builds a Configuration with one connector and one flow reading from it.
func newConfig(connName, connType, driver string, fromParams map[string]interface{}) *parser.Configuration {
	return &parser.Configuration{
		Connectors: []*connector.Config{
			{Name: connName, Type: connType, Driver: driver},
		},
		Flows: []*flow.Config{
			{
				Name: "test_flow",
				From: &flow.FromConfig{Connector: connName, ConnectorParams: fromParams},
			},
		},
	}
}

func TestValidateFlowSchemas_RESTRequiresOperation(t *testing.T) {
	reg := NewSchemaRegistry()
	cfg := newConfig("api", "rest", "", map[string]interface{}{})

	errs := ValidateFlowSchemas(cfg, reg)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	msg := errs[0].Error()
	for _, want := range []string{"test_flow", "operation", "api", "rest"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
}

func TestValidateFlowSchemas_RESTWithOperationPasses(t *testing.T) {
	reg := NewSchemaRegistry()
	cfg := newConfig("api", "rest", "", map[string]interface{}{"operation": "GET /users"})

	if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 0 {
		t.Fatalf("expected no errors, got: %v", errs)
	}
}

func TestValidateFlowSchemas_QueueOperationIsOptional(t *testing.T) {
	// Message queues default operation to the catch-all "*", so omitting it is legal.
	reg := NewSchemaRegistry()
	for _, driver := range []string{"rabbitmq", "kafka", "redis"} {
		cfg := newConfig("q", "mq", driver, map[string]interface{}{})
		if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 0 {
			t.Errorf("driver %s: expected no errors, got: %v", driver, errs)
		}
	}
}

func TestValidateFlowSchemas_OtherOptionalSources(t *testing.T) {
	reg := NewSchemaRegistry()
	for _, connType := range []string{"mqtt", "cdc", "websocket", "file"} {
		cfg := newConfig("src", connType, "", map[string]interface{}{})
		if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 0 {
			t.Errorf("type %s: expected no errors, got: %v", connType, errs)
		}
	}
}

func TestValidateFlowSchemas_OtherRequiredSources(t *testing.T) {
	reg := NewSchemaRegistry()
	for _, connType := range []string{"graphql", "grpc", "soap", "tcp", "sse"} {
		cfg := newConfig("src", connType, "", map[string]interface{}{})
		if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 1 {
			t.Errorf("type %s: expected 1 error, got %d: %v", connType, len(errs), errs)
		}
	}
}

func TestValidateFlowSchemas_BlankOperationCountsAsMissing(t *testing.T) {
	reg := NewSchemaRegistry()
	cfg := newConfig("api", "rest", "", map[string]interface{}{"operation": "   "})

	if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 1 {
		t.Fatalf("expected 1 error for blank operation, got %d: %v", len(errs), errs)
	}
}

func TestValidateFlowSchemas_UnknownConnectorTypeSkipped(t *testing.T) {
	// Plugin-provided connectors have no registered schema; they must not be flagged.
	reg := NewSchemaRegistry()
	cfg := newConfig("custom", "my-wasm-plugin", "", map[string]interface{}{})

	if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 0 {
		t.Fatalf("expected no errors for unknown connector type, got: %v", errs)
	}
}

func TestValidateFlowSchemas_UnknownConnectorNameSkipped(t *testing.T) {
	// registerFlows reports the dangling reference with a clearer message.
	reg := NewSchemaRegistry()
	cfg := newConfig("api", "rest", "", map[string]interface{}{"operation": "GET /x"})
	cfg.Flows[0].From.Connector = "does_not_exist"

	if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 0 {
		t.Fatalf("expected no errors for unknown connector name, got: %v", errs)
	}
}

func TestValidateFlowSchemas_NoFromBlockSkipped(t *testing.T) {
	reg := NewSchemaRegistry()
	cfg := newConfig("api", "rest", "", nil)
	cfg.Flows[0].From = nil

	if errs := ValidateFlowSchemas(cfg, reg); len(errs) != 0 {
		t.Fatalf("expected no errors for flow without from block, got: %v", errs)
	}
}

func TestValidateFlowSchemas_NilInputs(t *testing.T) {
	if errs := ValidateFlowSchemas(nil, NewSchemaRegistry()); errs != nil {
		t.Errorf("expected nil for nil config, got: %v", errs)
	}
	if errs := ValidateFlowSchemas(newConfig("api", "rest", "", nil), nil); errs != nil {
		t.Errorf("expected nil for nil registry, got: %v", errs)
	}
}
