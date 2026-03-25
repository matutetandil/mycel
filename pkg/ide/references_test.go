package ide

import (
	"testing"
)

func TestFindReferences(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors/api.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
`)))
	e.index.updateFile(parseHCL("flows/users.mycel", []byte(`
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
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  to {
    connector = "db"
    target    = "users"
  }
}
`)))
	e.index.updateFile(parseHCL("aspects/log.mycel", []byte(`
aspect "logger" {
  on   = ["*"]
  when = "after"
  action {
    connector = "api"
  }
}
`)))

	refs := e.FindReferences("connector", "api")

	// Definition (1) + from blocks (2) + aspect action (1) = 4
	if len(refs) < 4 {
		t.Errorf("expected at least 4 references to connector 'api', got %d", len(refs))
		for _, r := range refs {
			t.Logf("  %s:%d %s.%s attr=%s", r.File, r.Line, r.BlockType, r.BlockName, r.AttrName)
		}
	}

	// Verify definition is included
	hasDef := false
	for _, r := range refs {
		if r.BlockType == "connector" && r.BlockName == "api" && r.AttrName == "" {
			hasDef = true
		}
	}
	if !hasDef {
		t.Error("expected definition of connector 'api' in references")
	}

	// Verify flow references
	flowRefs := 0
	for _, r := range refs {
		if r.AttrName == "connector" && r.BlockType == "from" {
			flowRefs++
		}
	}
	if flowRefs != 2 {
		t.Errorf("expected 2 flow 'from' references, got %d", flowRefs)
	}
}

func TestFindReferencesNoRefs(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors/db.mycel", []byte(`
connector "db" {
  type   = "database"
  driver = "postgres"
}
`)))

	refs := e.FindReferences("connector", "db")
	// Only the definition, no references
	if len(refs) != 1 {
		t.Errorf("expected 1 reference (definition only), got %d", len(refs))
	}
}

func TestRenameEntity(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors/api.mycel", []byte(`
connector "old_api" {
  type = "rest"
  port = 3000
}
`)))
	e.index.updateFile(parseHCL("flows/users.mycel", []byte(`
flow "get_users" {
  from {
    connector = "old_api"
  }
  to {
    connector = "old_api"
  }
}
`)))

	edits := e.RenameEntity("connector", "old_api", "new_api")

	// Definition (1) + 2 references = 3
	if len(edits) < 3 {
		t.Errorf("expected at least 3 rename edits, got %d", len(edits))
		for _, ed := range edits {
			t.Logf("  %s:%d → %q", ed.File, ed.Range.Start.Line, ed.NewText)
		}
	}

	for _, ed := range edits {
		if ed.NewText != "new_api" {
			t.Errorf("expected NewText='new_api', got %q", ed.NewText)
		}
	}
}

func TestRenameEntitySameName(t *testing.T) {
	e := NewEngine("")
	edits := e.RenameEntity("connector", "api", "api")
	if edits != nil {
		t.Error("expected nil when old and new name are the same")
	}
}
