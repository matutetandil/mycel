package functions

import (
	"testing"
)

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}

	names := reg.ModuleNames()
	if len(names) != 0 {
		t.Errorf("expected empty registry, got %d modules", len(names))
	}
}

func TestRegistry_GetFunction_NotFound(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.GetFunction("nonexistent", "fn")
	if err == nil {
		t.Error("expected error for non-existent module")
	}
}

func TestRegistry_GetAllFunctions_Empty(t *testing.T) {
	reg := NewRegistry()

	funcs := reg.GetAllFunctions()
	if len(funcs) != 0 {
		t.Errorf("expected empty map, got %d functions", len(funcs))
	}
}

func TestRegistry_FunctionNames_NotFound(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.FunctionNames("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent module")
	}
}

func TestRegistry_Close(t *testing.T) {
	reg := NewRegistry()

	err := reg.Close()
	if err != nil {
		t.Errorf("unexpected error closing empty registry: %v", err)
	}
}

// TestConfig verifies the Config struct fields
func TestConfig(t *testing.T) {
	cfg := &Config{
		Name:    "math",
		WASM:    "./functions/math.wasm",
		Exports: []string{"add", "multiply", "divide"},
	}

	if cfg.Name != "math" {
		t.Errorf("expected name 'math', got %q", cfg.Name)
	}

	if cfg.WASM != "./functions/math.wasm" {
		t.Errorf("expected wasm path './functions/math.wasm', got %q", cfg.WASM)
	}

	if len(cfg.Exports) != 3 {
		t.Errorf("expected 3 exports, got %d", len(cfg.Exports))
	}

	expected := []string{"add", "multiply", "divide"}
	for i, exp := range expected {
		if cfg.Exports[i] != exp {
			t.Errorf("expected export[%d] = %q, got %q", i, exp, cfg.Exports[i])
		}
	}
}

// TestRegistry_Register_InvalidPath verifies error handling for invalid WASM path
func TestRegistry_Register_InvalidPath(t *testing.T) {
	reg := NewRegistry()

	cfg := &Config{
		Name:    "test",
		WASM:    "/nonexistent/path/module.wasm",
		Exports: []string{"validate"},
	}

	err := reg.Register(cfg)
	if err == nil {
		t.Error("expected error for non-existent WASM file")
	}
}
