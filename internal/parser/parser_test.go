package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	tmpFile := filepath.Join(tmpDir, "connector.mycel")
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	if flow.From.GetOperation() != "GET /users" {
		t.Errorf("expected from operation 'GET /users', got '%s'", flow.From.GetOperation())
	}
	if flow.To.Connector != "postgres" {
		t.Errorf("expected to connector 'postgres', got '%s'", flow.To.Connector)
	}
	if flow.To.GetTarget() != "users" {
		t.Errorf("expected to target 'users', got '%s'", flow.To.GetTarget())
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
	tmpFile := filepath.Join(tmpDir, "type.mycel")
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
	if err := os.WriteFile(filepath.Join(connDir, "db.mycel"), []byte(connHCL), 0644); err != nil {
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
	if err := os.WriteFile(filepath.Join(flowDir, "test.mycel"), []byte(flowHCL), 0644); err != nil {
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
	tmpFile := filepath.Join(tmpDir, "db.mycel")
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	// FilterConfig should also be set for string syntax
	if flow.From.FilterConfig == nil {
		t.Fatal("expected FilterConfig to be set for string filter syntax")
	}
	if flow.From.FilterConfig.Condition != "input.metadata.origin != 'internal'" {
		t.Errorf("expected FilterConfig.Condition to match, got '%s'", flow.From.FilterConfig.Condition)
	}
	if flow.From.FilterConfig.OnReject != "ack" {
		t.Errorf("expected OnReject 'ack', got '%s'", flow.From.FilterConfig.OnReject)
	}
}

func TestParseFlowWithFilterBlock(t *testing.T) {
	hcl := `
flow "process_sales" {
  from {
    connector = "rabbit"
    operation = "events"

    filter {
      condition   = "input.headers.elementType == 'sales-associate'"
      on_reject   = "requeue"
      id_field    = "input.properties.message_id"
      max_requeue = 5
    }
  }

  to {
    connector = "db"
    target    = "sales"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	if flow.From.FilterConfig == nil {
		t.Fatal("expected FilterConfig to be set")
	}
	fc := flow.From.FilterConfig
	if fc.Condition != "input.headers.elementType == 'sales-associate'" {
		t.Errorf("expected condition, got '%s'", fc.Condition)
	}
	if fc.OnReject != "requeue" {
		t.Errorf("expected on_reject 'requeue', got '%s'", fc.OnReject)
	}
	if fc.IDField != "input.properties.message_id" {
		t.Errorf("expected id_field, got '%s'", fc.IDField)
	}
	if fc.MaxRequeue != 5 {
		t.Errorf("expected max_requeue 5, got %d", fc.MaxRequeue)
	}
	// Filter string should be set for backwards compatibility
	if flow.From.Filter != fc.Condition {
		t.Errorf("expected From.Filter to equal FilterConfig.Condition")
	}
}

func TestParseFlowWithFilterBlockReject(t *testing.T) {
	hcl := `
flow "process_events" {
  from {
    connector = "kafka"
    operation = "events"

    filter {
      condition = "input.body.type == 'order'"
      on_reject = "reject"
    }
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	flow := config.Flows[0]
	if flow.From.FilterConfig == nil {
		t.Fatal("expected FilterConfig to be set")
	}
	if flow.From.FilterConfig.OnReject != "reject" {
		t.Errorf("expected on_reject 'reject', got '%s'", flow.From.FilterConfig.OnReject)
	}
	// max_requeue should default to 3
	if flow.From.FilterConfig.MaxRequeue != 3 {
		t.Errorf("expected default max_requeue 3, got %d", flow.From.FilterConfig.MaxRequeue)
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	if step1.GetQuery() != "SELECT * FROM users WHERE id = :user_id" {
		t.Errorf("expected query, got '%s'", step1.GetQuery())
	}
	if step1.GetParams()["user_id"] != "input.user_id" {
		t.Errorf("expected param user_id='input.user_id', got '%v'", step1.GetParams()["user_id"])
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
	if step3.GetOperation() != "POST /calculate" {
		t.Errorf("expected operation 'POST /calculate', got '%s'", step3.GetOperation())
	}
	if step3.Timeout != "30s" {
		t.Errorf("expected timeout '30s', got '%s'", step3.Timeout)
	}
	if step3.GetBody()["items"] != "input.items" {
		t.Errorf("expected body.items='input.items', got '%v'", step3.GetBody()["items"])
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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

func TestParseFlowWithErrorResponse(t *testing.T) {
	hcl := `
flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  error_handling {
    error_response {
      status = 422

      body {
        code    = "'VALIDATION_ERROR'"
        message = "error.message"
      }
    }
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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

	if flow.ErrorHandling == nil {
		t.Fatal("expected error_handling block")
	}

	if flow.ErrorHandling.ErrorResponse == nil {
		t.Fatal("expected error_response block")
	}

	er := flow.ErrorHandling.ErrorResponse
	if er.Status != 422 {
		t.Errorf("expected status 422, got %d", er.Status)
	}

	if len(er.Body) != 2 {
		t.Errorf("expected 2 body fields, got %d", len(er.Body))
	}

	if er.Body["code"] != "'VALIDATION_ERROR'" {
		t.Errorf("expected body.code = \"'VALIDATION_ERROR'\", got %q", er.Body["code"])
	}

	if er.Body["message"] != "error.message" {
		t.Errorf("expected body.message = \"error.message\", got %q", er.Body["message"])
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	if dest1.GetTarget() != "orders" {
		t.Errorf("expected first dest target 'orders', got '%s'", dest1.GetTarget())
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	if flow.To.GetOperation() != "Subscription.orderUpdated" {
		t.Errorf("expected to.operation 'Subscription.orderUpdated', got '%s'", flow.To.GetOperation())
	}
	if flow.To.GetFilter() != "input.user_id == context.auth.user_id" {
		t.Errorf("expected to.filter, got '%s'", flow.To.GetFilter())
	}
}

func TestParseSaga(t *testing.T) {
	hcl := `
saga "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }

  step "order" {
    action {
      connector = "orders_db"
      operation = "INSERT"
      target    = "orders"
      data = {
        status = "pending"
      }
    }
    compensate {
      connector = "orders_db"
      operation = "DELETE"
      target    = "orders"
    }
  }

  step "payment" {
    on_error = "skip"
    action {
      connector = "stripe"
      operation = "POST /charges"
      body = {
        amount = "100"
      }
    }
    compensate {
      connector = "stripe"
      operation = "POST /refunds"
    }
  }

  on_complete {
    connector = "orders_db"
    operation = "UPDATE"
    target    = "orders"
    set = {
      status = "confirmed"
    }
  }

  on_failure {
    connector = "notifications"
    operation = "POST /send"
    template  = "order_failed"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "saga.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(config.Sagas) != 1 {
		t.Fatalf("expected 1 saga, got %d", len(config.Sagas))
	}

	saga := config.Sagas[0]
	if saga.Name != "create_order" {
		t.Errorf("expected saga name 'create_order', got '%s'", saga.Name)
	}
	if saga.From == nil {
		t.Fatal("expected saga from block")
	}
	if saga.From.Connector != "api" {
		t.Errorf("expected from connector 'api', got '%s'", saga.From.Connector)
	}
	if saga.From.Operation != "POST /orders" {
		t.Errorf("expected from operation 'POST /orders', got '%s'", saga.From.Operation)
	}
	if len(saga.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(saga.Steps))
	}
	if saga.Steps[0].Name != "order" {
		t.Errorf("expected step 0 name 'order', got '%s'", saga.Steps[0].Name)
	}
	if saga.Steps[0].Action == nil {
		t.Fatal("expected step 0 action")
	}
	if saga.Steps[0].Compensate == nil {
		t.Fatal("expected step 0 compensate")
	}
	if saga.Steps[1].OnError != "skip" {
		t.Errorf("expected step 1 on_error 'skip', got '%s'", saga.Steps[1].OnError)
	}
	if saga.OnComplete == nil {
		t.Fatal("expected on_complete block")
	}
	if saga.OnComplete.Operation != "UPDATE" {
		t.Errorf("expected on_complete operation 'UPDATE', got '%s'", saga.OnComplete.Operation)
	}
	if saga.OnFailure == nil {
		t.Fatal("expected on_failure block")
	}
	if saga.OnFailure.Template != "order_failed" {
		t.Errorf("expected on_failure template 'order_failed', got '%s'", saga.OnFailure.Template)
	}
}

func TestParseSagaWithDelayAndAwait(t *testing.T) {
	hcl := `
saga "order_fulfillment" {
  timeout = "24h"

  from {
    connector = "api"
    operation = "POST /orders"
  }

  step "create_order" {
    action {
      connector = "orders_db"
      operation = "INSERT"
      target    = "orders"
    }
  }

  step "wait_processing" {
    delay = "5m"
  }

  step "await_payment" {
    await   = "payment_confirmed"
    timeout = "1h"
  }

  step "ship_order" {
    action {
      connector = "shipping"
      operation = "POST /shipments"
    }
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "saga.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(config.Sagas) != 1 {
		t.Fatalf("expected 1 saga, got %d", len(config.Sagas))
	}

	saga := config.Sagas[0]
	if saga.Timeout != "24h" {
		t.Errorf("expected saga timeout '24h', got '%s'", saga.Timeout)
	}
	if len(saga.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(saga.Steps))
	}

	// Step 0: normal action
	if saga.Steps[0].Action == nil {
		t.Error("expected step 0 to have action")
	}

	// Step 1: delay
	if saga.Steps[1].Delay != "5m" {
		t.Errorf("expected step 1 delay '5m', got '%s'", saga.Steps[1].Delay)
	}
	if saga.Steps[1].Action != nil {
		t.Error("delay step should not have action")
	}

	// Step 2: await with timeout
	if saga.Steps[2].Await != "payment_confirmed" {
		t.Errorf("expected step 2 await 'payment_confirmed', got '%s'", saga.Steps[2].Await)
	}
	if saga.Steps[2].Timeout != "1h" {
		t.Errorf("expected step 2 timeout '1h', got '%s'", saga.Steps[2].Timeout)
	}

	// Step 3: normal action
	if saga.Steps[3].Action == nil {
		t.Error("expected step 3 to have action")
	}
}

func TestParseServiceWithWorkflow(t *testing.T) {
	tmpDir := t.TempDir()

	configHCL := `
service {
  name    = "order-service"
  version = "1.0.0"

  workflow {
    storage     = "orders_db"
    table       = "workflow_instances"
    auto_create = true
  }
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(configHCL), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	p := NewHCLParser()
	config, err := p.Parse(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if config.ServiceConfig == nil {
		t.Fatal("expected ServiceConfig to be set")
	}
	if config.ServiceConfig.Workflow == nil {
		t.Fatal("expected Workflow config to be set")
	}
	if config.ServiceConfig.Workflow.Storage != "orders_db" {
		t.Errorf("expected workflow storage 'orders_db', got '%s'", config.ServiceConfig.Workflow.Storage)
	}
	if config.ServiceConfig.Workflow.Table != "workflow_instances" {
		t.Errorf("expected workflow table 'workflow_instances', got '%s'", config.ServiceConfig.Workflow.Table)
	}
	if !config.ServiceConfig.Workflow.AutoCreate {
		t.Error("expected workflow auto_create to be true")
	}
}

func TestParseStateMachine(t *testing.T) {
	hcl := `
state_machine "order_status" {
  initial = "pending"

  state "pending" {
    on "pay" {
      transition_to = "paid"
    }
    on "cancel" {
      transition_to = "cancelled"
    }
  }

  state "paid" {
    on "ship" {
      transition_to = "shipped"
      guard         = "input.tracking_number != ''"
      action {
        connector = "notifications"
        operation = "POST /send"
        template  = "order_shipped"
      }
    }
  }

  state "shipped" {
    on "deliver" {
      transition_to = "delivered"
    }
  }

  state "delivered" {
    final = true
  }

  state "cancelled" {
    final = true
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "machine.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(config.StateMachines) != 1 {
		t.Fatalf("expected 1 state machine, got %d", len(config.StateMachines))
	}

	sm := config.StateMachines[0]
	if sm.Name != "order_status" {
		t.Errorf("expected name 'order_status', got '%s'", sm.Name)
	}
	if sm.Initial != "pending" {
		t.Errorf("expected initial 'pending', got '%s'", sm.Initial)
	}
	if len(sm.States) != 5 {
		t.Errorf("expected 5 states, got %d", len(sm.States))
	}

	// Check pending state
	pending := sm.States["pending"]
	if pending == nil {
		t.Fatal("expected 'pending' state")
	}
	if len(pending.Transitions) != 2 {
		t.Errorf("expected 2 transitions from pending, got %d", len(pending.Transitions))
	}
	if pending.Transitions["pay"].TransitionTo != "paid" {
		t.Errorf("expected pay -> paid, got '%s'", pending.Transitions["pay"].TransitionTo)
	}

	// Check paid state with guard and action
	paid := sm.States["paid"]
	if paid == nil {
		t.Fatal("expected 'paid' state")
	}
	ship := paid.Transitions["ship"]
	if ship == nil {
		t.Fatal("expected 'ship' transition")
	}
	if ship.Guard != "input.tracking_number != ''" {
		t.Errorf("expected guard expression, got '%s'", ship.Guard)
	}
	if ship.Action == nil {
		t.Fatal("expected ship transition action")
	}
	if ship.Action.Connector != "notifications" {
		t.Errorf("expected action connector 'notifications', got '%s'", ship.Action.Connector)
	}

	// Check final states
	delivered := sm.States["delivered"]
	if delivered == nil || !delivered.Final {
		t.Error("expected 'delivered' to be a final state")
	}
	cancelled := sm.States["cancelled"]
	if cancelled == nil || !cancelled.Final {
		t.Error("expected 'cancelled' to be a final state")
	}
}

func TestParseFlowWithStateTransition(t *testing.T) {
	hcl := `
flow "update_order_status" {
  from {
    connector = "api"
    operation = "POST /orders/:id/status"
  }

  state_transition {
    machine = "order_status"
    entity  = "orders"
    id      = "input.params.id"
    event   = "input.event"
    data    = "input.data"
  }

  to {
    connector = "orders_db"
    target    = "orders"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	if flow.StateTransition == nil {
		t.Fatal("expected state_transition block")
	}
	if flow.StateTransition.Machine != "order_status" {
		t.Errorf("expected machine 'order_status', got '%s'", flow.StateTransition.Machine)
	}
	if flow.StateTransition.Entity != "orders" {
		t.Errorf("expected entity 'orders', got '%s'", flow.StateTransition.Entity)
	}
	if flow.StateTransition.ID != "input.params.id" {
		t.Errorf("expected id 'input.params.id', got '%s'", flow.StateTransition.ID)
	}
	if flow.StateTransition.Event != "input.event" {
		t.Errorf("expected event 'input.event', got '%s'", flow.StateTransition.Event)
	}
	if flow.StateTransition.Data != "input.data" {
		t.Errorf("expected data 'input.data', got '%s'", flow.StateTransition.Data)
	}
}

func TestParseServiceWithAdminPort(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-parser-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configHCL := `
service {
  name       = "worker-service"
  version    = "2.0.0"
  admin_port = 8081
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(configHCL), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	p := NewHCLParser()
	config, err := p.Parse(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if config.ServiceConfig == nil {
		t.Fatal("expected ServiceConfig to be set")
	}
	if config.ServiceConfig.Name != "worker-service" {
		t.Errorf("expected name 'worker-service', got '%s'", config.ServiceConfig.Name)
	}
	if config.ServiceConfig.AdminPort != 8081 {
		t.Errorf("expected admin_port 8081, got %d", config.ServiceConfig.AdminPort)
	}
}

func TestParseSkipsPluginManifests(t *testing.T) {
	// Create a config tree with a plugin manifest inside it.
	// The parser should skip the manifest and parse the rest.
	tmpDir := t.TempDir()

	// Main config
	mainCfg := `
connector "api" {
  type = "rest"
  port = 3000
}

plugin "my-plugin" {
  source = "./my-plugin"
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.mycel"), []byte(mainCfg), 0644); err != nil {
		t.Fatal(err)
	}

	// Plugin directory with manifest (should be skipped)
	pluginDir := filepath.Join(tmpDir, "my-plugin")
	os.MkdirAll(pluginDir, 0755)

	manifest := `
plugin {
  name    = "my-plugin"
  version = "1.0.0"
}
provides {
  validator "test_v" {
    wasm       = "v.wasm"
    entrypoint = "validate"
  }
}
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mycel"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewHCLParser()
	config, err := p.Parse(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("Parse failed (should skip manifest): %v", err)
	}

	if len(config.Connectors) != 1 {
		t.Errorf("expected 1 connector, got %d", len(config.Connectors))
	}
	if len(config.Plugins) != 1 {
		t.Errorf("expected 1 plugin declaration, got %d", len(config.Plugins))
	}
}

func TestIsPluginManifest(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name: "plugin manifest with provides",
			content: `
plugin {
  name    = "test"
  version = "1.0.0"
}
provides {
  validator "always_valid" {
    wasm       = "validators.wasm"
    entrypoint = "validate"
  }
}`,
			expected: true,
		},
		{
			name: "config plugin declaration with label",
			content: `
plugin "test" {
  source  = "./plugins/test"
  version = "^1.0"
}`,
			expected: false,
		},
		{
			name: "regular config file",
			content: `
connector "api" {
  type = "rest"
  port = 3000
}`,
			expected: false,
		},
		{
			name: "plugin block without provides",
			content: `
plugin {
  name    = "test"
  version = "1.0.0"
}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "test.mycel")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			got := isPluginManifest(tmpFile)
			if got != tt.expected {
				t.Errorf("isPluginManifest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseFlowWithAccept(t *testing.T) {
	hcl := `
flow "process_orders" {
  from {
    connector = "rabbit"
    operation = "orders"
    filter    = "input.type == 'order'"
  }

  accept {
    when      = "input.region == 'us-east'"
    on_reject = "requeue"
  }

  to {
    connector = "db"
    target    = "orders"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
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
	if flow.Accept == nil {
		t.Fatal("expected Accept to be set")
	}
	if flow.Accept.When != "input.region == 'us-east'" {
		t.Errorf("expected accept when expression, got '%s'", flow.Accept.When)
	}
	if flow.Accept.OnReject != "requeue" {
		t.Errorf("expected on_reject 'requeue', got '%s'", flow.Accept.OnReject)
	}
}

func TestParseFlowWithAcceptDefaultOnReject(t *testing.T) {
	hcl := `
flow "process_events" {
  from {
    connector = "kafka"
    operation = "events"
  }

  accept {
    when = "input.payload.type == 'A1'"
  }

  to {
    connector = "db"
    target    = "events"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	flow := config.Flows[0]
	if flow.Accept == nil {
		t.Fatal("expected Accept to be set")
	}
	if flow.Accept.OnReject != "ack" {
		t.Errorf("expected default on_reject 'ack', got '%s'", flow.Accept.OnReject)
	}
}

func TestParseFlowWithAcceptInvalidOnReject(t *testing.T) {
	hcl := `
flow "bad_flow" {
  from {
    connector = "rabbit"
    operation = "events"
  }

  accept {
    when      = "input.type == 'X'"
    on_reject = "invalid"
  }

  to {
    connector = "db"
    target    = "events"
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	_, err := parser.ParseFile(context.Background(), tmpFile)
	if err == nil {
		t.Fatal("expected error for invalid on_reject value")
	}
}

// TestSyncStorageNumericPort covers the canonical case: port given as a number.
func TestSyncStorageNumericPort(t *testing.T) {
	hcl := `
flow "x" {
  from {
    connector = "redis"
    target    = "trigger"
  }

  coordinate {
    storage {
      driver = "redis"
      host   = "localhost"
      port   = 6379
      db     = 0
    }
    timeout = "5s"
    wait {
      when = "true"
      for  = "'k'"
    }
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	cfg, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(cfg.Flows) != 1 || cfg.Flows[0].Coordinate == nil || cfg.Flows[0].Coordinate.Storage == nil {
		t.Fatalf("expected coordinate.storage to be parsed")
	}
	if cfg.Flows[0].Coordinate.Storage.Port != 6379 {
		t.Errorf("expected port 6379, got %d", cfg.Flows[0].Coordinate.Storage.Port)
	}
}

// TestSyncStorageStringPort guards against the v1.19.0 panic: env() returns
// strings, so port = "6379" must parse cleanly instead of crashing the runtime.
func TestSyncStorageStringPort(t *testing.T) {
	hcl := `
flow "x" {
  from {
    connector = "redis"
    target    = "trigger"
  }

  coordinate {
    storage {
      driver = "redis"
      host   = "localhost"
      port   = "6379"
      db     = "0"
    }
    timeout = "5s"
    wait {
      when = "true"
      for  = "'k'"
    }
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	cfg, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	storage := cfg.Flows[0].Coordinate.Storage
	if storage.Port != 6379 {
		t.Errorf("expected port 6379, got %d", storage.Port)
	}
	if storage.DB != 0 {
		t.Errorf("expected db 0, got %d", storage.DB)
	}
}

// TestSyncStorageInvalidPort ensures non-numeric strings produce a typed error
// instead of a panic.
func TestSyncStorageInvalidPort(t *testing.T) {
	hcl := `
flow "x" {
  from {
    connector = "redis"
    target    = "trigger"
  }

  coordinate {
    storage {
      driver = "redis"
      host   = "localhost"
      port   = "not-a-number"
    }
    timeout = "5s"
    wait {
      when = "true"
      for  = "'k'"
    }
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flow.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	_, err := parser.ParseFile(context.Background(), tmpFile)
	if err == nil {
		t.Fatal("expected error for non-numeric port")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("expected error to mention port, got: %v", err)
	}
}

// TestConnectorRetryBlock validates the canonical retry { attempts = N } form
// on connectors. Previously the parser accepted attempts + backoff but the
// docs/IDE schema declared count + interval + backoff — the divergence meant
// no documented form actually worked.
func TestConnectorRetryBlock(t *testing.T) {
	hcl := `
connector "api" {
  type     = "http"
  base_url = "https://api.example.com"

  retry {
    attempts = 5
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "connector.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	cfg, err := parser.ParseFile(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(cfg.Connectors) != 1 {
		t.Fatalf("expected 1 connector, got %d", len(cfg.Connectors))
	}
	retry, ok := cfg.Connectors[0].Properties["retry"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected retry properties to be a map, got %T", cfg.Connectors[0].Properties["retry"])
	}
	if retry["attempts"] != 5 {
		t.Errorf("expected retry.attempts = 5, got %v", retry["attempts"])
	}
}

// TestConnectorRetryBlockRejectsCount guards against the v1.19.0 documentation
// vocabulary (count) silently breaking — until the docs are aligned, the
// parser must reject "count" with a clear error rather than appearing to work.
func TestConnectorRetryBlockRejectsCount(t *testing.T) {
	hcl := `
connector "api" {
  type     = "http"
  base_url = "https://api.example.com"

  retry {
    count = 5
  }
}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "connector.mycel")
	if err := os.WriteFile(tmpFile, []byte(hcl), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	parser := NewHCLParser()
	_, err := parser.ParseFile(context.Background(), tmpFile)
	if err == nil {
		t.Fatal("expected error for unsupported retry attribute 'count'")
	}
	if !strings.Contains(err.Error(), "count") {
		t.Errorf("expected error to mention 'count', got: %v", err)
	}
}
