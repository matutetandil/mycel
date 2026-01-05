package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/matutetandil/mycel/internal/plugin"
)

func TestParsePluginBlock(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "mycel-plugin-parser-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test HCL file with plugin blocks
	hclContent := `
plugin "salesforce" {
  source  = "./plugins/salesforce"
}

plugin "stripe" {
  source  = "github.com/acme/mycel-stripe"
  version = "~> 1.0"
}
`

	testFile := filepath.Join(tmpDir, "plugins.hcl")
	if err := os.WriteFile(testFile, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Parse the file
	parser := NewHCLParser()
	config, err := parser.ParseFile(context.Background(), testFile)
	if err != nil {
		t.Fatalf("failed to parse file: %v", err)
	}

	// Verify we got 2 plugin declarations
	if len(config.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(config.Plugins))
	}

	// Check first plugin
	if config.Plugins[0].Name != "salesforce" {
		t.Errorf("expected plugin name 'salesforce', got '%s'", config.Plugins[0].Name)
	}
	if config.Plugins[0].Source != "./plugins/salesforce" {
		t.Errorf("expected source './plugins/salesforce', got '%s'", config.Plugins[0].Source)
	}
	if config.Plugins[0].Version != "" {
		t.Errorf("expected empty version, got '%s'", config.Plugins[0].Version)
	}

	// Check second plugin
	if config.Plugins[1].Name != "stripe" {
		t.Errorf("expected plugin name 'stripe', got '%s'", config.Plugins[1].Name)
	}
	if config.Plugins[1].Source != "github.com/acme/mycel-stripe" {
		t.Errorf("expected source 'github.com/acme/mycel-stripe', got '%s'", config.Plugins[1].Source)
	}
	if config.Plugins[1].Version != "~> 1.0" {
		t.Errorf("expected version '~> 1.0', got '%s'", config.Plugins[1].Version)
	}
}

func TestParsePluginBlock_MissingSource(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mycel-plugin-parser-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Plugin without required source attribute
	hclContent := `
plugin "invalid" {
}
`

	testFile := filepath.Join(tmpDir, "plugins.hcl")
	if err := os.WriteFile(testFile, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	parser := NewHCLParser()
	_, err = parser.ParseFile(context.Background(), testFile)
	if err == nil {
		t.Error("expected error for plugin without source, got nil")
	}
}

func TestConfigurationMerge_WithPlugins(t *testing.T) {
	config1 := NewConfiguration()
	config2 := NewConfiguration()

	// Add plugins to config2
	config2.Plugins = append(config2.Plugins, &plugin.PluginDeclaration{Name: "plugin1", Source: "./plugin1"})
	config2.Plugins = append(config2.Plugins, &plugin.PluginDeclaration{Name: "plugin2", Source: "./plugin2"})

	// Merge
	config1.Merge(config2)

	// Verify
	if len(config1.Plugins) != 2 {
		t.Errorf("expected 2 plugins after merge, got %d", len(config1.Plugins))
	}
}
