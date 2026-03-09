package sanitize

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxInputLength != DefaultMaxInputLength {
		t.Errorf("expected MaxInputLength %d, got %d", DefaultMaxInputLength, cfg.MaxInputLength)
	}
	if cfg.MaxFieldLength != DefaultMaxFieldLength {
		t.Errorf("expected MaxFieldLength %d, got %d", DefaultMaxFieldLength, cfg.MaxFieldLength)
	}
	if cfg.MaxFieldDepth != DefaultMaxFieldDepth {
		t.Errorf("expected MaxFieldDepth %d, got %d", DefaultMaxFieldDepth, cfg.MaxFieldDepth)
	}
	if !cfg.AllowedControlChars['\t'] || !cfg.AllowedControlChars['\n'] || !cfg.AllowedControlChars['\r'] {
		t.Error("expected tab, newline, cr to be allowed by default")
	}
}

func TestSanitizeNilInput(t *testing.T) {
	p := NewPipeline(nil)
	result, err := p.Sanitize(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestSanitizeCleanInput(t *testing.T) {
	p := NewPipeline(nil)
	input := map[string]interface{}{
		"name":  "John Doe",
		"email": "john@example.com",
		"age":   float64(30),
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "John Doe" {
		t.Errorf("expected 'John Doe', got %q", result["name"])
	}
}

func TestSanitizeStripNullBytes(t *testing.T) {
	p := NewPipeline(nil)
	input := map[string]interface{}{
		"name": "John\x00Doe",
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "JohnDoe" {
		t.Errorf("expected 'JohnDoe', got %q", result["name"])
	}
}

func TestSanitizeStripControlChars(t *testing.T) {
	p := NewPipeline(nil)
	// \x01 (SOH) should be stripped, \t should stay
	input := map[string]interface{}{
		"data": "hello\x01\tworld",
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["data"] != "hello\tworld" {
		t.Errorf("expected 'hello\\tworld', got %q", result["data"])
	}
}

func TestSanitizePreserveAllowedControlChars(t *testing.T) {
	p := NewPipeline(nil)
	input := map[string]interface{}{
		"text": "line1\nline2\r\nline3\ttab",
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["text"] != "line1\nline2\r\nline3\ttab" {
		t.Errorf("expected preserved control chars, got %q", result["text"])
	}
}

func TestSanitizeInvalidUTF8(t *testing.T) {
	p := NewPipeline(nil)
	input := map[string]interface{}{
		"name": "hello\xc0\xafworld",
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	name := result["name"].(string)
	if strings.Contains(name, "\xc0") {
		t.Errorf("expected invalid UTF-8 to be stripped, got %q", name)
	}
}

func TestSanitizeMaxInputLength(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxInputLength = 100
	p := NewPipeline(cfg)

	input := map[string]interface{}{
		"data": strings.Repeat("x", 200),
	}
	_, err := p.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for oversized input")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected size error, got: %v", err)
	}
}

func TestSanitizeMaxFieldLength(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFieldLength = 50
	p := NewPipeline(cfg)

	input := map[string]interface{}{
		"data": strings.Repeat("x", 100),
	}
	_, err := p.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for oversized field")
	}
	if !strings.Contains(err.Error(), "string length") {
		t.Errorf("expected field length error, got: %v", err)
	}
}

func TestSanitizeMaxDepth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFieldDepth = 3
	p := NewPipeline(cfg)

	// Build nested input 5 levels deep
	inner := map[string]interface{}{"val": "deep"}
	for i := 0; i < 4; i++ {
		inner = map[string]interface{}{"nested": inner}
	}
	_, err := p.Sanitize(inner)
	if err == nil {
		t.Fatal("expected error for deeply nested input")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("expected depth error, got: %v", err)
	}
}

func TestSanitizeNestedStructures(t *testing.T) {
	p := NewPipeline(nil)
	input := map[string]interface{}{
		"user": map[string]interface{}{
			"name":  "John\x00",
			"items": []interface{}{"a\x00b", "cd"},
		},
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user := result["user"].(map[string]interface{})
	if user["name"] != "John" {
		t.Errorf("expected 'John', got %q", user["name"])
	}
	items := user["items"].([]interface{})
	if items[0] != "ab" {
		t.Errorf("expected 'ab', got %q", items[0])
	}
}

func TestSanitizeBidiOverride(t *testing.T) {
	p := NewPipeline(nil)
	input := map[string]interface{}{
		"filename": "invoice\u202Efdp.exe",
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result["filename"].(string), "\u202E") {
		t.Error("expected bidi override to be stripped")
	}
}

func TestSanitizeCustomRule(t *testing.T) {
	p := NewPipeline(nil)
	p.AddRule(&testRule{name: "test_rule"})

	input := map[string]interface{}{
		"name": "hello",
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["_sanitized"] != true {
		t.Error("expected custom rule to add _sanitized flag")
	}
}

func TestSanitizePassthroughNonStrings(t *testing.T) {
	p := NewPipeline(nil)
	input := map[string]interface{}{
		"count":   float64(42),
		"active":  true,
		"nothing": nil,
	}
	result, err := p.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["count"] != float64(42) {
		t.Errorf("expected 42, got %v", result["count"])
	}
	if result["active"] != true {
		t.Errorf("expected true, got %v", result["active"])
	}
}

func TestParseAllowedControlChars(t *testing.T) {
	result := ParseAllowedControlChars([]string{"tab", "newline"})
	if !result['\t'] {
		t.Error("expected tab to be allowed")
	}
	if !result['\n'] {
		t.Error("expected newline to be allowed")
	}
	if result['\r'] {
		t.Error("expected CR to not be allowed")
	}
}

// testRule is a simple rule for testing the custom rule mechanism.
type testRule struct {
	name string
}

func (r *testRule) Name() string { return r.name }
func (r *testRule) Sanitize(value interface{}) (interface{}, error) {
	if m, ok := value.(map[string]interface{}); ok {
		m["_sanitized"] = true
		return m, nil
	}
	return value, nil
}
