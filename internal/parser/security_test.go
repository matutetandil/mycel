package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSecurityBlock(t *testing.T) {
	// Create a temp file with security config
	dir := t.TempDir()
	hclContent := `
security {
  max_input_length      = 2097152
  max_field_length      = 131072
  max_field_depth       = 30
  allowed_control_chars = ["tab", "newline"]

  sanitizer "strip_html" {
    source     = "wasm"
    wasm       = "plugins/strip_html.wasm"
    entrypoint = "sanitize"
    apply_to   = ["flows/api/*"]
    fields     = ["body", "description"]
  }

  flow "bulk_import" {
    max_input_length = 10485760
    sanitizers       = ["strip_html"]
  }
}
`
	path := filepath.Join(dir, "security.mycel")
	if err := os.WriteFile(path, []byte(hclContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewHCLParser()
	config, err := p.ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	sec := config.Security
	if sec == nil {
		t.Fatal("expected security config, got nil")
	}

	if sec.MaxInputLength != 2097152 {
		t.Errorf("expected MaxInputLength=2097152, got %d", sec.MaxInputLength)
	}
	if sec.MaxFieldLength != 131072 {
		t.Errorf("expected MaxFieldLength=131072, got %d", sec.MaxFieldLength)
	}
	if sec.MaxFieldDepth != 30 {
		t.Errorf("expected MaxFieldDepth=30, got %d", sec.MaxFieldDepth)
	}
	if len(sec.AllowedControlChars) != 2 {
		t.Errorf("expected 2 allowed control chars, got %d", len(sec.AllowedControlChars))
	}

	// Check sanitizer
	if len(sec.Sanitizers) != 1 {
		t.Fatalf("expected 1 sanitizer, got %d", len(sec.Sanitizers))
	}
	s := sec.Sanitizers[0]
	if s.Name != "strip_html" {
		t.Errorf("expected sanitizer name 'strip_html', got %q", s.Name)
	}
	if s.Source != "wasm" {
		t.Errorf("expected source 'wasm', got %q", s.Source)
	}
	if s.WASM != "plugins/strip_html.wasm" {
		t.Errorf("expected wasm path, got %q", s.WASM)
	}
	if s.Entrypoint != "sanitize" {
		t.Errorf("expected entrypoint 'sanitize', got %q", s.Entrypoint)
	}
	if len(s.ApplyTo) != 1 || s.ApplyTo[0] != "flows/api/*" {
		t.Errorf("expected apply_to=['flows/api/*'], got %v", s.ApplyTo)
	}
	if len(s.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(s.Fields))
	}

	// Check flow override
	if len(sec.FlowOverrides) != 1 {
		t.Fatalf("expected 1 flow override, got %d", len(sec.FlowOverrides))
	}
	fo := sec.FlowOverrides["bulk_import"]
	if fo == nil {
		t.Fatal("expected flow override for 'bulk_import'")
	}
	if fo.MaxInputLength != 10485760 {
		t.Errorf("expected MaxInputLength=10485760, got %d", fo.MaxInputLength)
	}
	if len(fo.Sanitizers) != 1 || fo.Sanitizers[0] != "strip_html" {
		t.Errorf("expected sanitizers=['strip_html'], got %v", fo.Sanitizers)
	}
}

func TestParseSecurityDefaults(t *testing.T) {
	// Minimal security block
	dir := t.TempDir()
	hclContent := `
security {
}
`
	path := filepath.Join(dir, "security.mycel")
	if err := os.WriteFile(path, []byte(hclContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := NewHCLParser()
	config, err := p.ParseFile(context.Background(), path)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	sec := config.Security
	if sec == nil {
		t.Fatal("expected security config, got nil")
	}

	// All values should be zero (defaults applied at runtime)
	if sec.MaxInputLength != 0 {
		t.Errorf("expected MaxInputLength=0 (default), got %d", sec.MaxInputLength)
	}
}
