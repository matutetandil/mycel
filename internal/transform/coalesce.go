package transform

import "strings"

// RewriteCoalesce expands the `??` null-coalescing operator into CEL syntax
// the runtime can evaluate. CEL itself has no `??` operator, but Mycel's
// documentation and idiomatic flows lean on it heavily, so we preprocess
// expressions before handing them to the CEL compiler.
//
// Two cases:
//
//   - When the left-hand side is a simple dotted path (e.g.
//     `input.body.payload.jobId`), the rewrite uses CEL's `has()` macro so
//     missing intermediate fields fall back to the default rather than
//     raising a "no such key" error. The full form is:
//
//     has(path) ? coalesce(path, default) : default
//
//     The inner `coalesce()` still catches present-but-null and present-but-
//     empty-string values, matching existing behavior.
//
//   - For any other left-hand side (function calls, parenthesized
//     expressions, arithmetic), the rewrite is just `coalesce(lhs, rhs)`.
//     CEL evaluates lhs eagerly in this form, so callers must guarantee it
//     does not error. This matches what `coalesce()` does today.
//
// Chaining is right-associative: `a ?? b ?? c` becomes
// `coalesce(a, coalesce(b, c))` (with `has()` wrappers as appropriate).
//
// `??` binds tighter than the ternary `?:` and stops at a map-literal key,
// as in JS/C#: `c ? a ?? d : b` is `c ? (a ?? d) : b`, and
// `{'k': input.v ?? ""}` defaults the value, not the entry. To coalesce a
// whole ternary, parenthesize it: `(c ? a : b) ?? d`.
func RewriteCoalesce(expr string) string {
	if !strings.Contains(expr, "??") {
		return expr
	}
	return processCoalesce(expr)
}

// processCoalesce recurses into nested groups (parens, brackets, braces)
// and rewrites their contents. After the recursion it folds any remaining
// top-level `??` chains, splitting at top-level commas first so a `??`
// inside one function argument does not pull in a sibling argument.
func processCoalesce(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case '\'', '"':
			end := skipString(s, i)
			out.WriteString(s[i:end])
			i = end
		case '(', '[', '{':
			close := matchingClose(c)
			j := findClose(s, i, c, close)
			if j < 0 {
				out.WriteString(s[i:])
				return foldByCommas(out.String())
			}
			inner := processCoalesce(s[i+1 : j])
			out.WriteByte(c)
			out.WriteString(inner)
			out.WriteByte(close)
			i = j + 1
		default:
			out.WriteByte(c)
			i++
		}
	}
	return foldByCommas(out.String())
}

// foldByCommas splits the (already group-resolved) string at top-level
// commas, then folds each segment's `??` chain independently.
func foldByCommas(s string) string {
	if !strings.Contains(s, "??") {
		return s
	}
	segments := splitTopLevel(s, ',')
	for i, seg := range segments {
		segments[i] = foldOperands(seg)
	}
	return strings.Join(segments, ",")
}

// foldOperands takes a comma-free segment and folds the `??` chain of each
// operand in it separately. An operand ends where a map-literal key or a
// ternary branch does: at a top-level `:` or a lone `?`.
//
// The chain used to be folded across the whole segment, so the operand of a
// `??` was assumed to start at the segment's beginning. Inside a map literal
// the segment is `'open': input.open ?? ""`, and the key was swallowed into
// the call — `coalesce('open': input.open, "")` — which does not parse. The
// failure was at request time, since `mycel validate` does not compile CEL,
// and it read as the map literal being unsupported. Splitting at the same
// boundaries also gives `??` the precedence it has in JavaScript and C#,
// tighter than `?:`, so `c ? a ?? d : b` becomes `c ? coalesce(a, d) : b`
// rather than a call spanning both branches.
func foldOperands(segment string) string {
	if !strings.Contains(segment, "??") {
		return segment
	}
	var out strings.Builder
	out.Grow(len(segment))
	start := 0
	i := 0
	for i < len(segment) {
		c := segment[i]
		if c == '\'' || c == '"' {
			i = skipString(segment, i)
			continue
		}
		if close := matchingClose(c); close != 0 {
			if j := findClose(segment, i, c, close); j >= 0 {
				i = j + 1
				continue
			}
		}
		if c == '?' && i+1 < len(segment) && segment[i+1] == '?' {
			i += 2
			continue
		}
		if c == ':' || c == '?' {
			out.WriteString(foldCoalesceChain(segment[start:i]))
			out.WriteByte(c)
			start = i + 1
		}
		i++
	}
	out.WriteString(foldCoalesceChain(segment[start:]))
	return out.String()
}

// foldCoalesceChain takes a single operand — no top-level commas, map keys
// or ternary branches — splits it at top-level `??` occurrences and folds
// right-associatively.
func foldCoalesceChain(segment string) string {
	positions := findTopLevelCoalesce(segment)
	if len(positions) == 0 {
		return segment
	}

	// Capture leading and trailing whitespace so we don't change formatting
	// outside the operator itself.
	leading := segment[:len(segment)-len(strings.TrimLeft(segment, " \t"))]
	trailing := segment[len(strings.TrimRight(segment, " \t")):]
	core := strings.TrimSpace(segment)

	// Recompute positions on the trimmed string.
	positions = findTopLevelCoalesce(core)
	if len(positions) == 0 {
		return segment
	}

	parts := make([]string, 0, len(positions)+1)
	start := 0
	for _, p := range positions {
		parts = append(parts, strings.TrimSpace(core[start:p]))
		start = p + 2
	}
	parts = append(parts, strings.TrimSpace(core[start:]))

	result := parts[len(parts)-1]
	for i := len(parts) - 2; i >= 0; i-- {
		result = emitCoalesce(parts[i], result)
	}
	return leading + result + trailing
}

// emitCoalesce wraps `lhs ?? rhs`. When lhs is a simple dotted path the
// emit guards every intermediate field with has() so a missing parent does
// not raise "no such key"; otherwise it just calls coalesce().
//
// The emitted form for a path `a.b.c` is:
//
//	(has(a.b) && has(a.b.c) ? coalesce(a.b.c, rhs) : rhs)
//
// CEL's has() macro is shallow — it requires the parent to exist — so each
// level needs its own check. && short-circuits, so once a level is missing
// the chain returns false and falls back to the default.
func emitCoalesce(lhs, rhs string) string {
	if isSafePath(lhs) {
		check := buildHasChain(lhs)
		return "(" + check + " ? coalesce(" + lhs + ", " + rhs + ") : " + rhs + ")"
	}
	return "coalesce(" + lhs + ", " + rhs + ")"
}

// buildHasChain produces a chain of has() checks covering every parent
// segment of path. For input "a.b.c.d" it emits
// "has(a.b) && has(a.b.c) && has(a.b.c.d)". The first identifier is a CEL
// variable and is presumed to exist in the environment.
func buildHasChain(path string) string {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		// isSafePath requires at least one dot, so this branch should not
		// normally fire — return a constant the CEL parser accepts anyway.
		return "true"
	}
	checks := make([]string, 0, len(parts)-1)
	for i := 2; i <= len(parts); i++ {
		checks = append(checks, "has("+strings.Join(parts[:i], ".")+")")
	}
	return strings.Join(checks, " && ")
}

// isSafePath returns true if expr is a dotted identifier path of the form
// `var.field(.field)+` — what CEL's has() macro can wrap. Bare identifiers,
// function calls, indexing, parens and operators all disqualify the
// expression.
func isSafePath(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	hasDot := false
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if i == 0 {
			if !isIdentStart(c) {
				return false
			}
			continue
		}
		if c == '.' {
			// A trailing dot or `..` is invalid.
			if i == len(expr)-1 || expr[i+1] == '.' {
				return false
			}
			hasDot = true
			continue
		}
		if !isIdentChar(c) {
			return false
		}
	}
	return hasDot
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// splitTopLevel splits s at occurrences of sep that are at the top level
// (not inside string literals — nested groups are already gone by the time
// we call this).
func splitTopLevel(s string, sep byte) []string {
	var segments []string
	start := 0
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\'' || c == '"' {
			i = skipString(s, i)
			continue
		}
		// A separator inside brackets is not a top-level one. Without this,
		// the comma in `join(input.tags, ",")` split the call in half, and
		// every `??` sharing an expression with a two-argument call was
		// rewritten into CEL that does not compile — `coalesce(input.x,
		// join(input.tags) : join(input.tags), ",")`.
		if close := matchingClose(c); close != 0 {
			if j := findClose(s, i, c, close); j >= 0 {
				i = j + 1
				continue
			}
		}
		if c == sep {
			segments = append(segments, s[start:i])
			start = i + 1
		}
		i++
	}
	segments = append(segments, s[start:])
	return segments
}

func matchingClose(open byte) byte {
	switch open {
	case '(':
		return ')'
	case '[':
		return ']'
	case '{':
		return '}'
	}
	return 0
}

func findClose(s string, openPos int, open, close byte) int {
	depth := 1
	i := openPos + 1
	for i < len(s) {
		c := s[i]
		if c == '\'' || c == '"' {
			i = skipString(s, i)
			continue
		}
		if c == open {
			depth++
		} else if c == close {
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

func skipString(s string, start int) int {
	quote := s[start]
	i := start + 1
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if s[i] == quote {
			return i + 1
		}
		i++
	}
	return len(s)
}

func findTopLevelCoalesce(s string) []int {
	var positions []int
	i := 0
	for i < len(s)-1 {
		c := s[i]
		if c == '\'' || c == '"' {
			i = skipString(s, i)
			continue
		}
		// Likewise: what is inside brackets has already been folded by the
		// recursion, and splitting an argument list at an operator that
		// belongs to one of its arguments produces nonsense.
		if close := matchingClose(c); close != 0 {
			if j := findClose(s, i, c, close); j >= 0 {
				i = j + 1
				continue
			}
		}
		if c == '?' && s[i+1] == '?' {
			positions = append(positions, i)
			i += 2
			continue
		}
		i++
	}
	return positions
}
