package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Loading a plugin is the step between a declaration in a configuration file
// and something the runtime can call. Nothing exercised it end to end: the
// manifest was parsed in isolation, and whether a declared plugin turned into a
// connector a flow could name went unchecked.

// pluginDir writes a plugin directory the way one arrives on disk: a manifest
// and the modules it names.
func pluginDir(t *testing.T, base, name, manifest string, modules ...string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the plugin directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.mycel"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	for _, module := range modules {
		if err := os.WriteFile(filepath.Join(dir, module), MinimalValidatorWASM(), 0o600); err != nil {
			t.Fatalf("writing %s: %v", module, err)
		}
	}
	return dir
}

const connectorManifest = `
plugin {
  name        = "storefront"
  version     = "1.2.0"
  description = "A store nobody else speaks to"
  author      = "Waterworks"
  license     = "MIT"
}

provides {
  connector "storefront" {
    wasm = "connector.wasm"

    config {
      base_url = string({ required = true, description = "Where the store lives" })
      api_key  = string({ required = true, sensitive = true })
      timeout  = string({ default = "30s" })
      retries  = "number"
    }
  }

  functions {
    wasm    = "functions.wasm"
    exports = ["normalise_sku", "price_with_tax"]
  }
}
`

func TestADeclaredPluginBecomesSomethingAFlowCanName(t *testing.T) {
	base := t.TempDir()
	pluginDir(t, base, "storefront", connectorManifest, "connector.wasm", "functions.wasm")

	registry := NewRegistry(base)
	decl := &PluginDeclaration{Name: "storefront", Source: "./storefront"}

	if err := registry.LoadAll(context.Background(), []*PluginDeclaration{decl}); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	loaded, ok := registry.GetPlugin("storefront")
	if !ok {
		t.Fatal("the plugin was loaded and cannot be found by name")
	}
	if loaded.Manifest.Version != "1.2.0" {
		t.Errorf("version = %q", loaded.Manifest.Version)
	}

	// The connector it provides, which is the point: a flow writes
	// connector = "storefront" and the runtime has to find this.
	if _, err := registry.GetConnector("storefront", "storefront"); err != nil {
		t.Errorf("the connector the plugin provides is not there: %v", err)
	}
	if types := registry.GetConnectorTypes(); types["storefront"] != "storefront" {
		t.Errorf("connector types = %v", types)
	}

	// And the functions, which the runtime adds to the CEL environment.
	configs := registry.GetFunctionsConfigs()
	if len(configs["storefront"].FunctionsExports) != 2 {
		t.Errorf("exports = %v, want both", configs["storefront"].FunctionsExports)
	}
}

func TestEachInstanceOfAPluginConnectorGetsItsOwnConfiguration(t *testing.T) {
	// Two stores, one plugin: the second instance must not be handed the first
	// one's credentials, which is what sharing the template would do.
	base := t.TempDir()
	pluginDir(t, base, "storefront", connectorManifest, "connector.wasm", "functions.wasm")

	registry := NewRegistry(base)
	if err := registry.Load(context.Background(), &PluginDeclaration{
		Name: "storefront", Source: "./storefront",
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	first, err := registry.CreateConnectorInstance("storefront", "storefront", "au_store",
		map[string]interface{}{"base_url": "https://au.example.com"})
	if err != nil {
		t.Fatalf("CreateConnectorInstance: %v", err)
	}
	second, err := registry.CreateConnectorInstance("storefront", "storefront", "nz_store",
		map[string]interface{}{"base_url": "https://nz.example.com"})
	if err != nil {
		t.Fatalf("CreateConnectorInstance: %v", err)
	}

	if first.Name() == second.Name() {
		t.Error("both instances answer to the same name")
	}
	if first.(*WASMConnector).config["base_url"] == second.(*WASMConnector).config["base_url"] {
		t.Error("the two instances share a configuration")
	}
}

func TestAskingForSomethingAPluginDoesNotProvideIsReported(t *testing.T) {
	base := t.TempDir()
	pluginDir(t, base, "storefront", connectorManifest, "connector.wasm", "functions.wasm")

	registry := NewRegistry(base)
	if err := registry.Load(context.Background(), &PluginDeclaration{
		Name: "storefront", Source: "./storefront",
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := registry.GetConnector("nobody", "storefront"); err == nil {
		t.Error("a plugin nobody declared was found")
	}
	if _, err := registry.GetConnector("storefront", "warehouse"); err == nil {
		t.Error("a connector the plugin does not provide was found")
	}
	if _, err := registry.CreateConnectorInstance("nobody", "x", "i", nil); err == nil {
		t.Error("an instance was made of a plugin nobody declared")
	}
	if _, err := registry.CreateConnectorInstance("storefront", "warehouse", "i", nil); err == nil {
		t.Error("an instance was made of a connector nobody provides")
	}
}

func TestLoadingTheSamePluginTwiceIsHarmless(t *testing.T) {
	// Two flows naming the same plugin is the ordinary case, and reloading the
	// module for each would be both slower and a different instance.
	base := t.TempDir()
	pluginDir(t, base, "storefront", connectorManifest, "connector.wasm", "functions.wasm")

	registry := NewRegistry(base)
	decl := &PluginDeclaration{Name: "storefront", Source: "./storefront"}

	for i := 0; i < 2; i++ {
		if err := registry.Load(context.Background(), decl); err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
	}
	if len(registry.GetConnectorTypes()) != 1 {
		t.Errorf("connector types = %v, want one", registry.GetConnectorTypes())
	}
}

func TestAPluginThatIsNotThereIsReportedByName(t *testing.T) {
	// The three ways a declaration fails to resolve, each of which is somebody
	// mistyping a path — and each of which must say so rather than starting a
	// service whose flows quietly refer to nothing.
	base := t.TempDir()

	notADirectory := filepath.Join(base, "plugin.wasm")
	if err := os.WriteFile(notADirectory, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	// A directory with no manifest in it.
	if err := os.MkdirAll(filepath.Join(base, "empty"), 0o755); err != nil {
		t.Fatalf("creating: %v", err)
	}

	for name, decl := range map[string]*PluginDeclaration{
		"a path that is not there":     {Name: "absent", Source: "./nowhere"},
		"a path that is a file":        {Name: "file", Source: "./plugin.wasm"},
		"a directory with no manifest": {Name: "empty", Source: "./empty"},
		"a source in no format at all": {Name: "odd", Source: "who-knows"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := NewRegistry(base).Load(context.Background(), decl); err == nil {
				t.Error("it was accepted")
			}
		})
	}
}

func TestAManifestNamingAModuleThatIsNotThereIsReported(t *testing.T) {
	// Otherwise the plugin loads, the flow binds, and the first message
	// discovers there is no module — in production rather than at startup.
	base := t.TempDir()
	pluginDir(t, base, "storefront", connectorManifest) // no .wasm files written

	err := NewRegistry(base).Load(context.Background(), &PluginDeclaration{
		Name: "storefront", Source: "./storefront",
	})
	if err == nil {
		t.Fatal("a plugin whose module is missing was loaded")
	}
	if !strings.Contains(err.Error(), "connector.wasm") {
		t.Errorf("error = %q, want it to name the module that is missing", err)
	}
}

func TestAConnectorsConfigurationSchemaIsRead(t *testing.T) {
	// This is what tells somebody writing the configuration which settings the
	// plugin takes, which are required, and which must not be printed.
	base := t.TempDir()
	pluginDir(t, base, "storefront", connectorManifest, "connector.wasm", "functions.wasm")

	loaded, err := NewLoader(base).Load(context.Background(), &PluginDeclaration{
		Name: "storefront", Source: "./storefront",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	provided := loaded.Manifest.Provides.Connectors
	if len(provided) != 1 {
		t.Fatalf("%d connectors provided, want one", len(provided))
	}
	schema := provided[0].ConfigSchema

	if schema["base_url"] == nil || !schema["base_url"].Required {
		t.Errorf("base_url = %+v, want a required setting", schema["base_url"])
	}
	if schema["api_key"] == nil || !schema["api_key"].Sensitive {
		t.Errorf("api_key = %+v, want it marked as not to be printed", schema["api_key"])
	}
	if schema["timeout"] == nil || schema["timeout"].Default != "30s" {
		t.Errorf("timeout = %+v, want its default carried through", schema["timeout"])
	}
	// The short form, where the type is the whole declaration.
	if schema["retries"] == nil || schema["retries"].Type != "number" {
		t.Errorf("retries = %+v", schema["retries"])
	}
}

func TestALockFileIsOnlyWrittenWhenThereIsSomethingToLock(t *testing.T) {
	// A local plugin has no version to pin, so a service using only local
	// plugins should not find a lock file appearing in its configuration
	// directory.
	base := t.TempDir()
	pluginDir(t, base, "storefront", connectorManifest, "connector.wasm", "functions.wasm")

	registry := NewRegistry(base)
	if err := registry.LoadAll(context.Background(), []*PluginDeclaration{
		{Name: "storefront", Source: "./storefront"},
	}); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "plugins.lock")); err == nil {
		t.Error("a lock file was written for a plugin with nothing to pin")
	}
}

func TestASettingWrittenInAFormNothingUnderstandsIsReported(t *testing.T) {
	// This used to be skipped without a word, which meant a plugin author's
	// schema was not there: nothing required was required, nothing sensitive
	// was hidden, and no default was applied — with no indication why.
	base := t.TempDir()
	pluginDir(t, base, "storefront", `
plugin {
  name    = "storefront"
  version = "1.0.0"
}

provides {
  connector "storefront" {
    wasm = "connector.wasm"

    config {
      base_url = string({ required = whatever_that_is })
    }
  }
}
`, "connector.wasm")

	err := NewRegistry(base).Load(context.Background(), &PluginDeclaration{
		Name: "storefront", Source: "./storefront",
	})
	if err == nil {
		t.Fatal("a schema nothing could read was accepted")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Errorf("error = %q, want it to name the setting", err)
	}
}

func TestEveryFormASchemaCanBeWrittenIn(t *testing.T) {
	// Four, and all four have to work: a plugin author reading one page of
	// documentation should not find their schema silently absent because they
	// wrote it the way another page shows.
	base := t.TempDir()
	pluginDir(t, base, "storefront", `
plugin {
  name    = "storefront"
  version = "1.0.0"
}

provides {
  connector "storefront" {
    wasm = "connector.wasm"

    config {
      quoted      = "number"
      bare        = number
      constrained = string({ required = true })
      spelled_out = { type = "string", sensitive = true }
    }
  }
}
`, "connector.wasm")

	loaded, err := NewLoader(base).Load(context.Background(), &PluginDeclaration{
		Name: "storefront", Source: "./storefront",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	schema := loaded.Manifest.Provides.Connectors[0].ConfigSchema

	if schema["quoted"].Type != "number" || schema["bare"].Type != "number" {
		t.Errorf("quoted = %+v, bare = %+v", schema["quoted"], schema["bare"])
	}
	if !schema["constrained"].Required || schema["constrained"].Type != "string" {
		t.Errorf("constrained = %+v", schema["constrained"])
	}
	if !schema["spelled_out"].Sensitive || schema["spelled_out"].Type != "string" {
		t.Errorf("spelled out = %+v", schema["spelled_out"])
	}
}
