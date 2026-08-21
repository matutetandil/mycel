package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A service block split across files keeps all of it.
//
// The second block replaced the first entirely, so a project that put
// `service { admin_port = … }` in one file and the name, version and workflow
// configuration in another ended up with a service that had no name, no
// version and no workflow engine — and the workflow endpoints, which that
// block is what asks for, simply were not there. Nothing said so.
//
// Every .mycel file is merged and the file names are for the reader's benefit:
// that is what the documentation says, and splitting a service block is the
// obvious thing to do with it.
func TestAServiceBlockSplitAcrossFilesKeepsAllOfIt(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a-service.mycel", `
service {
  name    = "orders"
  version = "1.0.0"

  workflow {
    storage = "postgres"
  }
}
`)
	write("b-ports.mycel", `
service {
  admin_port = 9409
}
`)

	config, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if config.ServiceConfig == nil {
		t.Fatal("no service configuration at all")
	}

	if config.ServiceConfig.Name != "orders" {
		t.Errorf("name = %q, want the one the other file set", config.ServiceConfig.Name)
	}
	if config.ServiceConfig.Version != "1.0.0" {
		t.Errorf("version = %q", config.ServiceConfig.Version)
	}
	if config.ServiceConfig.AdminPort != 9409 {
		t.Errorf("admin_port = %d, want the one this file set", config.ServiceConfig.AdminPort)
	}
	if config.ServiceConfig.Workflow == nil {
		t.Fatal("the workflow block was dropped, so the workflow endpoints would not be served")
	}
	if config.ServiceConfig.Workflow.Storage != "postgres" {
		t.Errorf("workflow storage = %q", config.ServiceConfig.Workflow.Storage)
	}
}

// And a later block that names something wins, which is what an overlay is for.
func TestALaterServiceBlockOverridesWhatItNames(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"a.mycel": "service {\n  name       = \"orders\"\n  admin_port = 9090\n}\n",
		"b.mycel": "service {\n  admin_port = 9999\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	config, err := NewHCLParser().Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if config.ServiceConfig.AdminPort != 9999 {
		t.Errorf("admin_port = %d, want the later one", config.ServiceConfig.AdminPort)
	}
	if config.ServiceConfig.Name != "orders" {
		t.Errorf("name = %q, want the one nothing overrode", config.ServiceConfig.Name)
	}
}
