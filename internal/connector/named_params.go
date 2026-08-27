package connector

import (
	"fmt"
	"strings"
)

// Named parameter binding for SQL destinations.
//
// A query written with :name placeholders has to be rewritten into whatever
// the driver accepts — `?` for MySQL and SQLite, `$1`, `$2` for Postgres —
// with the values collected in the order the placeholders appear.
//
// Doing that means telling code apart from the parts of a statement that only
// look like code. Each driver used to carry its own copy of this scanner and
// each copy knew about exactly one of them, single-quoted strings, so a
// comment was read as if it were code:
//
//	-- the item's parent
//	SELECT id FROM t WHERE sku = :sku
//
// The apostrophe in "item's" opened a string literal that never closed, and
// every placeholder after it was copied through as literal text — the query
// reached the driver still saying `:sku`, with no arguments bound. It failed
// the same way whether or not the comment was the point, and nothing in the
// message mentioned the comment.
//
// It went wrong in the other direction too: `-- ratio:sku` had its `:sku`
// replaced with a placeholder and an argument appended, so a comment consumed
// one of the statement's arguments and the rest shifted by one.
//
// So the scanner knows the lexical structure that can hide a colon or a quote:
// line comments, block comments, string literals and quoted identifiers. It is
// not a SQL parser and does not need to be — it only has to know which
// stretches of a statement to copy through untouched.

// SQLDialect carries the lexical differences between the drivers. Everything
// else about the scan is shared.
type SQLDialect struct {
	// Placeholder renders the driver's placeholder for the nth argument,
	// counting from 1.
	Placeholder func(n int) string

	// HashComments enables `#` to end of line (MySQL).
	HashComments bool

	// DashDashNeedsSpace requires whitespace after `--` for it to start a
	// comment (MySQL). Without it `a--b` is subtraction of a negated value,
	// and reading it as a comment would swallow the rest of the statement —
	// the failure this scanner exists to prevent, reintroduced elsewhere.
	DashDashNeedsSpace bool

	// BackslashEscapes treats `\x` inside a string literal as an escaped
	// character (MySQL, unless NO_BACKSLASH_ESCAPES). Without it `'it\'s'`
	// ends at the middle quote and the rest of the statement is scanned as
	// if it were inside a string.
	BackslashEscapes bool

	// NestedBlockComments lets `/* a /* b */ c */` close only at the last
	// `*/` (Postgres). Elsewhere the first one closes it.
	NestedBlockComments bool

	// IdentifierQuotes are the characters that open a quoted identifier,
	// each paired with what closes it.
	IdentifierQuotes map[byte]byte
}

// MySQLDialect: `?` placeholders, `#` comments, backslash escapes, backtick
// identifiers, and `--` only when followed by whitespace.
var MySQLDialect = SQLDialect{
	Placeholder:        func(int) string { return "?" },
	HashComments:       true,
	DashDashNeedsSpace: true,
	BackslashEscapes:   true,
	IdentifierQuotes:   map[byte]byte{'`': '`', '"': '"'},
}

// PostgresDialect: `$N` placeholders, nested block comments, double-quoted
// identifiers.
var PostgresDialect = SQLDialect{
	Placeholder:         func(n int) string { return fmt.Sprintf("$%d", n) },
	NestedBlockComments: true,
	IdentifierQuotes:    map[byte]byte{'"': '"'},
}

// SQLiteDialect: `?` placeholders, and the three identifier quotings SQLite
// accepts.
var SQLiteDialect = SQLDialect{
	Placeholder:      func(int) string { return "?" },
	IdentifierQuotes: map[byte]byte{'"': '"', '`': '`', '[': ']'},
}

// GenericSQLDialect is for callers that only want to know which placeholders a
// statement carries and have no driver in hand. It takes the conservative
// reading of every difference: `--` always comments, `#` never does, block
// comments do not nest, and the quotings all three drivers agree on.
var GenericSQLDialect = SQLDialect{
	Placeholder:      func(int) string { return "?" },
	IdentifierQuotes: map[byte]byte{'"': '"', '`': '`'},
}

// BindNamedParams rewrites :name placeholders into the dialect's own and
// returns the arguments in the order they appear.
//
// A placeholder with no matching entry in params is left exactly as written.
// That is what makes `::int` work — Postgres casts are not parameters — and it
// leaves a genuine typo visible in the statement the driver rejects rather
// than turning it into a silently missing argument.
func BindNamedParams(sql string, params map[string]interface{}, d SQLDialect) (string, []interface{}) {
	if len(params) == 0 {
		return sql, nil
	}

	var out strings.Builder
	out.Grow(len(sql))
	var args []interface{}
	argNum := 1

	s := []byte(sql)
	n := len(s)
	i := 0

	for i < n {
		if skipped := scanSkippable(s, i, d); skipped > i {
			out.Write(s[i:skipped])
			i = skipped
			continue
		}

		// A cast is two colons, not a parameter named after the second one.
		if s[i] == ':' && i+1 < n && s[i+1] == ':' {
			out.WriteString("::")
			i += 2
			continue
		}

		if s[i] == ':' {
			j := i + 1
			for j < n && isNamedParamChar(s[j]) {
				j++
			}
			if j > i+1 {
				name := string(s[i+1 : j])
				if val, ok := params[name]; ok {
					out.WriteString(d.Placeholder(argNum))
					args = append(args, val)
					argNum++
				} else {
					out.Write(s[i:j])
				}
				i = j
				continue
			}
		}

		out.WriteByte(s[i])
		i++
	}

	return out.String(), args
}

// NamedParamsIn returns the :name placeholders a statement carries, in order
// and without repeats, ignoring the ones inside comments, string literals and
// quoted identifiers.
func NamedParamsIn(sql string, d SQLDialect) []string {
	if sql == "" {
		return nil
	}

	var out []string
	seen := map[string]bool{}

	s := []byte(sql)
	n := len(s)
	i := 0

	for i < n {
		if skipped := scanSkippable(s, i, d); skipped > i {
			i = skipped
			continue
		}
		if s[i] == ':' && i+1 < n && s[i+1] == ':' {
			i += 2
			continue
		}
		if s[i] == ':' {
			j := i + 1
			for j < n && isNamedParamChar(s[j]) {
				j++
			}
			if j > i+1 {
				if name := string(s[i+1 : j]); !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
				i = j
				continue
			}
		}
		i++
	}

	return out
}

// scanSkippable reports the index just past the comment, string literal or
// quoted identifier starting at i, or i itself when nothing starts there.
// Everything between is copied through verbatim and never scanned for
// placeholders.
func scanSkippable(s []byte, i int, d SQLDialect) int {
	n := len(s)

	// Line comments run to the newline, which is left for the caller so the
	// statement keeps its shape.
	lineComment := false
	switch {
	case s[i] == '-' && i+1 < n && s[i+1] == '-':
		// MySQL wants whitespace after the dashes; the others do not.
		lineComment = !d.DashDashNeedsSpace ||
			i+2 >= n || s[i+2] == ' ' || s[i+2] == '\t' || s[i+2] == '\n' || s[i+2] == '\r'
	case d.HashComments && s[i] == '#':
		lineComment = true
	}
	if lineComment {
		j := i
		for j < n && s[j] != '\n' {
			j++
		}
		return j
	}

	// Block comments. An unterminated one runs to the end of the statement,
	// which is what the driver would do with it too.
	if s[i] == '/' && i+1 < n && s[i+1] == '*' {
		depth := 1
		j := i + 2
		for j < n {
			if s[j] == '*' && j+1 < n && s[j+1] == '/' {
				depth--
				j += 2
				if depth == 0 {
					return j
				}
				continue
			}
			if d.NestedBlockComments && s[j] == '/' && j+1 < n && s[j+1] == '*' {
				depth++
				j += 2
				continue
			}
			j++
		}
		return n
	}

	// String literals. A doubled quote is an escaped one and does not close
	// the literal; so is a backslash-escaped quote where the dialect allows
	// backslash escapes.
	if s[i] == '\'' {
		j := i + 1
		for j < n {
			if d.BackslashEscapes && s[j] == '\\' {
				j += 2
				continue
			}
			if s[j] == '\'' {
				if j+1 < n && s[j+1] == '\'' {
					j += 2
					continue
				}
				return j + 1
			}
			j++
		}
		return n
	}

	// Quoted identifiers. A doubled closing character is an escaped one, the
	// same as in a string; brackets have no escape and simply end at the
	// first `]`.
	if closer, ok := d.IdentifierQuotes[s[i]]; ok {
		j := i + 1
		for j < n {
			if s[j] == closer {
				if closer != ']' && j+1 < n && s[j+1] == closer {
					j += 2
					continue
				}
				return j + 1
			}
			j++
		}
		return n
	}

	return i
}

// isNamedParamChar reports whether a byte can appear in a :name placeholder.
func isNamedParamChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}
