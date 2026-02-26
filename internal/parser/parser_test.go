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

func TestParseFlowWithFilter(t *testing.T) {
	hcl := `
flow "process_orders" {
  from {
    connector = "rabbit"
    operation = "orders.new"
    filter    = "input.metadata.origin != 'internal'"
  }

  to {
    connector = "db"
    target    = "orders"
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
	if flow.Name != "process_orders" {
		t.Errorf("expected name 'process_orders', got '%s'", flow.Name)
	}
	if flow.From.Filter != "input.metadata.origin != 'internal'" {
		t.Errorf("expected filter expression, got '%s'", flow.From.Filter)
	}
}

func TestParseFlowWithSteps(t *testing.T) {
	hcl := `
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  step "get_user" {
    connector = "db"
    query     = "SELECT * FROM users WHERE id = :user_id"
    params    = { user_id = "input.user_id" }
  }

  step "get_prices" {
    connector = "pricing_api"
    operation = "GET /prices"
    when      = "input.include_prices == true"
    on_error  = "skip"
    default   = []
  }

  step "calculate" {
    connector = "calculator"
    operation = "POST /calculate"
    body      = { items = "input.items", prices = "step.get_prices" }
    timeout   = "30s"
  }

  transform {
    user_email = "step.get_user.email"
    total      = "step.calculate.total"
    items      = "input.items"
  }

  to {
    connector = "db"
    target    = "orders"
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
	if flow.Name != "create_order" {
		t.Errorf("expected name 'create_order', got '%s'", flow.Name)
	}

	// Check steps
	if len(flow.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(flow.Steps))
	}

	// Check step 1: get_user
	step1 := flow.Steps[0]
	if step1.Name != "get_user" {
		t.Errorf("expected step name 'get_user', got '%s'", step1.Name)
	}
	if step1.Connector != "db" {
		t.Errorf("expected connector 'db', got '%s'", step1.Connector)
	}
	if step1.Query != "SELECT * FROM users WHERE id = :user_id" {
		t.Errorf("expected query, got '%s'", step1.Query)
	}
	if step1.Params["user_id"] != "input.user_id" {
		t.Errorf("expected param user_id='input.user_id', got '%v'", step1.Params["user_id"])
	}

	// Check step 2: get_prices (with conditional)
	step2 := flow.Steps[1]
	if step2.Name != "get_prices" {
		t.Errorf("expected step name 'get_prices', got '%s'", step2.Name)
	}
	if step2.When != "input.include_prices == true" {
		t.Errorf("expected when condition, got '%s'", step2.When)
	}
	if step2.OnError != "skip" {
		t.Errorf("expected on_error 'skip', got '%s'", step2.OnError)
	}

	// Check step 3: calculate (with body and timeout)
	step3 := flow.Steps[2]
	if step3.Name != "calculate" {
		t.Errorf("expected step name 'calculate', got '%s'", step3.Name)
	}
	if step3.Operation != "POST /calculate" {
		t.Errorf("expected operation 'POST /calculate', got '%s'", step3.Operation)
	}
	if step3.Timeout != "30s" {
		t.Errorf("expected timeout '30s', got '%s'", step3.Timeout)
	}
	if step3.Body["items"] != "input.items" {
		t.Errorf("expected body.items='input.items', got '%v'", step3.Body["items"])
	}

	// Check transform references step results
	if flow.Transform == nil {
		t.Fatal("expected transform block")
	}
	if flow.Transform.Mappings["user_email"] != "step.get_user.email" {
		t.Errorf("expected user_email mapping, got '%s'", flow.Transform.Mappings["user_email"])
	}
}

func TestParseFlowWithErrorHandling(t *testing.T) {
	hcl := `
flow "process_orders" {
  from {
    connector = "rabbit"
    operation = "orders.new"
  }

  error_handling {
    retry {
      attempts  = 3
      delay     = "1s"
      max_delay = "30s"
      backoff   = "exponential"
    }

    fallback {
      connector     = "dlq"
      target        = "orders.failed"
      include_error = true
    }
  }

  to {
    connector = "db"
    target    = "orders"
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
	if flow.Name != "process_orders" {
		t.Errorf("expected name 'process_orders', got '%s'", flow.Name)
	}

	// Check error_handling
	if flow.ErrorHandling == nil {
		t.Fatal("expected error_handling block")
	}

	// Check retry
	if flow.ErrorHandling.Retry == nil {
		t.Fatal("expected retry block")
	}
	if flow.ErrorHandling.Retry.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", flow.ErrorHandling.Retry.Attempts)
	}
	if flow.ErrorHandling.Retry.Delay != "1s" {
		t.Errorf("expected delay '1s', got '%s'", flow.ErrorHandling.Retry.Delay)
	}
	if flow.ErrorHandling.Retry.MaxDelay != "30s" {
		t.Errorf("expected max_delay '30s', got '%s'", flow.ErrorHandling.Retry.MaxDelay)
	}
	if flow.ErrorHandling.Retry.Backoff != "exponential" {
		t.Errorf("expected backoff 'exponential', got '%s'", flow.ErrorHandling.Retry.Backoff)
	}

	// Check fallback
	if flow.ErrorHandling.Fallback == nil {
		t.Fatal("expected fallback block")
	}
	if flow.ErrorHandling.Fallback.Connector != "dlq" {
		t.Errorf("expected fallback connector 'dlq', got '%s'", flow.ErrorHandling.Fallback.Connector)
	}
	if flow.ErrorHandling.Fallback.Target != "orders.failed" {
		t.Errorf("expected fallback target 'orders.failed', got '%s'", flow.ErrorHandling.Fallback.Target)
	}
	if !flow.ErrorHandling.Fallback.IncludeError {
		t.Error("expected include_error to be true")
	}
}

func TestParseFlowWithMultiTo(t *testing.T) {
	hcl := `
flow "fan_out_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  transform {
    id         = "uuid()"
    user_id    = "input.user_id"
    product_id = "input.product_id"
    total      = "input.total"
    created_at = "now()"
  }

  # Primary destination
  to {
    connector = "orders_db"
    target    = "orders"
  }

  # Analytics - parallel
  to {
    connector = "analytics_db"
    target    = "order_events"
    transform {
      event_type = "'order_created'"
      order_id   = "output.id"
      timestamp  = "now()"
    }
  }

  # Notification queue - conditional, sequential
  to {
    connector = "rabbit"
    target    = "orders.created"
    when      = "output.total > 1000"
    parallel  = false
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
	if flow.Name != "fan_out_order" {
		t.Errorf("expected flow name 'fan_out_order', got '%s'", flow.Name)
	}

	// Should have MultiTo, not single To
	if flow.To != nil {
		t.Error("expected single To to be nil when multiple to blocks exist")
	}

	if len(flow.MultiTo) != 3 {
		t.Fatalf("expected 3 multi-to destinations, got %d", len(flow.MultiTo))
	}

	// Check first destination (orders_db)
	dest1 := flow.MultiTo[0]
	if dest1.Connector != "orders_db" {
		t.Errorf("expected first dest connector 'orders_db', got '%s'", dest1.Connector)
	}
	if dest1.Target != "orders" {
		t.Errorf("expected first dest target 'orders', got '%s'", dest1.Target)
	}
	if !dest1.Parallel {
		t.Error("expected first dest parallel to be true (default)")
	}

	// Check second destination (analytics_db with transform)
	dest2 := flow.MultiTo[1]
	if dest2.Connector != "analytics_db" {
		t.Errorf("expected second dest connector 'analytics_db', got '%s'", dest2.Connector)
	}
	if len(dest2.Transform) == 0 {
		t.Error("expected second dest to have per-destination transform")
	}
	if dest2.Transform["event_type"] != "'order_created'" {
		t.Errorf("expected transform event_type to be \"'order_created'\", got '%s'", dest2.Transform["event_type"])
	}

	// Check third destination (rabbit with when condition)
	dest3 := flow.MultiTo[2]
	if dest3.Connector != "rabbit" {
		t.Errorf("expected third dest connector 'rabbit', got '%s'", dest3.Connector)
	}
	if dest3.When != "output.total > 1000" {
		t.Errorf("expected third dest when condition, got '%s'", dest3.When)
	}
	if dest3.Parallel {
		t.Error("expected third dest parallel to be false")
	}
}

func TestParseFlowWithDedupe(t *testing.T) {
	hcl := `
flow "process_order" {
  from {
    connector = "rabbit"
    operation = "orders.new"
  }

  dedupe {
    storage      = "redis_cache"
    key          = "'order:' + input.order_id"
    ttl          = "24h"
    on_duplicate = "skip"
  }

  transform {
    id       = "uuid()"
    order_id = "input.order_id"
    status   = "'processing'"
  }

  to {
    connector = "orders_db"
    target    = "orders"
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
	if flow.Dedupe == nil {
		t.Fatal("expected dedupe config to be set")
	}

	if flow.Dedupe.Storage != "redis_cache" {
		t.Errorf("expected dedupe storage 'redis_cache', got '%s'", flow.Dedupe.Storage)
	}
	if flow.Dedupe.Key != "'order:' + input.order_id" {
		t.Errorf("expected dedupe key \"'order:' + input.order_id\", got '%s'", flow.Dedupe.Key)
	}
	if flow.Dedupe.TTL != "24h" {
		t.Errorf("expected dedupe TTL '24h', got '%s'", flow.Dedupe.TTL)
	}
	if flow.Dedupe.OnDuplicate != "skip" {
		t.Errorf("expected dedupe on_duplicate 'skip', got '%s'", flow.Dedupe.OnDuplicate)
	}
}

func TestParseFlowWithEntity(t *testing.T) {
	hcl := `
flow "resolve_product" {
  entity = "Product"

  from {
    connector = "api"
    operation = "Query.product"
  }

  returns = "Product"

  to {
    connector = "db"
    target    = "products"
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
	if flow.Entity != "Product" {
		t.Errorf("expected entity 'Product', got '%s'", flow.Entity)
	}
	if flow.Returns != "Product" {
		t.Errorf("expected returns 'Product', got '%s'", flow.Returns)
	}
}

func TestParseFlowWithSubscriptionTo(t *testing.T) {
	hcl := `
flow "order_updates" {
  from {
    connector = "rabbit"
    operation = "order.*"
  }

  transform {
    orderId = "input.id"
    status  = "input.status"
  }

  to {
    connector = "api"
    operation = "Subscription.orderUpdated"
    filter    = "input.user_id == context.auth.user_id"
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
	if flow.To.Operation != "Subscription.orderUpdated" {
		t.Errorf("expected to.operation 'Subscription.orderUpdated', got '%s'", flow.To.Operation)
	}
	if flow.To.Filter != "input.user_id == context.auth.user_id" {
		t.Errorf("expected to.filter, got '%s'", flow.To.Filter)
	}
}
