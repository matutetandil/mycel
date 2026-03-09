package sanitize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAttack_SQLInjection verifies that classic SQL injection payloads
// are sanitized (null bytes stripped, control chars removed).
// Note: actual SQL injection prevention is done via parameterized queries
// in each database connector — the sanitizer provides defense-in-depth.
func TestAttack_SQLInjection(t *testing.T) {
	p := NewPipeline(nil)

	tests := []struct {
		name  string
		input map[string]interface{}
		check func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "classic OR 1=1 passthrough (handled by prepared statements)",
			input: map[string]interface{}{
				"username": "admin' OR '1'='1",
				"password": "' OR ''='",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				// SQL injection strings pass through sanitizer (they're valid UTF-8)
				// but are harmless because connectors use parameterized queries
				if result["username"] != "admin' OR '1'='1" {
					t.Errorf("expected SQL string to pass through as data, got %q", result["username"])
				}
			},
		},
		{
			name: "null byte injection stripped",
			input: map[string]interface{}{
				"name": "admin\x00' OR '1'='1",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				name := result["name"].(string)
				if strings.Contains(name, "\x00") {
					t.Error("null byte should be stripped")
				}
				if name != "admin' OR '1'='1" {
					t.Errorf("expected null byte removed, got %q", name)
				}
			},
		},
		{
			name: "union select with control chars",
			input: map[string]interface{}{
				"id": "1\x00 UNION SELECT * FROM users--",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				id := result["id"].(string)
				if strings.Contains(id, "\x00") {
					t.Error("null byte should be stripped from SQL injection")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Sanitize(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, result)
		})
	}
}

// TestAttack_XSS verifies that XSS payloads have dangerous control
// characters stripped and bidi overrides removed.
func TestAttack_XSS(t *testing.T) {
	p := NewPipeline(nil)

	tests := []struct {
		name  string
		input map[string]interface{}
		check func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "script tag passthrough (XSS handled at output layer)",
			input: map[string]interface{}{
				"comment": "<script>alert('xss')</script>",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				// HTML/JS is valid UTF-8 — XSS prevention is at the output layer
				// (Content-Type headers, template escaping, etc.)
				if result["comment"] != "<script>alert('xss')</script>" {
					t.Errorf("script tag should pass through sanitizer (not its job to handle XSS)")
				}
			},
		},
		{
			name: "null byte in script tag stripped",
			input: map[string]interface{}{
				"name": "<scr\x00ipt>alert(1)</script>",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				name := result["name"].(string)
				if strings.Contains(name, "\x00") {
					t.Error("null byte should be stripped from XSS payload")
				}
			},
		},
		{
			name: "bidi override in filename stripped",
			input: map[string]interface{}{
				"filename": "document\u202Efdp.exe",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				name := result["filename"].(string)
				if strings.Contains(name, "\u202E") {
					t.Error("bidi override should be stripped")
				}
				if name != "documentfdp.exe" {
					t.Errorf("expected 'documentfdp.exe', got %q", name)
				}
			},
		},
		{
			name: "all bidi overrides stripped from trojan filename",
			input: map[string]interface{}{
				"file": "report\u2066\u2069.pdf\u202Eexe.pdf",
			},
			check: func(t *testing.T, result map[string]interface{}) {
				name := result["file"].(string)
				for _, r := range name {
					if isBidiOverride(r) {
						t.Errorf("bidi char U+%04X should be stripped", r)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Sanitize(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, result)
		})
	}
}

// TestAttack_NullByteInjection verifies that null bytes are stripped
// in all positions and nested structures.
func TestAttack_NullByteInjection(t *testing.T) {
	p := NewPipeline(nil)

	tests := []struct {
		name  string
		input map[string]interface{}
	}{
		{
			name: "null byte at start",
			input: map[string]interface{}{
				"data": "\x00malicious",
			},
		},
		{
			name: "null byte at end",
			input: map[string]interface{}{
				"data": "filename.txt\x00.exe",
			},
		},
		{
			name: "multiple null bytes",
			input: map[string]interface{}{
				"data": "a\x00b\x00c\x00d",
			},
		},
		{
			name: "null bytes in nested map",
			input: map[string]interface{}{
				"user": map[string]interface{}{
					"name":  "John\x00",
					"email": "\x00admin@evil.com",
				},
			},
		},
		{
			name: "null bytes in array",
			input: map[string]interface{}{
				"tags": []interface{}{"safe", "evil\x00tag", "\x00"},
			},
		},
		{
			name: "null bytes in map key",
			input: map[string]interface{}{
				"normal\x00key": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Sanitize(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Verify no null bytes anywhere in the result
			assertNoNullBytes(t, result)
		})
	}
}

func assertNoNullBytes(t *testing.T, v interface{}) {
	t.Helper()
	switch val := v.(type) {
	case string:
		if strings.Contains(val, "\x00") {
			t.Errorf("null byte found in string: %q", val)
		}
	case map[string]interface{}:
		for k, v := range val {
			if strings.Contains(k, "\x00") {
				t.Errorf("null byte found in key: %q", k)
			}
			assertNoNullBytes(t, v)
		}
	case []interface{}:
		for _, item := range val {
			assertNoNullBytes(t, item)
		}
	}
}

// TestAttack_ControlCharInjection verifies that dangerous control characters
// are stripped while preserving safe ones (tab, newline, CR).
func TestAttack_ControlCharInjection(t *testing.T) {
	p := NewPipeline(nil)

	tests := []struct {
		name     string
		input    string
		contains string
		absent   string
	}{
		{
			name:   "header injection via CRLF",
			input:  "value\r\nX-Injected: evil",
			contains: "value\r\nX-Injected: evil", // CR and LF are allowed by default
		},
		{
			name:   "bell character stripped",
			input:  "hello\x07world",
			absent: "\x07",
		},
		{
			name:   "escape character stripped",
			input:  "hello\x1bworld",
			absent: "\x1b",
		},
		{
			name:   "backspace stripped",
			input:  "admin\x08\x08\x08\x08\x08root",
			absent: "\x08",
		},
		{
			name:   "form feed stripped",
			input:  "page1\x0cpage2",
			absent: "\x0c",
		},
		{
			name:     "tab preserved",
			input:    "col1\tcol2",
			contains: "\t",
		},
		{
			name:     "newline preserved",
			input:    "line1\nline2",
			contains: "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Sanitize(map[string]interface{}{
				"data": tt.input,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			data := result["data"].(string)
			if tt.contains != "" && !strings.Contains(data, tt.contains) {
				t.Errorf("expected %q to be preserved, got %q", tt.contains, data)
			}
			if tt.absent != "" && strings.Contains(data, tt.absent) {
				t.Errorf("expected %q to be stripped, got %q", tt.absent, data)
			}
		})
	}
}

// TestAttack_OversizedPayload verifies that oversized inputs are rejected.
func TestAttack_OversizedPayload(t *testing.T) {
	t.Run("body exceeds max input length", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxInputLength = 1024 // 1KB limit
		p := NewPipeline(cfg)

		input := map[string]interface{}{
			"data": strings.Repeat("A", 2000),
		}
		_, err := p.Sanitize(input)
		if err == nil {
			t.Fatal("expected error for oversized input")
		}
		if !strings.Contains(err.Error(), "exceeds maximum") {
			t.Errorf("expected size error, got: %v", err)
		}
	})

	t.Run("single field exceeds max field length", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MaxFieldLength = 100
		p := NewPipeline(cfg)

		input := map[string]interface{}{
			"bio": strings.Repeat("B", 200),
		}
		_, err := p.Sanitize(input)
		if err == nil {
			t.Fatal("expected error for oversized field")
		}
		if !strings.Contains(err.Error(), "string length") {
			t.Errorf("expected field length error, got: %v", err)
		}
	})
}

// TestAttack_DeepNesting verifies that deeply nested payloads (JSON bomb) are rejected.
func TestAttack_DeepNesting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxFieldDepth = 10
	p := NewPipeline(cfg)

	// Build a 15-level deep structure
	inner := map[string]interface{}{"value": "deep"}
	for i := 0; i < 14; i++ {
		inner = map[string]interface{}{"nested": inner}
	}

	_, err := p.Sanitize(inner)
	if err == nil {
		t.Fatal("expected error for deeply nested input (JSON bomb)")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("expected depth error, got: %v", err)
	}
}

// TestAttack_InvalidUTF8 verifies that malformed UTF-8 is sanitized.
func TestAttack_InvalidUTF8(t *testing.T) {
	p := NewPipeline(nil)

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "overlong encoding (classic UTF-8 attack)",
			input: "hello\xc0\xafworld",
		},
		{
			name:  "truncated multi-byte sequence",
			input: "test\xe2\x80data",
		},
		{
			name:  "invalid continuation byte",
			input: "abc\x80def",
		},
		{
			name:  "mixed valid and invalid",
			input: "good\xc0\xaf中文\xfe\xff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Sanitize(map[string]interface{}{
				"data": tt.input,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			data := result["data"].(string)
			// Verify result is valid UTF-8
			if !utf8.ValidString(data) {
				t.Errorf("result is not valid UTF-8: %q", data)
			}
		})
	}
}

// TestAttack_BidiTrojanSource verifies protection against Trojan Source attacks
// using Unicode bidirectional override characters.
func TestAttack_BidiTrojanSource(t *testing.T) {
	p := NewPipeline(nil)

	bidiChars := []rune{
		'\u202A', // Left-to-Right Embedding
		'\u202B', // Right-to-Left Embedding
		'\u202C', // Pop Directional Formatting
		'\u202D', // Left-to-Right Override
		'\u202E', // Right-to-Left Override
		'\u2066', // Left-to-Right Isolate
		'\u2067', // Right-to-Left Isolate
		'\u2068', // First Strong Isolate
		'\u2069', // Pop Directional Isolate
	}

	for _, ch := range bidiChars {
		t.Run("strip bidi char", func(t *testing.T) {
			input := map[string]interface{}{
				"code": "var x = " + string(ch) + "safe",
			}
			result, err := p.Sanitize(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			code := result["code"].(string)
			if strings.ContainsRune(code, ch) {
				t.Errorf("bidi char U+%04X should be stripped", ch)
			}
		})
	}
}

// TestAttack_RealisticPayloads tests realistic attack scenarios
// that combine multiple techniques.
func TestAttack_RealisticPayloads(t *testing.T) {
	p := NewPipeline(nil)

	t.Run("polyglot injection (SQL + XSS + null byte)", func(t *testing.T) {
		input := map[string]interface{}{
			"search": "'; DROP TABLE users;--<script>alert(1)</script>\x00",
		}
		result, err := p.Sanitize(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data := result["search"].(string)
		if strings.Contains(data, "\x00") {
			t.Error("null byte should be stripped from polyglot payload")
		}
	})

	t.Run("nested malicious payload", func(t *testing.T) {
		input := map[string]interface{}{
			"user": map[string]interface{}{
				"name":    "Admin\x00",
				"email":   "admin@evil.com\x01",
				"profile": map[string]interface{}{
					"bio":    "Hello\x07World",
					"avatar": "image\u202E.png",
				},
			},
			"tags": []interface{}{
				"normal",
				"evil\x00tag",
				"\x1b[31mred\x1b[0m",
			},
		}
		result, err := p.Sanitize(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		user := result["user"].(map[string]interface{})
		if strings.Contains(user["name"].(string), "\x00") {
			t.Error("null byte in name should be stripped")
		}
		if strings.Contains(user["email"].(string), "\x01") {
			t.Error("SOH in email should be stripped")
		}

		profile := user["profile"].(map[string]interface{})
		if strings.Contains(profile["bio"].(string), "\x07") {
			t.Error("bell char in bio should be stripped")
		}
		if strings.Contains(profile["avatar"].(string), "\u202E") {
			t.Error("bidi override in avatar should be stripped")
		}

		tags := result["tags"].([]interface{})
		if strings.Contains(tags[1].(string), "\x00") {
			t.Error("null byte in tag should be stripped")
		}
		if strings.Contains(tags[2].(string), "\x1b") {
			t.Error("escape char in tag should be stripped")
		}
	})

	t.Run("CRLF header injection in field value", func(t *testing.T) {
		// With restrictive config that disallows CR/LF
		cfg := DefaultConfig()
		cfg.AllowedControlChars = map[byte]bool{'\t': true} // only tab allowed
		p := NewPipeline(cfg)

		input := map[string]interface{}{
			"header": "value\r\nX-Injected: evil\r\n\r\nHTTP/1.1 200 OK",
		}
		result, err := p.Sanitize(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data := result["header"].(string)
		if strings.Contains(data, "\r") || strings.Contains(data, "\n") {
			t.Error("CR/LF should be stripped when not in allowed list")
		}
		expected := "valueX-Injected: evilHTTP/1.1 200 OK"
		if data != expected {
			t.Errorf("expected %q, got %q", expected, data)
		}
	})
}
