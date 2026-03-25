package ide

import (
	"strings"
	"testing"
)

func TestExtractTransform(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/create_user.mycel", []byte(`
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
  to {
    connector = "db"
    target    = "users"
  }
}
`)))

	result := e.ExtractTransform("create_user", "")
	if result == nil {
		t.Fatal("expected ExtractTransformResult")
	}

	// Default name derived from flow name
	if result.Name != "create_user_transform" {
		t.Errorf("expected name 'create_user_transform', got %q", result.Name)
	}

	// New transform should have all mappings
	if !strings.Contains(result.NewTransform, "id") {
		t.Error("new transform should contain 'id' mapping")
	}
	if !strings.Contains(result.NewTransform, "email") {
		t.Error("new transform should contain 'email' mapping")
	}
	if !strings.Contains(result.NewTransform, "name") {
		t.Error("new transform should contain 'name' mapping")
	}

	// Flow edit should replace with use = "name"
	if !strings.Contains(result.FlowEdit.NewText, `use = "create_user_transform"`) {
		t.Errorf("flow edit should reference named transform, got: %s", result.FlowEdit.NewText)
	}

	// Suggested file
	if !strings.Contains(result.SuggestedFile, "transforms") {
		t.Errorf("suggested file should be in transforms dir, got: %s", result.SuggestedFile)
	}
}

func TestExtractTransformCustomName(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/orders.mycel", []byte(`
flow "process_order" {
  from { connector = "rabbit" }
  transform {
    total = "input.price * input.qty"
  }
  to { connector = "db" }
}
`)))

	result := e.ExtractTransform("process_order", "calculate_total")
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Name != "calculate_total" {
		t.Errorf("expected name 'calculate_total', got %q", result.Name)
	}
	if !strings.Contains(result.FlowEdit.NewText, `use = "calculate_total"`) {
		t.Error("flow edit should use custom name")
	}
}

func TestExtractTransformAlreadyNamed(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/test.mycel", []byte(`
flow "test" {
  from { connector = "api" }
  transform {
    use = "existing_transform"
  }
  to { connector = "db" }
}
`)))

	result := e.ExtractTransform("test", "")
	if result != nil {
		t.Error("should return nil when transform already uses a named reference")
	}
}

func TestExtractTransformNoTransform(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/test.mycel", []byte(`
flow "test" {
  from { connector = "api" }
  to { connector = "db" }
}
`)))

	result := e.ExtractTransform("test", "")
	if result != nil {
		t.Error("should return nil when flow has no transform")
	}
}

func TestRenameFile(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors/old.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
`)))

	// Verify it exists at old path
	if e.index.Connectors["api"] == nil {
		t.Fatal("expected connector 'api' in index")
	}
	if e.index.Connectors["api"].File != "connectors/old.mycel" {
		t.Fatalf("expected file 'connectors/old.mycel', got %s", e.index.Connectors["api"].File)
	}

	// Rename
	e.RenameFile("connectors/old.mycel", "connectors/api.mycel")

	// Verify old path is gone
	e.index.mu.RLock()
	_, oldExists := e.index.Files["connectors/old.mycel"]
	_, newExists := e.index.Files["connectors/api.mycel"]
	e.index.mu.RUnlock()

	if oldExists {
		t.Error("old path should be removed from index")
	}
	if !newExists {
		t.Error("new path should exist in index")
	}

	// Entity should reference new path
	if e.index.Connectors["api"].File != "connectors/api.mycel" {
		t.Errorf("expected file 'connectors/api.mycel', got %s", e.index.Connectors["api"].File)
	}
}
