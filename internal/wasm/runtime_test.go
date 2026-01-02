package wasm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewRuntime(t *testing.T) {
	ctx := context.Background()
	r, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer r.Close()

	// Runtime should be created successfully
	if r == nil {
		t.Fatal("runtime is nil")
	}
}

func TestLoadModule_FileNotFound(t *testing.T) {
	ctx := context.Background()
	r, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer r.Close()

	_, err = r.LoadModule("test", "/nonexistent/path.wasm")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestGetModule_NotLoaded(t *testing.T) {
	ctx := context.Background()
	r, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer r.Close()

	_, ok := r.GetModule("nonexistent")
	if ok {
		t.Error("expected false for non-existent module")
	}
}

// TestLoadModule_ValidWASM tests loading a valid WASM module.
// This test is skipped if no test WASM file is available.
func TestLoadModule_ValidWASM(t *testing.T) {
	// Look for a test WASM file
	testWASM := findTestWASM()
	if testWASM == "" {
		t.Skip("no test WASM file available - skipping integration test")
	}

	ctx := context.Background()
	r, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer r.Close()

	module, err := r.LoadModule("test", testWASM)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	if module.Name() != "test" {
		t.Errorf("expected name 'test', got %q", module.Name())
	}

	if module.Path() != testWASM {
		t.Errorf("expected path %q, got %q", testWASM, module.Path())
	}

	// Should be able to get the module
	m, ok := r.GetModule("test")
	if !ok {
		t.Error("failed to get loaded module")
	}
	if m != module {
		t.Error("got different module instance")
	}

	// Loading again should return same module
	module2, err := r.LoadModule("test", testWASM)
	if err != nil {
		t.Fatalf("failed to reload module: %v", err)
	}
	if module2 != module {
		t.Error("expected same module instance on reload")
	}
}

func TestUnloadModule(t *testing.T) {
	testWASM := findTestWASM()
	if testWASM == "" {
		t.Skip("no test WASM file available - skipping integration test")
	}

	ctx := context.Background()
	r, err := NewRuntime(ctx)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	defer r.Close()

	_, err = r.LoadModule("test", testWASM)
	if err != nil {
		t.Fatalf("failed to load module: %v", err)
	}

	// Unload
	err = r.UnloadModule("test")
	if err != nil {
		t.Fatalf("failed to unload module: %v", err)
	}

	// Should not be found
	_, ok := r.GetModule("test")
	if ok {
		t.Error("module should not be found after unload")
	}

	// Unload non-existent should not error
	err = r.UnloadModule("nonexistent")
	if err != nil {
		t.Errorf("unexpected error unloading non-existent module: %v", err)
	}
}

// findTestWASM looks for a test WASM file in known locations.
func findTestWASM() string {
	// Check for test fixtures
	paths := []string{
		"testdata/test.wasm",
		"../testdata/test.wasm",
		"../../testdata/test.wasm",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}

	return ""
}
