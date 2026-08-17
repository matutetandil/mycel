package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// The schema exists so that tooling can generate configuration: completions,
// `mycel add`, exported documentation. That only works if everything the schema
// describes is something the parser accepts. When the two drift, the generator
// produces files that do not parse — and the drift is invisible until someone
// runs the generated file.
//
// This test closes the loop mechanically: it renders a document exercising
// every attribute and every nested block a schema declares, then feeds it to
// the real parser. An attribute the parser does not know is an "Unsupported
// argument" diagnostic, and the test fails naming it.
//
// It is the reverse of the drift that bit on_drop, where the runtime knew about
// a value the schema did not. Both directions now have a test.
func TestSchemaParity(t *testing.T) {
	// Every root block, not a chosen few: the schema is the single source of
	// truth for the whole language, and a block left out of this loop is one
	// where it can quietly stop being true.
	for _, blk := range schema.BuiltinRootSchemas() {
		t.Run(blk.Type, func(t *testing.T) {
			assertSchemaParses(t, blk, "example")
		})
	}
}

// assertSchemaParses renders the block from its schema and parses it.
func assertSchemaParses(t *testing.T, blk schema.Block, name string) {
	t.Helper()

	labels := make([]string, blk.Labels)
	for i := range labels {
		labels[i] = name
	}
	doc := renderBlockFromSchema(blk, labels, 0, true)

	// Not mustParse: the failure is the point, and it should name the block
	// and show what was rendered.
	cfg, err := tryParse(t, doc)
	if err != nil {
		t.Fatalf("the %s schema describes something the parser rejects: %v\n\n%s",
			blk.Type, err, doc)
	}
	if cfg == nil {
		t.Fatalf("%s parsed to nothing", blk.Type)
	}
}

// renderBlockFromSchema turns a schema block into HCL that exercises all of it.
//
// Every attribute gets a value of the right type, and every child block is
// nested one level deep. Depth is capped because schemas are recursive — a
// transform can hold a transform — and the goal is coverage of each declared
// name, not of every path through them.
func renderBlockFromSchema(blk schema.Block, labels []string, depth int, withChildren bool) string {
	var b strings.Builder

	b.WriteString(blockHeader(blk, labels) + " {\n")

	indent := strings.Repeat("  ", depth+1)

	// Sort for a stable rendering, so a failure reads the same way twice.
	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })

	for _, a := range attrs {
		if skipAttrForParity(blk.Type, a.Name) {
			continue
		}
		// In the children variant, an attribute a child block excludes is left
		// out; the attribute-only variant covers it.
		if withChildren && excludedByAChild(blk, a.Name) {
			continue
		}
		fmt.Fprintf(&b, "%s%s = %s\n", indent, a.Name, sampleValue(a))
	}

	// An Open block's content is named by the author, so the schema lists none
	// — but some require at least one, like a dedupe fingerprint. Rendering a
	// sample keeps those blocks representable.
	if blk.Open && len(blk.Attrs) == 0 {
		fmt.Fprintf(&b, "%sexample = %q\n", indent, "input.id")
	}

	if withChildren && depth < 5 {
		for _, child := range blk.Children {
			childLabels := make([]string, child.Labels)
			for i := range childLabels {
				childLabels[i] = fmt.Sprintf("%s_%d", child.Type, i)
			}
			nested := renderBlockFromSchema(child, childLabels, depth+1, true)
			for _, line := range strings.Split(strings.TrimRight(nested, "\n"), "\n") {
				b.WriteString(indent + line + "\n")
			}

			// A child whose presence excludes some of its parent's attributes
			// leaves those attributes untested, so a second copy of the same
			// block carries them, with no children of its own. A flow may
			// declare several destinations, so two `to` blocks is valid config
			// rather than a trick: one writes via a transaction, the other
			// via query.
			if childExcludesAttrs(child) {
				alt := renderBlockFromSchema(child, childLabels, depth+1, false)
				for _, line := range strings.Split(strings.TrimRight(alt, "\n"), "\n") {
					b.WriteString(indent + line + "\n")
				}
			}
		}
	}

	b.WriteString(strings.Repeat("  ", depth) + "}\n")
	return b.String()
}

// blockHeader renders the block's opening line.
//
// Most blocks are `type "label"`, but a few have a shape of their own — an
// each block reads `each "<var>" in "<listExpr>"`, three labels where the
// middle one is the keyword `in`. The schema records the count; the form is
// documented in its Doc, so it is spelled out here.
func blockHeader(blk schema.Block, labels []string) string {
	if blk.Type == "each" && blk.Labels == 3 {
		return `each "item" in "input.items"`
	}
	header := blk.Type
	for _, l := range labels {
		header += fmt.Sprintf(" %q", l)
	}
	return header
}

// sampleValue produces a literal of the attribute's declared type. A declared
// enum uses its first value, so a schema that lists a value the parser rejects
// fails here rather than in production.
func sampleValue(a schema.Attr) string {
	if len(a.Values) > 0 {
		return fmt.Sprintf("%q", a.Values[0])
	}
	switch a.Type {
	case schema.TypeNumber:
		// Distinct per attribute, because some numbers may not be equal to
		// each other: a workflow api on the admin port is refused, and every
		// number being 1 turned that rule into a failure of this test.
		return fmt.Sprintf("%d", 1+len(a.Name))
	case schema.TypeBool:
		return "true"
	case schema.TypeList:
		return `["a"]`
	case schema.TypeMap:
		return `{ key = "value" }`
	case schema.TypeDuration:
		return `"5s"`
	default:
		return `"value"`
	}
}

// skipAttrForParity excludes attributes that are valid on their own but
// contradict another attribute rendered alongside them. Each exclusion is a
// real mutual exclusion in the language, not a parser shortcoming.
func skipAttrForParity(blockType, attr string) bool {
	// `use` points at a named block declared elsewhere; rendering it next to
	// inline attributes asks the parser to resolve a reference this document
	// does not contain.
	if attr == "use" {
		return true
	}

	// An action calls a connector or a flow, never both — the parser says so
	// by name. Rendering every attribute at once would ask for both, so one is
	// dropped and the other still gets exercised.
	if blockType == "action" && attr == "flow" {
		return true
	}

	return false
}

// excludedByAChild reports whether one of the block's children forbids this
// attribute. The exclusions are the language's, and each is stated in the
// child's own documentation.
func excludedByAChild(blk schema.Block, attr string) bool {
	for _, c := range blk.Children {
		if c.Type != "transaction" {
			continue
		}
		// A transaction is the write; there is nothing left for these to say.
		switch attr {
		case "query", "target", "operation", "envelope":
			return true
		}
	}
	return false
}

// childExcludesAttrs reports whether rendering this block will have dropped
// attributes its own children forbid.
func childExcludesAttrs(blk schema.Block) bool {
	for _, a := range blk.Attrs {
		if excludedByAChild(blk, a.Name) {
			return true
		}
	}
	return false
}

// tryParse parses without failing the test, so the caller can report the
// diagnostic against the document that produced it.
func tryParse(t *testing.T, hcl string) (cfg *Configuration, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return parseString(t, hcl)
}

// parseString parses HCL text through the real parser, returning the error
// instead of failing.
func parseString(t *testing.T, hcl string) (*Configuration, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.mycel")
	if err := os.WriteFile(path, []byte(hcl), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return NewHCLParser().ParseFile(context.Background(), path)
}

// tryParseFiles parses a directory of files through the real loader, which is
// where Merge runs. Parsing a single file bypasses it entirely, so a field
// dropped by the merge looks fine to every single-file test.
func tryParseFiles(t *testing.T, files map[string]string) (*Configuration, error) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return NewHCLParser().Parse(context.Background(), dir)
}
