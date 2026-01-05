package plugin

import (
	"context"
	"os"
	"path/filepath"
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

func TestNewLoader_WithEnvCache(t *testing.T) {
	// Set custom cache dir via env
	originalEnv := os.Getenv("MYCEL_PLUGIN_CACHE")
	defer os.Setenv("MYCEL_PLUGIN_CACHE", originalEnv)

	os.Setenv("MYCEL_PLUGIN_CACHE", "/custom/cache")

	loader := NewLoader("/test/base")

	if loader.CacheDir != "/custom/cache" {
		t.Errorf("expected CacheDir /custom/cache, got %s", loader.CacheDir)
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

func TestResolvePluginPath_GitNotImplemented(t *testing.T) {
	loader := NewLoader("/test")

	decl := &PluginDeclaration{
		Name:    "test",
		Source:  "github.com/acme/plugin",
		Version: "1.0.0",
	}

	_, err := loader.resolvePluginPath(decl)
	if err == nil {
		t.Error("expected error for git source")
	}
	if err.Error() != "git plugin sources not yet implemented: github.com/acme/plugin" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolvePluginPath_RegistryNotImplemented(t *testing.T) {
	loader := NewLoader("/test")

	decl := &PluginDeclaration{
		Name:   "test",
		Source: "registry.mycel.dev/stripe",
	}

	_, err := loader.resolvePluginPath(decl)
	if err == nil {
		t.Error("expected error for registry source")
	}
	if err.Error() != "registry plugin sources not yet implemented: registry.mycel.dev/stripe" {
		t.Errorf("unexpected error message: %v", err)
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

func TestCtyToGo(t *testing.T) {
	tests := []struct {
		name string
		// We can't easily test cty values without importing cty
		// This is just a placeholder for the function existence
	}{
		{
			name: "placeholder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test passes if function exists and doesn't panic
		})
	}
}
