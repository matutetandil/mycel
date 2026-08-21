package transform

import "strings"

// RewriteDefault makes `default(input.field, fallback)` survive a field that
// is not there.
//
// `default()` is the documented way to give an optional field a value — the
// quick start reaches for it at the moment it introduces an optional field —
// and it did not work for the case it exists for. CEL evaluates a function's
// arguments before calling it, so `default(input.description, ”)` on a
// request that carries no description fails with "no such key: description"
// before `default` is ever reached. A reader following the page got a 500 from
// the very request the page is demonstrating.
//
// The fix is the one `??` already uses: when the first argument is a plain
// dotted path, guard it with has() and hand back the fallback when it is
// missing.
//
//	default(a.b, c)  →  ((has(a.b)) ? default(a.b, c) : c)
//
// Anything else — a function call, an index, arithmetic — is left alone, since
// has() cannot wrap it and the caller has to guarantee it evaluates.
func RewriteDefault(expr string) string {
	if !strings.Contains(expr, "default(") {
		return expr
	}

	var out strings.Builder
	i := 0
	for i < len(expr) {
		// Strings are copied through untouched, so `"default("` in a literal
		// is not a call.
		if expr[i] == '"' || expr[i] == '\'' {
			end := skipString(expr, i)
			out.WriteString(expr[i:end])
			i = end
			continue
		}

		if !strings.HasPrefix(expr[i:], "default(") || (i > 0 && isIdentChar(expr[i-1])) {
			out.WriteByte(expr[i])
			i++
			continue
		}

		open := i + len("default")
		close := findClose(expr, open, '(', ')')
		if close < 0 {
			out.WriteString(expr[i:])
			break
		}

		args := splitTopLevel(expr[open+1:close], ',')
		if len(args) != 2 {
			// Not the two-argument form this rewrites; leave it as written.
			out.WriteString(expr[i : close+1])
			i = close + 1
			continue
		}

		path := strings.TrimSpace(args[0])
		fallback := strings.TrimSpace(RewriteDefault(args[1]))
		if !isSafePath(path) {
			out.WriteString("default(" + strings.TrimSpace(RewriteDefault(args[0])) + ", " + fallback + ")")
			i = close + 1
			continue
		}

		out.WriteString("((" + buildHasChain(path) + ") ? default(" + path + ", " + fallback + ") : " + fallback + ")")
		i = close + 1
	}

	return out.String()
}
