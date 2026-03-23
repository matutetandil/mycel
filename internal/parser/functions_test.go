package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFunctionsBlock(t *testing.T) {
	// Create a temporary HCL file with functions block
	content := `
functions "pricing" {
  wasm    = "./functions/pricing.wasm"
  exports = ["calculate_price", "apply_discount"]
}

functions "geo" {
  wasm    = "./functions/geo.wasm"
  exports = ["distance_km", "in_polygon", "nearest_location"]
}
`
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "mycel-functions-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write test file
	testFile := filepath.Join(tmpDir, "functions.mycel")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Parse the file
	p := NewHCLParser()
	config, err := p.ParseFile(context.Background(), testFile)
	if err != nil {
		t.Fatalf("failed to parse file: %v", err)
	}

	// Verify functions were parsed
	if len(config.Functions) != 2 {
		t.Fatalf("expected 2 functions configs, got %d", len(config.Functions))
	}

	// Verify first function config (pricing)
	pricing := config.Functions[0]
	if pricing.Name != "pricing" {
		t.Errorf("expected name 'pricing', got %q", pricing.Name)
	}
	if pricing.WASM != "./functions/pricing.wasm" {
		t.Errorf("expected wasm './functions/pricing.wasm', got %q", pricing.WASM)
	}
	if len(pricing.Exports) != 2 {
		t.Errorf("expected 2 exports, got %d", len(pricing.Exports))
	}
	if pricing.Exports[0] != "calculate_price" {
		t.Errorf("expected first export 'calculate_price', got %q", pricing.Exports[0])
	}
	if pricing.Exports[1] != "apply_discount" {
		t.Errorf("expected second export 'apply_discount', got %q", pricing.Exports[1])
	}

	// Verify second function config (geo)
	geo := config.Functions[1]
	if geo.Name != "geo" {
		t.Errorf("expected name 'geo', got %q", geo.Name)
	}
	if geo.WASM != "./functions/geo.wasm" {
		t.Errorf("expected wasm './functions/geo.wasm', got %q", geo.WASM)
	}
	if len(geo.Exports) != 3 {
		t.Errorf("expected 3 exports, got %d", len(geo.Exports))
	}
}

func TestParseFunctionsBlock_MissingWASM(t *testing.T) {
	content := `
functions "test" {
  exports = ["fn1"]
}
`
	tmpDir, err := os.MkdirTemp("", "mycel-functions-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "functions.mycel")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewHCLParser()
	_, err = p.ParseFile(context.Background(), testFile)
	if err == nil {
		t.Error("expected error for missing 'wasm' attribute")
	}
}

func TestParseFunctionsBlock_MissingExports(t *testing.T) {
	content := `
functions "test" {
  wasm = "./test.wasm"
}
`
	tmpDir, err := os.MkdirTemp("", "mycel-functions-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "functions.mycel")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewHCLParser()
	_, err = p.ParseFile(context.Background(), testFile)
	if err == nil {
		t.Error("expected error for missing 'exports' attribute")
	}
}

func TestParseFunctionsBlock_MissingName(t *testing.T) {
	content := `
functions {
  wasm    = "./test.wasm"
  exports = ["fn1"]
}
`
	tmpDir, err := os.MkdirTemp("", "mycel-functions-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "functions.mycel")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p := NewHCLParser()
	_, err = p.ParseFile(context.Background(), testFile)
	if err == nil {
		t.Error("expected error for missing name label")
	}
}
