package openapi

import (
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/parser"
)

// TestGenerator_QueryMethodSkipped: QUERY (RFC 10008) has no slot in OpenAPI
// 3.0's fixed verb set — a QUERY flow must be skipped gracefully, without
// failing the export or dropping sibling flows on the same path.
func TestGenerator_QueryMethodSkipped(t *testing.T) {
	config := &parser.Configuration{
		ServiceConfig: &parser.ServiceConfig{Name: "test-service", Version: "1.0.0"},
		Connectors: []*connector.Config{
			{Name: "api", Type: "rest", Properties: map[string]interface{}{"port": 3000}},
		},
		Flows: []*flow.Config{
			{
				Name: "search_users",
				From: &flow.FromConfig{
					Connector:       "api",
					ConnectorParams: map[string]interface{}{"operation": "QUERY /users"},
				},
				To: &flow.ToConfig{
					Connector:       "db",
					ConnectorParams: map[string]interface{}{"target": "users"},
				},
			},
			{
				Name: "get_users",
				From: &flow.FromConfig{
					Connector:       "api",
					ConnectorParams: map[string]interface{}{"operation": "GET /users"},
				},
				To: &flow.ToConfig{
					Connector:       "db",
					ConnectorParams: map[string]interface{}{"target": "users"},
				},
			},
		},
	}

	spec, err := NewGenerator(config).Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	item, ok := spec.Paths["/users"]
	if !ok {
		t.Fatal("expected /users path from the GET sibling flow")
	}
	if item.Get == nil {
		t.Error("GET operation on /users missing — QUERY skip must not affect siblings")
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
