package ide

import (
	"strings"
	"testing"
)

// --- CEL Completions ---

func TestCELCompletionsInTransform(t *testing.T) {
	src := []byte(`
flow "test" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  step "prices" {
    connector = "db"
    query     = "SELECT * FROM prices"
  }
  transform {
    name = ""
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`)
	fi := parseHCL("test.hcl", src)
	idx := newProjectIndex()
	idx.updateFile(fi)

	// Cursor in transform value (line 12: `    name = ""`, col 12 is on the value)
	items := complete(fi, idx, 12, 12)

	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	if !labels["input"] {
		t.Error("expected 'input' variable in CEL completions")
	}
	if !labels["step.prices"] {
		t.Error("expected 'step.prices' variable in CEL completions")
	}
	if !labels["uuid"] {
		t.Error("expected 'uuid' function in CEL completions")
	}
	if !labels["lower"] {
		t.Error("expected 'lower' function in CEL completions")
	}
}

func TestCELCompletionsInResponse(t *testing.T) {
	src := []byte(`
flow "test" {
  from {
    connector = "api"
    operation = "GET /test"
  }
  response {
    result = ""
  }
}
`)
	fi := parseHCL("test.hcl", src)
	idx := newProjectIndex()
	idx.updateFile(fi)

	items := complete(fi, idx, 8, 14)
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	if !labels["input"] {
		t.Error("expected 'input' in response CEL completions")
	}
	if !labels["output"] {
		t.Error("expected 'output' in response CEL completions")
	}
}

func TestIsCELContext(t *testing.T) {
	tests := []struct {
		path     []string
		attr     string
		expected bool
	}{
		{[]string{"flow", "transform"}, "name", true},
		{[]string{"flow", "transform"}, "use", false},
		{[]string{"flow", "response"}, "result", true},
		{[]string{"flow", "from"}, "filter", true},
		{[]string{"flow", "accept"}, "when", true},
		{[]string{"flow", "from"}, "connector", false},
		{[]string{"flow", "to"}, "target", false},
	}

	for _, tt := range tests {
		result := isCELContext(tt.path, tt.attr)
		if result != tt.expected {
			t.Errorf("isCELContext(%v, %q) = %v, want %v", tt.path, tt.attr, result, tt.expected)
		}
	}
}

// --- Connector Type Validation ---

func TestConnectorTypeValidation(t *testing.T) {
	fi := parseHCL("test.hcl", []byte(`
connector "db" {
  type = "database"
}
`))

	diags := diagnoseFile(fi)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "database connector requires attribute driver") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing driver for database connector")
	}
}

func TestConnectorTypeCompletions(t *testing.T) {
	src := []byte(`
connector "db" {
  type = "database"

}
`)
	fi := parseHCL("test.hcl", src)
	idx := newProjectIndex()
	idx.updateFile(fi)

	items := complete(fi, idx, 4, 3)

	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	if !labels["driver"] {
		t.Error("expected 'driver' in database connector completions")
	}
	if !labels["database"] {
		t.Error("expected 'database' in database connector completions")
	}
}

// --- Operation Validation ---

func TestOperationValidation(t *testing.T) {
	fi := parseHCL("test.hcl", []byte(`
flow "test" {
  from {
    connector = "api"
    operation = "GETX /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`))

	diags := diagnoseFile(fi)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown HTTP method") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about unknown HTTP method GETX")
	}
}

func TestOperationCompletionsREST(t *testing.T) {
	idx := newProjectIndex()
	idx.updateFile(parseHCL("conn.hcl", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
`)))

	src := []byte(`
flow "test" {
  from {
    connector = "api"
    operation = ""
  }
}
`)
	fi := parseHCL("test.hcl", src)
	idx.updateFile(fi)

	items := complete(fi, idx, 5, 18)

	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	if !labels["GET /"] {
		t.Error("expected 'GET /' in REST operation completions")
	}
	if !labels["POST /"] {
		t.Error("expected 'POST /' in REST operation completions")
	}
}

// --- Rename ---

func TestRenameConnector(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors.hcl", []byte(`
connector "old_api" {
  type = "rest"
  port = 3000
}
`)))
	e.index.updateFile(parseHCL("flows.hcl", []byte(`
flow "test" {
  from {
    connector = "old_api"
    operation = "GET /test"
  }
  to {
    connector = "old_api"
    target    = "test"
  }
}
`)))

	// Rename from the reference in flows.hcl
	edits := e.Rename("flows.hcl", 4, 18, "new_api")

	if len(edits) < 2 {
		t.Fatalf("expected at least 2 rename edits (definition + references), got %d", len(edits))
	}

	// Verify edits cover both the definition and references
	files := make(map[string]int)
	for _, edit := range edits {
		files[edit.File]++
		if edit.NewText != "new_api" {
			t.Errorf("expected NewText='new_api', got %q", edit.NewText)
		}
	}

	if files["connectors.hcl"] < 1 {
		t.Error("expected at least 1 edit in connectors.hcl (definition)")
	}
	if files["flows.hcl"] < 2 {
		t.Error("expected at least 2 edits in flows.hcl (from + to references)")
	}
}

// --- Code Actions ---

func TestCodeActionCreateConnector(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows.hcl", []byte(`
flow "test" {
  from {
    connector = "missing_api"
    operation = "GET /test"
  }
  to {
    connector = "missing_api"
    target    = "test"
  }
}
`)))

	actions := e.CodeActions("flows.hcl", 4, 18)

	found := false
	for _, a := range actions {
		if strings.Contains(a.Title, "Create connector") && strings.Contains(a.Title, "missing_api") {
			found = true
			if len(a.Edits) == 0 {
				t.Error("expected edits in code action")
			}
		}
	}
	if !found {
		t.Error("expected 'Create connector' code action for undefined connector")
	}
}

// --- Symbols ---

func TestWorkspaceSymbols(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors.hcl", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
connector "db" {
  type   = "database"
  driver = "postgres"
}
`)))
	e.index.updateFile(parseHCL("flows.hcl", []byte(`
flow "get_users" {
  from { connector = "api" }
  to { connector = "db" }
}
flow "create_user" {
  from { connector = "api" }
  to { connector = "db" }
}
`)))

	symbols := e.Symbols()
	if len(symbols) != 4 {
		t.Errorf("expected 4 symbols (2 connectors + 2 flows), got %d", len(symbols))
	}

	names := make(map[string]bool)
	for _, s := range symbols {
		names[s.Name] = true
	}
	for _, expected := range []string{"api", "db", "get_users", "create_user"} {
		if !names[expected] {
			t.Errorf("expected symbol %q", expected)
		}
	}
}

func TestFileSymbols(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows.hcl", []byte(`
flow "get_users" {
  from { connector = "api" }
  to { connector = "db" }
}
flow "create_user" {
  from { connector = "api" }
  to { connector = "db" }
}
`)))

	symbols := e.SymbolsForFile("flows.hcl")
	if len(symbols) != 2 {
		t.Errorf("expected 2 file symbols, got %d", len(symbols))
	}
}

// --- Transform Rules ---

func TestTransformRules(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows.hcl", []byte(`
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  transform {
    id    = "uuid()"
    email = "lower(input.email)"
    name  = "input.name"
  }
  response {
    result = "'created'"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`)))

	rules := e.TransformRules("create_user")
	if len(rules) != 4 {
		t.Fatalf("expected 4 rules (3 transform + 1 response), got %d", len(rules))
	}

	if rules[0].Target != "id" || rules[0].Stage != "transform" {
		t.Errorf("rule 0: expected target=id stage=transform, got target=%s stage=%s", rules[0].Target, rules[0].Stage)
	}
	if rules[3].Target != "result" || rules[3].Stage != "response" {
		t.Errorf("rule 3: expected target=result stage=response, got target=%s stage=%s", rules[3].Target, rules[3].Stage)
	}
}

func TestFlowStages(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows.hcl", []byte(`
flow "complex" {
  from {
    connector = "rabbit"
    operation = "events"
    filter    = "input.type == 'order'"
  }
  accept {
    when = "input.region == 'us'"
  }
  validate {
    input = "order"
  }
  step "prices" {
    connector = "db"
    query     = "SELECT * FROM prices"
  }
  transform {
    id = "input.id"
  }
  to {
    connector = "db"
    target    = "orders"
  }
  response {
    status = "'created'"
  }
}
`)))

	stages := e.FlowStages("complex")

	expected := []string{"input", "sanitize", "filter", "accept", "validate_input", "step", "transform", "write", "response"}
	if len(stages) != len(expected) {
		t.Fatalf("expected %d stages, got %d: %v", len(expected), len(stages), stages)
	}
	for i, s := range expected {
		if stages[i] != s {
			t.Errorf("stage[%d]: expected %q, got %q", i, s, stages[i])
		}
	}
}
