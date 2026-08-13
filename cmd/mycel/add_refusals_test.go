package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `mycel add` generates from the schema, so what it writes parses. What it
// cannot check from the schema is whether the thing being referred to exists —
// a flow naming a connector nobody declared writes a file that validates and
// then fails at startup. These are the refusals that stop that, and the point
// of testing them is the message: a refusal that does not say what does exist
// sends someone reading files.

func inProject(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	previous := configDir
	configDir = dir
	t.Cleanup(func() { configDir = previous })
}

func TestAddRefusesAConnectorNobodyDeclared(t *testing.T) {
	inProject(t, map[string]string{
		"service.mycel": `
service {
  name = "x"
}

connector "orders_api" {
  type = "rest"
  port = 8080
}

connector "warehouse" {
  type     = "database"
  driver   = "sqlite"
  database = "./data/app.db"
}
`,
	})

	err := ensureConnectorExists("shipping")
	if err == nil {
		t.Fatal("a connector that does not exist was accepted")
	}
	// Naming the ones that do turns a typo into an answer.
	for _, want := range []string{"shipping", "orders_api", "warehouse"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if err := ensureConnectorExists("warehouse"); err != nil {
		t.Errorf("a declared connector was refused: %v", err)
	}
}

func TestAddSaysSoWhenTheProjectHasNoConnectors(t *testing.T) {
	// The first thing someone does in an empty project, so the message points
	// at the command that fixes it rather than listing nothing.
	inProject(t, map[string]string{"service.mycel": "service {\n  name = \"x\"\n}\n"})

	err := ensureConnectorExists("anything")
	if err == nil {
		t.Fatal("a connector was accepted in a project that declares none")
	}
	if !strings.Contains(err.Error(), "mycel add connector") {
		t.Errorf("error = %q, want it to name the command that adds one", err)
	}
}

func TestAddRefusesAFlowNobodyDeclared(t *testing.T) {
	inProject(t, map[string]string{
		"service.mycel": `
service {
  name = "x"
}

connector "api" {
  type = "rest"
  port = 8080
}

flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
}
`,
	})

	if err := ensureFlowExists("create_order"); err != nil {
		t.Errorf("a declared flow was refused: %v", err)
	}
	err := ensureFlowExists("ship_order")
	if err == nil {
		t.Fatal("a flow that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "create_order") {
		t.Errorf("error %q does not say which flows exist", err)
	}
}

func TestAnAspectPatternHasToMatchSomething(t *testing.T) {
	// An aspect whose patterns match nothing is a file that does nothing, and
	// the reason is a typo often enough to be worth catching here.
	inProject(t, map[string]string{
		"service.mycel": `
service {
  name = "x"
}

connector "api" {
  type = "rest"
  port = 8080
}

flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
}

flow "cancel_order" {
  from {
    connector = "api"
    operation = "POST /orders/:id/cancel"
  }
}
`,
	})

	for _, spec := range []string{"create_*", "*_order", "create_order", "*"} {
		if err := ensurePatternsMatchAFlow(spec); err != nil {
			t.Errorf("%q matches a flow and was refused: %v", spec, err)
		}
	}

	err := ensurePatternsMatchAFlow("ship_*")
	if err == nil {
		t.Fatal("a pattern matching no flow was accepted")
	}
	if !strings.Contains(err.Error(), "ship_*") {
		t.Errorf("error %q does not name the pattern", err)
	}
}

func TestSplitPatterns(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want int
	}{
		{"create_*", 1},
		{"create_*,update_*", 2},
		{" create_* , update_* ", 2},
		{"create_*,,update_*", 2}, // an empty entry is a stray comma, not a pattern
		{"", 0},
		{"  ", 0},
	} {
		if got := splitPatterns(tc.spec); len(got) != tc.want {
			t.Errorf("splitPatterns(%q) = %v, want %d entries", tc.spec, got, tc.want)
		}
	}
}
