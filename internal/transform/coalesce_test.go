package transform

import "testing"

func TestRewriteCoalesce(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Pass-throughs
		{"no coalesce", "a + b", "a + b"},
		{"empty", "", ""},

		// Safe-path lhs uses has() wrapper
		{"safe path simple", "input.x ?? 'd'", "(has(input.x) ? coalesce(input.x, 'd') : 'd')"},
		{"safe path empty default", "input.body.payload.UUID ?? ''", "(has(input.body) && has(input.body.payload) && has(input.body.payload.UUID) ? coalesce(input.body.payload.UUID, '') : '')"},
		{"safe path list default", "input.x ?? []", "(has(input.x) ? coalesce(input.x, []) : [])"},
		{"safe path object default", "input.x ?? {}", "(has(input.x) ? coalesce(input.x, {}) : {})"},

		// Non-path lhs falls back to plain coalesce
		{"function call lhs", "trim(input.name) ?? ''", "coalesce(trim(input.name), '')"},
		{"parenthesized lhs", "(a + b) ?? c", "coalesce((a + b), c)"},

		// Chained — right-associative, has() applied per safe-path
		{"chained safe paths", "input.a ?? input.b ?? 'last'", "(has(input.a) ? coalesce(input.a, (has(input.b) ? coalesce(input.b, 'last') : 'last')) : (has(input.b) ? coalesce(input.b, 'last') : 'last'))"},

		// Recursion into nested groups
		{"inside function call", "trim(input.x ?? '')", "trim((has(input.x) ? coalesce(input.x, '') : ''))"},
		{"sibling args independent", "concat(a.x ?? 'd1', b.y ?? 'd2')", "concat((has(a.x) ? coalesce(a.x, 'd1') : 'd1'), (has(b.y) ? coalesce(b.y, 'd2') : 'd2'))"},
		{"inside ternary parenthesized", "(x ?? '') != '' ? a : b", "(coalesce(x, '')) != '' ? a : b"},

		// String literals are pass-through
		{"string contains ??", "concat('?? not real', input.x ?? 'd')", "concat('?? not real', (has(input.x) ? coalesce(input.x, 'd') : 'd'))"},
		{"double-quoted string", `"?? in string" + (a ?? b)`, `"?? in string" + (coalesce(a, b))`},

		// Bare identifiers are not safe paths — no has() wrap
		{"bare identifier", "x ?? 'd'", "coalesce(x, 'd')"},

		// Deeply nested path produces a chain of has() checks
		{"three-level path", "input.body.payload.id ?? ''", "(has(input.body) && has(input.body.payload) && has(input.body.payload.id) ? coalesce(input.body.payload.id, '') : '')"},

		// has() rejects invalid paths
		{"path with index not safe", "input[0] ?? 'd'", "coalesce(input[0], 'd')"},
		{"path with call not safe", "input.f() ?? 'd'", "coalesce(input.f(), 'd')"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RewriteCoalesce(tc.in)
			if got != tc.want {
				t.Errorf("RewriteCoalesce(%q):\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRewriteCoalesceCompiles verifies the rewriter's output passes through
// the real CEL environment without compile errors.
func TestRewriteCoalesceCompiles(t *testing.T) {
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to build transformer: %v", err)
	}
	exprs := []string{
		"input.x ?? 'default'",
		"input.x ?? input.y ?? 'fallback'",
		"trim(input.name ?? '')",
		"input.list ?? []",
		"input.obj ?? {}",
		"(input.x ?? '') != ''",
		"input.body.payload.jobId ?? ''",
	}
	for _, e := range exprs {
		t.Run(e, func(t *testing.T) {
			if _, err := tr.Compile(e); err != nil {
				t.Errorf("compile %q failed: %v", e, err)
			}
		})
	}
}

// TestRewriteCoalesceRuntime exercises the real runtime semantics: missing
// keys, present-but-null values, and present-but-empty strings should all
// fall back; present non-empty values pass through.
func TestRewriteCoalesceRuntime(t *testing.T) {
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to build transformer: %v", err)
	}
	tests := []struct {
		name  string
		expr  string
		input map[string]interface{}
		want  interface{}
	}{
		{"present non-empty", "input.name ?? 'fallback'", map[string]interface{}{"name": "ada"}, "ada"},
		{"missing key", "input.name ?? 'fallback'", map[string]interface{}{}, "fallback"},
		{"present empty", "input.name ?? 'fallback'", map[string]interface{}{"name": ""}, "fallback"},
		{"chained, second present", "input.a ?? input.b ?? 'last'", map[string]interface{}{"b": "second"}, "second"},
		{"chained, all missing", "input.a ?? input.b ?? 'last'", map[string]interface{}{}, "last"},
		{"chained, first present", "input.a ?? input.b ?? 'last'", map[string]interface{}{"a": "first", "b": "second"}, "first"},
		{"deeply nested missing", "input.body.payload.jobId ?? ''", map[string]interface{}{"body": map[string]interface{}{"payload": map[string]interface{}{}}}, ""},
		{"deeply nested intermediate missing", "input.body.payload.jobId ?? ''", map[string]interface{}{"body": map[string]interface{}{}}, ""},
		{"deeply nested all missing", "input.body.payload.jobId ?? ''", map[string]interface{}{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := tr.Compile(tc.expr)
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}
			out, _, err := prog.Eval(map[string]interface{}{"input": tc.input})
			if err != nil {
				t.Fatalf("eval failed: %v", err)
			}
			got := out.Value()
			if got != tc.want {
				t.Errorf("expr=%q input=%v: got %v (%T), want %v", tc.expr, tc.input, got, got, tc.want)
			}
		})
	}
}
