package rules

import (
	"strings"
	"testing"
)

// --- XML Entity Rule Tests ---

func TestXMLEntityRule_CleanInput(t *testing.T) {
	rule := &XMLEntityRule{}
	input := map[string]interface{}{"name": "John &amp; Jane"}
	result, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]interface{})
	if m["name"] != "John &amp; Jane" {
		t.Errorf("expected clean passthrough, got %q", m["name"])
	}
}

func TestXMLEntityRule_BlockDOCTYPE(t *testing.T) {
	rule := &XMLEntityRule{}
	input := map[string]interface{}{
		"xml": `<!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>`,
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for DOCTYPE")
	}
	if !strings.Contains(err.Error(), "XXE") {
		t.Errorf("expected XXE error, got: %v", err)
	}
}

func TestXMLEntityRule_BlockCustomEntity(t *testing.T) {
	rule := &XMLEntityRule{}
	input := map[string]interface{}{
		"data": "Hello &xxe; World",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for custom entity")
	}
}

func TestXMLEntityRule_AllowNumericRef(t *testing.T) {
	rule := &XMLEntityRule{}
	input := map[string]interface{}{
		"data": "Hello &#169; World",
	}
	_, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error for numeric reference: %v", err)
	}
}

func TestXMLEntityRule_NestedInput(t *testing.T) {
	rule := &XMLEntityRule{}
	input := map[string]interface{}{
		"user": map[string]interface{}{
			"name": "safe",
			"bio":  `<!ENTITY x "evil">`,
		},
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for nested entity")
	}
}

// --- SQL Identifier Rule Tests ---

func TestSQLIdentifierRule_ValidIdentifier(t *testing.T) {
	rule := &SQLIdentifierRule{IdentifierFields: []string{"table"}}
	input := map[string]interface{}{
		"table": "users",
	}
	_, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSQLIdentifierRule_InvalidIdentifier(t *testing.T) {
	rule := &SQLIdentifierRule{IdentifierFields: []string{"table"}}
	input := map[string]interface{}{
		"table": "users; DROP TABLE users",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for SQL injection in identifier")
	}
}

func TestSQLIdentifierRule_DottedIdentifier(t *testing.T) {
	rule := &SQLIdentifierRule{IdentifierFields: []string{"table"}}
	input := map[string]interface{}{
		"table": "schema.users",
	}
	_, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error for dotted identifier: %v", err)
	}
}

func TestValidateSQLIdentifier(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"users", true},
		{"my_table", true},
		{"schema.table", true},
		{"_private", true},
		{"123invalid", false},
		{"users; DROP", false},
		{"table--comment", false},
		{"", false},
	}

	for _, tt := range tests {
		result := ValidateSQLIdentifier(tt.input)
		if result != tt.valid {
			t.Errorf("ValidateSQLIdentifier(%q) = %v, want %v", tt.input, result, tt.valid)
		}
	}
}

// --- File Path Rule Tests ---

func TestFilePathRule_SafePath(t *testing.T) {
	rule := &FilePathRule{BasePath: "/data"}
	input := map[string]interface{}{
		"path": "reports/2024/file.csv",
	}
	_, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilePathRule_TraversalContained(t *testing.T) {
	// ../../etc/passwd resolves to /data/etc/passwd — safely contained
	err := ValidatePathContainment("../../etc/passwd", "/data")
	if err != nil {
		t.Fatalf("path traversal should be safely contained, got error: %v", err)
	}
}

func TestFilePathRule_AbsolutePathContained(t *testing.T) {
	// /etc/passwd resolves to /data/etc/passwd — safely contained
	err := ValidatePathContainment("/etc/passwd", "/data")
	if err != nil {
		t.Fatalf("absolute path should be safely contained, got error: %v", err)
	}
}

func TestFilePathRule_NullByteBlocked(t *testing.T) {
	err := ValidatePathContainment("file\x00.csv", "/data")
	if err == nil {
		t.Fatal("expected error for null byte in path")
	}
}

// --- Exec Shell Rule Tests ---

func TestExecShellRule_SafeInput(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"arg"}}
	input := map[string]interface{}{
		"arg": "hello-world",
	}
	_, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecShellRule_DetectsSemicolon(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"arg"}}
	input := map[string]interface{}{
		"arg": "file.txt; rm -rf /",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for semicolon")
	}
}

func TestExecShellRule_DetectsBacktick(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"arg"}}
	input := map[string]interface{}{
		"arg": "`whoami`",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for backtick")
	}
}

func TestExecShellRule_DetectsDollarParen(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"arg"}}
	input := map[string]interface{}{
		"arg": "$(cat /etc/passwd)",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("expected error for dollar-paren")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\\''s'"},
		{"; rm -rf /", "'; rm -rf /'"},
	}
	for _, tt := range tests {
		result := ShellQuote(tt.input)
		if result != tt.expected {
			t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
