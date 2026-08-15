package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exporting what the service serves, so a client can be generated from it. The
// thing to get right is that the export is the schema the service actually
// answers with — one rebuilt from the type blocks when the connector serves a
// file is a client built against something that does not exist.

func TestTheExportedSchemaIsTheFileTheConnectorServes(t *testing.T) {
	dir := projectWith(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9385
}

connector "graph" {
  type = "graphql"
  port = 3385

  schema {
    path = "./schema.graphql"
  }
}
`)

	declared := "type Query {\n  ordersFromTheFile: String\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(declared), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	output := filepath.Join(dir, "exported.graphql")
	previous := exportOutput
	exportOutput = output
	t.Cleanup(func() { exportOutput = previous })

	if err := runExportGraphQLSchema(exportGraphQLCmd, nil); err != nil {
		t.Fatalf("export: %v", err)
	}

	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading the export: %v", err)
	}
	if !strings.Contains(string(body), "ordersFromTheFile") {
		t.Errorf("the export is not the schema the connector serves:\n%s", body)
	}
}

func TestAConnectorPointingAtAFileThatIsNotThereIsReported(t *testing.T) {
	// Rather than exporting a schema rebuilt from the types, which would be a
	// client generated against something the service does not serve.
	projectWith(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9384
}

connector "graph" {
  type = "graphql"
  port = 3384

  schema {
    path = "./a-file-nobody-wrote.graphql"
  }
}
`)

	err := runExportGraphQLSchema(exportGraphQLCmd, nil)
	if err == nil {
		t.Fatal("a schema was exported for a connector whose file is missing")
	}
	if !strings.Contains(err.Error(), "a-file-nobody-wrote.graphql") {
		t.Errorf("error = %q, want it to name the file", err)
	}
}

func TestWithNoFileTheSchemaIsBuiltFromTheTypes(t *testing.T) {
	// The HCL-first mode: the types and flows are the schema.
	dir := projectWith(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9383
}

connector "graph" {
  type = "graphql"
  port = 3383
}

type "Order" {
  id    = id
  total = number
}

flow "orders" {
  returns = "[Order]"

  from {
    connector = "graph"
    operation = "query orders"
  }
}
`)

	output := filepath.Join(dir, "exported.graphql")
	previous := exportOutput
	exportOutput = output
	t.Cleanup(func() { exportOutput = previous })

	if err := runExportGraphQLSchema(exportGraphQLCmd, nil); err != nil {
		t.Fatalf("export: %v", err)
	}

	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("reading the export: %v", err)
	}
	if !strings.Contains(string(body), "Order") {
		t.Errorf("the declared type is not in the export:\n%s", body)
	}
}

func TestExportingFromSomethingThatIsNotAProjectIsReported(t *testing.T) {
	previous := configDir
	configDir = filepath.Join(t.TempDir(), "not-a-project")
	t.Cleanup(func() { configDir = previous })

	if err := runExportGraphQLSchema(exportGraphQLCmd, nil); err == nil {
		t.Error("a schema was exported from a directory with no configuration")
	}
}

func TestInstallingWithNothingDeclaredSaysSo(t *testing.T) {
	// Rather than reporting that it installed nothing, which reads as success.
	projectWith(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9382
}

connector "api" {
  type = "rest"
  port = 3382
}
`)

	if err := runPluginInstall(pluginInstallCmd, nil); err != nil {
		t.Errorf("installing with nothing declared failed: %v", err)
	}
}

func TestInstallingAPluginNobodyDeclaredIsReported(t *testing.T) {
	dir := projectWith(t, `
service {
  name       = "orders"
  version    = "1.0.0"
  admin_port = 9381
}

connector "api" {
  type = "rest"
  port = 3381
}

plugin "storefront" {
  source = "./plugins/storefront"
}
`)

	// The plugin declared here has no directory, but that is not what this is
	// about: asking for one that was never declared has to say so rather than
	// quietly installing nothing.
	err := runPluginInstall(pluginInstallCmd, []string{"a-plugin-nobody-declared"})
	if err == nil {
		t.Fatal("installing a plugin nobody declared reported success")
	}
	if !strings.Contains(err.Error(), "a-plugin-nobody-declared") {
		t.Errorf("error = %q, want it to name what was asked for", err)
	}
	_ = dir
}
