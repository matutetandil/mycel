package ide

import (
	"os"
	"strings"
	"testing"
)

func TestParseHCL(t *testing.T) {
	src := []byte(`
connector "api" {
  type = "rest"
  port = 3000
}

connector "db" {
  type   = "database"
  driver = "postgres"
  host   = "localhost"
}

flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`)
	fi := parseHCL("test.mycel", src)
	if len(fi.ParseDiags) > 0 {
		t.Fatalf("unexpected parse errors: %v", fi.ParseDiags)
	}
	if len(fi.Blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(fi.Blocks))
	}

	// Check connector
	if fi.Blocks[0].Type != "connector" || fi.Blocks[0].Name != "api" {
		t.Errorf("expected connector 'api', got %s %q", fi.Blocks[0].Type, fi.Blocks[0].Name)
	}
	if fi.Blocks[0].GetAttr("type") != "rest" {
		t.Errorf("expected type=rest, got %q", fi.Blocks[0].GetAttr("type"))
	}

	// Check flow
	flow := fi.Blocks[2]
	if flow.Type != "flow" || flow.Name != "get_users" {
		t.Errorf("expected flow 'get_users', got %s %q", flow.Type, flow.Name)
	}
	if len(flow.Children) != 2 {
		t.Fatalf("expected 2 children (from, to), got %d", len(flow.Children))
	}
	from := flow.Children[0]
	if from.GetAttr("connector") != "api" {
		t.Errorf("expected from.connector=api, got %q", from.GetAttr("connector"))
	}
}

func TestParseHCLWithErrors(t *testing.T) {
	src := []byte(`
connector "broken" {
  type = "rest"
  port =
}
`)
	fi := parseHCL("broken.mycel", src)
	if len(fi.ParseDiags) == 0 {
		t.Fatal("expected parse errors for invalid HCL")
	}
}

func TestProjectIndex(t *testing.T) {
	idx := newProjectIndex()

	fi1 := parseHCL("connectors.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
connector "db" {
  type   = "database"
  driver = "postgres"
}
`))
	fi2 := parseHCL("flows.mycel", []byte(`
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`))

	idx.updateFile(fi1)
	idx.updateFile(fi2)

	if len(idx.Connectors) != 2 {
		t.Errorf("expected 2 connectors, got %d", len(idx.Connectors))
	}
	if len(idx.Flows) != 1 {
		t.Errorf("expected 1 flow, got %d", len(idx.Flows))
	}
	if idx.Connectors["api"].ConnType != "rest" {
		t.Errorf("expected api type=rest, got %q", idx.Connectors["api"].ConnType)
	}
	if idx.Connectors["db"].Driver != "postgres" {
		t.Errorf("expected db driver=postgres, got %q", idx.Connectors["db"].Driver)
	}

	// Remove file
	idx.removeFile("connectors.mycel")
	if len(idx.Connectors) != 0 {
		t.Errorf("expected 0 connectors after removal, got %d", len(idx.Connectors))
	}
}

func TestDiagnoseUnknownBlock(t *testing.T) {
	fi := parseHCL("test.mycel", []byte(`
flw "bad" {
  from { connector = "x" }
}
`))
	diags := diagnoseFile(fi, nil)
	found := false
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Message, "unknown block type") {
			found = true
		}
	}
	if !found {
		t.Error("expected diagnostic for unknown block type 'flw'")
	}
}

func TestDiagnoseInvalidValue(t *testing.T) {
	fi := parseHCL("test.mycel", []byte(`
connector "api" {
  type = "invalid_type"
}
`))
	diags := diagnoseFile(fi, nil)
	found := false
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Message, "invalid value") {
			found = true
		}
	}
	if !found {
		t.Error("expected diagnostic for invalid connector type value")
	}
}

func TestDiagnoseCrossRefUndefinedConnector(t *testing.T) {
	idx := newProjectIndex()
	fi := parseHCL("flows.mycel", []byte(`
flow "get_users" {
  from {
    connector = "nonexistent"
    operation = "GET /users"
  }
  to {
    connector = "also_nonexistent"
    target    = "users"
  }
}
`))
	idx.updateFile(fi)

	diags := diagnoseCrossRefs(idx)
	count := 0
	for _, d := range diags {
		if strings.Contains(d.Message, "undefined connector") {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 undefined connector diagnostics, got %d", count)
	}
}

func TestCompleteRootLevel(t *testing.T) {
	fi := parseHCL("test.mycel", []byte(``))
	idx := newProjectIndex()
	idx.updateFile(fi)

	items := complete(fi, idx, nil, 1, 1)
	found := false
	for _, item := range items {
		if item.Label == "connector" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'connector' in root-level completions")
	}
}

func TestCompleteInsideFlow(t *testing.T) {
	src := []byte(`
flow "test" {

}
`)
	fi := parseHCL("test.mycel", src)
	idx := newProjectIndex()
	idx.updateFile(fi)

	items := complete(fi, idx, nil, 3, 3)

	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	expected := []string{"from", "to", "accept", "transform", "step"}
	for _, e := range expected {
		if !labels[e] {
			t.Errorf("expected %q in flow completions", e)
		}
	}
}

func TestCompleteConnectorRef(t *testing.T) {
	idx := newProjectIndex()
	idx.updateFile(parseHCL("connectors.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
connector "db" {
  type   = "database"
  driver = "postgres"
}
`)))

	src := []byte(`
flow "test" {
  from {
    connector = ""
  }
}
`)
	fi := parseHCL("flows.mycel", src)
	idx.updateFile(fi)

	// Cursor inside connector = "" — line 4, after the =
	items := complete(fi, idx, nil, 4, 18)

	names := make(map[string]bool)
	for _, item := range items {
		names[item.Label] = true
	}
	if !names["api"] || !names["db"] {
		t.Errorf("expected 'api' and 'db' in connector ref completions, got %v", names)
	}
}

func TestCompleteAcceptBlock(t *testing.T) {
	src := []byte(`
flow "test" {
  from {
    connector = "rabbit"
    operation = "events"
  }
  accept {

  }
}
`)
	fi := parseHCL("test.mycel", src)
	idx := newProjectIndex()
	idx.updateFile(fi)

	items := complete(fi, idx, nil, 8, 5)

	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	if !labels["when"] {
		t.Error("expected 'when' in accept block completions")
	}
	if !labels["on_reject"] {
		t.Error("expected 'on_reject' in accept block completions")
	}
}

func TestCompleteOnRejectValues(t *testing.T) {
	src := []byte(`
flow "test" {
  accept {
    when      = "input.type == 'A1'"
    on_reject = ""
  }
}
`)
	fi := parseHCL("test.mycel", src)
	idx := newProjectIndex()
	idx.updateFile(fi)

	// Cursor on on_reject value
	items := complete(fi, idx, nil, 5, 18)

	values := make(map[string]bool)
	for _, item := range items {
		values[item.Label] = true
	}

	expected := []string{"ack", "reject", "requeue"}
	for _, e := range expected {
		if !values[e] {
			t.Errorf("expected %q in on_reject value completions", e)
		}
	}
}

func TestDefinitionConnectorRef(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
`)))
	e.index.updateFile(parseHCL("flows.mycel", []byte(`
flow "test" {
  from {
    connector = "api"
    operation = "GET /test"
  }
  to {
    connector = "api"
    target    = "test"
  }
}
`)))

	loc := e.Definition("flows.mycel", 4, 18)
	if loc == nil {
		t.Fatal("expected definition location for connector ref")
	}
	if loc.File != "connectors.mycel" {
		t.Errorf("expected file=connectors.hcl, got %s", loc.File)
	}
}

func TestHoverBlockType(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("test.mycel", []byte(`
flow "test" {
  accept {
    when = "input.x == true"
  }
}
`)))

	result := e.Hover("test.mycel", 3, 5)
	if result == nil {
		t.Fatal("expected hover result for accept block")
	}
	if !strings.Contains(result.Content, "Business-level gate") {
		t.Errorf("expected accept doc, got %q", result.Content)
	}
}

func TestEngineFullReindex(t *testing.T) {
	// Create a temp directory with HCL files
	dir := t.TempDir()

	writeFile(t, dir, "connectors.mycel", `
connector "api" {
  type = "rest"
  port = 3000
}
`)
	writeFile(t, dir, "flows.mycel", `
flow "get_users" {
  from {
    connector = "api"
    operation = "GET /users"
  }
  to {
    connector = "api"
    target    = "users"
  }
}
`)

	e := NewEngine(dir)
	diags := e.FullReindex()

	// Should have no errors (connectors exist)
	errors := 0
	for _, d := range diags {
		if d.Severity == SeverityError {
			errors++
		}
	}
	if errors > 0 {
		for _, d := range diags {
			t.Logf("diag: %s", d.Message)
		}
		t.Errorf("expected 0 errors, got %d", errors)
	}

	idx := e.GetIndex()
	if len(idx.Connectors) != 1 {
		t.Errorf("expected 1 connector, got %d", len(idx.Connectors))
	}
	if len(idx.Flows) != 1 {
		t.Errorf("expected 1 flow, got %d", len(idx.Flows))
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	err := os.WriteFile(dir+"/"+name, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
}
