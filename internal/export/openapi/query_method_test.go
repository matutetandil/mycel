package openapi

import (
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

func queryTestConfig(flows ...*flow.Config) *parser.Configuration {
	return &parser.Configuration{
		ServiceConfig: &parser.ServiceConfig{Name: "test-service", Version: "1.0.0"},
		Connectors: []*connector.Config{
			{Name: "api", Type: "rest", Properties: map[string]interface{}{"port": 3000}},
		},
		Flows: flows,
	}
}

func restFlow(name, operation string) *flow.Config {
	return &flow.Config{
		Name: name,
		From: &flow.FromConfig{
			Connector:       "api",
			ConnectorParams: map[string]interface{}{"operation": operation},
		},
		To: &flow.ToConfig{
			Connector:       "db",
			ConnectorParams: map[string]interface{}{"target": "users"},
		},
	}
}

// TestGenerator_QueryMethodEmitsOpenAPI32: QUERY (RFC 10008) has an operation
// slot from OpenAPI 3.2 on — a config with QUERY flows upgrades the emitted
// spec version and documents the operation, request body included.
func TestGenerator_QueryMethodEmitsOpenAPI32(t *testing.T) {
	config := queryTestConfig(
		restFlow("search_users", "QUERY /users"),
		restFlow("get_users", "GET /users"),
	)

	spec, err := NewGenerator(config).Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if spec.OpenAPI != "3.2.0" {
		t.Errorf("OpenAPI = %v, want 3.2.0 when QUERY flows are present", spec.OpenAPI)
	}

	item, ok := spec.Paths["/users"]
	if !ok {
		t.Fatal("expected /users path")
	}
	if item.Query == nil {
		t.Fatal("QUERY operation missing from path item")
	}
	if item.Query.OperationID != "search_users" {
		t.Errorf("query operationId = %v, want search_users", item.Query.OperationID)
	}
	if item.Query.RequestBody == nil {
		t.Error("QUERY operation must document its request body")
	}
	if item.Get == nil {
		t.Error("GET sibling on the same path must be unaffected")
	}
}

// TestGenerator_NoQueryKeepsDefaultVersion: configs without QUERY flows keep
// emitting 3.0.3 for maximum tooling compatibility.
func TestGenerator_NoQueryKeepsDefaultVersion(t *testing.T) {
	config := queryTestConfig(restFlow("get_users", "GET /users"))

	spec, err := NewGenerator(config).Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("OpenAPI = %v, want 3.0.3 without QUERY flows", spec.OpenAPI)
	}
}

func TestParseOperation_QueryAccepted(t *testing.T) {
	method, path, err := parseOperation("QUERY /search")
	if err != nil {
		t.Fatalf("parseOperation(QUERY /search) error = %v, want QUERY recognized", err)
	}
	if method != "QUERY" || path != "/search" {
		t.Errorf("parseOperation = (%s, %s), want (QUERY, /search)", method, path)
	}
}
