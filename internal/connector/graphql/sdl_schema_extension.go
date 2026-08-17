package graphql

import "strings"

// Federation v2 declares itself at the top of a subgraph's schema file:
//
//	extend schema
//	  @link(url: "https://specs.apollo.dev/federation/v2.0", import: ["@key"])
//
// That preamble is not optional — it is how a subgraph says which version of
// the specification it speaks, and every schema file exported by a gateway or
// written by hand for federation v2 begins with it.
//
// The AST parser behind ParseSDLComplete only understands `extend type`, so it
// refused the whole file at the second line: a service whose schema file was a
// real subgraph schema would not start, reporting a syntax error at a line the
// author had copied from the specification. The same preamble is what Mycel
// generates for its own _service reply, so it was emitting SDL it could not
// read back.
//
// The declaration is stripped rather than interpreted because nothing here
// needs it: which federation version is in force comes from the connector's own
// configuration, and the directives it imports are recognised by name wherever
// they are used.

// stripSchemaExtensions removes the parts of a schema file the AST parser
// cannot represent, leaving the rest of the document untouched.
//
// Two things go. A schema extension goes whole, because the parser has no node
// for one. The `repeatable` keyword on a directive definition goes on its own —
// the definition around it parses fine — and the names it was written on come
// back as the second result, so the fact survives the removal. Federation's own
// @key and @tag are declared that way, so a fully exported subgraph schema
// carries several.
//
// A `schema { query: Query }` block keeps its body, since that names the root
// types and the parser understands it; only the directives beside it go.
func stripSchemaExtensions(sdl string) (string, map[string]bool) {
	repeatable := map[string]bool{}
	if !strings.Contains(sdl, "schema") && !strings.Contains(sdl, "repeatable") {
		return sdl, repeatable
	}

	var (
		out strings.Builder
		s   = &sdlScanner{src: sdl}
	)

	for !s.done() {
		if trivia := s.skipTrivia(); trivia != "" {
			out.WriteString(trivia)
			continue
		}

		word := s.readWord()
		if word == "" {
			// Anything that is not an identifier is copied through, but the
			// braces are counted so a field named "schema" inside a type body is
			// never mistaken for a definition.
			out.WriteString(s.readAny())
			continue
		}

		// `extend schema` — an extension the parser cannot represent at all, so
		// the whole definition goes, body included.
		if word == "extend" && s.depth == 0 {
			save := s.pos
			gap := s.skipTrivia()
			if next := s.readWord(); next == "schema" {
				s.skipDirectives()
				s.readBraceBlock()
				continue
			}
			s.pos = save
			out.WriteString(word)
			_ = gap
			continue
		}

		// `schema @directive { query: Query }` — the body names the root types
		// and the parser understands it; only the directives beside it go.
		if word == "schema" && s.depth == 0 {
			s.skipDirectives()
			out.WriteString(word)
			out.WriteString(s.readBraceBlock())
			continue
		}

		// `directive @key(...) repeatable on OBJECT` — copy the definition
		// through, dropping only the keyword and remembering it was there.
		if word == "directive" && s.depth == 0 {
			out.WriteString(word)
			if name := s.copyDirectiveHeader(&out); name != "" {
				repeatable[name] = true
			}
			continue
		}

		out.WriteString(word)
	}

	return out.String(), repeatable
}

// sdlScanner walks SDL text while keeping track of the things that must not be
// read as source: strings, block strings, comments, and nesting.
type sdlScanner struct {
	src   string
	pos   int
	depth int
}

func (s *sdlScanner) done() bool { return s.pos >= len(s.src) }

// skipTrivia consumes whitespace, comments and strings, returning them so the
// caller can copy them through unchanged.
func (s *sdlScanner) skipTrivia() string {
	start := s.pos
	for !s.done() {
		switch c := s.src[s.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			s.pos++
		case c == '#':
			for !s.done() && s.src[s.pos] != '\n' {
				s.pos++
			}
		case strings.HasPrefix(s.src[s.pos:], `"""`):
			s.pos += 3
			if i := strings.Index(s.src[s.pos:], `"""`); i >= 0 {
				s.pos += i + 3
			} else {
				s.pos = len(s.src)
			}
		case c == '"':
			s.readString()
		default:
			return s.src[start:s.pos]
		}
	}
	return s.src[start:s.pos]
}

func (s *sdlScanner) readString() {
	s.pos++ // opening quote
	for !s.done() {
		switch s.src[s.pos] {
		case '\\':
			s.pos += 2
		case '"':
			s.pos++
			return
		default:
			s.pos++
		}
	}
}

func isWordByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

func (s *sdlScanner) readWord() string {
	start := s.pos
	for !s.done() && isWordByte(s.src[s.pos]) {
		s.pos++
	}
	return s.src[start:s.pos]
}

// readAny consumes a single byte, tracking brace depth so that identifiers
// inside a type body are never treated as definitions.
func (s *sdlScanner) readAny() string {
	c := s.src[s.pos]
	switch c {
	case '{':
		s.depth++
	case '}':
		if s.depth > 0 {
			s.depth--
		}
	}
	s.pos++
	return string(c)
}

// skipDirectives consumes a run of directives, including their arguments,
// without copying them anywhere.
func (s *sdlScanner) skipDirectives() {
	for {
		save := s.pos
		s.skipTrivia()
		if s.done() || s.src[s.pos] != '@' {
			s.pos = save
			return
		}
		s.pos++ // @
		s.readWord()
		s.skipTrivia()
		if !s.done() && s.src[s.pos] == '(' {
			s.skipBalanced('(', ')')
		}
	}
}

// copyDirectiveHeader copies a directive definition's name and arguments to
// out, dropping a `repeatable` keyword if one follows them. It returns the
// directive's name when the keyword was there, and the empty string otherwise.
func (s *sdlScanner) copyDirectiveHeader(out *strings.Builder) string {
	out.WriteString(s.skipTrivia())
	if s.done() || s.src[s.pos] != '@' {
		return ""
	}
	out.WriteString("@")
	s.pos++
	name := s.readWord()
	out.WriteString(name)

	out.WriteString(s.skipTrivia())
	if !s.done() && s.src[s.pos] == '(' {
		start := s.pos
		s.skipBalanced('(', ')')
		out.WriteString(s.src[start:s.pos])
	}

	save := s.pos
	gap := s.skipTrivia()
	if s.readWord() == "repeatable" {
		return name
	}
	s.pos = save
	_ = gap
	return ""
}

// readBraceBlock consumes a balanced brace block if one follows, returning it
// with the whitespace that preceded it.
func (s *sdlScanner) readBraceBlock() string {
	save := s.pos
	lead := s.skipTrivia()
	if s.done() || s.src[s.pos] != '{' {
		s.pos = save
		return ""
	}
	start := s.pos
	s.skipBalanced('{', '}')
	return lead + s.src[start:s.pos]
}

func (s *sdlScanner) skipBalanced(open, close byte) {
	level := 0
	for !s.done() {
		switch c := s.src[s.pos]; {
		case strings.HasPrefix(s.src[s.pos:], `"""`), c == '"':
			s.skipTrivia()
			continue
		case c == '#':
			for !s.done() && s.src[s.pos] != '\n' {
				s.pos++
			}
			continue
		case c == open:
			level++
		case c == close:
			level--
			if level == 0 {
				s.pos++
				return
			}
		}
		s.pos++
	}
}
