package openapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/validate"
)

func TestGenerator_Generate(t *testing.T) {
	config := &parser.Configuration{
		ServiceConfig: &parser.ServiceConfig{
			Name:    "test-service",
			Version: "1.0.0",
		},
		Connectors: []*connector.Config{
			{
				Name: "api",
				Type: "rest",
				Properties: map[string]interface{}{
					"port": 3000,
				},
			},
		},
		Flows: []*flow.Config{
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
			{
				Name: "get_user",
				From: &flow.FromConfig{
					Connector:       "api",
					ConnectorParams: map[string]interface{}{"operation": "GET /users/:id"},
				},
				To: &flow.ToConfig{
					Connector:       "db",
					ConnectorParams: map[string]interface{}{"target": "users"},
				},
			},
			{
				Name: "create_user",
				From: &flow.FromConfig{
					Connector:       "api",
					ConnectorParams: map[string]interface{}{"operation": "POST /users"},
				},
				To: &flow.ToConfig{
					Connector:       "db",
					ConnectorParams: map[string]interface{}{"target": "users"},
				},
			},
		},
		Types: []*validate.TypeSchema{
			{
				Name: "User",
				Fields: []validate.FieldSchema{
					{Name: "id", Type: "integer", Required: true},
					{Name: "email", Type: "string", Required: true},
					{Name: "name", Type: "string"},
				},
			},
		},
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Check basic info
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("OpenAPI = %v, want 3.0.3", spec.OpenAPI)
	}
	if spec.Info.Title != "test-service API" {
		t.Errorf("Info.Title = %v, want test-service API", spec.Info.Title)
	}
	if spec.Info.Version != "1.0.0" {
		t.Errorf("Info.Version = %v, want 1.0.0", spec.Info.Version)
	}

	// Check server
	if len(spec.Servers) != 1 {
		t.Errorf("len(Servers) = %d, want 1", len(spec.Servers))
	} else if spec.Servers[0].URL != "http://localhost:3000" {
		t.Errorf("Servers[0].URL = %v, want http://localhost:3000", spec.Servers[0].URL)
	}

	// Check paths
	if len(spec.Paths) != 2 {
		t.Errorf("len(Paths) = %d, want 2", len(spec.Paths))
	}

	// Check /users path
	usersPath, ok := spec.Paths["/users"]
	if !ok {
		t.Fatal("expected /users path")
	}
	if usersPath.Get == nil {
		t.Error("expected GET operation on /users")
	}
	if usersPath.Post == nil {
		t.Error("expected POST operation on /users")
	}

	// Check /users/{id} path
	userByIDPath, ok := spec.Paths["/users/{id}"]
	if !ok {
		t.Fatal("expected /users/{id} path")
	}
	if userByIDPath.Get == nil {
		t.Error("expected GET operation on /users/{id}")
	}
	if len(userByIDPath.Get.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(userByIDPath.Get.Parameters))
	} else {
		param := userByIDPath.Get.Parameters[0]
		if param.Name != "id" {
			t.Errorf("param.Name = %v, want id", param.Name)
		}
		if param.In != "path" {
			t.Errorf("param.In = %v, want path", param.In)
		}
		if !param.Required {
			t.Error("expected param to be required")
		}
	}

	// Check components
	if spec.Components == nil || len(spec.Components.Schemas) != 1 {
		t.Error("expected 1 schema in components")
	}
	userSchema, ok := spec.Components.Schemas["User"]
	if !ok {
		t.Fatal("expected User schema")
	}
	if len(userSchema.Properties) != 3 {
		t.Errorf("expected 3 properties, got %d", len(userSchema.Properties))
	}
	if len(userSchema.Required) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(userSchema.Required))
	}
}

func TestSpec_ToJSON(t *testing.T) {
	spec := &Spec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: map[string]PathItem{
			"/test": {
				Get: &Operation{
					Summary: "Test endpoint",
					Responses: map[string]Response{
						"200": {Description: "OK"},
					},
				},
			},
		},
	}

	jsonBytes, err := spec.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", parsed["openapi"])
	}
}

func TestSpec_ToYAML(t *testing.T) {
	spec := &Spec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: make(map[string]PathItem),
	}

	yamlBytes, err := spec.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML() error = %v", err)
	}

	yamlStr := string(yamlBytes)
	if !strings.Contains(yamlStr, "openapi: 3.0.3") {
		t.Error("expected openapi: 3.0.3 in YAML output")
	}
	if !strings.Contains(yamlStr, "title: Test API") {
		t.Error("expected title: Test API in YAML output")
	}
}

func TestParseOperation(t *testing.T) {
	tests := []struct {
		op         string
		wantMethod string
		wantPath   string
		wantErr    bool
	}{
		{"GET /users", "GET", "/users", false},
		{"POST /users", "POST", "/users", false},
		{"PUT /users/:id", "PUT", "/users/:id", false},
		{"DELETE /users/:id", "DELETE", "/users/:id", false},
		{"PATCH /users/:id/status", "PATCH", "/users/:id/status", false},
		{"INVALID", "", "", true},
		{"UNKNOWN /path", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			method, path, err := parseOperation(tt.op)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseOperation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if method != tt.wantMethod {
				t.Errorf("method = %v, want %v", method, tt.wantMethod)
			}
			if path != tt.wantPath {
				t.Errorf("path = %v, want %v", path, tt.wantPath)
			}
		})
	}
}

func TestConvertPathParams(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/users/:id", "/users/{id}"},
		{"/users/:userId/orders/:orderId", "/users/{userId}/orders/{orderId}"},
		{"/users", "/users"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := convertPathParams(tt.path)
			if got != tt.want {
				t.Errorf("convertPathParams(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
