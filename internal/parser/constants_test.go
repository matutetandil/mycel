package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A constants block declares a value once and every part of the configuration
// reads it the same way.
//
// The case that asked for this was a list of SKUs to skip, written into four
// queries. Before it there was nowhere to put such a thing: env() holds a
// string somebody has to parse, and a step fetches it with a call per message.
func TestAConstantIsReadableFromHCLAndFromCEL(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "constants.mycel", `
constants {
  skus_to_skip = ["SKU-1", "SKU-2"]
  page_size    = 25
  region       = env("MYCEL_TEST_REGION", "us")
}
`)
	write(t, dir, "service.mycel", `
connector "api" {
  type = "rest"
  port = 8080
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data.db"
}

flow "list" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    query     = "SELECT * FROM items LIMIT ${constants.page_size}"
  }
}

flow "keep" {
  from {
    connector = "api"
    operation = "POST /items"
  }
  accept {
    when      = "!(input.sku in constants.skus_to_skip)"
    on_reject = "reject"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`)

	config, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Read by HCL, so the value is already in the query.
	if got := config.Flows[0].To.GetQuery(); !strings.Contains(got, "LIMIT 25") {
		t.Errorf("query = %q, want the constant folded in", got)
	}

	// Handed to CEL, so an expression naming it can be evaluated later.
	if config.Constants["page_size"] == nil {
		t.Fatalf("constants = %#v", config.Constants)
	}
	skus, ok := config.Constants["skus_to_skip"].([]interface{})
	if !ok || len(skus) != 2 {
		t.Errorf("skus_to_skip = %#v, want the list", config.Constants["skus_to_skip"])
	}
	if config.Constants["region"] != "us" {
		t.Errorf("region = %#v; env() is a literal by the time anything else runs", config.Constants["region"])
	}
	// The expression that reads it is left alone: CEL evaluates it per
	// message, against the same values.
	if got := config.Flows[1].Accept.When; !strings.Contains(got, "constants.skus_to_skip") {
		t.Errorf("accept.when = %q, want the reference untouched", got)
	}
}

// A constant declared in one file is readable from another, whatever order
// they are walked in.
func TestAConstantCrossesFiles(t *testing.T) {
	dir := t.TempDir()
	// Named so the walk reaches the user before the declaration.
	write(t, dir, "a-flows.mycel", `
connector "api" {
  type = "rest"
  port = 8080
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data.db"
}

flow "list" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    query     = "SELECT * FROM items LIMIT ${constants.page_size}"
  }
}
`)
	write(t, dir, "z-constants.mycel", "constants {\n  page_size = 7\n}\n")

	config, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := config.Flows[0].To.GetQuery(); !strings.Contains(got, "LIMIT 7") {
		t.Errorf("query = %q; a constant declared later in the walk was not there yet", got)
	}
}

// Declaring the same name twice is refused, naming both files.
//
// A constant holds one value. Letting the last file win makes a configuration
// depend on the order its files happen to be read in, which is the one thing
// nothing else in Mycel does.
func TestTheSameConstantTwiceIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.mycel", "constants {\n  page_size = 10\n}\n")
	write(t, dir, "two.mycel", "constants {\n  page_size = 20\n}\n")

	_, err := NewHCLParser().Parse(context.Background(), dir)
	if err == nil {
		t.Fatal("two declarations of the same constant were accepted")
	}
	for _, want := range []string{"page_size", "one.mycel", "two.mycel"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %s: %v", want, err)
		}
	}
}

// Naming a constant that was never declared fails where it is written, rather
// than becoming an empty string somewhere downstream.
func TestAnUndeclaredConstantIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "service.mycel", `
connector "api" {
  type = "rest"
  port = 8080
}

connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "./data.db"
}

flow "list" {
  from {
    connector = "api"
    operation = "GET /items"
  }
  to {
    connector = "db"
    query     = "SELECT * FROM items LIMIT ${constants.page_size}"
  }
}
`)

	_, err := NewHCLParser().Parse(context.Background(), dir)
	if err == nil {
		t.Fatal("a query naming a constant nothing declared was accepted")
	}
	if !strings.Contains(err.Error(), "page_size") && !strings.Contains(err.Error(), "constants") {
		t.Errorf("the refusal says nothing about the constant: %v", err)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
