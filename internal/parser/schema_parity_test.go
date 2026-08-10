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
	for _, blk := range []schema.Block{
		schema.SagaSchema(),
		schema.StateMachineSchema(),
		schema.ValidatorSchema(),
		schema.TransformSchema(),
	} {
		t.Run(blk.Type, func(t *testing.T) {
			assertSchemaParses(t, blk, "example")
		})
	}
}

// assertSchemaParses renders the block from its schema and parses it.
func assertSchemaParses(t *testing.T, blk schema.Block, name string) {
	t.Helper()

	doc := renderBlockFromSchema(blk, []string{name}, 0)
	t.Logf("rendered from schema:\n%s", doc)

	// Not mustParse: the failure is the point, and it should name the block.
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
func renderBlockFromSchema(blk schema.Block, labels []string, depth int) string {
	var b strings.Builder

	header := blk.Type
	for _, l := range labels {
		header += fmt.Sprintf(" %q", l)
	}
	b.WriteString(header + " {\n")

	indent := strings.Repeat("  ", depth+1)

	// Sort for a stable rendering, so a failure reads the same way twice.
	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })

	for _, a := range attrs {
		if skipAttrForParity(blk.Type, a.Name) {
			continue
		}
		fmt.Fprintf(&b, "%s%s = %s\n", indent, a.Name, sampleValue(a))
	}

	if depth < 3 {
		for _, child := range blk.Children {
			childLabels := make([]string, child.Labels)
			for i := range childLabels {
				childLabels[i] = fmt.Sprintf("%s_%d", child.Type, i)
			}
			nested := renderBlockFromSchema(child, childLabels, depth+1)
			for _, line := range strings.Split(strings.TrimRight(nested, "\n"), "\n") {
				b.WriteString(indent + line + "\n")
			}
		}
	}

	b.WriteString(strings.Repeat("  ", depth) + "}\n")
	return b.String()
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
		return "1"
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
	return attr == "use"
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
