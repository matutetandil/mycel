package ide

import (
	"strings"
	"testing"
)

func TestHintMultipleBlocks(t *testing.T) {
	fi := parseHCL("connectors/connectors.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
connector "db" {
  type   = "database"
  driver = "postgres"
}
connector "rabbit" {
  type   = "mq"
  driver = "rabbitmq"
}
`))

	hints := hintsForFile(fi)

	multiCount := 0
	for _, h := range hints {
		if h.Kind == HintMultipleBlocksInFile {
			multiCount++
			if h.SuggestedFile == "" {
				t.Error("expected SuggestedFile for multi-block hint")
			}
		}
	}
	if multiCount != 3 {
		t.Errorf("expected 3 multi-block hints (one per connector), got %d", multiCount)
	}
}

func TestHintFileNameMismatch(t *testing.T) {
	// File named "orders.mycel" but contains flow "save_customer" → mismatch
	fi := parseHCL("flows/orders.mycel", []byte(`
flow "save_customer" {
  from { connector = "api" }
  to { connector = "db" }
}
`))

	hints := hintsForFile(fi)

	found := false
	for _, h := range hints {
		if h.Kind == HintFileNameMismatch {
			found = true
			if !strings.Contains(h.Message, "save_customer") {
				t.Errorf("expected message to mention block name, got: %s", h.Message)
			}
			if !strings.Contains(h.SuggestedFile, "save_customer.mycel") {
				t.Errorf("expected suggested file save_customer.mycel, got: %s", h.SuggestedFile)
			}
		}
	}
	if !found {
		t.Error("expected file name mismatch hint")
	}
}

func TestHintMismatchForGenericNames(t *testing.T) {
	// "flows.mycel" with a single flow "save_customer" → SHOULD suggest renaming
	fi := parseHCL("flows/flows.mycel", []byte(`
flow "save_customer" {
  from { connector = "api" }
  to { connector = "db" }
}
`))

	hints := hintsForFile(fi)

	found := false
	for _, h := range hints {
		if h.Kind == HintFileNameMismatch {
			found = true
		}
	}
	if !found {
		t.Error("expected file name mismatch hint for flows.mycel containing flow 'save_customer'")
	}
}

func TestHintNoMismatchWhenNameMatches(t *testing.T) {
	fi := parseHCL("flows/save_customer.mycel", []byte(`
flow "save_customer" {
  from { connector = "api" }
  to { connector = "db" }
}
`))

	hints := hintsForFile(fi)

	for _, h := range hints {
		if h.Kind == HintFileNameMismatch {
			t.Error("should not hint when file name matches block name")
		}
	}
}

func TestHintMixedTypes(t *testing.T) {
	fi := parseHCL("everything.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
flow "get_users" {
  from { connector = "api" }
  to { connector = "db" }
}
`))

	hints := hintsForFile(fi)

	found := false
	for _, h := range hints {
		if h.Kind == HintMixedTypesInFile {
			found = true
			if !strings.Contains(h.Message, "connector") || !strings.Contains(h.Message, "flow") {
				t.Errorf("expected message to list both types, got: %s", h.Message)
			}
		}
	}
	if !found {
		t.Error("expected mixed types hint")
	}
}

func TestHintWrongDirectory(t *testing.T) {
	fi := parseHCL("flows/database.mycel", []byte(`
connector "db" {
  type   = "database"
  driver = "postgres"
}
`))

	hints := hintsForFile(fi)

	found := false
	for _, h := range hints {
		if h.Kind == HintWrongDirectory {
			found = true
			if !strings.Contains(h.Message, "connectors/") {
				t.Errorf("expected suggestion to move to connectors/, got: %s", h.Message)
			}
		}
	}
	if !found {
		t.Error("expected wrong directory hint")
	}
}

func TestHintConfigFileNoMismatch(t *testing.T) {
	// service block has no label (Labels: 0), so no name mismatch hint
	fi := parseHCL("config.mycel", []byte(`
service {
  name    = "my-api"
  version = "1.0.0"
}
`))

	hints := hintsForFile(fi)
	for _, h := range hints {
		if h.Kind == HintFileNameMismatch {
			t.Error("service block has no label — should not trigger name mismatch")
		}
	}
}

func TestHintConfigWithEverything(t *testing.T) {
	// Everything in one file → should get mixed types hint
	fi := parseHCL("config.mycel", []byte(`
service {
  name = "my-api"
}
connector "api" {
  type = "rest"
  port = 3000
}
flow "get_users" {
  from { connector = "api" }
  to { connector = "db" }
}
`))

	hints := hintsForFile(fi)
	found := false
	for _, h := range hints {
		if h.Kind == HintMixedTypesInFile {
			found = true
		}
	}
	if !found {
		t.Error("expected mixed types hint for config.mycel with service + connector + flow")
	}
}

func TestHintServiceNotInConfig(t *testing.T) {
	fi := parseHCL("my-api.mycel", []byte(`
service {
  name    = "my-api"
  version = "1.0.0"
}
`))

	hints := hintsForFile(fi)
	found := false
	for _, h := range hints {
		if h.Kind == HintServiceNotInConfig {
			found = true
			if !strings.Contains(h.Message, "config.mycel") {
				t.Errorf("expected message to mention config.mycel, got: %s", h.Message)
			}
			if !strings.Contains(h.SuggestedFile, "config.mycel") {
				t.Errorf("expected suggested file config.mycel, got: %s", h.SuggestedFile)
			}
		}
	}
	if !found {
		t.Error("expected hint for service block not in config.mycel")
	}
}

func TestHintServiceInConfigIsOk(t *testing.T) {
	fi := parseHCL("config.mycel", []byte(`
service {
  name = "my-api"
}
`))

	hints := hintsForFile(fi)
	for _, h := range hints {
		if h.Kind == HintServiceNotInConfig {
			t.Error("service in config.mycel should not trigger hint")
		}
	}
}

func TestHintNoDirectoryStructure(t *testing.T) {
	e := NewEngine("")
	// All blocks in root, no subdirectories
	e.index.updateFile(parseHCL("config.mycel", []byte(`
service { name = "api" }
`)))
	e.index.updateFile(parseHCL("connectors.mycel", []byte(`
connector "api" { type = "rest" }
`)))
	e.index.updateFile(parseHCL("flows.mycel", []byte(`
flow "get_users" {
  from { connector = "api" }
  to { connector = "db" }
}
`)))

	hints := e.Hints()
	found := false
	for _, h := range hints {
		if h.Kind == HintNoDirectoryStructure {
			found = true
			if !strings.Contains(h.Message, "connectors/") || !strings.Contains(h.Message, "flows/") {
				t.Errorf("expected suggestion for connectors/ and flows/, got: %s", h.Message)
			}
		}
	}
	if !found {
		t.Error("expected no-directory-structure hint")
	}
}

func TestHintNoDirectoryStructureNotTriggeredWhenOrganized(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("config.mycel", []byte(`service { name = "api" }`)))
	e.index.updateFile(parseHCL("connectors/api.mycel", []byte(`connector "api" { type = "rest" }`)))
	e.index.updateFile(parseHCL("flows/users.mycel", []byte(`
flow "get_users" {
  from { connector = "api" }
  to { connector = "db" }
}
`)))

	hints := e.Hints()
	for _, h := range hints {
		if h.Kind == HintNoDirectoryStructure {
			t.Error("should not trigger when project has subdirectory structure")
		}
	}
}

func TestEngineHints(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("connectors.mycel", []byte(`
connector "api" {
  type = "rest"
  port = 3000
}
connector "db" {
  type   = "database"
  driver = "postgres"
}
`)))
	e.index.updateFile(parseHCL("flows/save_customer.mycel", []byte(`
flow "save_customer" {
  from { connector = "api" }
  to { connector = "db" }
}
`)))

	hints := e.Hints()

	// connectors.mycel should have multi-block hints, save_customer should have none
	hasMulti := false
	for _, h := range hints {
		if h.Kind == HintMultipleBlocksInFile {
			hasMulti = true
		}
	}
	if !hasMulti {
		t.Error("expected multi-block hints for connectors.mycel")
	}
}
