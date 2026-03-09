package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
)

func TestNewLoader(t *testing.T) {
	loader := NewLoader("/test/base")

	if loader.BaseDir != "/test/base" {
		t.Errorf("expected BaseDir /test/base, got %s", loader.BaseDir)
	}

	if loader.CacheDir == "" {
		t.Error("expected CacheDir to be set")
	}
}

func TestNewLoader_CacheDirDefault(t *testing.T) {
	loader := NewLoader("/test/base")

	expected := "/test/base/mycel_plugins"
	if loader.CacheDir != expected {
		t.Errorf("expected CacheDir %s, got %s", expected, loader.CacheDir)
	}
}

func TestResolvePluginPath_Local(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "mycel-plugin-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a plugin directory
	pluginDir := filepath.Join(tmpDir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	loader := NewLoader(tmpDir)

	tests := []struct {
		name    string
		source  string
		wantErr bool
	}{
		{
			name:    "relative path",
			source:  "./my-plugin",
			wantErr: false,
		},
		{
			name:    "non-existent path",
			source:  "./non-existent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decl := &PluginDeclaration{
				Name:   "test",
				Source: tt.source,
			}

			path, err := loader.resolvePluginPath(decl)
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.wantErr && path == "" {
				t.Error("expected path but got empty string")
			}
		})
	}
}

func TestResolvePluginPath_LocalWithCopy(t *testing.T) {
	tmp := t.TempDir()

	// Create a local plugin with files
	pluginDir := filepath.Join(tmp, "my-plugin")
	os.MkdirAll(pluginDir, 0755)
	os.WriteFile(filepath.Join(pluginDir, "plugin.hcl"), []byte("plugin {}"), 0644)
	os.WriteFile(filepath.Join(pluginDir, "conn.wasm"), []byte("wasm"), 0644)

	loader := NewLoader(tmp)

	// Without copy — returns original path
	decl := &PluginDeclaration{Name: "test", Source: "./my-plugin", Copy: false}
	path, err := loader.resolvePluginPath(decl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != pluginDir {
		t.Errorf("expected original path %s, got %s", pluginDir, path)
	}

	// With copy — returns path inside mycel_plugins/
	declCopy := &PluginDeclaration{Name: "test", Source: "./my-plugin", Copy: true}
	copiedPath, err := loader.resolvePluginPath(declCopy)
	if err != nil {
		t.Fatalf("unexpected error with copy: %v", err)
	}
	if copiedPath == pluginDir {
		t.Error("expected copied path to differ from original")
	}
	if _, err := os.Stat(filepath.Join(copiedPath, "plugin.hcl")); err != nil {
		t.Error("plugin.hcl not found in copied location")
	}
	if _, err := os.Stat(filepath.Join(copiedPath, "conn.wasm")); err != nil {
		t.Error("conn.wasm not found in copied location")
	}
}

func TestResolvePluginPath_GitSourceDetection(t *testing.T) {
	// Verify that git sources are routed to resolveGitPlugin, not rejected
	// as "unknown source format". We check the routing without making
	// network calls by using an invalid version constraint.
	loader := NewLoader(t.TempDir())

	gitSources := []string{
		"github.com/acme/plugin",
		"gitlab.com/org/repo",
		"bitbucket.org/team/plugin",
	}

	for _, src := range gitSources {
		decl := &PluginDeclaration{Name: "test", Source: src, Version: "!!!invalid!!!"}
		_, err := loader.resolvePluginPath(decl)
		if err == nil {
			t.Errorf("expected error for %s", src)
			continue
		}
		// Should fail on version parsing (git path), NOT "unknown source format"
		if strings.Contains(err.Error(), "unknown plugin source format") {
			t.Errorf("source %q should be detected as git, got: %v", src, err)
		}
		if !strings.Contains(err.Error(), "invalid version constraint") {
			t.Errorf("expected version constraint error for %q, got: %v", src, err)
		}
	}
}

func TestResolvePluginPath_UnknownSource(t *testing.T) {
	loader := NewLoader("/test")

	decl := &PluginDeclaration{
		Name:   "test",
		Source: "some-invalid-source",
	}

	_, err := loader.resolvePluginPath(decl)
	if err == nil {
		t.Error("expected error for unknown source format")
	}
	if !strings.Contains(err.Error(), "unknown plugin source format") {
		t.Errorf("expected 'unknown plugin source format' error, got: %v", err)
	}
}

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry("/test")

	if reg.loader == nil {
		t.Error("expected loader to be set")
	}
	if reg.plugins == nil {
		t.Error("expected plugins map to be initialized")
	}
}

func TestRegistry_GetPlugin_NotFound(t *testing.T) {
	reg := NewRegistry("/test")

	_, ok := reg.GetPlugin("nonexistent")
	if ok {
		t.Error("expected plugin not found")
	}
}

func TestRegistry_GetConnector_NotFound(t *testing.T) {
	reg := NewRegistry("/test")

	_, err := reg.GetConnector("nonexistent", "some-type")
	if err == nil {
		t.Error("expected error for non-existent plugin")
	}
}

func TestRegistry_GetConnectorTypes_Empty(t *testing.T) {
	reg := NewRegistry("/test")

	types := reg.GetConnectorTypes()
	if len(types) != 0 {
		t.Errorf("expected 0 types, got %d", len(types))
	}
}

func TestRegistry_Close_Empty(t *testing.T) {
	reg := NewRegistry("/test")

	err := reg.Close(context.Background())
	if err != nil {
		t.Errorf("unexpected error closing empty registry: %v", err)
	}
}

func TestRegistry_Plugins_Empty(t *testing.T) {
	reg := NewRegistry("/test")

	plugins := reg.Plugins()
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(plugins))
	}
}

func TestNewWASMConnector(t *testing.T) {
	tests := []struct {
		name     string
		connName string
		typeName string
		wasmPath string
		config   map[string]interface{}
		wantErr  bool
	}{
		{
			name:     "valid connector",
			connName: "my-connector",
			typeName: "salesforce",
			wasmPath: "/path/to/connector.wasm",
			config:   map[string]interface{}{"api_key": "test"},
			wantErr:  false,
		},
		{
			name:     "empty wasm path",
			connName: "my-connector",
			typeName: "salesforce",
			wasmPath: "",
			config:   nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := NewWASMConnector(tt.connName, tt.typeName, tt.wasmPath, tt.config)
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.wantErr && conn == nil {
				t.Error("expected connector but got nil")
			}
			if !tt.wantErr && conn.Name() != tt.connName {
				t.Errorf("expected name %s, got %s", tt.connName, conn.Name())
			}
			if !tt.wantErr && conn.Type() != tt.typeName {
				t.Errorf("expected type %s, got %s", tt.typeName, conn.Type())
			}
		})
	}
}

func TestWASMConnector_Health_NotConnected(t *testing.T) {
	conn, err := NewWASMConnector("test", "salesforce", "/path/to/wasm", nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	err = conn.Health(context.Background())
	if err == nil {
		t.Error("expected error for health check on disconnected connector")
	}
}

func TestWASMConnector_Close_NotConnected(t *testing.T) {
	conn, err := NewWASMConnector("test", "salesforce", "/path/to/wasm", nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	// Should not error when closing a not-connected connector
	err = conn.Close(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWASMConnector_Read_NotConnected(t *testing.T) {
	conn, err := NewWASMConnector("test", "salesforce", "/path/to/wasm", nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	_, err = conn.Read(context.Background(), connector.Query{
		Target:    "accounts",
		Operation: "SELECT",
	})
	if err == nil {
		t.Error("expected error for read on disconnected connector")
	}
}

func TestWASMConnector_Write_NotConnected(t *testing.T) {
	conn, err := NewWASMConnector("test", "salesforce", "/path/to/wasm", nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	_, err = conn.Write(context.Background(), &connector.Data{
		Target:    "accounts",
		Operation: "INSERT",
	})
	if err == nil {
		t.Error("expected error for write on disconnected connector")
	}
}

func TestWASMConnector_Call_NotConnected(t *testing.T) {
	conn, err := NewWASMConnector("test", "salesforce", "/path/to/wasm", nil)
	if err != nil {
		t.Fatalf("failed to create connector: %v", err)
	}

	_, err = conn.Call(context.Background(), "getAccount", map[string]interface{}{"id": "123"})
	if err == nil {
		t.Error("expected error for call on disconnected connector")
	}
}

func TestFactory_Supports(t *testing.T) {
	reg := NewRegistry("/test")
	factory := NewFactory(reg)

	tests := []struct {
		name          string
		connectorType string
		driver        string
		want          bool
	}{
		{
			name:          "plugin type",
			connectorType: "plugin",
			driver:        "",
			want:          true,
		},
		{
			name:          "unknown type",
			connectorType: "unknown",
			driver:        "",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := factory.Supports(tt.connectorType, tt.driver)
			if got != tt.want {
				t.Errorf("Supports(%s, %s) = %v, want %v", tt.connectorType, tt.driver, got, tt.want)
			}
		})
	}
}

func TestParseManifest_WithValidatorsAndSanitizers(t *testing.T) {
	tmp := t.TempDir()

	// Create a plugin manifest with validators and sanitizers
	manifest := `
plugin {
  name    = "tax-utils"
  version = "1.0.0"
}

provides {
  validator "cuit" {
    wasm       = "validators.wasm"
    entrypoint = "validate_cuit"
    message    = "Invalid CUIT number"
  }

  validator "cnpj" {
    wasm    = "validators.wasm"
    message = "Invalid CNPJ number"
  }

  sanitizer "pii_filter" {
    wasm       = "sanitizers.wasm"
    entrypoint = "filter_pii"
    apply_to   = ["flows/api/*"]
    fields     = ["email", "phone"]
  }

  sanitizer "strip_html" {
    wasm = "sanitizers.wasm"
  }
}
`
	os.WriteFile(filepath.Join(tmp, "plugin.hcl"), []byte(manifest), 0644)

	loader := NewLoader(tmp)
	parsed, err := loader.parseManifest(tmp)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if parsed.Name != "tax-utils" {
		t.Errorf("expected name tax-utils, got %s", parsed.Name)
	}

	// Check validators
	if parsed.Provides == nil {
		t.Fatal("expected provides section")
	}
	if len(parsed.Provides.Validators) != 2 {
		t.Fatalf("expected 2 validators, got %d", len(parsed.Provides.Validators))
	}

	cuit := parsed.Provides.Validators[0]
	if cuit.Name != "cuit" {
		t.Errorf("expected validator name cuit, got %s", cuit.Name)
	}
	if cuit.WASM != "validators.wasm" {
		t.Errorf("expected wasm validators.wasm, got %s", cuit.WASM)
	}
	if cuit.Entrypoint != "validate_cuit" {
		t.Errorf("expected entrypoint validate_cuit, got %s", cuit.Entrypoint)
	}
	if cuit.Message != "Invalid CUIT number" {
		t.Errorf("expected message, got %s", cuit.Message)
	}

	cnpj := parsed.Provides.Validators[1]
	if cnpj.Entrypoint != "validate_cnpj" {
		t.Errorf("expected default entrypoint validate_cnpj, got %s", cnpj.Entrypoint)
	}

	// Check sanitizers
	if len(parsed.Provides.Sanitizers) != 2 {
		t.Fatalf("expected 2 sanitizers, got %d", len(parsed.Provides.Sanitizers))
	}

	pii := parsed.Provides.Sanitizers[0]
	if pii.Name != "pii_filter" {
		t.Errorf("expected sanitizer name pii_filter, got %s", pii.Name)
	}
	if pii.Entrypoint != "filter_pii" {
		t.Errorf("expected entrypoint filter_pii, got %s", pii.Entrypoint)
	}
	if len(pii.ApplyTo) != 1 || pii.ApplyTo[0] != "flows/api/*" {
		t.Errorf("expected apply_to [flows/api/*], got %v", pii.ApplyTo)
	}
	if len(pii.Fields) != 2 || pii.Fields[0] != "email" || pii.Fields[1] != "phone" {
		t.Errorf("expected fields [email, phone], got %v", pii.Fields)
	}

	strip := parsed.Provides.Sanitizers[1]
	if strip.Entrypoint != "sanitize" {
		t.Errorf("expected default entrypoint sanitize, got %s", strip.Entrypoint)
	}
}

func TestParseManifest_ConnectorsOnly(t *testing.T) {
	tmp := t.TempDir()

	// A manifest with only connectors (backward compat)
	manifest := `
plugin {
  name    = "salesforce"
  version = "2.0.0"
}

provides {
  connector "salesforce" {
    wasm = "connector.wasm"
  }
}
`
	os.WriteFile(filepath.Join(tmp, "plugin.hcl"), []byte(manifest), 0644)

	loader := NewLoader(tmp)
	parsed, err := loader.parseManifest(tmp)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if len(parsed.Provides.Connectors) != 1 {
		t.Errorf("expected 1 connector, got %d", len(parsed.Provides.Connectors))
	}
	if len(parsed.Provides.Validators) != 0 {
		t.Errorf("expected 0 validators, got %d", len(parsed.Provides.Validators))
	}
	if len(parsed.Provides.Sanitizers) != 0 {
		t.Errorf("expected 0 sanitizers, got %d", len(parsed.Provides.Sanitizers))
	}
}
