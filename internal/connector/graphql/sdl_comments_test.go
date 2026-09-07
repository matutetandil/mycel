package graphql

import (
	"strings"
	"testing"
)

// A `#` comment carries no schema meaning, yet a non-ASCII character inside
// one — an em dash, an accented letter, a warning sign — made the whole file
// fail to parse, and the syntax error pointed inside the comment. The lexer
// the SDL is handed to does not read every code point the specification
// allows in a comment, so from that character on the rest of the comment
// block was read as SDL tokens. Whether the file survived depended on what
// followed the comments: a `"""description"""` before the first type happened
// to hide it, which is why every schema file written so far had loaded.

func TestANonASCIICharacterInACommentIsNotASyntaxError(t *testing.T) {
	for name, sdl := range map[string]string{
		"em dash":  "# Subgraph schema — Apollo Federation v2.\n\ntype Item {\n  text: String\n}\n\ntype Query { item: Item }\n",
		"accent":   "# Esquema del catálogo.\ntype Query { hello: String }\n",
		"symbol":   "type Query {\n  # ⚠ deprecated\n  hello: String\n}\n",
		"trailing": "type Query { hello: String } # sí\n",
	} {
		schema, err := ParseSDLComplete(sdl)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if schema.Query == nil || schema.Query.Fields["hello"] == nil && schema.Query.Fields["item"] == nil {
			t.Errorf("%s: Query type not parsed", name)
		}
	}
}

func TestDroppingCommentsKeepsLineNumbersAndStrings(t *testing.T) {
	// The comments go, but the lines they were on stay, so an error in the
	// schema itself is still reported on the line the author sees in the file.
	sdl := "# header — one\n# header — two\ntype Query {\n  hello: String\n  broken\n}\n"
	_, err := ParseSDLComplete(sdl)
	if err == nil {
		t.Fatal("a field without a type parsed")
	}
	if !strings.Contains(err.Error(), "(6:1)") {
		t.Errorf("error should point at line 6 of the original file, got: %v", err)
	}

	// A `#` inside a string or a description is text, not a comment.
	sdl = "type Query {\n  \"\"\"The # sign — kept\"\"\"\n  hello(tag: String = \"#1 — default\"): String\n}\n"
	schema, err := ParseSDLComplete(sdl)
	if err != nil {
		t.Fatal(err)
	}
	field := schema.Query.Fields["hello"]
	if field.Description != "The # sign — kept" {
		t.Errorf("description was altered: %q", field.Description)
	}
	if got := field.Args["tag"].DefaultValue; got != "#1 — default" {
		t.Errorf("default value was altered: %#v", got)
	}
}
