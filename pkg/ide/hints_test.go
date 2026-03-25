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

func TestHintNoMismatchForGenericNames(t *testing.T) {
	// "flows.mycel" is a generic name for a flow — should NOT trigger hint
	fi := parseHCL("flows/flows.mycel", []byte(`
flow "save_customer" {
  from { connector = "api" }
  to { connector = "db" }
}
`))

	hints := hintsForFile(fi)

	for _, h := range hints {
		if h.Kind == HintFileNameMismatch {
			t.Errorf("should not hint for generic file name 'flows.mycel', got: %s", h.Message)
		}
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

func TestHintConfigFileSkipped(t *testing.T) {
	fi := parseHCL("config.mycel", []byte(`
service {
  name    = "my-api"
  version = "1.0.0"
}
`))

	hints := hintsForFile(fi)
	if len(hints) != 0 {
		t.Errorf("config.mycel should produce no hints, got %d", len(hints))
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
