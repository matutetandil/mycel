package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseConnector(t *testing.T) {
	hcl := `
connector "postgres" {
  type     = "database"
  driver   = "postgres"
  host     = "localhost"
  port     = 5432
  database = "myapp"

  pool {
    min = 5
    max = 20
  }
}
`
	// Write temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "connector.hcl")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(config.Connectors) != 1 {
		t.Fatalf("expected 1 connector, got %d", len(config.Connectors))
	}

	conn := config.Connectors[0]
	if conn.Name != "postgres" {
		t.Errorf("expected name 'postgres', got '%s'", conn.Name)
	}
	if conn.Type != "database" {
		t.Errorf("expected type 'database', got '%s'", conn.Type)
	}
	if conn.Properties["driver"] != "postgres" {
		t.Errorf("expected driver 'postgres', got '%v'", conn.Properties["driver"])
	}
	if conn.Properties["port"] != 5432 {
		t.Errorf("expected port 5432, got '%v'", conn.Properties["port"])
	}
}

func TestParseFlow(t *testing.T) {
	hcl := `
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }

  to {
    connector = "postgres"
    target    = "users"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.hcl")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(config.Flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(config.Flows))
	}

	flow := config.Flows[0]
	if flow.Name != "get_users" {
		t.Errorf("expected name 'get_users', got '%s'", flow.Name)
	}
	if flow.From.Connector != "api" {
		t.Errorf("expected from connector 'api', got '%s'", flow.From.Connector)
	}
	if flow.From.Operation != "GET /users" {
		t.Errorf("expected from operation 'GET /users', got '%s'", flow.From.Operation)
	}
	if flow.To.Connector != "postgres" {
		t.Errorf("expected to connector 'postgres', got '%s'", flow.To.Connector)
	}
	if flow.To.Target != "users" {
		t.Errorf("expected to target 'users', got '%s'", flow.To.Target)
	}
}

func TestParseType(t *testing.T) {
	hcl := `
type "user" {
  id    = number
  email = string
  name  = string
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "type.hcl")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(config.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(config.Types))
	}

	typ := config.Types[0]
	if typ.Name != "user" {
		t.Errorf("expected name 'user', got '%s'", typ.Name)
	}
	if len(typ.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(typ.Fields))
	}
}

func TestParseDirectory(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	connDir := filepath.Join(tmpDir, "connectors")
	flowDir := filepath.Join(tmpDir, "flows")
	if err := os.MkdirAll(connDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(flowDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write connector file
	connHCL := `
connector "db" {
  type   = "database"
  driver = "sqlite"
}
`
	if err := os.WriteFile(filepath.Join(connDir, "db.hcl"), []byte(connHCL), 0644); err != nil {
		t.Fatal(err)
	}

	// Write flow file
	flowHCL := `
flow "test" {
  from {
    connector = "api"
    operation = "GET /test"
  }
  to {
    connector = "db"
    target    = "test"
  }
}
`
	if err := os.WriteFile(filepath.Join(flowDir, "test.hcl"), []byte(flowHCL), 0644); err != nil {
		t.Fatal(err)
	}

	parser := NewHCLParser()
	config, err := parser.Parse(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(config.Connectors) != 1 {
		t.Errorf("expected 1 connector, got %d", len(config.Connectors))
	}
	if len(config.Flows) != 1 {
		t.Errorf("expected 1 flow, got %d", len(config.Flows))
	}
}

func TestEnvFunction(t *testing.T) {
	os.Setenv("TEST_DB_HOST", "testhost")
	defer os.Unsetenv("TEST_DB_HOST")

	hcl := `
connector "db" {
  type = "database"
  host = env("TEST_DB_HOST")
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "db.hcl")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatal(err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if config.Connectors[0].Properties["host"] != "testhost" {
		t.Errorf("expected host 'testhost', got '%v'", config.Connectors[0].Properties["host"])
	}
}
